// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type textMarshalerType int

func (t textMarshalerType) MarshalText() ([]byte, error) {
	return []byte("custom-text"), nil
}

// parseFlags parses the given CLI struct with args and returns the resulting Kong flags.
func parseFlags(t *testing.T, cli any, args ...string) []*kong.Flag {
	t.Helper()
	parser, err := kong.New(cli)
	require.NoError(t, err)
	kctx, err := parser.Parse(args)
	require.NoError(t, err)
	return kctx.Flags()
}

func TestApplyShortcutFlags(t *testing.T) {
	t.Run("string fields", func(t *testing.T) {
		var cli struct {
			Image string `shortcut:"image"`
			Metro string `shortcut:"metro"`
		}
		flags := parseFlags(t, &cli, "--image=nginx:latest", "--metro=fra")

		var args SetArgs
		err := ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		assert.Equal(t, []map[string]string{
			{"image": "nginx:latest"},
			{"metro": "fra"},
		}, args.Set)
	})

	t.Run("zero values are skipped", func(t *testing.T) {
		var cli struct {
			Image string `shortcut:"image"`
			Metro string `shortcut:"metro"`
			Name  string `shortcut:"name"`
		}
		flags := parseFlags(t, &cli, "--image=nginx:latest")

		var args SetArgs
		err := ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		assert.Equal(t, []map[string]string{
			{"image": "nginx:latest"},
		}, args.Set)
	})

	t.Run("bool pointer fields", func(t *testing.T) {
		t.Run("nil pointer is skipped", func(t *testing.T) {
			var cli struct {
				Autostart *bool `shortcut:"autostart"`
			}
			flags := parseFlags(t, &cli)

			var args SetArgs
			err := ApplyShortcutFlags(&args, flags)
			require.NoError(t, err)
			assert.Nil(t, args.Set)
		})

		t.Run("true", func(t *testing.T) {
			var cli struct {
				Autostart *bool `shortcut:"autostart"`
			}
			flags := parseFlags(t, &cli, "--autostart")

			var args SetArgs
			err := ApplyShortcutFlags(&args, flags)
			require.NoError(t, err)
			assert.Equal(t, []map[string]string{{"autostart": "true"}}, args.Set)
		})

		t.Run("false", func(t *testing.T) {
			var cli struct {
				Autostart *bool `shortcut:"autostart" negatable:""`
			}
			// *bool(false) is the zero value of the dereferenced bool, but
			// the pointer itself is non-nil, so it should still be applied.
			flags := parseFlags(t, &cli, "--no-autostart")

			var args SetArgs
			err := ApplyShortcutFlags(&args, flags)
			require.NoError(t, err)
			assert.Equal(t, []map[string]string{{"autostart": "false"}}, args.Set)
		})
	})

	t.Run("int fields", func(t *testing.T) {
		var cli struct {
			Vcpus int   `shortcut:"resources.vcpus"`
			Count int64 `shortcut:"replicas"`
		}
		flags := parseFlags(t, &cli, "--vcpus=4", "--count=3")

		var args SetArgs
		err := ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		assert.Equal(t, []map[string]string{
			{"resources.vcpus": "4"},
			{"replicas": "3"},
		}, args.Set)
	})

	t.Run("slice fields produce one entry per element", func(t *testing.T) {
		var cli struct {
			Features []string `shortcut:"features"`
		}
		flags := parseFlags(t, &cli, "--features=feat-a", "--features=feat-b")

		var args SetArgs
		err := ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		assert.Equal(t, []map[string]string{
			{"features": "feat-a"},
			{"features": "feat-b"},
		}, args.Set)
	})

	t.Run("empty slice is skipped", func(t *testing.T) {
		var cli struct {
			Features []string `shortcut:"features"`
		}
		flags := parseFlags(t, &cli)

		var args SetArgs
		err := ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		assert.Nil(t, args.Set)
	})

	t.Run("TextMarshaler types", func(t *testing.T) {
		var cli struct {
			Memory textMarshalerType `shortcut:"resources.memory"`
		}
		flags := parseFlags(t, &cli, "--memory=128")

		var args SetArgs
		err := ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		assert.Equal(t, []map[string]string{
			{"resources.memory": "custom-text"},
		}, args.Set)
	})

	t.Run("fields without shortcut tag are ignored", func(t *testing.T) {
		var cli struct {
			Image   string `shortcut:"image"`
			Ignored string `help:"no shortcut tag"`
			Also    string
		}
		flags := parseFlags(t, &cli, "--image=nginx", "--ignored=x", "--also=y")

		var args SetArgs
		err := ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		assert.Equal(t, []map[string]string{{"image": "nginx"}}, args.Set)
	})

	t.Run("shortcut dash tag is ignored", func(t *testing.T) {
		var cli struct {
			Image string `shortcut:"-"`
		}
		flags := parseFlags(t, &cli, "--image=nginx")

		var args SetArgs
		err := ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		assert.Nil(t, args.Set)
	})

	t.Run("appends to existing Set entries", func(t *testing.T) {
		var cli struct {
			Metro string `shortcut:"metro"`
		}
		flags := parseFlags(t, &cli, "--metro=fra")

		args := SetArgs{
			Set: []map[string]string{{"image": "nginx:latest"}},
		}
		err := ApplyShortcutFlags(&args, flags)
		require.NoError(t, err)
		assert.Equal(t, []map[string]string{
			{"image": "nginx:latest"},
			{"metro": "fra"},
		}, args.Set)
	})

	t.Run("nil flags slice", func(t *testing.T) {
		var args SetArgs
		err := ApplyShortcutFlags(&args, nil)
		require.NoError(t, err)
		assert.Nil(t, args.Set)
	})

	t.Run("empty flags slice", func(t *testing.T) {
		var args SetArgs
		err := ApplyShortcutFlags(&args, []*kong.Flag{})
		require.NoError(t, err)
		assert.Nil(t, args.Set)
	})
}
