// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").

package main

import "testing"

func configTests(t *testing.T, r *testRunner) {
	t.Run("help", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "config", "--help"}},
			{args: []string{unikraftCmd, "config", "get", "--help"}},
		})
	})
	t.Run("get", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "config", "get"}},
				{args: []string{unikraftCmd, "config", "get", "-o", "json"}},
				{args: []string{unikraftCmd, "config", "get", "-o", "yaml"}},
			})
	})
}
