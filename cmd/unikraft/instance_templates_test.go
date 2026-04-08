// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"testing"
)

func instanceTemplatesTests(t *testing.T, r *testRunner) {
	metroName := ""
	if r.cfg != nil {
		metroName = r.cfg.MetroName
	}

	t.Run("template", func(t *testing.T) {
		r.
			online().
			withCleaners(instanceCleaners).
			run(t, []command{
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=false",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}},
				{
					args: []string{
						unikraftCmd, "instance", "template", "create", "test-$UNIQ_INST",
						"--output", "template={{ .name }}",
					},
					captureEnv: "TEMPLATE_NAME",
				},
				{args: []string{unikraftCmd, "instance", "template", "list"}},
				{args: []string{unikraftCmd, "instance", "template", "inspect", "$TEMPLATE_NAME"}},
				{args: []string{unikraftCmd, "instance", "template", "edit", "$TEMPLATE_NAME", "--set", "tags=env-dev"}},
				{args: []string{unikraftCmd, "instance", "template", "inspect", "$TEMPLATE_NAME"}},
				{args: []string{unikraftCmd, "instance", "template", "delete", "$TEMPLATE_NAME"}},
			})
	})

	t.Run("create-from-template", func(t *testing.T) {
		r.
			online().
			withCleaners(instanceCleaners).
			run(t, []command{
				// Create a base instance
				{args: []string{
					unikraftCmd, "instance", "create",
					"--output", "quiet",
					"--set", "name=test-base-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "image=nginx:latest",
					"--set", "autostart=false",
					"--set", "resources.memory=128",
					"--set", "resources.vcpus=1",
				}},
				// Create template from instance
				{
					args: []string{
						unikraftCmd, "instance", "template", "create", "test-base-$UNIQ_INST",
						"--output", "template={{ .name }}",
					},
					captureEnv: "TEMPLATE_NAME",
				},
				// Create new instance from template
				{args: []string{
					unikraftCmd, "instance", "create",
					"--set", "name=test-from-template-$UNIQ_INST",
					"--set", "metro=" + metroName,
					"--set", "template=$TEMPLATE_NAME",
				}},
				// Verify the new instance was created
				{args: []string{unikraftCmd, "instance", "inspect", "test-from-template-$UNIQ_INST"}},
				// Clean up template
				{args: []string{unikraftCmd, "instance", "template", "delete", "$TEMPLATE_NAME"}},
			})
	})
}
