// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"cmp"
	"fmt"
	"path/filepath"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"unikraft.com/x/kraftfile"
)

func KraftfileToBuildOpts(dir string, kf *kraftfile.Kraftfile) (BuildOpts, error) {
	var opts BuildOpts

	opts.Cmd = []string(kf.Cmd)
	opts.Env = kf.Env
	opts.Labels = kf.Labels

	if kf.Runtime != nil {
		opts.Runtime = string(*kf.Runtime)
	}

	if kf.Unikraft != nil {
		return BuildOpts{}, fmt.Errorf("unikraft configuration not currently supported")
	}
	if kf.Libraries != nil {
		// these are the same build process as kf.Unikraft
		return BuildOpts{}, fmt.Errorf("library configuration not currently supported")
	}
	if kf.Volumes != nil {
		return BuildOpts{}, fmt.Errorf("volumes configuration not currently supported")
	}
	if kf.Template != nil {
		return BuildOpts{}, fmt.Errorf("template configuration not currently supported")
	}

	for _, target := range kf.Targets {
		features := make([]string, 0, len(target.KConfig))
		for _, kv := range target.KConfig {
			features = append(features, fmt.Sprintf("%s=%v", kv.Key, kv.Value))
		}
		version := fmt.Sprint(target.KConfig.Get("CONFIG_UK_FULLVERSION"))
		opts.Platform = append(opts.Platform, ocispec.Platform{
			Architecture: target.Arch,
			OS:           target.Plat,
			OSVersion:    version,
			OSFeatures:   features,
		})
	}

	for _, rom := range kf.Roms {
		romPath := filepath.Join(dir, rom.Source.Path)
		romFormat := cmp.Or(rom.Format, kraftfile.FsTypeErofs)
		romOpt := FSOpts{
			Path:   romPath,
			Format: romFormat,
			Type:   rom.Source.Type,
			// Pad the file to page-size alignment. This is required by the platform
			// which rejects ROM files that are not page-aligned.
			Pad: 4096,
		}
		if romOpt.Type == "" {
			typ, err := DetectSourceType(romPath)
			if err != nil {
				return BuildOpts{}, fmt.Errorf("detecting rom type for %q: %w", rom.Source, err)
			}
			romOpt.Type = typ
		}
		opts.Roms = append(opts.Roms, romOpt)
	}

	if kf.Rootfs != nil {
		opts.Rootfs.Path = filepath.Join(dir, kf.Rootfs.Source.Path)
		opts.Rootfs.Format = kf.Rootfs.Format
		opts.Rootfs.Type = kf.Rootfs.Source.Type
		opts.Rootfs.Dockerfile = kf.Rootfs.Source.Dockerfile
		if opts.Rootfs.Dockerfile != "" && opts.Rootfs.Type == "" {
			opts.Rootfs.Type = kraftfile.SourceTypeDockerfile
		} else if opts.Rootfs.Type == "" {
			typ, err := DetectSourceType(opts.Rootfs.Path)
			if err != nil {
				return BuildOpts{}, fmt.Errorf("detecting rootfs type for %q: %w", opts.Rootfs.Path, err)
			}
			opts.Rootfs.Type = typ
		}
	}

	return opts, nil
}
