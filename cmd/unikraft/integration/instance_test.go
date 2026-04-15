// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestInstances(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		out := r.Run(t, []string{"unikraft", "instance", "list", "--output", "quiet"})
		assert.Empty(t, strings.TrimSpace(out))

		out = r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.env=A=1,B=2,C=3",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})
		assert.Regexp(t, `name:\s+test-`, out)
		assert.Regexp(t, `image:\s+nginx`, out)
		assert.Regexp(t, `memory:\s+128`, out)
		assert.Regexp(t, `state:\s+(running|starting)`, out)
		assert.Regexp(t, `A:\s+1`, out)

		out = r.Run(t, []string{"unikraft", "instance", "list"})
		assert.Regexp(t, `test-.*nginx`, out)

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `image:\s+nginx`, out)
		assert.Regexp(t, `memory:\s+128`, out)
		assert.Regexp(t, `state:\s+(running|starting)`, out)

		out = r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		assert.Regexp(t, `test-`, out)

		out = r.Run(t, []string{"unikraft", "instance", "list", "--output", "quiet"})
		assert.Empty(t, strings.TrimSpace(out))
	})

	t.Run("create-oom", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=16Mib",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==stopped", "test-" + instName})
		assert.Regexp(t, `state:\s+stopped`, out)
		assert.Regexp(t, `stop:`, out)
		assert.Regexp(t, `reason:.*(page fault|out of memory)`, out)
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("connect", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		domainName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.env=A=1,B=2,C=3",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainName,
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdn := strings.TrimSpace(out)

		r.Run(t, []string{"unikraft", "--timeout", "10s", "instance", "wait", "--until", "state==running", "test-" + instName})

		body := integ.HTTPGet(t, "https://"+fqdn)
		assert.Contains(t, body, "Welcome to nginx!")

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("start-stop", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.env=A=1,B=2,C=3",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{"unikraft", "instance", "stop", "test-" + instName})
		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+(stopped|stopping)\b`, out)

		r.Run(t, []string{"unikraft", "instance", "start", "test-" + instName})
		// TODO: start doesn't actually wait to start; re-enable once it does.
		// out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		// assert.Regexp(t, `state:\s+running`, out)

		r.Run(t, []string{"unikraft", "instance", "edit", "test-" + instName, "--set", "state=stopped"})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+(stopped|stopping)\b`, out)

		r.Run(t, []string{"unikraft", "instance", "edit", "test-" + instName, "--set", "state=running"})
		// TODO: start doesn't actually wait to start; re-enable once it does.
		// out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		// assert.Regexp(t, `state:\s+running`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("start-follow", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		volName := uniq()
		imageTag := uniq()

		baseImagePrefix := r.Config.Profile.Organization + "/busybox-start-follow-e2e"
		baseImage := baseImagePrefix + ":" + imageTag

		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("base", 0o755),
			fstest.CreateFile("base/Dockerfile", []byte(`FROM busybox:latest`), 0o644),
			fstest.CreateFile("base/Kraftfile", []byte(`
spec: v0.7
name: busybox-start-follow-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["cat", "/rom/hello.txt"]
`), 0o644),
		).Apply(dir))

		r.Run(t, []string{"unikraft", "build", "base", "--output", baseImage}, integ.WithWorkDir(dir))

		// Volume provides persistent counter across boots.
		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=20",
			"--set", "metro=" + r.Config.MetroName,
		})

		// On each boot, increment /data/n and echo "starting N".
		script := `n=$(cat /data/n 2>/dev/null || echo 0); n=$((n+1)); echo $n > /data/n; echo starting $n; sleep 30`
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + baseImage,
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=test-" + volName + ":/data",
			"--set", `runtime.args=["sh","-c","` + script + `"]`,
		})

		// First boot ("starting 1"): start, wait running, then stop.
		r.Run(t, []string{"unikraft", "instance", "start", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "stop", "test-" + instName})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==stopped", "--timeout", "30s", "test-" + instName})

		// Second boot via start --follow: output must contain "starting 2" only.
		out := r.Run(t, []string{
			"unikraft", "instance", "start",
			"--follow",
			"test-" + instName,
		}, integ.WithTimeout(5*time.Second), integ.AllowFail())
		assert.Contains(t, out, "starting 2")
		assert.NotContains(t, out, "starting 1")

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("restart-follow", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		volName := uniq()
		imageTag := uniq()

		baseImagePrefix := r.Config.Profile.Organization + "/busybox-restart-follow-e2e"
		baseImage := baseImagePrefix + ":" + imageTag

		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("base", 0o755),
			fstest.CreateFile("base/Dockerfile", []byte(`FROM busybox:latest`), 0o644),
			fstest.CreateFile("base/Kraftfile", []byte(`
spec: v0.7
name: busybox-restart-follow-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["cat", "/rom/hello.txt"]
`), 0o644),
		).Apply(dir))

		r.Run(t, []string{"unikraft", "build", "base", "--output", baseImage}, integ.WithWorkDir(dir))

		// Volume provides persistent counter across boots.
		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=20",
			"--set", "metro=" + r.Config.MetroName,
		})

		// On each boot, increment /data/n and echo "starting N".
		script := `n=$(cat /data/n 2>/dev/null || echo 0); n=$((n+1)); echo $n > /data/n; echo starting $n; sleep 30`
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + baseImage,
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=test-" + volName + ":/data",
			"--set", `runtime.args=["sh","-c","` + script + `"]`,
		})
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-" + instName})

		// Second boot via restart --follow: output must contain "starting 2" only.
		out := r.Run(t, []string{
			"unikraft", "instance", "restart",
			"--follow",
			"test-" + instName,
		}, integ.WithTimeout(5*time.Second), integ.AllowFail())
		assert.Contains(t, out, "starting 2")
		assert.NotContains(t, out, "starting 1")

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("edit", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "runtime.args=before,first",
			"--set", "runtime.env=A=1,B=2",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "image=redis:latest",
			"--set", "runtime.args=after,second",
			"--set", "runtime.env=A=3,B=4",
			"--set", "resources.memory=256",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+(stopped|stopping)\b`, out)
		assert.Regexp(t, `image:\s+redis`, out)
		assert.Regexp(t, `memory:\s+256`, out)
		assert.Regexp(t, `args:`, out)
		assert.Regexp(t, `A:\s+3`, out)
		assert.Regexp(t, `B:\s+4`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("volume", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=20",
			"--set", "metro=" + r.Config.MetroName,
		})

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=test-" + volName + ":/mnt",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/mnt`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("volume-inline", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=:/data:size=20",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/data`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("shortcut-service-volume", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		svcName := uniq()
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "service", "create",
			"--output", "quiet",
			"--set", "name=test-" + svcName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "services=443:8080/tls+http",
		})

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=20",
			"--set", "metro=" + r.Config.MetroName,
		})

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--service", "test-" + svcName,
			"-v", "test-" + volName + ":/mnt",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/mnt`, out)
		assert.Regexp(t, `service:`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
		r.Run(t, []string{"unikraft", "service", "delete", "test-" + svcName})
	})

	t.Run("rom-attach", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("romdata", 0o755),
			fstest.CreateFile("romdata/hello.txt", []byte("Hello from ROM!\n"), 0o644),
		).Apply(dir))

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

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "roms=dir=romdata,at=/rom",
		}, integ.WithWorkDir(dir))

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/rom`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("rom-add", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("romdata1", 0o755),
			fstest.CreateFile("romdata1/hello.txt", []byte("Hello from ROM 1!\n"), 0o644),
			fstest.CreateDir("romdata2", 0o755),
			fstest.CreateFile("romdata2/goodbye.txt", []byte("Goodbye from ROM 2!\n"), 0o644),
		).Apply(dir))

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--rom", "dir=romdata1,at=/rom1,name=rom1",
		}, integ.WithWorkDir(dir))

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--add", "roms=dir=romdata2,at=/rom2,name=rom2",
		}, integ.WithWorkDir(dir))

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/rom1`, out)
		assert.Regexp(t, `at:\s+/rom2`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("rom-detach", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		dir := t.TempDir()
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("romdata", 0o755),
			fstest.CreateFile("romdata/hello.txt", []byte("Hello from ROM!\n"), 0o644),
		).Apply(dir))

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--rom", "dir=romdata,at=/rom,name=myrom",
		}, integ.WithWorkDir(dir))

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--del", "roms=name=myrom",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `image:\s+nginx`, out)
		assert.NotRegexp(t, `roms:`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("volume-add", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=10",
			"--set", "metro=" + r.Config.MetroName,
		})

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

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--add", "volumes=test-" + volName + ":/data",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/data`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("volume-del", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "size=10",
			"--set", "metro=" + r.Config.MetroName,
		})

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "volumes=test-" + volName + ":/data",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `at:\s+/data`, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--del", "volumes=test-" + volName,
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.NotRegexp(t, `at:\s+/data`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + volName})
	})

	t.Run("autostart", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+(running|starting)`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("suspend", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==running", "test-" + instName})

		// No scale-to-zero so state will show as stopped.
		r.Run(t, []string{"unikraft", "instance", "suspend", "test-" + instName})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==stopped", "test-" + instName})
		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+stopped`, out)
		assert.Regexp(t, `stop:`, out)
		assert.Regexp(t, `reason:.*user stop`, out)

		r.Run(t, []string{"unikraft", "instance", "start", "test-" + instName})
		r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==running", "test-" + instName})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `state:\s+running`, out)
		assert.NotRegexp(t, `stop:`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("rm", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		// Create a running instance with --rm so it is auto-deleted
		// when stopped.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--rm",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})

		r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==running", "test-" + instName})

		// Stop the instance so delete-on-stop removes it (deletion is async).
		r.Run(t, []string{"unikraft", "instance", "stop", "test-" + instName})

		// Verify the instance no longer exists.
		time.Sleep(time.Second)
		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName}, integ.ExpectFail())
		assert.Regexp(t, `references not found`, out)
	})

	t.Run("add-domain", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		domainName := uniq()

		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", "template={{ .service.name }}",
		})
		serviceName := strings.TrimSpace(out)

		r.Run(t, []string{
			"unikraft", "service", "edit", serviceName,
			"--output", "quiet",
			"--add", "domains=name=" + domainName,
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `service:`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("sched-priority", func(t *testing.T) {
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
			"--sched-priority", "medium",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `sched-priority:\s+medium`, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--sched-priority", "high",
		})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `sched-priority:\s+high`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("tags", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()

		// Create instance with tags.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=false",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--tags", "env-prod",
			"--tags", "team-core",
		})

		// Verify tags appear in inspect output.
		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `tags:.*env-prod`, out)
		assert.Regexp(t, `tags:.*team-core`, out)

		// Filter by tag.
		out = r.Run(t, []string{"unikraft", "instance", "list", "--filter", "tags.*==env-prod"})
		assert.Contains(t, out, "test-"+instName)

		out = r.Run(t, []string{"unikraft", "instance", "list", "--filter", "tags.*==no-match"})
		assert.NotContains(t, out, "test-"+instName)

		// Edit: set (replace all tags).
		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "tags=new-tag",
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `tags:.*new-tag`, out)
		assert.NotRegexp(t, `env-prod`, out)
		assert.NotRegexp(t, `team-core`, out)

		// Edit: add a tag.
		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--add", "tags=added-tag",
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `tags:.*new-tag`, out)
		assert.Regexp(t, `tags:.*added-tag`, out)

		// Edit: del a tag.
		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--del", "tags=new-tag",
		})
		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.NotRegexp(t, `new-tag`, out)
		assert.Regexp(t, `tags:.*added-tag`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})
	})

	t.Run("delete-lock", func(t *testing.T) {
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

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "delete-lock=true",
		})

		out := r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName, "-f", "+delete-lock"})
		assert.Regexp(t, `delete-lock:\s+true`, out)

		out = r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName}, integ.ExpectFail())
		assert.Regexp(t, `(?i)deletion protection`, out)

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName})
		assert.Regexp(t, `name:\s+test-`+instName, out)

		r.Run(t, []string{
			"unikraft", "instance", "edit", "test-" + instName,
			"--output", "quiet",
			"--set", "delete-lock=false",
		})

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName})

		out = r.Run(t, []string{"unikraft", "instance", "inspect", "test-" + instName}, integ.ExpectFail())
		assert.Regexp(t, `not found`, out)
	})

	t.Run("pull-policy", func(t *testing.T) {
		r := runner(t, true)
		imageTag := "test-" + uniq()
		warmName := uniq()
		ifNotPresentName := uniq()
		alwaysName := uniq()

		image := r.Config.Profile.Organization + "/pull-policy-e2e:" + imageTag

		dir := t.TempDir()

		// Build and push v1: short-lived instance that prints a known marker.
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("v1", 0o755),
			fstest.CreateFile("v1/Dockerfile", []byte(`
FROM busybox:latest
RUN echo pull-policy-v1 > /marker.txt
`), 0o644),
			fstest.CreateFile("v1/Kraftfile", []byte(`
spec: v0.7
name: pull-policy-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["cat", "/marker.txt"]
`), 0o644),
		).Apply(dir))
		r.Run(t, []string{"unikraft", "build", "v1", "--output", image}, integ.WithWorkDir(dir))

		// Warm the node cache: run v1, wait for it to stop, check output.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + warmName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
		})
		r.Run(t, []string{
			"unikraft", "--timeout", "30s", "instance", "wait",
			"--until", "state==stopped", "test-" + warmName,
		})
		out := r.Run(t, []string{"unikraft", "instance", "logs", "test-" + warmName})
		assert.Contains(t, out, "pull-policy-v1")
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + warmName})

		// Build and push v2 under the same tag: different marker.
		require.NoError(t, fstest.Apply(
			fstest.CreateDir("v2", 0o755),
			fstest.CreateFile("v2/Dockerfile", []byte(`
FROM busybox:latest
RUN echo pull-policy-v2 > /marker.txt
`), 0o644),
			fstest.CreateFile("v2/Kraftfile", []byte(`
spec: v0.7
name: pull-policy-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["cat", "/marker.txt"]
`), 0o644),
		).Apply(dir))
		r.Run(t, []string{"unikraft", "build", "v2", "--output", image}, integ.WithWorkDir(dir))

		// if_not_present: node already has v1 cached under this tag; must NOT pull v2.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + ifNotPresentName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--pull-policy", "if_not_present",
		})
		r.Run(t, []string{
			"unikraft", "--timeout", "30s", "instance", "wait",
			"--until", "state==stopped", "test-" + ifNotPresentName,
		})
		out = r.Run(t, []string{"unikraft", "instance", "logs", "test-" + ifNotPresentName})
		assert.Contains(t, out, "pull-policy-v1")
		assert.NotContains(t, out, "pull-policy-v2")
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + ifNotPresentName})

		// always: must pull fresh v2 regardless of cache.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--output", "quiet",
			"--set", "name=test-" + alwaysName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=" + image,
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--pull-policy", "always",
		})
		r.Run(t, []string{
			"unikraft", "--timeout", "30s", "instance", "wait",
			"--until", "state==stopped", "test-" + alwaysName,
		})
		out = r.Run(t, []string{"unikraft", "instance", "logs", "test-" + alwaysName})
		assert.Contains(t, out, "pull-policy-v2")
		assert.NotContains(t, out, "pull-policy-v1")
		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + alwaysName})

		r.Run(t, []string{"unikraft", "image", "delete", image})
	})

	t.Run("watch-timeout", func(t *testing.T) {
		r := runner(t, true)
		r.Run(t, []string{"unikraft", "--timeout=1s", "instance", "ls", "-w"}, integ.AllowFail())
	})

	t.Run("watch-no-timeout", func(t *testing.T) {
		r := runner(t, true)

		done := make(chan error, 1)
		go func() {
			_, err := r.RunRaw(t, []string{"unikraft", "instance", "ls", "-w"}, integ.AllowFail())
			done <- err
		}()

		select {
		case err := <-done:
			t.Fatalf("expected command to still be running after 10 seconds, got: %v", err)
		case <-time.After(10 * time.Second):
			// Command still running
		}
	})

	t.Run("branch", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		branchName := uniq()
		domainName := uniq()
		domainBranch := uniq()
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
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 0`)

		// Branch the running instance.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--branch", "test-" + instName,
			"--set", "name=test-branch-" + branchName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "autostart=true",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainBranch,
		})
		out = r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-branch-" + branchName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdnBranch := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-branch-" + branchName})
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 0`)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName, "test-branch-" + branchName})
		r.Run(t, []string{"unikraft", "image", "delete", image})
	})

	// branch-state verifies that --branch preserves current in-memory state and
	// that the original and branched instances are fully independent. It builds
	// a counter HTTP server, increments to 5, branches, verifies the branched
	// instance has counter=5, then mutates each independently.
	t.Run("branch-state", func(t *testing.T) {
		r := runner(t, true)
		instName := uniq()
		branchName := uniq()
		domainName := uniq()
		domainBranch := uniq()
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

		// Increment counter to 5.
		for range 5 {
			integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		}
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		// Branch the running instance.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--branch", "test-" + instName,
			"--set", "name=test-branch-" + branchName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "autostart=true",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainBranch,
		})
		out = r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-branch-" + branchName,
			"--output", "template=" + `{{ (index .service.domains 0).fqdn }}`,
		})
		fqdnBranch := strings.TrimSpace(out)
		r.Run(t, []string{"unikraft", "instance", "wait", "--until", "state==running", "--timeout", "30s", "test-branch-" + branchName})

		// Branched counter should also be at 5.
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 5`)

		// Increment branched by 10 → 15.
		integ.HTTPPost(t, "https://"+fqdnBranch+"/increment", "application/json", `{"delta":10}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 15`)

		// Original should still be at 5 (unaffected by branch).
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 5`)

		// Increment original by 1 → 6.
		integ.HTTPPost(t, "https://"+fqdn+"/increment", "application/json", `{"delta":1}`)
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdn+"/count"), `"count": 6`)

		// Branched should still be at 15 (unaffected by original).
		assert.Contains(t, integ.HTTPGet(t, "https://"+fqdnBranch+"/count"), `"count": 15`)

		r.Run(t, []string{"unikraft", "instance", "delete", "test-" + instName, "test-branch-" + branchName})
		r.Run(t, []string{"unikraft", "image", "delete", image})
	})
}

// applyCounterContext writes the build context for a minimal Python HTTP counter
// server into dir. The server maintains an in-memory counter that can be read
// via GET /count and incremented via POST /increment with a JSON body.
func applyCounterContext(dir string) error {
	return fstest.Apply(
		fstest.CreateFile("Dockerfile", []byte(`
FROM python:3.12-slim
COPY server.py /app/server.py
`), 0o644),
		fstest.CreateFile("server.py", []byte(`
import json
from http.server import HTTPServer, BaseHTTPRequestHandler

counter = 0

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/count":
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"count": counter}).encode())
        else:
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        global counter
        if self.path == "/increment":
            length = int(self.headers.get("Content-Length", 0))
            body = json.loads(self.rfile.read(length)) if length else {}
            delta = body.get("delta", 1)
            if delta == "reset":
                counter = 0
            else:
                counter += int(delta)
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps({"count": counter}).encode())
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, format, *args):
        pass  # silence request logging

HTTPServer(("", 8080), Handler).serve_forever()
`), 0o644),
		fstest.CreateFile("Kraftfile", []byte(`
spec: v0.7
name: counter-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["python3", "/app/server.py"]
`), 0o644),
	).Apply(dir)
}
