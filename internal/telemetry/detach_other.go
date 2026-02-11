// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !unix

package telemetry

import "github.com/posthog/posthog-go"

// spawnDetachedAnalytics is a no-op on non-Unix platforms.
// Windows support for detached processes would require different syscall flags
// (CREATE_NEW_PROCESS_GROUP, DETACHED_PROCESS), but telemetry is best-effort
// so we simply skip it on unsupported platforms.
func spawnDetachedAnalytics(posthog.Capture) {
	// No-op: detached subprocess spawning not implemented for this platform
}
