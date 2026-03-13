// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package muxreader

import (
	"bufio"
	"bytes"
	"io"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/golden"
)

func TestMuxReader(t *testing.T) {
	p1r, p1w := io.Pipe()
	p2r, p2w := io.Pipe()
	t.Cleanup(func() { _ = p1w.Close() })
	t.Cleanup(func() { _ = p2w.Close() })

	m := New()
	m.With("foo", p1r)
	m.With("test", p2r)
	m.Seal()

	r := bufio.NewReader(m)

	var out bytes.Buffer
	writeThenReadLine := func(w *io.PipeWriter, line string) {
		t.Helper()
		_, err := w.Write([]byte(line))
		require.NoError(t, err)

		gotLine, err := r.ReadString('\n')
		require.NoError(t, err)
		out.WriteString(gotLine)
	}

	writeThenReadLine(p1w, "one\n")
	writeThenReadLine(p1w, "two\n")
	writeThenReadLine(p2w, "three\n")
	writeThenReadLine(p2w, "four\n")
	writeThenReadLine(p1w, "five\n")

	require.NoError(t, p1w.Close())
	require.NoError(t, p2w.Close())
	result, err := io.ReadAll(r)
	require.NoError(t, err)
	out.Write(result)

	got := ansi.Strip(out.String())
	golden.Assert(t, got, t.Name())
}
