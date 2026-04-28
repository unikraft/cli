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
	"io/fs"
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

	goerofs "github.com/unikraft/go-archivefs/erofs"
	gotar "github.com/unikraft/go-archivefs/tarfs"
	gocpio "github.com/unikraft/go-cpio"
	"unikraft.com/cli/internal/builder/buildfs"
	"unikraft.com/cli/internal/buildkit"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/images"
	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"
)

// buildImageConfig constructs a minimal OCI image config from build options.
// Used when a BuildKit solve is not performed.
func buildImageConfig(opts BuildOpts) ocispec.ImageConfig {
	var cfg ocispec.ImageConfig
	if opts.Cmd != nil {
		cfg.Cmd = opts.Cmd
	}
	if opts.Env != nil {
		env := make([]string, 0, len(opts.Env))
		for _, kv := range opts.Env {
			env = append(env, fmt.Sprintf("%s=%s", kv.Key, kv.Value))
		}
		cfg.Env = env
	}
	cfg.Labels = opts.Labels
	return cfg
}

// DetectSourceType inspects path and returns the detected RootfsType.
func DetectSourceType(path string) (kraftfile.SourceType, error) {
	if path == "" {
		return "", fmt.Errorf("empty rootfs path")
	}

	base := filepath.Base(path)
	if base == "Dockerfile" || slices.Contains(strings.Split(base, "."), "Dockerfile") {
		return kraftfile.SourceTypeDockerfile, nil
	}

	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("rootfs path does not exist")
		}
		return "", fmt.Errorf("checking rootfs source %q: %w", path, err)
	}

	switch {
	case fi.IsDir():
		return kraftfile.SourceTypeDirectory, nil
	case fi.Mode().IsRegular(), fi.Mode()&os.ModeSymlink != 0:
		if gocpio.IsValidPath(path) {
			return kraftfile.SourceTypeCpio, nil
		}
		if goerofs.IsValidPath(path) {
			return kraftfile.SourceTypeErofs, nil
		}
		if f, err := os.Open(path); err == nil {
			defer f.Close()
			r, err := buildfs.MaybeGunzip(f)
			if err != nil {
				return "", nil
			}
			if gotar.IsValid(r) {
				return kraftfile.SourceTypeTarball, nil
			}
		}
		return "", fmt.Errorf("could not detect file rootfs type %q", path)
	default:
		return "", fmt.Errorf("could not detect rootfs type %q", path)
	}
}

func BuildRoms(ctx context.Context, opts BuildOpts) (_ []imagespec.File, rerr error) {
	// Use a single dummy platform since ROMs are platform-independent.
	dummyPlatform := []ocispec.Platform{{Architecture: "x86_64", OS: "kraftcloud"}}

	var romFiles []imagespec.File
	for _, rom := range opts.Roms {
		format := rom.Format
		if format == "" {
			format = kraftfile.FsTypeErofs
		}

		romBuildOpts := BuildOpts{
			Rootfs: RootfsOpts{
				Path:   rom.Path,
				Type:   rom.Type,
				Format: format,
			},
			Platform: dummyPlatform,
		}

		imgs, err := BuildRootfs(ctx, romBuildOpts)
		if err != nil {
			return nil, fmt.Errorf("building rom from %q: %w", rom.Path, err)
		}
		if len(imgs) == 0 || imgs[0].Initrd == nil {
			return nil, fmt.Errorf("rom build from %q produced no output", rom.Path)
		}

		romFile := imgs[0].Initrd
		imgs[0].Initrd = nil // detach so Close doesn't close it
		for _, img := range imgs {
			img.Close()
		}

		romFiles = append(romFiles, romFile)
	}
	return romFiles, nil
}

