// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
)

// defaultMetro returns the metro if non-empty, otherwise falls back to the
// default metro from the current profile in the context.
func defaultMetro(ctx context.Context, metro string) string {
	if metro != "" {
		return metro
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return ""
	}
	return profile.GetDefaultMetro()
}

func getFromListable(ctx context.Context, listable resource.ListableResource, keys []string) ([]resource.Resource, error) {
	all, err := listable.List(ctx)
	if err != nil {
		return nil, err
	}

	found := make([]resource.Resource, 0, len(keys))
	var notFound []string
loop:
	for _, key := range keys {
		for _, resource := range all {
			if resource.Key().String() == key {
				found = append(found, resource)
				continue loop
			}
		}
		notFound = append(notFound, key)
	}

	if len(notFound) == 1 {
		return nil, fmt.Errorf("%s not found: %s", listable.Type().Name, notFound)
	} else if len(notFound) > 0 {
		return nil, fmt.Errorf("%s not found: %s", listable.Type().Names, notFound)
	}
	return found, nil
}

type patchOp string

const (
	patchOpSet patchOp = "set"
	patchOpAdd patchOp = "add"
	patchOpDel patchOp = "del"
)

type patchReq[P ~string] struct {
	Op    patchOp
	Prop  P
	Value any
}

func patchRequests[P ~string](fields []resource.Field, specFor func(path string, op patchOp, value any) (P, any, error)) ([]patchReq[P], error) {
	var reqs []patchReq[P]
	addReq := func(op patchOp, path string, value any) error {
		if value == nil {
			return nil
		}
		prop, converted, err := specFor(path, op, value)
		if err != nil {
			return err
		}
		if converted == nil {
			return nil
		}
		// If the converted value is a map, try to merge it with an existing
		// patch for the same prop/op. This allows multiple fields to be
		// aggregated into a single patch request.
		if m, ok := converted.(map[string]any); ok {
			for i := range reqs {
				if reqs[i].Prop == prop && reqs[i].Op == op {
					if existing, ok := reqs[i].Value.(map[string]any); ok {
						maps.Copy(existing, m)
						return nil
					}
				}
			}
		}
		reqs = append(reqs, patchReq[P]{Op: op, Prop: prop, Value: converted})
		return nil
	}

	for key, field := range resource.IterFields(fields) {
		if field.Edit == nil {
			continue
		}
		path := key.String()
		if err := addReq(patchOpSet, path, field.Edit.Set); err != nil {
			return nil, err
		}
		if err := addReq(patchOpAdd, path, field.Edit.Add); err != nil {
			return nil, err
		}
		if err := addReq(patchOpDel, path, field.Edit.Del); err != nil {
			return nil, err
		}
	}
	return reqs, nil
}

// listGetOpError determines whether list/get handlers should continue processing
// response payloads when the API also returned an error.
//
// Rules:
//   - NotFound-only errors are treated as non-fatal and suppressed.
//   - Any error with successful items (non-empty data slice) is preserved but
//     processing continues, enabling partial-success output.
//   - Other errors are fatal and should abort processing.
func listGetOpError(err error, items any) (error, bool) {
	if err == nil {
		return nil, true
	}
	if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
		return nil, true
	}
	if hasSuccessfulItems(items) {
		return err, true
	}
	return err, false
}

// hasSuccessfulItems checks if a slice/array has at least one item.
func hasSuccessfulItems(items any) bool {
	if items == nil {
		return false
	}
	v := reflect.ValueOf(items)
	if !v.IsValid() || v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return false
	}
	return v.Len() > 0
}

// deleteOpError determines whether delete handlers should return an error when
// some items were successfully deleted.
//
// Rules:
// - If any items were deleted, return nil (partial success - suppresses the error).
// - NotFound-only errors are treated as non-fatal and suppressed.
// - Other errors are returned as-is (fatal).
func deleteOpError(err error, deletedCount int) error {
	if deletedCount > 0 {
		return nil
	}
	if err == nil || platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
		return nil
	}
	return err
}

func partialFailureReason(err error) string {
	// TODO: Extract reason and metro from typed ResponseError once the SDK adds
	// message and metro fields, and use those values instead of err.Error().
	if err == nil {
		return "partial failure"
	}
	return fmt.Sprintf("partial failure: %v", err)
}

// PartialResult represents a partial-success operation (207 Multi-Status semantics).
// It tracks successful and failed items across metros.
type PartialResult struct {
	Successful []group.Ref          // Items that succeeded
	Failed     map[group.Ref]string // Items that failed with error reason
}

// IsPartial returns true if there are both successes and failures.
func (pr *PartialResult) IsPartial() bool {
	return pr != nil && len(pr.Successful) > 0 && len(pr.Failed) > 0
}

// SuccessCount returns the number of successful items.
func (pr *PartialResult) SuccessCount() int {
	if pr == nil {
		return 0
	}
	return len(pr.Successful)
}

// FailureCount returns the number of failed items.
func (pr *PartialResult) FailureCount() int {
	if pr == nil {
		return 0
	}
	return len(pr.Failed)
}

// Error implements the error interface, formatting both successes and failures.
func (pr *PartialResult) Error() string {
	if pr == nil {
		return ""
	}
	var parts []string

	if len(pr.Successful) > 0 {
		parts = append(parts, fmt.Sprintf("%d successful", len(pr.Successful)))
	}
	if len(pr.Failed) > 0 {
		var failedItems []string
		for ref, reason := range pr.Failed {
			key := ref.Name
			if key == "" {
				key = ref.UUID
			}
			failedItems = append(failedItems, fmt.Sprintf("%s (%s): %s", key, ref.Metro, reason))
		}
		slices.Sort(failedItems)
		parts = append(parts, fmt.Sprintf("%d failed:\n  %s", len(pr.Failed), strings.Join(failedItems, "\n  ")))
	}

	if len(parts) == 0 {
		return "operation completed"
	}
	return strings.Join(parts, ", ")
}

// NewPartialResult creates a PartialResult with preallocated slices.
func NewPartialResult() *PartialResult {
	return &PartialResult{
		Successful: make([]group.Ref, 0),
		Failed:     make(map[group.Ref]string),
	}
}

// CombineErrors combines multiple errors into a single error, preserving
// partial-result information when present.
func CombineErrors(errs ...error) error {
	var partialResults []*PartialResult
	var otherErrs []error

	for _, err := range errs {
		if pr, ok := err.(*PartialResult); ok {
			partialResults = append(partialResults, pr)
		} else if err != nil {
			otherErrs = append(otherErrs, err)
		}
	}

	// If we have partial results, merge them and include any other errors
	if len(partialResults) > 0 {
		merged := NewPartialResult()
		for _, pr := range partialResults {
			merged.Successful = append(merged.Successful, pr.Successful...)
			maps.Copy(merged.Failed, pr.Failed)
		}

		// If there are other errors and we had partial results, convert to combined error
		if len(otherErrs) > 0 {
			otherErr := errors.Join(otherErrs...)
			return fmt.Errorf("%w (and %d operation(s) partially succeeded)", otherErr, merged.SuccessCount())
		}
		return merged
	}

	// No partial results, just combine other errors
	return errors.Join(otherErrs...)
}
