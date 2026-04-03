// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import "testing"

func volumesTests(t *testing.T, r *testRunner) {
	t.Run("help", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "volume", "--help"}},
			{args: []string{unikraftCmd, "volume", "get", "--help"}},
			{args: []string{unikraftCmd, "volume", "list", "--help"}},
			{args: []string{unikraftCmd, "volume", "wait", "--help"}},
			{args: []string{unikraftCmd, "volume", "create", "--help"}},
			{args: []string{unikraftCmd, "volume", "clone", "--help"}},
			{args: []string{unikraftCmd, "volume", "import", "--help"}},
			{args: []string{unikraftCmd, "volume", "edit", "--help"}},
			{args: []string{unikraftCmd, "volume", "delete", "--help"}},
		})
	})

	metroName := ""
	if r.cfg != nil {
		metroName = r.cfg.MetroName
	}

	t.Run("create", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "volume", "list"}},
				{args: []string{unikraftCmd, "volume", "create", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}},
				{args: []string{unikraftCmd, "volume", "list"}},
				{args: []string{unikraftCmd, "volume", "inspect", "test-$UNIQ_VOLUME"}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOLUME"}},
			})
	})

	t.Run("edit", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "volume", "create", "--output", "quiet", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}},
				{args: []string{unikraftCmd, "volume", "edit", "test-$UNIQ_VOLUME", "--output", "quiet", "--set", "size=20"}},
				{args: []string{unikraftCmd, "volume", "inspect", "test-$UNIQ_VOLUME"}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOLUME"}},
			})
	})

	t.Run("clone", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "volume", "create", "--output", "quiet", "--set", "name=test-$UNIQ_VOLUME", "--set", "size=10", "--set", "metro=" + metroName}},
				{args: []string{unikraftCmd, "volume", "clone", "test-$UNIQ_VOLUME", "--output", "quiet", "--set", "name=test-$UNIQ_VOLUME_CLONE"}},
				{args: []string{unikraftCmd, "volume", "inspect", "test-$UNIQ_VOLUME", "test-$UNIQ_VOLUME_CLONE"}},
				{args: []string{unikraftCmd, "volume", "delete", "test-$UNIQ_VOLUME", "test-$UNIQ_VOLUME_CLONE"}},
			})
	})
}
