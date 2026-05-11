// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").

package main

import "testing"

func configTests(t *testing.T, r *integrationRunner) {
	t.Run("get", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "config", "get"}, match: []string{`profile:\s+\S+`, `token:`}},
				{args: []string{unikraftCmd, "config", "get", "-o", "json"}, match: []string{`"token":`, `"profile":`}},
				{args: []string{unikraftCmd, "config", "get", "-o", "yaml"}, match: []string{`token:`, `profile:`}},
			})
	})
}

func configHelpTests(t *testing.T, unikraftPath string) {
	r := newTestEnv(t, unikraftPath)
	gild(t.Context(), t, r.cli,
		[]string{unikraftCmd, "config", "--help"},
		[]string{unikraftCmd, "config", "get", "--help"},
	)
}
