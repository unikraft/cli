// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"regexp"
	"testing"

	"unikraft.com/cli/internal/integration"
)

func instancesTestCases(t *testing.T, cfg *integration.Config) []testCase {
	t.Helper()
	if cfg == nil {
		t.Skip("integration config not found")
	}

	metroName := cfg.MetroName

	return []testCase{
		{
			name: "help",
			commands: []command{
				{args: []string{unikraftCmd, "instance", "--help"}},
				{args: []string{unikraftCmd, "instance", "get", "--help"}},
				{args: []string{unikraftCmd, "instance", "list", "--help"}},
				{args: []string{unikraftCmd, "instance", "wait", "--help"}},
				{args: []string{unikraftCmd, "instance", "create", "--help"}},
				{args: []string{unikraftCmd, "instance", "edit", "--help"}},
				{args: []string{unikraftCmd, "instance", "delete", "--help"}},
				{args: []string{unikraftCmd, "instance", "logs", "--help"}},
				{args: []string{unikraftCmd, "instance", "start", "--help"}},
				{args: []string{unikraftCmd, "instance", "stop", "--help"}},
				{args: []string{unikraftCmd, "instance", "restart", "--help"}},
			},
		},
		{
			name:   "create",
			online: true,
			commands: []command{
				{args: []string{unikraftCmd, "instance", "list"}},

				// Create an nginx instance
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "runtime.env=A=1,B=2,C=3",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}},

				{args: []string{unikraftCmd, "instance", "list"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}},

				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "list"}},
			},
			cleaners: instanceCleaners,
		},
		{
			name:   "create-oom",
			online: true,
			commands: []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=true",
					"--set", "resources.memory=16Mib",
					"--set", "resources.vcpus=1",
				}},
				{args: []string{unikraftCmd, "instance", "wait", "--until", "state==stopped", "--timeout", "10s", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			},
			cleaners: instanceCleaners,
		},
		{
			name:   "connect",
			online: true,
			commands: []command{
				// Create an nginx instance with a service
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "runtime.env=A=1,B=2,C=3",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
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
				{args: []string{unikraftCmd, "instance", "wait", "--until", "state==running", "--timeout", "10s", "test-$UNIQ_INST"}},
				{args: []string{
					"curl",
					"-k",
					"--fail",
					"--silent",
					"--show-error",
					"--output", "/dev/null",
					"--write-out", `HTTP %{http_code} OK\n%header{server}\n`,
					"--retry", "10",
					"--retry-delay", "2",
					"--retry-all-errors",
					"--connect-timeout", "5",
					"--max-time", "10",
					"https://$FQDN",
				}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			},
			cleaners: instanceCleaners,
		},
		{
			name:   "start-stop",
			online: true,
			commands: []command{
				// Create an nginx instance
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "runtime.env=A=1,B=2,C=3",
					"--set", "autostart=true",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}},

				{args: []string{unikraftCmd, "instance", "stop", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}},

				{args: []string{unikraftCmd, "instance", "start", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}},

				{args: []string{unikraftCmd, "instance", "edit", "test-$UNIQ_INST", "--set", "state=stopped"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}},

				{args: []string{unikraftCmd, "instance", "edit", "test-$UNIQ_INST", "--set", "state=running"}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}},

				// {args: []string{unikraftCmd, "instance", "restart", "test-$UNIQ_INST"}},
				// {args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}},

				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			},
			cleaners: instanceCleaners,
		},
		{
			name:   "edit",
			online: true,
			commands: []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "runtime.args=before,first",
					"--set", "runtime.env=A=1,B=2",
					"--set", "autostart=false",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}},
				{args: []string{
					unikraftCmd, "instance", "edit", "test-$UNIQ_INST",
					"--output", "quiet",
					"--set", "image=redis:latest",
					"--set", "runtime.args=after,second",
					"--set", "runtime.env=A=3,B=4",
					"--set", "resources.memory=256",
					// "--set", "resources.vcpus=2",
				}},
				{args: []string{unikraftCmd, "instance", "inspect", "test-$UNIQ_INST"}},
				{args: []string{unikraftCmd, "instance", "delete", "test-$UNIQ_INST"}},
			},
			cleaners: instanceCleaners,
		},
	}
}

var instanceCleaners = []cleaner{
	{
		// auto-generated service names like "falling-sky-7cay704w"
		pattern: regexp.MustCompile(`\b[a-z]+-[a-z]+-[a-z0-9]{8}\b`),
		repl:    "<SERVICE_NAME>",
	},
	{
		// auto-generated domain names like "foo.ukp-stable.apw.unikraft.internal"
		pattern: regexp.MustCompile(`\b\.[a-z0-9.\-]+\.unikraft\.(app|internal)\b`),
		repl:    ".unikraft.internal",
	},
	{
		// states can be running/starting
		pattern: regexp.MustCompile(`\bstate:(\s+)(running|starting)`),
		repl:    "state:${1}running",
	},
	{
		// states can be stopping/stoped
		pattern: regexp.MustCompile(`\bstate:(\s+)(stopping|stopped)`),
		repl:    "state:${1}stopped",
	},
}
