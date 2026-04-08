// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").

package main

import (
	"regexp"
	"testing"
)

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
			withCleaners([]cleaner{
				{
					pattern: regexp.MustCompile(`(?m)^(\s*insecure:\s*)\S+`),
					repl:    "${1}false",
				},
				{
					pattern: regexp.MustCompile(`(?m)("insecure":\s*)(true|false)`),
					repl:    "${1}false",
				},
			}).
			run(t, []command{
				{args: []string{unikraftCmd, "config", "get"}},
				{args: []string{unikraftCmd, "config", "get", "-o", "json"}},
				{args: []string{unikraftCmd, "config", "get", "-o", "yaml"}},
			})
	})
}
