// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package io

import (
	"io"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"unikraft.com/x/guesstermwidth"
)

// Unwrap peels off known io.Writer wrappers to expose the underlying writer.
//
// This is needed for TTY detection and terminal-width queries: those checks
// rely on type-asserting the writer to an `interface{ Fd() uintptr }`, but
// some wrappers (notably *colorprofile.Writer) hide the underlying file
// descriptor. Calling Unwrap before such a check ensures it reaches the real
// *os.File when one exists.
//
// Use only for inspection (Fd, IsTerminal, width queries). For actual writes,
// keep using the original writer so wrapper behavior (e.g. color downgrading)
// is preserved.
func Unwrap(w io.Writer) io.Writer {
	for {
		switch ww := w.(type) {
		case *colorprofile.Writer:
			w = ww.Forward
		default:
			return w
		}
	}
}

// IsTTY reports whether the writer ultimately targets a terminal, transparently
// peeling off known wrappers via Unwrap. Prefer this over calling
// `term.IsTerminal` or `guesstermwidth.IsTTY` directly on writers obtained from
// `config.Stdio`, since those may be wrapped (e.g. by `colorprofile.Writer`)
// and would otherwise always report false.
func IsTTY(w io.Writer) bool {
	inner := Unwrap(w)
	fdWriter, ok := inner.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	return term.IsTerminal(fdWriter.Fd())
}

// TermWidth returns the terminal width for the writer, transparently peeling
// off known wrappers via Unwrap. Falls back to `guesstermwidth.GuessTermWidth`
// (which honors COLUMNS and defaults to 80) when the writer isn't a TTY.
func TermWidth(w io.Writer) int {
	return guesstermwidth.GuessTermWidth(Unwrap(w))
}
