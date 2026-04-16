// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package multimetro

import (
	"context"

	"unikraft.com/x/filters"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
)

func filterMetrosFromContext(ctx context.Context, metros []config.Metro) []config.Metro {
	spec := resource.FilterFromContext(ctx)
	return filterMetros(metros, spec)
}

func filterMetros(metros []config.Metro, spec filters.Filter) []config.Metro {
	if spec == nil {
		return metros
	}

	// Extract metro names as candidates
	candidates := make([]string, len(metros))
	for i, m := range metros {
		candidates[i] = m.Name
	}

	// Filter candidates based on the metro field
	matched := filters.Restrict(spec, "metro", candidates)

	// Build result preserving order
	matchedSet := make(map[string]bool, len(matched))
	for _, m := range matched {
		matchedSet[m] = true
	}

	result := make([]config.Metro, 0, len(matched))
	for _, metro := range metros {
		if matchedSet[metro.Name] {
			result = append(result, metro)
		}
	}
	return result
}
