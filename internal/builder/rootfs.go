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
	"path/filepath"
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

// PackagedRootfs holds a rootfs archive for a single platform produced by
// PackageRootfs, together with the OCI image config extracted during the build.
type PackagedRootfs struct {
	Platform ocispec.Platform
	Config   ocispec.Image
	File     *os.File
}

// PackageRootfs builds a Dockerfile into a per-platform rootfs archive using BuildKit.
// On success the caller owns the returned files and must close / remove them
// when done; on error all temporary files are cleaned up before returning.
func PackageRootfs(ctx context.Context, c *client.Client, opts BuildOpts) (_ []*PackagedRootfs, rerr error) {
	if len(opts.Platform) == 0 {
		return nil, fmt.Errorf("at least one platform must be specified")
	}

	dockerConfig := dockerconfig.LoadDefaultConfigFile(os.Stderr)

	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}

	sess := []session.Attachable{
		authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
			AuthConfigProvider: images.LoadBuildkitAuthConfig(dockerConfig, profile),
		}),
	}

	attrs := map[string]string{}
	if len(opts.Platform) > 0 {
		attrs["multi-platform"] = "true"
	}
	localDirs := map[string]string{}
	if err := applyBuildOpts(attrs, localDirs, &sess, opts); err != nil {
		return nil, err
	}

	localDest, err := os.MkdirTemp("", "unikraft-buildkit-*")
	if err != nil {
		return nil, fmt.Errorf("could not create temporary directory: %w", err)
	}
	defer os.RemoveAll(localDest)

	solveOpt := client.SolveOpt{
		Ref:     identity.NewID(),
		Session: sess,
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

	var roots []*PackagedRootfs

	defer func() {
		if rerr != nil {
			for _, root := range roots {
				if root.File != nil {
					root.File.Close()
					os.Remove(root.File.Name())
				}
			}
		}
	}()

	for i, p := range opts.Platform {
		ep := expPlatforms[i]
		config := configs[i]

		path := filepath.Join(
			localDest,
			strings.ReplaceAll(ep.ID, "/", "_"),
		)

		f, err := os.CreateTemp("", "unikraft-rootfs-*."+string(opts.Rootfs.Format))
		if err != nil {
			return nil, fmt.Errorf("could not create temporary file: %w", err)
		}

		// Append before processing so the deferred cleanup above handles
		// this file on any subsequent error.
		roots = append(roots, &PackagedRootfs{Platform: p, Config: config, File: f})

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
	}

	return roots, nil
}

// ExportRootfs converts packaged rootfs archives into OCI images by applying
// command, environment, and label overrides from opts.
func ExportRootfs(_ context.Context, roots []*PackagedRootfs, opts BuildOpts) ([]*imagespec.Image, error) {
	imgs := make([]*imagespec.Image, 0, len(roots))
	for _, root := range roots {
		config := root.Config

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
			imagespec.WithPlatform(root.Platform),
			imagespec.WithInitrd(imagespec.NewTempOSFile(root.File)),
		))
	}
	return imgs, nil
}

// BuildRootfs builds OCI images from a Dockerfile for each platform in opts.
func BuildRootfs(ctx context.Context, c *client.Client, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	roots, err := PackageRootfs(ctx, c, opts)
	if err != nil {
		return nil, err
	}

	defer func() {
		if rerr != nil {
			for _, root := range roots {
				if root.File != nil {
					root.File.Close()
					os.Remove(root.File.Name())
				}
			}
		}
	}()

	return ExportRootfs(ctx, roots, opts)
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
