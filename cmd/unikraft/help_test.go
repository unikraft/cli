// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import "testing"

// generalHelpTests checks that top-level help, version, and error output
// stays stable. Deterministic and offline.
func generalHelpTests(t *testing.T, unikraftPath string) {
	r := newTestEnv(t, unikraftPath)
	gild(t.Context(), t, r.cli,
		[]string{unikraftCmd},
		[]string{unikraftCmd, "version"},
		[]string{unikraftCmd, "--help"},
		[]string{unikraftCmd, "invalid"},
		[]string{unikraftCmd, "--help", "--bad-flag"},
		[]string{unikraftCmd, "--help", "bad-arg"},
		[]string{unikraftCmd, "--log-type=json", "invalid"},
		[]string{unikraftCmd, "--log-level=fatal", "invalid"},
	)
}
