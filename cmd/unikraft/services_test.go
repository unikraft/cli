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
)

func servicesTests(t *testing.T, r *integrationRunner) {
	metroName := ""
	if r.cfg != nil {
		metroName = r.cfg.MetroName
	}

	t.Run("create", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "service", "list"}, match: []string{`METRO\s+NAME`}},
				{args: []string{
					unikraftCmd, "service", "create",
					"--set", "name=test-$UNIQ_SVC_A",
					"--set", "metro=" + metroName,
					"--set", "domains=fqdn=$UNIQ_DOMAIN_A.unikraft.example",
					"--set", "services=443:8080/tls+http",
					"--set", "services=80:443/http+redirect",
				}, match: []string{`name:\s+test-`, `443:8080/tls\+http`}},
				{args: []string{
					unikraftCmd, "service", "create",
					"--set", "name=test-$UNIQ_SVC_B",
					"--set", "metro=" + metroName,
					"--set", "domains=fqdn=$UNIQ_DOMAIN_B.unikraft.example",
					"--set", "services=443:8080/tls+http",
					"--set", "services=80:443/http+redirect",
				}, match: []string{`name:\s+test-`, `443:8080/tls\+http`}},
				{args: []string{unikraftCmd, "service", "list"}, match: []string{`test-`}},
				{args: []string{unikraftCmd, "service", "inspect", "test-$UNIQ_SVC_A", "test-$UNIQ_SVC_B"}, match: []string{`443:8080/tls\+http`}},
				{args: []string{unikraftCmd, "service", "delete", "test-$UNIQ_SVC_A", "test-$UNIQ_SVC_B"}, match: []string{`test-`}},
			})
	})

	t.Run("edit", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{
					unikraftCmd, "service", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_SVC",
					"--set", "metro=" + metroName,
					"--set", "domains=fqdn=$UNIQ_DOMAIN.unikraft.example",
					"--set", "services=443:8080/tls+http",
				}},
				{args: []string{
					unikraftCmd, "service", "edit", "test-$UNIQ_SVC",
					"--output", "quiet",
					"--set", "limits.soft=2",
					"--set", "limits.hard=10",
					"--set", "domains=fqdn=$UNIQ_DOMAIN_EDIT.unikraft.example",
					"--set", "services=1000:2000/tls",
				}},
				{args: []string{unikraftCmd, "service", "inspect", "test-$UNIQ_SVC"}, match: []string{`soft:\s+2`, `hard:\s+10`, `1000:2000/tls`}},
				{args: []string{unikraftCmd, "service", "delete", "test-$UNIQ_SVC"}},
			})
	})
}

func servicesHelpTests(t *testing.T, unikraftPath string) {
	r := newTestEnv(t, unikraftPath)
	gild(t.Context(), t, r.cli,
		[]string{unikraftCmd, "service", "--help"},
		[]string{unikraftCmd, "service", "get", "--help"},
		[]string{unikraftCmd, "service", "list", "--help"},
		[]string{unikraftCmd, "service", "wait", "--help"},
		[]string{unikraftCmd, "service", "create", "--help"},
		[]string{unikraftCmd, "service", "edit", "--help"},
		[]string{unikraftCmd, "service", "delete", "--help"},
	)
}

func servicesOutputTests(t *testing.T) {
	sample := cmd.ServiceGroup{
		MetroName:  "fra",
		Name:       "my-service",
		UUID:       "b2c3d4e5-f6a7-8901-bcde-f12345678901",
		Persistent: true,
		Autoscale:  true,
	}
	sample.Limits.Soft = 5
	sample.Limits.Hard = 50
	sample.Services = []*cmd.Service{
		{
			Source:      443,
			Destination: 8080,
			Handlers:    []platform.ConnectionHandler{"tls", "http"},
		},
		{
			Source:      80,
			Destination: 443,
			Handlers:    []platform.ConnectionHandler{"http", "redirect"},
		},
	}
	sample.Domains = []cmd.Domain{
		{FQDN: "example.unikraft.app"},
	}

	gild[resource.Resource](t.Context(), t, dumpResource, sample)
}
