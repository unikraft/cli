// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2024, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package buildfs

// createOptions holds shared options for filesystem creation.
type createOptions struct {
	allRoot bool
}

// CreateOption is an option for CreateCPIO and CreateEROFS.
type CreateOption func(*createOptions)

// WithAllRoot toggles whether all file ownership should be set to root:root
// instead of the original file permissions.
func WithAllRoot(allRoot bool) CreateOption {
	return func(co *createOptions) {
		co.allRoot = allRoot
	}
}
