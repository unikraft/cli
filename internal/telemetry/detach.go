// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package telemetry

import (
	"encoding/json"
	"os"

	"github.com/posthog/posthog-go"

	"unikraft.com/cli/internal/spawndetach"
)

// spawnDetachedAnalytics spawns a detached subprocess to send analytics.
func spawnDetachedAnalytics(event posthog.Capture) {
	executable, err := os.Executable()
	if err != nil {
		return
	}

	payload, err := json.Marshal(event.APIfy())
	if err != nil {
		return
	}

	spawndetach.SpawnDetached(executable, "_send_analytics", string(payload))
}
