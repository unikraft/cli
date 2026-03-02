// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build unix

package spawndetach

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// SpawnDetached spawns a detached subprocess to check for updates.
//
// On Unix, this uses process group detachment so the subprocess continues after
// the parent exits.
func SpawnDetached(name string, args ...string) {
	cmd := exec.CommandContext(context.Background(), name, args...)

	// Detach from parent process group so subprocess survives parent exit
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Don't hold the working directory
	cmd.Dir = "/"

	// Inherit environment (may be needed for network config)
	cmd.Env = os.Environ()

	// Start the process (non-blocking)
	if err := cmd.Start(); err != nil {
		return
	}

	// Release the process so it can run independently
	_ = cmd.Process.Release()
}
