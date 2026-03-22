// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"fmt"
	"testing"
)

func buildTests(t *testing.T, r *testRunner) {
	t.Run("help", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "build", "--help"}},
		})
	})

	var busybox, busyboxFull, metroName string
	if r.cfg != nil {
		busybox = r.cfg.Profile.Organization + "/busybox-e2e:$UNIQ_IMAGE"
		busyboxFull = fmt.Sprintf("%s/%s", r.cfg.Metro.Index().Host, busybox)
		metroName = r.cfg.MetroName
	}

	t.Run("busybox", func(t *testing.T) {
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
				"Kraftfile": `
spec: v0.7
name: busybox-e2e
runtime: base-compat:latest
rootfs: ./Dockerfile
cmd: ["sh", "/entrypoint.sh"]
`,
			}).
			run(t, []command{
				{args: []string{unikraftCmd, "build", ".", "--output", busyboxFull}},
				{args: []string{unikraftCmd, "run", "--name", "test-$UNIQ_INST", "--metro", metroName, "--output", "quiet", busybox}},
				{args: []string{unikraftCmd, "instance", "wait", "--until", "state==stopped", "--timeout", "10s", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "logs", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			})
	})
}
