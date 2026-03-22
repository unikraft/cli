// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import "testing"

func authTests(t *testing.T, r *testRunner) {
	t.Run("help", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "login", "--help"}},
			{args: []string{unikraftCmd, "logout", "--help"}},
			{args: []string{unikraftCmd, "profile", "--help"}},
			{args: []string{unikraftCmd, "profile", "get", "--help"}},
			{args: []string{unikraftCmd, "profile", "list", "--help"}},
			{args: []string{unikraftCmd, "profile", "use", "--help"}},
			{args: []string{unikraftCmd, "metro", "--help"}},
			{args: []string{unikraftCmd, "metro", "get", "--help"}},
			{args: []string{unikraftCmd, "metro", "list", "--help"}},
		})
	})
	t.Run("flow", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "login", "--check"}},
				{args: []string{unikraftCmd, "profile", "list"}},
				{args: []string{unikraftCmd, "metro", "list"}},
				{args: []string{unikraftCmd, "logout"}},
				{args: []string{unikraftCmd, "profile", "list"}, allowErr: true},
				{args: []string{unikraftCmd, "metro", "list"}, allowErr: true},
			})
	})
}
