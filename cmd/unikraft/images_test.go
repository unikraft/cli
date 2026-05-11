// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"fmt"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/types"
)

func imagesTests(t *testing.T, r *integrationRunner) {
	t.Run("inspect", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "image", "inspect", "nginx:latest"}, match: []string{`nginx`}},
			})
	})

	t.Run("copy-inspect-delete", func(t *testing.T) {
		if r.cfg == nil {
			t.Skip("online test requires config, but no config found")
		}

		imageName := r.cfg.Profile.Organization + "/nginx-copy:$UNIQ_IMAGE"
		imageFull := fmt.Sprintf("%s/%s", "index.unikraft.io", imageName)

		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "image", "copy", "nginx:latest", imageFull}},
				{args: []string{unikraftCmd, "image", "inspect", imageName}, match: []string{`nginx`}},
				{args: []string{unikraftCmd, "image", "delete", imageName}},
			})
	})
}

func imagesHelpTests(t *testing.T, unikraftPath string) {
	r := newTestEnv(t, unikraftPath)
	gild(t.Context(), t, r.cli,
		[]string{unikraftCmd, "image", "--help"},
		[]string{unikraftCmd, "image", "get", "--help"},
		[]string{unikraftCmd, "image", "list", "--help"},
		[]string{unikraftCmd, "image", "copy", "--help"},
	)
}

func imagesOutputTests(t *testing.T) {
	sample := &cmd.Image{}
	require.NoError(t, sample.Ref.UnmarshalText([]byte("nginx:latest")))
	sample.Digest = digest.Digest("sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4")
	require.NoError(t, sample.Config.Platform.UnmarshalText([]byte("linux/amd64")))
	sample.Config.Cmd = []string{"/usr/sbin/nginx", "-g", "daemon off;"}
	sample.Config.Env = map[string]string{"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	sample.Metadata.Author = "unikraft.io"
	sample.Kernel = &cmd.ImageFile{
		Digest:    digest.Digest("sha256:b3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"),
		MediaType: "application/vnd.unikraft.kernel.v1",
		Size:      types.SizeBytes(4194304),
	}
	sample.Initrd = &cmd.ImageFile{
		Digest:    digest.Digest("sha256:c3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"),
		MediaType: "application/vnd.unikraft.initrd.v1",
		Size:      types.SizeBytes(1048576),
	}
	sample.KernelDebug = &cmd.ImageFile{
		Digest:    digest.Digest("sha256:d3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"),
		MediaType: "application/vnd.unikraft.kernel.v1",
		Size:      types.SizeBytes(8388608),
	}
	sample.Roms = []cmd.ImageFile{
		{
			Digest:    digest.Digest("sha256:e3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"),
			MediaType: "application/vnd.unikraft.rom.v1",
			Size:      types.SizeBytes(524288),
		},
	}

	gild[resource.Resource](t.Context(), t, dumpResource, sample)
}
