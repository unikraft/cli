// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").

package main

import "testing"

func resourceTests(t *testing.T, r *integrationRunner) {
	metroName := ""
	if r.cfg != nil {
		metroName = r.cfg.MetroName
	}

	t.Run("volume-flow", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "resource", "create", "--set", "type=volume", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}, match: []string{`state:\s+available`}},
				{args: []string{unikraftCmd, "resource", "get", "volume:" + metroName + "/test-$UNIQ_VOLUME"}, match: []string{`size:\s+10`}},
				{args: []string{unikraftCmd, "resource", "list"}, match: []string{`volume`}},
				{args: []string{unikraftCmd, "resource", "edit", "volume:" + metroName + "/test-$UNIQ_VOLUME", "--set", "size=20"}},
				{args: []string{unikraftCmd, "resource", "get", "volume:" + metroName + "/test-$UNIQ_VOLUME"}, match: []string{`size:\s+20`}},
				{args: []string{unikraftCmd, "volume", "get", "test-$UNIQ_VOLUME"}, match: []string{`size:\s+20`}},
				{args: []string{unikraftCmd, "resource", "delete", "--all", "--force"}},
				{args: []string{unikraftCmd, "volume", "ls"}},
			})
	})
}

func resourceHelpTests(t *testing.T, unikraftPath string) {
	r := newTestEnv(t, unikraftPath)
	gild(t.Context(), t, r.cli,
		[]string{unikraftCmd, "resource", "--help"},
		[]string{unikraftCmd, "resource", "delete", "--help"},
	)
}
