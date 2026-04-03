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
	"unikraft.com/cli/internal/buildkit"
)

// BuildImportRootfs builds a CPIO archive from source and returns its file
// path and byte size.
// Source may be:
//
//   - a Dockerfile context — built with BuildKit and exported as a CPIO archive
//   - a local directory path — walked and archived with cpio.CreateFSFromDirectory
//   - an existing CPIO file — returned as-is (caller must NOT remove it)
//
// If the source is absent, the workdir is expected to container a Kraftfile.
// If a source is present, the workdir Kraftfile options are ignored.
// For the directory case a temporary file is created; the caller is
// responsible for removing it when done.
// TODO(craciunoiuc): Abstract to generic type with multiple formats once
// supported by the importer instance.
func BuildImportRootfs(ctx context.Context, workdir, source string, importOpts *BuildOpts) (path string, size int64, retErr error) {
	// Build the project (also Dockerfiles) if no source is provided
	if source == "" {
		if importOpts != nil {
			if _, statErr := os.Stat(importOpts.Rootfs.Path); statErr == nil {
				return buildImportRootfsFromDockerfile(ctx, importOpts)
			}
		}

		return "", -1, fmt.Errorf("no source provided for volume import")
	}

	if source == "." {
		var err error
		source, err = os.Getwd()
		if err != nil {
			return "", -1, fmt.Errorf("getting current directory: %w", err)
		}
	}

	fi, statErr := os.Stat(source)
	if statErr != nil {
		return "", -1, fmt.Errorf("checking source %q: %w", source, statErr)
	}

	// Build as a file if the source is a regular file.
	if fi.Mode().IsRegular() {
		if !cpio.IsCPIOFile(source) {
			return "", -1, fmt.Errorf("source %q is not a CPIO archive", source)
		}
		return source, fi.Size(), nil
	}

	// Build as a directory if the source is a directory.
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

	return "", -1, fmt.Errorf("unsupported source %q: expected a directory or an existing CPIO file", source)
}

// buildImportRootfsFromDockerfile builds a Dockerfile context into a single
// CPIO archive using PackageRootfs and returns its path and size.
func buildImportRootfsFromDockerfile(ctx context.Context, importOpts *BuildOpts) (path string, size int64, retErr error) {
	bkc, cleanup, err := buildkit.ConnectToBuildkit(ctx)
	if err != nil {
		return "", -1, fmt.Errorf("connecting to buildkit: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	roots, err := PackageRootfs(ctx, bkc, *importOpts)
	if err != nil {
		return "", -1, fmt.Errorf("building rootfs from Dockerfile: %w", err)
	}
	// Clean up archives on any subsequent error.
	defer func() {
		if retErr != nil {
			for _, root := range roots {
				if root.File != nil {
					root.File.Close()
					os.Remove(root.File.Name())
				}
			}
		}
	}()

	if len(roots) != 1 {
		return "", -1, fmt.Errorf("volume import requires exactly one rootfs, but Dockerfile build produced %d", len(roots))
	}

	root := roots[0]
	stat, err := root.File.Stat()
	if err != nil {
		return "", -1, fmt.Errorf("reading rootfs file metadata: %w", err)
	}
	rootPath := root.File.Name()

	// Close the descriptor; the file remains on disk for the caller to stream
	// and remove (the caller defers os.Remove on any path != the original source).
	if err := root.File.Close(); err != nil {
		return "", -1, fmt.Errorf("closing rootfs file: %w", err)
	}
	root.File = nil

	return rootPath, stat.Size(), nil
}
