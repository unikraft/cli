// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import "testing"

func authHelpTests(t *testing.T, unikraftPath string) {
	r := newTestEnv(t, unikraftPath)
	gild(t.Context(), t, r.cli,
		[]string{unikraftCmd, "login", "--help"},
		[]string{unikraftCmd, "logout", "--help"},
		[]string{unikraftCmd, "profile", "--help"},
		[]string{unikraftCmd, "profile", "get", "--help"},
		[]string{unikraftCmd, "profile", "list", "--help"},
		[]string{unikraftCmd, "profile", "use", "--help"},
		[]string{unikraftCmd, "metro", "--help"},
		[]string{unikraftCmd, "metro", "get", "--help"},
		[]string{unikraftCmd, "metro", "list", "--help"},
	)
}

func authTests(t *testing.T, r *integrationRunner) {
	t.Run("flow", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "login", "--check"}, match: []string{`authentication token found`}},
				{args: []string{unikraftCmd, "profile", "list"}, match: []string{`true`}},
				{args: []string{unikraftCmd, "metro", "list"}, match: []string{`https?://`}},
				{args: []string{unikraftCmd, "logout"}, match: []string{`logout successful`}},
				{args: []string{unikraftCmd, "profile", "list"}, err: errMaybe},
				{args: []string{unikraftCmd, "metro", "list"}, err: errMaybe},
			})
	})
}
