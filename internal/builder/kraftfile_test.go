// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"testing"

	"github.com/stretchr/testify/require"

	"unikraft.com/x/kraftfile"
)

func TestKraftfileToBuildOpts(t *testing.T) {
	rootfsDir := t.TempDir()
	rootfsPath := "Dockerfile"

	runtime := kraftfile.Runtime("unikraft.io/unikraft.org/base")
	kf := &kraftfile.Kraftfile{
		Cmd:     kraftfile.Command{"/server", "--flag"},
		Env:     kraftfile.Map{{Key: "A", Value: "1"}},
		Labels:  map[string]string{"label": "value"},
		Runtime: &runtime,
		Rootfs: &kraftfile.FS{
			Format: kraftfile.FsTypeErofs,
			Source: rootfsPath,
		},
		Targets: []kraftfile.Target{
			{
				Arch: "x86_64",
				Plat: "fc",
				KConfig: kraftfile.Map{
					{Key: "CONFIG_UK_FULLVERSION", Value: "1.2.3"},
					{Key: "CONFIG_DEBUG", Value: "y"},
				},
			},
		},
	}

	opts, err := KraftfileToBuildOpts(rootfsDir, kf)
	require.NoError(t, err)
	require.Equal(t, []string{"/server", "--flag"}, opts.Cmd)
	require.Equal(t, kraftfile.Map{{Key: "A", Value: "1"}}, opts.Env)
	require.Equal(t, map[string]string{"label": "value"}, opts.Labels)
	require.Equal(t, "unikraft.io/unikraft.org/base", opts.Runtime)
	require.Equal(t, kraftfile.FsTypeErofs, opts.Rootfs.Format)
	require.Equal(t, rootfsDir, opts.Rootfs.Path)
	require.Len(t, opts.Platform, 1)
	require.Equal(t, "x86_64", opts.Platform[0].Architecture)
	require.Equal(t, "fc", opts.Platform[0].OS)
	require.Equal(t, "1.2.3", opts.Platform[0].OSVersion)
	require.Equal(t, []string{
		"CONFIG_UK_FULLVERSION=1.2.3",
		"CONFIG_DEBUG=y",
	}, opts.Platform[0].OSFeatures)
}

func TestKraftfileToBuildOptsRootfsSourceError(t *testing.T) {
	rootfsDir := t.TempDir()
	rootfsPath := "rootfs.tar"

	runtime := kraftfile.Runtime("unikraft.io/unikraft.org/base")
	kf := &kraftfile.Kraftfile{
		Runtime: &runtime,
		Rootfs: &kraftfile.FS{
			Format: kraftfile.FsTypeCpio,
			Source: rootfsPath,
		},
	}

	_, err := KraftfileToBuildOpts(rootfsDir, kf)
	require.Error(t, err)
}
