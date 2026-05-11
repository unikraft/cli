// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"fmt"
	"strings"
	"testing"
)

func buildTests(t *testing.T, r *integrationRunner) {
	var metroName string
	type variant struct {
		name  string
		image string
	}
	var variants []variant
	if r.cfg != nil {
		metroName = r.cfg.MetroName

		variants = []variant{
			{
				name:  "registry",
				image: fmt.Sprintf("%s/busybox-e2e:$UNIQ_IMAGE", r.cfg.Profile.Organization),
			},
			{
				name:  "direct-push",
				image: fmt.Sprintf("%s/%s/busybox-e2e:$UNIQ_IMAGE", r.cfg.Metro.Index().Host, r.cfg.Profile.Organization),
			},
		}
	}

	// NOTE: only erofs is supported for ROM automounting by the Unikraft
	// kernel currently. CPIO ROMs are not automounted (the kernel hardcodes
	// erofs as the fs type for ROM mounts), so the CPIO variant omits
	// the at= mount option.
	for _, romFormat := range []string{"erofs", "cpio"} {
		t.Run("rom-"+romFormat, func(t *testing.T) {
			var romImage, baseImage string
			if r.cfg != nil {
				romImage = fmt.Sprintf("%s/rom-%s-e2e:$UNIQ_IMAGE", r.cfg.Profile.Organization, romFormat)
				baseImage = fmt.Sprintf("%s/busybox-rom-%s-e2e:$UNIQ_IMAGE", r.cfg.Profile.Organization, romFormat)
			}

			// Only erofs ROMs support kernel automounting via at=.
			// For CPIO, we extract manually from the block device.
			romFlag := "image=" + romImage + ",name=myrom"
			cmd := []string{"sh", "-c", "cd /tmp && cpio -id < /dev/ukp_rom_myrom && cat hello.txt"}
			if romFormat == "erofs" {
				romFlag += ",at=/rom"
				cmd = []string{"cat", "/rom/hello.txt"}
			}

			r.
				online().
				withContext(map[string]string{
					"base/Dockerfile": `FROM busybox:latest`,
					"base/Kraftfile": fmt.Sprintf(`
spec: v0.7
name: busybox-rom-%s-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["%s"]
`, romFormat, strings.Join(cmd, `", "`)),
					// ROM-only image context: just a directory with a text file.
					"rom/myrom/hello.txt": "Hello from ROM!\n",
					"rom/Kraftfile": fmt.Sprintf(`
spec: v0.7
name: rom-%s-e2e
roms:
  - source: ./myrom
    format: %s
`, romFormat, romFormat),
				}).
				run(t, []command{
					{args: []string{unikraftCmd, "build", "base", "--output", baseImage}},
					{args: []string{unikraftCmd, "build", "rom", "--output", romImage}},
					{args: []string{unikraftCmd, "run", "--name", "test-$UNIQ_INST", "--metro", metroName, "--output", "quiet", "--image", baseImage, "--rom", romFlag}},
					{args: []string{unikraftCmd, "instance", "wait", "--until", "state==stopped", "--timeout", "10s", "test-$UNIQ_INST"}},
					{args: []string{unikraftCmd, "instance", "logs", "test-$UNIQ_INST"}, match: []string{`Hello from ROM!`}},
					{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
				})
		})
	}

	t.Run("rom-dir", func(t *testing.T) {
		var baseImage string
		if r.cfg != nil {
			baseImage = fmt.Sprintf("%s/busybox-romdir-e2e:$UNIQ_IMAGE", r.cfg.Profile.Organization)
		}

		r.
			online().
			withContext(map[string]string{
				"base/Dockerfile": `FROM busybox:latest`,
				"base/Kraftfile": `
spec: v0.7
name: busybox-romdir-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["cat", "/rom/hello.txt"]
`,
				"romdata/hello.txt": "Hello from ROM!\n",
			}).
			run(t, []command{
				{args: []string{unikraftCmd, "build", "base", "--output", baseImage}},
				{args: []string{unikraftCmd, "run", "--name", "test-$UNIQ_INST", "--metro", metroName, "--output", "quiet", "--image", baseImage, "--rom", "dir=romdata,at=/rom"}},
				{args: []string{unikraftCmd, "instance", "wait", "--until", "state==stopped", "--timeout", "10s", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "logs", "test-$UNIQ_INST"}, match: []string{`Hello from ROM!`}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})

	t.Run("busybox", func(t *testing.T) {
		if r.cfg == nil {
			t.Skip("busybox tests require online config")
		}
		for _, format := range []string{"cpio", "erofs"} {
			t.Run(format, func(t *testing.T) {
				for _, v := range variants {
					t.Run(v.name, func(t *testing.T) {
						r.
							online().
							withContext(map[string]string{
								"Dockerfile": `
FROM busybox:latest
RUN echo "unikraft-e2e" > /etc/unikraft-e2e
COPY <<EOF /entrypoint.sh
#!/bin/sh
echo "== BEGIN /etc/unikraft-e2e =="
cat /etc/unikraft-e2e
echo "== END /etc/unikraft-e2e =="
echo "== BEGIN ls /etc/unikraft-e2e =="
ls /etc/unikraft-e2e
echo "== END ls /etc/unikraft-e2e =="
echo "== BEGIN status =="
echo UNIKRAFT_E2E_OK
echo "== END status =="
EOF
RUN chmod +x /entrypoint.sh
`,
								"Kraftfile": fmt.Sprintf(`
spec: v0.7
name: busybox-e2e
runtime: base-compat:latest
rootfs:
  format: %s
  source: ./Dockerfile
cmd: ["sh", "/entrypoint.sh"]
`, format),
							}).
							run(t, []command{
								{args: []string{unikraftCmd, "build", ".", "--output", v.image}},
								{args: []string{unikraftCmd, "image", "inspect", v.image}, match: []string{`busybox-e2e`}},
								{args: []string{unikraftCmd, "image", "ls", v.image, "-okv"}},
								{args: []string{unikraftCmd, "run", "--name", "test-$UNIQ_INST", "--metro", metroName, "--output", "quiet", "--image", v.image}},
								{args: []string{unikraftCmd, "instance", "wait", "--until", "state==stopped", "--timeout", "10s", "test-$UNIQ_INST"}},
								{args: []string{unikraftCmd, "instance", "logs", "test-$UNIQ_INST"}, match: []string{`UNIKRAFT_E2E_OK`}},
								{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
								{args: []string{unikraftCmd, "image", "delete", v.image}},
								{args: []string{unikraftCmd, "image", "inspect", v.image}, err: errYes},
								{args: []string{unikraftCmd, "image", "ls", v.image}, err: errYes},
							})
					})
				}
			})
		}
	})
}

func buildHelpTests(t *testing.T, unikraftPath string) {
	r := newTestEnv(t, unikraftPath)
	gild(t.Context(), t, r.cli,
		[]string{unikraftCmd, "build", "--help"},
	)
}
