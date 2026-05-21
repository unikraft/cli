// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestAPI(t *testing.T) {
	t.Run("healthz", func(t *testing.T) {
		r := runner(t, true)

		out := r.Run(t, []string{"unikraft", "api", "--metro=" + r.Config.MetroName, "/v1/healthz"})
		// Response should be valid JSON, pretty-printed (multi-line).
		assert.True(t, json.Valid([]byte(out)), "expected JSON response, got: %s", out)
		assert.Contains(t, out, "\n", "expected pretty-printed (multi-line) JSON, got: %s", out)
	})

	t.Run("quotas", func(t *testing.T) {
		r := runner(t, true)

		out := r.Run(t, []string{"unikraft", "api", "--metro=" + r.Config.MetroName, "/v1/users/quotas"})
		require.True(t, json.Valid([]byte(out)), "expected JSON response, got: %s", out)
		// Quotas responses include a "data" envelope.
		assert.Contains(t, out, `"data"`)
	})

	t.Run("instances list", func(t *testing.T) {
		r := runner(t, true)

		out := r.Run(t, []string{"unikraft", "api", "--metro=" + r.Config.MetroName, "/v1/instances"})
		assert.True(t, json.Valid([]byte(out)), "expected JSON response, got: %s", out)
	})

	t.Run("full url with insecure", func(t *testing.T) {
		r := runner(t, true)

		endpoint := strings.TrimRight(r.Config.Metro.Endpoint, "/") + "/v1/healthz"
		out := r.Run(t, []string{"unikraft", "api", "-k", endpoint})
		assert.True(t, json.Valid([]byte(out)), "expected JSON response, got: %s", out)
	})

	t.Run("missing endpoint fails", func(t *testing.T) {
		r := runner(t, true)

		out := r.Run(t, []string{"unikraft", "api", "--metro=" + r.Config.MetroName, "/v1/this-endpoint-does-not-exist"}, integ.ExpectFail())
		assert.Regexp(t, `HTTP 4\d\d`, out)
	})
}
