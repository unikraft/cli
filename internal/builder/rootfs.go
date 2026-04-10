// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/containerd/platforms"
	dockerconfig "github.com/docker/cli/cli/config"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/identity"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/util/progress/progresswriter"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	imagespec "unikraft.com/x/image-spec"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/builder/cpio"
	"unikraft.com/cli/internal/builder/erofs"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
	"unikraft.com/x/kraftfile"
)

type Rootfs struct {
	File *os.File
}

func BuildRootfs(ctx context.Context, c *client.Client, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	if len(opts.Platform) == 0 {
		return nil, fmt.Errorf("at least one platform must be specified")
	}
	if len(opts.Platform) > 1 {
		// HACK: disabled multi-platform mode due to buildkit symlink bug.
		// https://github.com/moby/buildkit/issues/6684
		return nil, fmt.Errorf("multi-platform builds are currently not supported")
	}

	dockerConfig := dockerconfig.LoadDefaultConfigFile(os.Stderr)

	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}

	session := []session.Attachable{
		authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
			AuthConfigProvider: images.LoadBuildkitAuthConfig(dockerConfig, profile),
		}),
	}

	attrs := map[string]string{}
	// HACK: disabled multi-platform mode due to buildkit symlink bug.
	// if len(opts.Platform) > 0 {
	// 	attrs["multi-platform"] = "true"
	// }
	localDirs := map[string]string{}
	if err := applyBuildOpts(attrs, localDirs, &session, opts); err != nil {
		return nil, err
	}

	localDest, err := os.MkdirTemp("", "unikraft-buildkit-*")
	if err != nil {
		return nil, fmt.Errorf("could not create temporary directory: %w", err)
	}
	defer os.RemoveAll(localDest)

	solveOpt := client.SolveOpt{
		Ref:     identity.NewID(),
		Session: session,
		Exports: []client.ExportEntry{
			{
				Type:      client.ExporterLocal,
				OutputDir: localDest,
			},
		},
		LocalDirs:     localDirs,
		Frontend:      "dockerfile.v0",
		FrontendAttrs: attrs,
	}

	pw, err := progresswriter.NewPrinter(context.WithoutCancel(ctx), os.Stderr, "auto")
	if err != nil {
		return nil, err
	}

	expPlatforms := getPlatforms(opts.Platform)

	configs := make([]ocispec.Image, 0, len(expPlatforms))
	_, err = c.Build(ctx, solveOpt, "buildctl", func(ctx context.Context, c gateway.Client) (*gateway.Result, error) {
		res, err := c.Solve(ctx, gateway.SolveRequest{
			Frontend:    solveOpt.Frontend,
			FrontendOpt: solveOpt.FrontendAttrs,
		})
		if err != nil {
			return nil, err
		}
		for _, p := range expPlatforms {
			if cfg := exptypes.ParseKey(res.Metadata, "containerimage.config", &p); cfg != nil {
				var config ocispec.Image
				if err := json.Unmarshal(cfg, &config); err != nil {
					return nil, err
				}
				configs = append(configs, config)
			} else {
				return nil, fmt.Errorf("could not find config for platform %s in build result metadata", p.ID)
			}
		}
		return res, nil
	}, pw.Status())
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-pw.Done():
	}
	if pw.Err() != nil {
		return nil, pw.Err()
	}

	var imgs []*imagespec.Image

	for i, p := range opts.Platform {
		ep := expPlatforms[i]
		config := configs[i]

		path := localDest
		_ = ep
		// HACK: only valid with multi-platform enabled
		// path := filepath.Join(
		// 	localDest,
		// 	strings.ReplaceAll(ep.ID, "/", "_"),
		// )

		f, err := os.CreateTemp("", "unikraft-rootfs-*."+string(opts.Rootfs.Format))
		if err != nil {
			return nil, fmt.Errorf("could not create temporary file: %w", err)
		}
		defer func() {
			if rerr != nil && f != nil {
				f.Close()
				os.Remove(f.Name())
			}
		}()

		switch opts.Rootfs.Format {
		case kraftfile.FsTypeCpio:
			var gw *gzip.Writer
			var w io.Writer = f
			if opts.Rootfs.Compress {
				gw = gzip.NewWriter(w)
				w = gw
			}

			err = cpio.CreateFSFromDirectory(ctx, w, path,
				cpio.WithAllRoot(!opts.Rootfs.KeepOwners),
			)
			if err != nil {
				return nil, fmt.Errorf("could not create CPIO archive: %w", err)
			}

			if gw != nil {
				if err := gw.Close(); err != nil {
					return nil, fmt.Errorf("could not close gzip writer: %w", err)
				}
			}
		case kraftfile.FsTypeErofs:
			if opts.Rootfs.Compress {
				log.G(ctx).Warn().Msg("compression is not supported for EROFS, ignoring compress option")
			}

			err = erofs.CreateFSFromDirectory(ctx, f, path,
				erofs.WithAllRoot(!opts.Rootfs.KeepOwners),
			)
			if err != nil {
				return nil, fmt.Errorf("could not create EroFS archive: %w", err)
			}
		default:
			return nil, fmt.Errorf("unknown filesystem type %q", opts.Rootfs.Format)
		}

		if err := f.Sync(); err != nil {
			return nil, fmt.Errorf("could not sync file: %w", err)
		}

		if opts.Cmd != nil {
			config.Config.Cmd = opts.Cmd
		}
		if opts.Env != nil {
			env := make([]string, 0, len(opts.Env))
			for _, kv := range opts.Env {
				env = append(env, fmt.Sprintf("%s=%s", kv.Key, kv.Value))
			}
			config.Config.Env = append(env, config.Config.Env...)
		}
		config.Config.Labels = opts.Labels

		imgs = append(imgs, imagespec.NewImage(
			imagespec.WithImageConfig(config.Config),
			imagespec.WithPlatform(p),
			imagespec.WithInitrd(imagespec.NewTempOSFile(f)),
		))
	}

	return imgs, nil
}

