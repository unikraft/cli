// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVolumeTemplates(t *testing.T) {
	t.Run("template", func(t *testing.T) {
		r := runner(t, true)
		volName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "size=10",
		})

		out := r.Run(t, []string{
			"unikraft", "volume", "template", "create", "test-" + volName,
			"--output", "template={{ .name }}",
		})
		templateName := strings.TrimSpace(out)

		out = r.Run(t, []string{"unikraft", "volume", "template", "list"})
		assert.Regexp(t, `NAME`, out)

		out = r.Run(t, []string{"unikraft", "volume", "template", "inspect", templateName})
		assert.Regexp(t, `state:\s+template`, out)
		assert.Regexp(t, `size:\s+10`, out)

		r.Run(t, []string{"unikraft", "volume", "template", "edit", templateName, "--set", "tags=env-dev"})

		out = r.Run(t, []string{"unikraft", "volume", "template", "inspect", templateName})
		assert.Regexp(t, `state:\s+template`, out)

		r.Run(t, []string{"unikraft", "volume", "template", "delete", templateName})
	})

	t.Run("clone", func(t *testing.T) {
		r := runner(t, true)
		volName := uniq()
		cloneName := uniq()

		r.Run(t, []string{
			"unikraft", "volume", "create",
			"--output", "quiet",
			"--set", "name=test-" + volName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "size=10",
		})

		out := r.Run(t, []string{
			"unikraft", "volume", "template", "create", "test-" + volName,
			"--output", "template={{ .name }}",
		})
		templateName := strings.TrimSpace(out)

		r.Run(t, []string{
			"unikraft", "volume", "template", "clone", templateName,
			"--output", "quiet",
			"--name", "test-" + cloneName,
		})

		out = r.Run(t, []string{"unikraft", "volume", "inspect", "test-" + cloneName})
		assert.Regexp(t, `state:\s+available`, out)
		assert.Regexp(t, `size:\s+10`, out)

		r.Run(t, []string{"unikraft", "volume", "delete", "test-" + cloneName})
		r.Run(t, []string{"unikraft", "volume", "template", "delete", templateName})
	})
}
