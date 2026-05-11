// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestInstanceCheckpoints(t *testing.T) {
	t.Run("checkpoint", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", "test-" + instName,
			"--output", "template={{ .name }}",
		})
		checkpointName := strings.TrimSpace(out)
		assert.NotEmpty(t, checkpointName)

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "list"})
		assert.Contains(t, out, checkpointName)

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "inspect", checkpointName})
		// A freshly-created checkpoint may briefly report "starting" before it
		// settles into the terminal "checkpoint" state.
		assert.Regexp(t, `state:\s+(checkpoint|starting)`, out)

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "edit", checkpointName, "--set", "tags=env-dev"})
		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "inspect", checkpointName, "-f", "all"})
		assert.Contains(t, out, "env-dev")

		// The source instance's history lists its checkpoints.
		out = r.Run(t, []string{"unikraft", "instance", "history", "test-" + instName})
		assert.Contains(t, out, checkpointName)

		// A checkpoint's own history (ancestor checkpoints) is empty for a
		// freshly-created checkpoint; the command should still succeed and
		// render the table header.
		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "history", checkpointName})
		assert.Contains(t, out, "CREATED")

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpointName})

		out = r.Run(t, []string{"unikraft", "instance", "checkpoint", "list", "--output", "quiet"})
		assert.NotContains(t, out, checkpointName)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("create-from-checkpoint", func(t *testing.T) {
		r := runner(t, true)
		baseName := uniq()
		restoredName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-base-" + baseName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", "test-base-" + baseName,
			"--output", "template={{ .name }}",
		})
		checkpointName := strings.TrimSpace(out)
		assert.NotEmpty(t, checkpointName)

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--checkpoint", checkpointName,
			"--set", "name=test-restored-" + restoredName,
			"--set", "metro=" + r.Config.MetroName,
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-restored-" + restoredName})
		assert.Regexp(t, `name:\s+test-restored-`, out)

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpointName})
		r.Run(t, []string{"unikraft", "instance", "delete", "test-base-" + baseName, "test-restored-" + restoredName})
	})

	// checkpoint-state verifies that a checkpoint preserves in-memory state and
	// that the restored instance is independent of the original. It builds a
	// counter HTTP server, increments to 3, takes a checkpoint, increments
	// further to 5, restores from the checkpoint (counter=3), then increments
	// each independently to prove isolation.
	t.Run("checkpoint-state", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		restoredName := uniq()
		domainName := uniq()
		domainRestored := uniq()
		imageTag := uniq()
		image := r.Config.Profile.Organization + "/counter-e2e:" + imageTag

		dir := t.TempDir()
		require.NoError(t, applyCounterContext(dir))

		r.Run(t, []string{"unikraft", "build", ".", "--output", image}, integ.WithWorkDir(dir))

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=256",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainName,
		})
		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdn := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		// Increment counter to 3.
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 3`)

		// Take a checkpoint (counter=3).
		out = r.Run(t, []string{
			"unikraft", "instance", "checkpoint", "create", "test-" + instName,
			"--output", "template={{ .name }}",
		})
		checkpointName := strings.TrimSpace(out)
		assert.NotEmpty(t, checkpointName)

		// Increment original further to 5.
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		// Restore from checkpoint into a new instance.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--checkpoint", checkpointName,
			"--set", "name=test-restored-" + restoredName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "autostart=true",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainRestored,
		})
		out = r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-restored-" + restoredName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdnRestored := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-restored-" + restoredName})

		// Restored counter should be back at 3.
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnRestored+"/count"), `"count": 3`)

		// Increment restored independently by 10 → 13.
		integ.HTTPPost(t, "https://"+fqdnRestored+"/increment", "application/json", `{"delta":10}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnRestored+"/count"), `"count": 13`)

		// Original should still be at 5 (unaffected by restored).
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		// Increment original by 1 → 6.
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 6`)

		// Restored should still be at 13 (unaffected by original).
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnRestored+"/count"), `"count": 13`)

		r.Run(t, []string{"unikraft", "instance", "checkpoint", "delete", checkpointName})
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName, "test-restored-" + restoredName})
		r.Run(t, []string{"unikraft", "image", "delete", image})
	})
}
