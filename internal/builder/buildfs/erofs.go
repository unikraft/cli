// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package buildfs

import (
	"io"
	"io/fs"

	"github.com/unikraft/go-archivefs/erofs"
)

// CreateEROFS creates an EroFS filesystem image from the given fs.FS.
func CreateEROFS(w io.WriterAt, fsys fs.FS, opts ...CreateOption) error {
	c := createOptions{}
	for _, opt := range opts {
		opt(&c)
	}
	var erofsOpts []erofs.ErofsCreateOption
	if c.allRoot {
		erofsOpts = append(erofsOpts, erofs.WithAllRoot(true))
	}
	return erofs.Create(w, fsys, erofsOpts...)
}
