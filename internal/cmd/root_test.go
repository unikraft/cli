// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd_test

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/config"
)

func TestTimeoutCancelFlag(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("UNIKRAFT_CONFIG", configPath)

	cfg := &config.Config{
		Path:           configPath,
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {
				Name:  "default",
				Type:  config.ProfileTypeCloud,
				Token: "test-token",
			},
		},
	}
	require.NoError(t, cfg.Save())

	stdio := config.Stdio{
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	ctx, _, _, cleanup, err := cmd.NewRootCmd(context.Background(), []string{"--timeout=1s", "version"}, stdio)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cleanup() })

	// Wait on a condition that is never met; the 1s deadline cancels it.
	select {
	case <-ctx.Done():
	}

	require.EqualError(t, ctx.Err(), "timed out")
}
