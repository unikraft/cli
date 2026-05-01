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

	Runtime string

	Platform []ocispec.Platform

	Cmd    []string
	Env    kraftfile.Map
	Labels map[string]string
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
	kernels, err := BuildKernel(ctx, opts)
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

	// Apply the rootfs format default: if no explicit format override was set
	if opts.Rootfs.Path != "" && opts.Rootfs.Format == "" {
		opts.Rootfs.Format = detectFormatFromKernelFeatures(kernels)
	}

	meta := imagespec.ImageMetadata{
		Created: new(time.Now()),
	}

	// If no rootfs path is set, build images with just the kernel.
	if opts.Rootfs.Path == "" {
		var cfg ocispec.ImageConfig
		cfg.Cmd = opts.Cmd
		cfg.Labels = opts.Labels
		if opts.Env != nil {
			env := make([]string, 0, len(opts.Env))
			for _, kv := range opts.Env {
				env = append(env, fmt.Sprintf("%s=%s", kv.Key, kv.Value))
			}
			cfg.Env = env
		}

		images := make([]*imagespec.Image, 0, len(kernels))
		for _, kernel := range kernels {
			images = append(images, imagespec.NewImage(
				imagespec.WithKernel(kernel.Kernel),
				imagespec.WithImageConfig(cfg),
				imagespec.WithImageMetadata(meta),
				imagespec.WithPlatform(kernel.Image.Platform),
			))
			kernel.Kernel = nil
		}
		return images, nil
	}

	roots, err := BuildRootfs(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, root := range roots {
			root.Close()
		}
	}()

	if len(kernels) != len(roots) {
		panic(fmt.Sprintf("internal error: number of kernels (%d) does not match number of root filesystems (%d)", len(kernels), len(roots)))
	}

	images := make([]*imagespec.Image, 0, len(kernels))
	for i, kernel := range kernels {
		root := roots[i]

		if platforms.Format(kernel.Image.Platform) != platforms.Format(root.Image.Platform) {
			panic(fmt.Sprintf("internal error: kernel platform (%s) does not match rootfs platform (%s)",
				platforms.Format(kernel.Image.Platform),
				platforms.Format(root.Image.Platform)),
			)
		}

		images = append(images, imagespec.NewImage(
			imagespec.WithKernel(kernel.Kernel),
			imagespec.WithInitrd(root.Initrd),
			imagespec.WithImageConfig(root.Image.Config),
			imagespec.WithImageMetadata(meta),
			imagespec.WithPlatform(root.Image.Platform),
		))

		// ensure we don't cleanup the kernel and initrd files we use above
		kernel.Kernel = nil
		root.Initrd = nil
	}

	return images, nil
}

func detectFormatFromKernelFeatures(kernels []*imagespec.Image) kraftfile.FsType {
	for _, kernel := range kernels {
		if kernel.Image == nil {
			continue
		}
		for _, feature := range kernel.Image.OSFeatures {
			if feature == "CONFIG_LIBUKFS_EROFS=y" || feature == "CONFIG_EROFS_FS=y" {
				return kraftfile.FsTypeErofs
			}
		}
	}
	return kraftfile.FsTypeCpio
}
