// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import "testing"

func helpTests(t *testing.T, r *testRunner) {
	t.Run("empty", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd}, allowErr: true},
		})
	})
	t.Run("version", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "version"}},
		})
	})
	t.Run("help", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "--help"}},
		})
	})
	t.Run("invalid/arg", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "invalid"}, allowErr: true},
		})
	})
	t.Run("invalid/help", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "--help", "--bad-flag"}, allowErr: true},
			{args: []string{unikraftCmd, "--help", "bad-arg"}, allowErr: true},
		})
	})
	t.Run("invalid/logs", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "--log-type=json", "invalid"}, allowErr: true},
			{args: []string{unikraftCmd, "--log-level=fatal", "invalid"}, allowErr: true},
		})
	})
}
