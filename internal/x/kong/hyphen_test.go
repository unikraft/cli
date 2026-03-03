// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kong

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

func TestHyphenStringDecode(t *testing.T) {
	var cli struct {
		Field HyphenString `short:"f"`
	}

	parser, err := kong.New(&cli)
	require.NoError(t, err)

	_, err = parser.Parse([]string{"-f", "-name"})
	require.NoError(t, err)
	require.Equal(t, HyphenString("-name"), cli.Field)
}

func TestHyphenStringsDecode(t *testing.T) {
	var cli struct {
		Field HyphenStrings `short:"f"`
		Other string        `short:"r"`
	}

	parser, err := kong.New(&cli)
	require.NoError(t, err)

	_, err = parser.Parse([]string{"-f", "-name,+id", "-r", "value"})
	require.NoError(t, err)
	require.Equal(t, HyphenStrings{"-name", "+id"}, cli.Field)
	require.Equal(t, "value", cli.Other)
}