// BuildRootfs builds a rootfs for each platform in opts.Platform from the
// source at opts.Rootfs.Path.
func BuildRootfs(ctx context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	if len(opts.Platform) == 0 {
		return nil, fmt.Errorf("at least one platform must be specified")
	}

	switch opts.Rootfs.Type {
	case kraftfile.SourceTypeCpio, kraftfile.SourceTypeErofs:
		if opts.Rootfs.Format != "" {
			expected := map[kraftfile.SourceType]kraftfile.FsType{
				kraftfile.SourceTypeCpio:  kraftfile.FsTypeCpio,
				kraftfile.SourceTypeErofs: kraftfile.FsTypeErofs,
			}
			if exp, ok := expected[opts.Rootfs.Type]; ok && opts.Rootfs.Format != exp {
				// TODO: maybe we could be smart and convert this
				return nil, fmt.Errorf("unsupported rootfs format mismatch: source is %s but requested format is %s", opts.Rootfs.Type, opts.Rootfs.Format)
			}
		}
		return buildRootfsPackaged(ctx, opts)
	case kraftfile.SourceTypeDirectory:
		return buildRootfsDirectory(ctx, opts)
	case kraftfile.SourceTypeTarball:
		return buildRootfsTarball(ctx, opts)
	case kraftfile.SourceTypeDockerfile:
		if f, err := os.Stat(opts.Rootfs.Path); err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("dockerfile does not exist")
			}
			return nil, fmt.Errorf("checking dockerfile path %q: %w", opts.Rootfs.Path, err)
		} else if !f.IsDir() {
			opts.Rootfs.Path = filepath.Dir(opts.Rootfs.Path)
		}
		return buildRootfsDockerfile(ctx, opts)
	default:
		return nil, fmt.Errorf("unsupported rootfs type %q", opts.Rootfs.Type)
	}
}

// buildRootfsFromPackaged returns images backed by an already-packaged rootfs
// file. The file is opened read-only per platform.
// The caller must not delete it.
func buildRootfsPackaged(_ context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	cfg := buildImageConfig(opts)

	var imgs []*imagespec.Image
	for _, p := range opts.Platform {
		f, err := os.Open(opts.Rootfs.Path)
		if err != nil {
			return nil, fmt.Errorf("opening pre-packaged rootfs: %w", err)
		}
		defer func() {
			if rerr != nil {
				f.Close()
			}
		}()

		imgs = append(imgs, imagespec.NewImage(
			imagespec.WithImageConfig(cfg),
			imagespec.WithPlatform(p),
			imagespec.WithInitrd(imagespec.NewOSFile(f)),
		))
	}
	return imgs, nil
}

// buildRootfsFromDirectory archives the source directory into a temporary
// rootfs file (CPIO or EroFS, based on opts.Rootfs.Format) for each platform.
func buildRootfsDirectory(ctx context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	cfg := buildImageConfig(opts)

	var imgs []*imagespec.Image
	for _, p := range opts.Platform {
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

		if err := packageRootfs(ctx, opts.Rootfs.Format, f, os.DirFS(opts.Rootfs.Path), opts); err != nil {
			return nil, err
		}

		imgs = append(imgs, imagespec.NewImage(
			imagespec.WithImageConfig(cfg),
			imagespec.WithPlatform(p),
			imagespec.WithInitrd(imagespec.NewTempOSFile(f)),
		))
	}
	return imgs, nil
}

// buildRootfsTarball opens the source tarball as an fs.FS and packages it
// into the requested rootfs format (CPIO or EroFS) for each platform.
func buildRootfsTarball(ctx context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
	cfg := buildImageConfig(opts)

	tarFile, err := os.Open(opts.Rootfs.Path)
	if err != nil {
		return nil, fmt.Errorf("could not open tarball: %w", err)
	}
	defer tarFile.Close()

	srcFS, err := buildfs.TarballFS(tarFile)
	if err != nil {
		return nil, fmt.Errorf("could not open tarball as filesystem: %w", err)
	}

	var imgs []*imagespec.Image
	for _, p := range opts.Platform {
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

		if err := packageRootfs(ctx, opts.Rootfs.Format, f, srcFS, opts); err != nil {
			return nil, err
		}

		imgs = append(imgs, imagespec.NewImage(
			imagespec.WithImageConfig(cfg),
			imagespec.WithPlatform(p),
			imagespec.WithInitrd(imagespec.NewTempOSFile(f)),
		))
	}
	return imgs, nil
}

