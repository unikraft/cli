// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"context"
	"fmt"
	"time"

	"github.com/containerd/platforms"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	imagespec "unikraft.com/x/image-spec"

	"unikraft.com/x/kraftfile"
)

type BuildOpts struct {
	Rootfs RootfsOpts
	Roms   []RomOpts

	Runtime string

	Platform []ocispec.Platform

	Cmd    []string
	Env    kraftfile.Map
	Labels map[string]string
}

// XXX: merge some of these opts together with RootfsOpts
type RomOpts struct {
	Path string
	Type kraftfile.SourceType

	// Output params
	Format kraftfile.FsType
}

type RootfsOpts struct {
	Path string
	Type kraftfile.SourceType

	// Output params
	Format     kraftfile.FsType
	Compress   bool
	KeepOwners bool

	// Buildkit params
	// Dockerfile string
	BuildArg []string
	Target   string
	Secrets  []*Secret
	SSH      []*SSH

	NoCache bool
}

// Build a unikraft image based on the provided build options.
func Build(ctx context.Context, opts BuildOpts) ([]*imagespec.Image, error) {
	if opts.Runtime == "" && opts.Rootfs.Path == "" && len(opts.Roms) == 0 {
		return nil, fmt.Errorf("no runtime, rootfs, or roms specified: nothing to build")
	}

	meta := imagespec.ImageMetadata{
		Created: new(time.Now()),
	}

	// Build kernel if a runtime is specified. This also determines the
	// set of platforms we're building for.
	var kernels []*imagespec.Image
	if opts.Runtime != "" {
		var err error
		kernels, err = BuildKernel(ctx, opts)
		if err != nil {
			return nil, err
		}
		defer func() {
			for _, kernel := range kernels {
				kernel.Close()
			}
		}()
		opts.Platform = make([]ocispec.Platform, 0, len(kernels))
		for _, kernel := range kernels {
			opts.Platform = append(opts.Platform, kernel.Image.Platform)
		}
	}

	// Without a kernel, use a default platform if none specified.
	if len(opts.Platform) == 0 {
		opts.Platform = []ocispec.Platform{
			// XXX: make consts? also used in roms?
			{Architecture: "x86_64", OS: "kraftcloud"},
		}
	}

	// Build ROMs if any are specified.
	romFiles, err := BuildRoms(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Build rootfs if a path is specified.
	var roots []*imagespec.Image
	if opts.Rootfs.Path != "" {
		var err error
		roots, err = BuildRootfs(ctx, opts)
		if err != nil {
			return nil, err
		}
		defer func() {
			for _, root := range roots {
				root.Close()
			}
		}()
	}

	// When we have both kernels and rootfs, they must match 1:1 by platform.
	if len(kernels) > 0 && len(roots) > 0 && len(kernels) != len(roots) {
		panic(fmt.Sprintf("internal error: number of kernels (%d) does not match number of root filesystems (%d)", len(kernels), len(roots)))
	}

	// Assemble images, one per platform.
	images := make([]*imagespec.Image, 0, len(opts.Platform))
	for i, p := range opts.Platform {
		imgOpts := []imagespec.NewImageOpt{
			imagespec.WithImageMetadata(meta),
			imagespec.WithPlatform(p),
		}

		// Attach kernel if available.
		if i < len(kernels) {
			if platforms.Format(kernels[i].Image.Platform) != platforms.Format(p) {
				panic(fmt.Sprintf("internal error: kernel platform (%s) does not match expected platform (%s)",
					platforms.Format(kernels[i].Image.Platform),
					platforms.Format(p)),
				)
			}
			imgOpts = append(imgOpts, imagespec.WithKernel(kernels[i].Kernel))
			kernels[i].Kernel = nil
		}

		// Attach rootfs/initrd and use its config if available.
		cfg := buildImageConfig(opts)
		if i < len(roots) {
			if platforms.Format(roots[i].Image.Platform) != platforms.Format(p) {
				panic(fmt.Sprintf("internal error: rootfs platform (%s) does not match expected platform (%s)",
					platforms.Format(roots[i].Image.Platform),
					platforms.Format(p)),
				)
			}
			imgOpts = append(imgOpts, imagespec.WithInitrd(roots[i].Initrd))
			roots[i].Initrd = nil
			// The rootfs build may have produced a richer config (e.g. from
			// a Dockerfile). Use it as the base and layer our overrides on top.
			cfg = roots[i].Image.Config
			if opts.Cmd != nil {
				cfg.Cmd = opts.Cmd
			}
			if opts.Env != nil {
				env := make([]string, 0, len(opts.Env))
				for _, kv := range opts.Env {
					env = append(env, fmt.Sprintf("%s=%s", kv.Key, kv.Value))
				}
				cfg.Env = append(env, cfg.Env...)
			}
			cfg.Labels = opts.Labels
		}
		imgOpts = append(imgOpts, imagespec.WithImageConfig(cfg))

		// Attach roms if available.
		for _, rom := range romFiles {
			imgOpts = append(imgOpts, imagespec.WithRom(rom))
		}

		images = append(images, imagespec.NewImage(imgOpts...))
	}

	return images, nil
}
