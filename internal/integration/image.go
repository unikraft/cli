// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/require"
)

// SharedEntrypoint* are the unique paths of the entrypoint scripts baked into
// the shared busybox image.
const (
	SharedEntrypointRomErofs = "/test-rom-erofs/entrypoint.sh"
	SharedEntrypointRomCpio  = "/test-rom-cpio/entrypoint.sh"
	SharedEntrypointRomDir   = "/test-rom-dir/entrypoint.sh"
	SharedEntrypointCustomDF = "/test-custom-df/entrypoint.sh"
)

// shareDockerfile is the Dockerfile used to build the shared busybox image.
// It copies in entrypoints for each test.
var sharedDockerfile = `FROM busybox:latest
COPY <<EOF ` + SharedEntrypointRomErofs + `
#!/bin/sh
cat /rom/hello.txt
EOF
COPY <<EOF ` + SharedEntrypointRomCpio + `
#!/bin/sh
cd /tmp && cpio -id < /dev/ukp_rom_myrom && cat hello.txt
EOF
COPY <<EOF ` + SharedEntrypointRomDir + `
#!/bin/sh
cat /rom/hello.txt
EOF
COPY <<EOF ` + SharedEntrypointCustomDF + `
#!/bin/sh
echo UNIKRAFT_E2E_OK
EOF
RUN chmod +x ` + SharedEntrypointRomErofs + ` ` + SharedEntrypointRomCpio + ` ` + SharedEntrypointRomDir + ` ` + SharedEntrypointCustomDF + `
`

// sharedKraftfile has no entrypoint to allow for injecting entrypoints
// via --args in tests.
var sharedKraftfile = `spec: v0.7
name: shared-busybox-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source:
    path: .
    dockerfile: App.dockerfile
cmd: ["sh"]
`

var (
	sharedImageOnce sync.Once
	sharedImageRef  string
	sharedImageErr  error
	sharedImageTag  = randomImageTag()
)

func randomImageTag() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "shared-" + hex.EncodeToString(b[:])
}

// BuildSharedBusyboxImage builds a single busybox-based image that bundles
// the entrypoint scripts for every test that needs a runnable busybox image,
// pushes it to the registry, and returns its reference.
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

	if out, err := env.RunRaw(t, []string{"unikraft", "build", ".", "--output", image}, WithWorkDir(dir)); err != nil {
		return "", fmt.Errorf("building shared image: %w\n%s", err, out)
	}
	return image, nil
}