func buildRootfsDockerfile(ctx context.Context, opts BuildOpts) (_ []*imagespec.Image, rerr error) {
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
	localDirs := map[string]string{}
	if err := applyBuildOpts(attrs, localDirs, &session, opts); err != nil {
		return nil, err
	}

	tarDest, err := os.CreateTemp("", "unikraft-buildkit-*.tar")
	if err != nil {
		return nil, fmt.Errorf("could not create temporary file: %w", err)
	}
	tarDestPath := tarDest.Name()
	defer func() {
		tarDest.Close()
		os.Remove(tarDestPath)
	}()

	solveOpt := client.SolveOpt{
		Ref:     identity.NewID(),
		Session: session,
		Exports: []client.ExportEntry{
			{
				Type: client.ExporterTar,
				Output: func(map[string]string) (io.WriteCloser, error) {
					return tarDest, nil
				},
			},
		},
		LocalDirs:     localDirs,
		Frontend:      "dockerfile.v0",
		FrontendAttrs: attrs,
	}

	c, cleanup, err := buildkit.ConnectToBuildkit(ctx)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
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

	// Reopen the tarball for reading.
	tarFile, err := os.Open(tarDestPath)
	if err != nil {
		return nil, fmt.Errorf("could not reopen tarball: %w", err)
	}
	defer tarFile.Close()

	srcFS, err := buildfs.TarballFS(tarFile)
	if err != nil {
		return nil, fmt.Errorf("could not open tarball as filesystem: %w", err)
	}

	var imgs []*imagespec.Image

	for i, p := range opts.Platform {
		ep := expPlatforms[i]
		config := configs[i]

		_ = ep
		// HACK: only valid with multi-platform enabled
		// srcFS would need to be scoped to the platform subdirectory

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

		if err := packageRootfs(ctx, opts.Rootfs.Format, f, srcFS, opts); err != nil {
			return nil, err
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

func packageRootfs(ctx context.Context, format kraftfile.FsType, rootfs *os.File, srcFS fs.FS, opts BuildOpts) error {
	log.G(ctx).Debug().Str("format", string(format)).Msg("packaging rootfs")
	switch format {
	case kraftfile.FsTypeCpio:
		var gw *gzip.Writer
		var w io.Writer = rootfs
		if opts.Rootfs.Compress {
			gw = gzip.NewWriter(w)
			w = gw
		}

		if err := buildfs.CreateCPIO(ctx, w, srcFS,
			buildfs.WithAllRoot(!opts.Rootfs.KeepOwners),
		); err != nil {
			return fmt.Errorf("could not create CPIO archive: %w", err)
		}

		if gw != nil {
			if err := gw.Close(); err != nil {
				return fmt.Errorf("could not close gzip writer: %w", err)
			}
		}
	case kraftfile.FsTypeErofs:
		if opts.Rootfs.Compress {
			log.G(ctx).Warn().Msg("compression is not supported for EROFS, ignoring compress option")
		}

		if err := buildfs.CreateEROFS(rootfs, srcFS,
			buildfs.WithAllRoot(!opts.Rootfs.KeepOwners),
		); err != nil {
			return fmt.Errorf("could not create EroFS archive: %w", err)
		}
	default:
		return fmt.Errorf("unknown filesystem type %q", format)
	}

	if err := rootfs.Sync(); err != nil {
		return fmt.Errorf("could not sync file: %w", err)
	}

	return nil
}

func applyBuildOpts(attrs map[string]string, localDirs map[string]string, sessions *[]session.Attachable, opts BuildOpts) error {
	var ps []string
	for _, p := range getPlatforms(opts.Platform) {
		ps = append(ps, p.ID)
	}
	slices.Sort(ps)
	ps = slices.Compact(ps)
	if len(ps) > 1 {
		// HACK: disabled multi-platform mode due to buildkit symlink bug.
		// https://github.com/moby/buildkit/issues/6684
		return fmt.Errorf("multi-platform builds are currently not supported")
	}
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
