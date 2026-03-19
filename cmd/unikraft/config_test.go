// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").

package main

import (
	"testing"

	"unikraft.com/cli/internal/integration"
)

func configTestCases(t *testing.T, _ *integration.Config) []testCase {
	t.Helper()

	return []testCase{
		{
			name: "help",
			commands: []command{
				{args: []string{unikraftCmd, "config", "--help"}},
				{args: []string{unikraftCmd, "config", "get", "--help"}},
			},
		},
		{
			name:   "get",
			online: true,
			commands: []command{
				{args: []string{unikraftCmd, "config", "get"}},
				{args: []string{unikraftCmd, "config", "get", "-o", "json"}},
				{args: []string{unikraftCmd, "config", "get", "-o", "yaml"}},
			},
		},
	}
}
