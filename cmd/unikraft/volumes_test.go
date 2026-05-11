// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"testing"

	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/types"
)

func volumesTests(t *testing.T, r *integrationRunner) {
	metroName := ""
	if r.cfg != nil {
		metroName = r.cfg.MetroName
	}

	t.Run("create", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "volume", "list"}, match: []string{`METRO\s+NAME`}},
				{args: []string{unikraftCmd, "volume", "create", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}, match: []string{`state:\s+available`, `size:\s+10`}},
				{args: []string{unikraftCmd, "volume", "list"}, match: []string{`test-.*available`}},
				{args: []string{unikraftCmd, "volume", "inspect", "test-$UNIQ_VOLUME"}, match: []string{`state:\s+available`, `size:\s+10`}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOLUME"}, match: []string{`test-`}},
			})
	})

	t.Run("edit", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "volume", "create", "--output", "quiet", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}},
				{args: []string{unikraftCmd, "volume", "edit", "test-$UNIQ_VOLUME", "--output", "quiet", "--set", "size=20"}},
				{args: []string{unikraftCmd, "volume", "inspect", "test-$UNIQ_VOLUME"}, match: []string{`size:\s+20`}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOLUME"}},
			})
	})

	t.Run("clone", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "volume", "create", "--output", "quiet", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}},
				{args: []string{unikraftCmd, "volume", "clone", "test-$UNIQ_VOLUME", "--output", "quiet", "--set", "name=test-$UNIQ_VOLUME_CLONE"}},
				{args: []string{unikraftCmd, "volume", "inspect", "test-$UNIQ_VOLUME", "test-$UNIQ_VOLUME_CLONE"}, match: []string{`state:\s+available`}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOLUME", "test-$UNIQ_VOLUME_CLONE"}},
			})
	})

	t.Run("import", func(t *testing.T) {
		t.Run("missing-source", func(t *testing.T) {
			r.run(t, []command{
				{args: []string{unikraftCmd, "volume", "import", "my-volume"}, err: errYes, match: []string{`source path is required`}},
			})
		})

		t.Run("invalid-port", func(t *testing.T) {
			r.run(t, []command{
				{args: []string{unikraftCmd, "volume", "import", "my-volume", "--source", ".", "--port", "80"}, err: errYes, match: []string{`port must be between`}},
			})
		})

		t.Run("invalid-port-high", func(t *testing.T) {
			r.run(t, []command{
				{args: []string{unikraftCmd, "volume", "import", "my-volume", "--source", ".", "--port", "99999"}, err: errYes, match: []string{`port must be between`}},
			})
		})

		t.Run("dir", func(t *testing.T) {
			r.
				online().
				withContext(map[string]string{
					"hello.txt": "hello from volume import\n",
				}).
				run(t, []command{
					{args: []string{unikraftCmd, "volume", "create", "--output", "quiet", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}},
					{args: []string{unikraftCmd, "volume", "import", "test-$UNIQ_VOLUME", "--source", "."}, match: []string{`import complete`}},
					{args: []string{unikraftCmd, "volume", "inspect", "test-$UNIQ_VOLUME"}, match: []string{`state:\s+available`}},
					{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOLUME"}},
				})
		})

		t.Run("serve", func(t *testing.T) {
			r.
				online().
				withContext(map[string]string{
					"index.html": "<html><body>hello from volume import</body></html>\n",
				}).
				run(t, []command{
					{args: []string{
						unikraftCmd, "volume", "create",
						"--output", "quiet",
						"--set", "name=test-$UNIQ_VOL",
						"--set", "size=50",
						"--set", "metro=" + metroName,
					}},
					{args: []string{unikraftCmd, "volume", "import", "test-$UNIQ_VOL", "--source", "."}, match: []string{`import complete`}},
					{args: []string{
						unikraftCmd, "instance", "create",
						"--set", "name=test-$UNIQ_INST",
						"--set", "metro=" + metroName,
						"--set", "image=nginx:latest",
						"--set", "autostart=true",
						"--set", "resources.memory=256",
						"--set", "resources.vcpus=1",
						"--set", "volumes=test-$UNIQ_VOL:/wwwroot",
						"--set", "service.services=443:8080/tls+http",
						"--set", "service.domains=name=$UNIQ_DOMAIN",
					}},
					{
						args: []string{
							unikraftCmd, "instance", "inspect", "test-$UNIQ_INST",
							"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
						},
						captureEnv: "FQDN",
					},
					{args: []string{unikraftCmd, "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-$UNIQ_INST"}},
					{args: []string{
						"curl",
						"-k",
						"--fail",
						"--silent",
						"--show-error",
						"--output", "response.html",
						"--retry", "10",
						"--retry-delay", "2",
						"--retry-all-errors",
						"--connect-timeout", "5",
						"--max-time", "10",
						"https://$FQDN",
					}},
					{args: []string{"grep", "hello from volume import", "response.html"}, match: []string{`hello from volume import`}},
					{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
					{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOL"}},
				})
		})
	})
}

func volumesHelpTests(t *testing.T, unikraftPath string) {
	r := newTestEnv(t, unikraftPath)
	gild(t.Context(), t, r.cli,
		[]string{unikraftCmd, "volume", "--help"},
		[]string{unikraftCmd, "volume", "get", "--help"},
		[]string{unikraftCmd, "volume", "list", "--help"},
		[]string{unikraftCmd, "volume", "wait", "--help"},
		[]string{unikraftCmd, "volume", "create", "--help"},
		[]string{unikraftCmd, "volume", "clone", "--help"},
		[]string{unikraftCmd, "volume", "import", "--help"},
		[]string{unikraftCmd, "volume", "edit", "--help"},
		[]string{unikraftCmd, "volume", "delete", "--help"},
		[]string{unikraftCmd, "volume", "template", "--help"},
		[]string{unikraftCmd, "volume", "template", "get", "--help"},
		[]string{unikraftCmd, "volume", "template", "list", "--help"},
		[]string{unikraftCmd, "volume", "template", "create", "--help"},
		[]string{unikraftCmd, "volume", "template", "edit", "--help"},
		[]string{unikraftCmd, "volume", "template", "delete", "--help"},
	)
}

func volumesOutputTests(t *testing.T) {
	sample := cmd.Volume{
		MetroName:   "fra",
		Name:        "my-volume",
		UUID:        "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		Tags:        []string{"env-prod"},
		State:       types.VolumeState(platform.VolumeStateAvailable),
		Size:        50,
		Filesystem:  "ext4",
		QuotaPolicy: "hard",
		Persistent:  true,
	}

	gild[resource.Resource](t.Context(), t, dumpResource, sample)
}
