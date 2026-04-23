// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package buildfs

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"

	"github.com/unikraft/go-archivefs/tarfs"
)

// TarballFS opens a tarball (optionally gzip-compressed) as an fs.FS.
// Unsupported tar entry types such as device nodes and FIFOs are represented
// in the filesystem but cannot be read as regular files. The returned
// filesystem implements fs.ReadLinkFS.
func TarballFS(source io.ReaderAt) (fs.FS, error) {
	source, err := maybeGunzip(source)
	if err != nil {
		return nil, err
	}
	return tarfs.Open(source)
}

// maybeGunzip returns a ReaderAt over the decompressed content if source is
// gzip-compressed, or the original source unchanged otherwise.
func maybeGunzip(source io.ReaderAt) (io.ReaderAt, error) {
	// Read the first two bytes to check the gzip magic number.
	var magic [2]byte
	if _, err := source.ReadAt(magic[:], 0); err != nil {
		return source, nil
	}
	if magic[0] != 0x1f || magic[1] != 0x8b {
		return source, nil
	}

	r := io.NewSectionReader(source, 0, 1<<63-1)
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}
