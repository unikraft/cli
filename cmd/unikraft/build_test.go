// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"fmt"
	"testing"

	"unikraft.com/cli/internal/integration"
)

func buildTestCases(t *testing.T, cfg *integration.Config) []testCase {
	t.Helper()
	if cfg == nil {
		t.Skip("integration config not found")
	}

	busybox := fmt.Sprintf("%s/busybox-e2e:$UNIQ_IMAGE", cfg.Profile.Organization)
	// this is what we'd use to test direct push
	// busybox := fmt.Sprintf("%s/%s/busybox-e2e:$UNIQ_IMAGE", cfg.Metro.Index().Host, cfg.Profile.Organization)

	return []testCase{
		{
			name: "help",
			commands: []command{
				{args: []string{unikraftCmd, "build", "--help"}},
			},
		},
		{
			name:   "busybox",
			online: true,
			context: map[string]string{
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
				"Kraftfile": `
spec: v0.7
name: busybox-e2e
runtime: base-compat:latest
rootfs: ./Dockerfile
cmd: ["sh", "/entrypoint.sh"]
`,
			},
			commands: []command{
				{args: []string{unikraftCmd, "build", ".", "--output", busybox}},
				{args: []string{unikraftCmd, "run", "--name", "test-$UNIQ_INST", "--metro", cfg.MetroName, "--output", "quiet", busybox}},
				{args: []string{unikraftCmd, "instance", "wait", "--until", "state==stopped", "--timeout", "10s", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "logs", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			},
		},
	}
}
