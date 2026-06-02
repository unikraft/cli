// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/require"
)

// sharedDockerfile is the Dockerfile used to build the shared busybox image.
var sharedDockerfile = `FROM busybox:latest`

// sharedKraftfile has 'sh -c' as entrypoint to allow arbitrary commands in
// tests using the shared image.
var sharedKraftfile = `spec: v0.7
name: shared-busybox-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source:
    path: .
    dockerfile: App.dockerfile
cmd: ["sh", "-c"]
`

var (
	sharedImageOnce    sync.Once
	sharedImageRef     string
	sharedImageErr     error
	sharedImageTag     = randomImageTag()
	sharedImageCleanup func()
)

func randomImageTag() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "shared-" + hex.EncodeToString(b[:])
}

// BuildSharedBusyboxImage builds a single busybox-based image.
func BuildSharedBusyboxImage(t *testing.T, env *TestEnv) string {
	t.Helper()
	require.NotNil(t, env.Config, "shared image build requires a config")
	sharedImageOnce.Do(func() {
		sharedImageRef, sharedImageErr = buildSharedBusyboxImage(t, env)
	})
	require.NoError(t, sharedImageErr)
	return sharedImageRef
}

func buildSharedBusyboxImage(t *testing.T, env *TestEnv) (string, error) {
	t.Helper()
	image := env.Config.Profile.Organization + "/shared-busybox-e2e:" + sharedImageTag

	dir := t.TempDir()
	if err := fstest.Apply(
		fstest.CreateFile("App.dockerfile", []byte(sharedDockerfile), 0o644),
		fstest.CreateFile("Kraftfile", []byte(sharedKraftfile), 0o644),
	).Apply(dir); err != nil {
		return "", fmt.Errorf("populating shared image build context: %w", err)
	}

	if out, err := env.RunRaw(t, []string{"unikraft", "build", ".", "--output", image}, WithWorkDir(dir), WithNoSandbox()); err != nil {
		return "", fmt.Errorf("building shared image: %w\n%s", err, out)
	}

	unikraftPath := env.unikraftPath
	cleanupDir, err := os.MkdirTemp("", "unikraft-shared-image-cleanup-*")
	if err != nil {
		return "", fmt.Errorf("creating cleanup temp dir: %w", err)
	}
	configData, err := os.ReadFile(env.configPath)
	if err != nil {
		_ = os.RemoveAll(cleanupDir)
		return "", fmt.Errorf("reading config for cleanup: %w", err)
	}
	cleanupConfigPath := filepath.Join(cleanupDir, "config.yaml")
	if err := os.WriteFile(cleanupConfigPath, configData, 0o600); err != nil {
		_ = os.RemoveAll(cleanupDir)
		return "", fmt.Errorf("writing cleanup config: %w", err)
	}

	sharedImageCleanup = func() {
		defer os.RemoveAll(cleanupDir)
		c := exec.Command(unikraftPath, "image", "delete", image)
		c.Env = slices.DeleteFunc(os.Environ(), func(s string) bool {
			return strings.HasPrefix(s, "UNIKRAFT_")
		})
		c.Env = append(c.Env, "NO_COLOR=1", "UNIKRAFT_CONFIG="+cleanupConfigPath)
		err := c.Run()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to cleanup shared image: %v\n", err)
		}
	}

	return image, nil
}

// CleanupSharedBusyboxImage deletes the shared busybox image from the registry.
func CleanupSharedBusyboxImage() {
	if sharedImageCleanup != nil {
		sharedImageCleanup()
	}
}
