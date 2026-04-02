// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"fmt"
	"regexp"
	"testing"
)

func imagesTests(t *testing.T, r *testRunner) {
	t.Run("help", func(t *testing.T) {
		r.run(t, []command{
			{args: []string{unikraftCmd, "image", "--help"}},
			{args: []string{unikraftCmd, "image", "get", "--help"}},
			{args: []string{unikraftCmd, "image", "list", "--help"}},
			{args: []string{unikraftCmd, "image", "copy", "--help"}},
		})
	})
	t.Run("inspect", func(t *testing.T) {
		r.
			online().
			withCleaners([]cleaner{
				{
					// exact nginx version numbers may change between runs
					pattern: regexp.MustCompile(`nginx:[0-9]+\.[0-9]+`),
					repl:    "nginx:X.Y",
				},
			}).
			run(t, []command{
				{args: []string{unikraftCmd, "image", "inspect", "nginx:latest"}},
			})
	})

	t.Run("copy-inspect-delete", func(t *testing.T) {
		if r.cfg == nil {
			t.Skip("online test requires config, but no config found")
		}

		imageName := r.cfg.Profile.Organization + "/nginx-copy:$UNIQ_IMAGE"
		imageFull := fmt.Sprintf("%s/%s", "index.unikraft.io", imageName)

		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "image", "copy", "nginx:latest", imageFull}},
				{args: []string{unikraftCmd, "image", "inspect", imageName}},
				{args: []string{unikraftCmd, "image", "delete", imageName}},
			})
	})
}
