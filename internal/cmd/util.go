// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"maps"

	"unikraft.com/cli/internal/resource"
)

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

func patchRequests[P ~string](fields []resource.Field, specFor func(path string, op patchOp, value any) (P, any)) []patchReq[P] {
	var reqs []patchReq[P]
	addReq := func(op patchOp, path string, value any) {
		if value == nil {
			return
		}
		prop, converted := specFor(path, op, value)
		if converted == nil {
			return
		}
		// If the converted value is a map, try to merge it with an existing
		// patch for the same prop/op. This allows multiple fields to be
		// aggregated into a single patch request.
		if m, ok := converted.(map[string]any); ok {
			for i := range reqs {
				if reqs[i].Prop == prop && reqs[i].Op == op {
					if existing, ok := reqs[i].Value.(map[string]any); ok {
						maps.Copy(existing, m)
						return
					}
				}
			}
		}
		reqs = append(reqs, patchReq[P]{Op: op, Prop: prop, Value: converted})
	}

	for key, field := range resource.IterFields(fields) {
		if field.Edit == nil {
			continue
		}
		path := key.String()
		addReq(patchOpSet, path, field.Edit.Set)
		addReq(patchOpAdd, path, field.Edit.Add)
		addReq(patchOpDel, path, field.Edit.Del)
	}
	return reqs
}