func applyBuildOpts(attrs map[string]string, localDirs map[string]string, sessions *[]session.Attachable, opts BuildOpts) error {
	var ps []string
	for _, p := range getPlatforms(opts.Platform) {
		ps = append(ps, p.ID)
	}
	slices.Sort(ps)
	ps = slices.Compact(ps)
	if len(ps) > 0 {
		attrs["platform"] = strings.Join(ps, ",")
	}

	localDirs["context"] = opts.Rootfs.Path
	localDirs["dockerfile"] = opts.Rootfs.Path
	if opts.Rootfs.Target != "" {
		attrs["target"] = opts.Rootfs.Target
	}

	if opts.Rootfs.NoCache {
		attrs["no-cache"] = ""
	}

	for _, buildArg := range opts.Rootfs.BuildArg {
		if buildArg == "" {
			continue
		}
		key, val, ok := strings.Cut(buildArg, "=")
		if key == "" {
			return fmt.Errorf("invalid build-arg %q", buildArg)
		}
		if !ok {
			val, _ = os.LookupEnv(key)
		}
		attrs["build-arg:"+key] = val
	}

	if len(opts.Rootfs.Secrets) > 0 {
		provider, err := CreateSecrets(opts.Rootfs.Secrets)
		if err != nil {
			return err
		}
		*sessions = append(*sessions, provider)
	}
	if len(opts.Rootfs.SSH) > 0 {
		provider, err := CreateSSH(opts.Rootfs.SSH)
		if err != nil {
			return err
		}
		*sessions = append(*sessions, provider)
	}

	return nil
}

func getPlatforms(ps []ocispec.Platform) (exp []exptypes.Platform) {
	for _, platform := range ps {
		platform.OS = "linux"
		platform.OSFeatures = nil
		platform.OSVersion = ""
		platform = platforms.Normalize(platform)
		exp = append(exp, exptypes.Platform{
			ID:       platforms.Format(platform),
			Platform: platform,
		})
	}
	return exp
}
