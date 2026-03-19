// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"testing"

	"unikraft.com/cli/internal/integration"
)

func volumesTestCases(t *testing.T, cfg *integration.Config) []testCase {
	t.Helper()
	if cfg == nil {
		t.Skip("integration config not found")
	}

	metroName := cfg.MetroName

	return []testCase{
		{
			name: "help",
			commands: []command{
				{args: []string{unikraftCmd, "volume", "--help"}},
				{args: []string{unikraftCmd, "volume", "get", "--help"}},
				{args: []string{unikraftCmd, "volume", "list", "--help"}},
				{args: []string{unikraftCmd, "volume", "wait", "--help"}},
				{args: []string{unikraftCmd, "volume", "create", "--help"}},
				{args: []string{unikraftCmd, "volume", "clone", "--help"}},
				{args: []string{unikraftCmd, "volume", "edit", "--help"}},
				{args: []string{unikraftCmd, "volume", "delete", "--help"}},
			},
		},
		{
			name:   "create",
			online: true,
			commands: []command{
				{args: []string{unikraftCmd, "volume", "list"}},
				{args: []string{unikraftCmd, "volume", "create", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}},
				{args: []string{unikraftCmd, "volume", "list"}},
				{args: []string{unikraftCmd, "volume", "inspect", "test-$UNIQ_VOLUME"}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOLUME"}},
			},
		},
		{
			name:   "edit",
			online: true,
			commands: []command{
				{args: []string{unikraftCmd, "volume", "create", "--output", "quiet", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}},
				{args: []string{unikraftCmd, "volume", "edit", "test-$UNIQ_VOLUME", "--output", "quiet", "--set", "size=20"}},
				{args: []string{unikraftCmd, "volume", "inspect", "test-$UNIQ_VOLUME"}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOLUME"}},
			},
		},
		{
			name:   "clone",
			online: true,
			commands: []command{
				{args: []string{unikraftCmd, "volume", "create", "--output", "quiet", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}},
				{args: []string{unikraftCmd, "volume", "clone", "test-$UNIQ_VOLUME", "--output", "quiet", "--set", "name=test-$UNIQ_VOLUME_CLONE"}},
				{args: []string{unikraftCmd, "volume", "inspect", "test-$UNIQ_VOLUME", "test-$UNIQ_VOLUME_CLONE"}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOLUME", "test-$UNIQ_VOLUME_CLONE"}},
			},
		},
	}
}
