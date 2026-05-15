// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/resource"
)

// TestEnv holds per-test environment for running CLI commands deterministically.
type TestEnv struct {
	// Config is the integration test config, or nil if not loaded (offline tests).
	Config *Config

	unikraftPath string
	configPath   string
	sandboxPath  string
}

func NewTestEnv(t *testing.T, unikraftPath string) *TestEnv {
	t.Helper()
	dir := t.TempDir()
	return &TestEnv{
		unikraftPath: unikraftPath,
		configPath:   filepath.Join(dir, "config.yaml"),
		sandboxPath:  filepath.Join(dir, "sandbox.json"),
	}
}

func (env *TestEnv) WithConfig(cfg *Config, configPath string) *TestEnv {
	env.Config = cfg
	env.configPath = configPath
	return env
}

func (env *TestEnv) WithSandboxPath(sandboxPath string) *TestEnv {
	env.sandboxPath = sandboxPath
	return env
}

type CmdOption func(*cmdConfig)

type cmdConfig struct {
	workDir    string
	expectFail bool
	allowFail  bool
	timeout    time.Duration
}

func WithWorkDir(dir string) CmdOption {
	return func(c *cmdConfig) { c.workDir = dir }
}

func ExpectFail() CmdOption {
	return func(c *cmdConfig) { c.expectFail = true }
}

func AllowFail() CmdOption {
	return func(c *cmdConfig) { c.allowFail = true }
}

// WithTimeout kills the command after d. The caller is responsible for
// handling the resulting error (e.g. by also passing AllowFail).
// Use for commands that run indefinitely (e.g. --follow).
func WithTimeout(d time.Duration) CmdOption {
	return func(c *cmdConfig) { c.timeout = d }
}

func (env *TestEnv) RunRaw(t *testing.T, args []string, opts ...CmdOption) (string, error) {
	t.Helper()
	var cfg cmdConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	t.Logf("executing: %s", strings.Join(args, " "))

	cmdCtx := t.Context()
	if cfg.timeout > 0 {
		var cancel context.CancelFunc
		cmdCtx, cancel = context.WithTimeout(cmdCtx, cfg.timeout)
		defer cancel()
	}

	var c *exec.Cmd
	if args[0] == "unikraft" {
		c = exec.CommandContext(cmdCtx, env.unikraftPath, args[1:]...)
	} else {
		c = exec.CommandContext(cmdCtx, args[0], args[1:]...)
	}

	var output bytes.Buffer
	c.Stdout = &output
	c.Stderr = &output
	c.Dir = cfg.workDir
	c.Env = os.Environ()
	c.Env = slices.DeleteFunc(c.Env, func(s string) bool {
		return strings.HasPrefix(s, "UNIKRAFT_")
	})
	c.Env = append(c.Env, "NO_COLOR=1")
	c.Env = append(c.Env, "UNIKRAFT_CONFIG="+env.configPath)
	c.Env = append(c.Env, "BUILDKIT_PROGRESS=quiet")
	c.Env = append(c.Env, resource.UnikraftSandboxEnv+"="+env.sandboxPath)

	err := c.Run()
	return ansi.Strip(output.String()), err
}

func (env *TestEnv) Run(t *testing.T, args []string, opts ...CmdOption) string {
	t.Helper()
	var cfg cmdConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	out, err := env.RunRaw(t, args, opts...)
	switch {
	case cfg.expectFail:
		require.Error(t, err, "command %q was expected to fail but succeeded\n%s", strings.Join(args, " "), out)
	case cfg.allowFail:
		// ignore error
	default:
		require.NoError(t, err, "command %q failed\n%s", strings.Join(args, " "), out)
	}
	return out
}
