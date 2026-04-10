// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"unikraft.com/cli/internal/resource"
)

// SortSpec represents a single field sort specification with direction.
type SortSpec struct {
	Path       resource.FieldPath
	Descending bool
}

// parseSortSpecs parses a comma-separated list of sort specifications.
// Each spec can be prefixed with - for descending or + for ascending (default).
// Examples:
//   - "name" or "+name" -> ascending by name
//   - "-name" -> descending by name
//   - "state,-timing.uptime" -> ascending by state, then descending by uptime
func parseSortSpecs(inputs ...string) ([]SortSpec, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	specs := make([]SortSpec, 0, len(inputs))

	for _, part := range inputs {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		var descending bool
		fieldName := part

		if after, found := strings.CutPrefix(part, "-"); found {
			descending = true
			fieldName = after
		} else if after, found := strings.CutPrefix(part, "+"); found {
			fieldName = after
		}

		if fieldName == "" {
			return nil, fmt.Errorf("empty field name in sort specification")
		}

		specs = append(specs, SortSpec{
			Path:       resource.ParseFieldPath(fieldName),
			Descending: descending,
		})
	}

	return specs, nil
}

// sortResources sorts resources by multiple field values specified by the sort
// specs.  Each spec includes a path and whether to sort descending.  Resources
// are sorted by the first spec, then ties are broken by subsequent specs.
func sortResources(ctx context.Context, resources []resource.Resource, specs []SortSpec) ([]resource.Resource, error) {
	if len(specs) == 0 {
		return resources, nil
	}
	if len(resources) == 0 {
		return resources, nil
	}

	// Validate all sort paths up-front against the resource's field schema.
	schemaFields, err := resources[0].Fields(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get fields for validation: %w", err)
	}
	for _, spec := range specs {
		_, missing := resource.FilterFieldsByPath(schemaFields, []resource.FieldPath{spec.Path}, false)
		if len(missing) > 0 {
			return nil, fmt.Errorf("unknown sort field: %s", spec.Path)
		}
	}

	// Collect all paths for resolution
	paths := make([]resource.FieldPath, len(specs))
	for i, spec := range specs {
		paths[i] = spec.Path
	}

	// Pre-resolve field values for sorting
	type resourceWithValues struct {
		res    resource.Resource
		values []any
	}

	resolved, err := resolveResources(ctx, resources, paths)
	if err != nil {
		return nil, err
	}

	items := make([]resourceWithValues, len(resolved))
	for i, res := range resolved {
		items[i].res = res
		items[i].values = make([]any, len(specs))

		fields, err := res.Fields(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get fields for resource %s: %w", res.Key(), err)
		}

		for j, spec := range specs {
			value, err := fieldValueByPath(fields, spec.Path, res.Key())
			if err != nil {
				return nil, err
			}
			items[i].values[j] = value
		}
	}

	// Sort by the extracted field values using type-aware comparison
	// Apply sort specs in order (first spec is primary, subsequent break ties)
	slices.SortStableFunc(items, func(a, b resourceWithValues) int {
		for i, spec := range specs {
			result := resource.CompareFieldValues(a.values[i], b.values[i])
			if result != 0 {
				if spec.Descending {
					return -result
				}
				return result
			}
		}
		return 0
	})

	// Extract sorted resources
	sorted := make([]resource.Resource, len(items))
	for i, item := range items {
		sorted[i] = item.res
	}
	return sorted, nil
}

func fieldValueByPath(fields []resource.Field, path resource.FieldPath, key resource.Key) (any, error) {
	matched := resource.GetFieldByPath(fields, path)
	if len(matched) == 0 {
		return nil, fmt.Errorf("sort field %s not found for resource %s", path, key)
	}
	if len(matched) > 1 {
		return nil, fmt.Errorf("sort field %s is ambiguous for resource %s (matched %d fields)", path, key, len(matched))
	}
	return matched[0].Value, nil
}
