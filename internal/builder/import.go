// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"fmt"
	"os"

	"unikraft.com/cli/internal/builder/cpio"
)

// BuildImportRootfs builds a CPIO archive from source and returns its file
// path and byte size.
// Source may be:
//
//   - a local directory path — walked and archived with cpio.CreateFSFromDirectory
//   - an existing CPIO file — returned as-is (caller must NOT remove it)
//
// For the directory case a temporary file is created; the caller is
// responsible for removing it when done.
func BuildImportRootfs(ctx context.Context, workdir, source string) (path string, size int64, retErr error) {
	if source == "." {
		var err error
		source, err = os.Getwd()
		if err != nil {
			return "", -1, fmt.Errorf("getting current directory: %w", err)
		}
	}

	// TODO(craciunoiuc): Abstract to generic type with multiple formats once
	// supported by the importer instance.
	fi, statErr := os.Stat(source)
	if statErr != nil {
		return "", -1, fmt.Errorf("checking source %q: %w", source, statErr)
	}

	// Use an existing CPIO archive directly without rebuilding it.
	if fi.Mode().IsRegular() {
		if !cpio.IsCPIOFile(source) {
			return "", -1, fmt.Errorf("source %q is not a CPIO archive", source)
		}
		return source, fi.Size(), nil
	}

	// Build a CPIO archive by walking the source directory.
	if fi.IsDir() {
		tmp, err := os.CreateTemp(workdir, "volimport-*.cpio")
		if err != nil {
			return "", -1, fmt.Errorf("creating temporary CPIO archive: %w", err)
		}
		defer func() {
			_ = tmp.Close()
			if retErr != nil {
				_ = os.Remove(tmp.Name())
			}
		}()

		if archErr := cpio.CreateFSFromDirectory(ctx, tmp, source); archErr != nil {
			return "", -1, fmt.Errorf("building CPIO archive from directory %q: %w", source, archErr)
		}

		stat, err := tmp.Stat()
		if err != nil {
			return "", -1, fmt.Errorf("reading CPIO archive metadata: %w", err)
		}
		return tmp.Name(), stat.Size(), nil
	}

	// TODO(craciunoiuc): detect and build from a Dockerfile via BuildRootfs
	// after separating it from packaging images.
	return "", -1, fmt.Errorf("unsupported source %q: expected a directory or an existing CPIO file", source)
}
