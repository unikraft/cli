// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").

package main

import (
	"testing"

	"unikraft.com/cli/internal/integration"
)

func resourceTestCases(t *testing.T, cfg *integration.Config) []testCase {
	t.Helper()
	if cfg == nil {
		t.Skip("integration config not found")
	}

	metroName := cfg.MetroName

	return []testCase{
		{
			name: "help",
			commands: []command{
				{args: []string{unikraftCmd, "resource", "--help"}},
				{args: []string{unikraftCmd, "resource", "delete", "--help"}},
			},
		},
		{
			name:   "volume-flow",
			online: true,
			commands: []command{
				{args: []string{unikraftCmd, "resource", "create", "--set", "type=volume", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}},
				{args: []string{unikraftCmd, "resource", "get", "volume:" + metroName + "/test-$UNIQ_VOLUME"}},
				{args: []string{unikraftCmd, "resource", "list"}},
				{args: []string{unikraftCmd, "resource", "edit", "volume:" + metroName + "/test-$UNIQ_VOLUME", "--set", "size=20"}},
				{args: []string{unikraftCmd, "resource", "get", "volume:" + metroName + "/test-$UNIQ_VOLUME"}},
				{args: []string{unikraftCmd, "volume", "get", "test-$UNIQ_VOLUME"}},
				{args: []string{unikraftCmd, "resource", "delete", "--all", "--force"}},
				{args: []string{unikraftCmd, "volume", "ls"}},
			},
		},
	}
}
