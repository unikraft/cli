// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"testing"
)

func volumeTemplatesTests(t *testing.T, r *testRunner) {
	metroName := ""
	if r.cfg != nil {
		metroName = r.cfg.MetroName
	}

	t.Run("template", func(t *testing.T) {
		r.
			online().
			withCleaners(volumeCleaners).
			run(t, []command{
				{args: []string{
					unikraftCmd, "volume", "create",
					"--output", "quiet",
					"--set", "name=test-$UNIQ_VOL",
					"--set", "metro=" + metroName,
					"--set", "size=10",
				}},
				{
					args: []string{
						unikraftCmd, "volume", "template", "create", "test-$UNIQ_VOL",
						"--output", "template={{ .name }}",
					},
					captureEnv: "TEMPLATE_NAME",
				},
				{args: []string{unikraftCmd, "volume", "template", "list"}},
				{args: []string{unikraftCmd, "volume", "template", "inspect", "$TEMPLATE_NAME"}},
				{args: []string{unikraftCmd, "volume", "template", "edit", "$TEMPLATE_NAME", "--set", "tags=env-dev"}},
				{args: []string{unikraftCmd, "volume", "template", "inspect", "$TEMPLATE_NAME"}},
				{args: []string{unikraftCmd, "volume", "template", "delete", "$TEMPLATE_NAME"}},
			})
	})
}

var volumeCleaners []cleaner
