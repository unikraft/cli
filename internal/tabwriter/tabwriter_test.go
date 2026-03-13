// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tabwriter

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestTabWriterBasic(t *testing.T) {
	var buf bytes.Buffer
	w := TabWriter(&buf)

	input := `
Name	Age	City
Alice	30	New York
Bob	25	London
`
	_, err := w.Write([]byte(input))
	require.NoError(t, err)
	err = w.Flush()
	require.NoError(t, err)

	expected := `
Name   Age  City
Alice  30   New York
Bob    25   London
`
	require.Equal(t, expected, buf.String())
}

func TestTabWriterAnsiWidths(t *testing.T) {
	var buf bytes.Buffer
	w := TabWriter(&buf)

	input := fmt.Sprintf(`
%s	Value
Longer	123
`, "\x1b[1mName\x1b[0m")
	_, err := w.Write([]byte(input))
	require.NoError(t, err)
	err = w.Flush()
	require.NoError(t, err)

	expected := `
Name    Value
Longer  123
`
	clean := ansi.Strip(buf.String())
	require.Equal(t, expected, clean)
}

func TestTabWriterPassthroughLines(t *testing.T) {
	var buf bytes.Buffer
	w := TabWriter(&buf)

	input := `
Heading line
Name	Age
Alice	30
`
	_, err := w.Write([]byte(input))
	require.NoError(t, err)
	err = w.Flush()
	require.NoError(t, err)

	expected := `
Heading line
Name   Age
Alice  30
`
	require.Equal(t, expected, buf.String())
}

func TestTabWriterSplitTables(t *testing.T) {
	var buf bytes.Buffer
	w := TabWriter(&buf)

	input := `
NAME	VALUE
LongerName	1
--- divider ---
ID	VAL
x	2

A	B
C	D
`
	_, err := w.Write([]byte(input))
	require.NoError(t, err)
	err = w.Flush()
	require.NoError(t, err)

	expected := `
NAME        VALUE
LongerName  1
--- divider ---
ID  VAL
x   2

A  B
C  D
`
	require.Equal(t, expected, buf.String())
}

func TestTabWriterPlainLineBreaksTable(t *testing.T) {
	var buf bytes.Buffer
	w := TabWriter(&buf)

	input := `
Name	Age
Alice	30
No tabs here
Bob	5
`
	_, err := w.Write([]byte(input))
	require.NoError(t, err)
	err = w.Flush()
	require.NoError(t, err)

	expected := `
Name   Age
Alice  30
No tabs here
Bob  5
`
	require.Equal(t, expected, buf.String())
}

func TestTabWriterOptions(t *testing.T) {
	var buf bytes.Buffer
	w := TabWriter(&buf, WithMinColumnWidth(6), WithPadding(1))

	input := `
a	b
`
	_, err := w.Write([]byte(input))
	require.NoError(t, err)
	err = w.Flush()
	require.NoError(t, err)

	expected := `
a     b
`
	require.Equal(t, expected, buf.String())
}

func TestTabWriterMaxWidthPadding(t *testing.T) {
	var buf bytes.Buffer
	w := TabWriter(&buf, WithMaxWidth(10))

	input := `
ColA	ColB	Tail
AAAA	BBBB	Tail
`
	_, err := w.Write([]byte(input))
	require.NoError(t, err)
	err = w.Flush()
	require.NoError(t, err)

	expected := `
C… Co… Ta…
A… BB… Ta…
`
	require.Equal(t, expected, buf.String())
	requireMaxLineWidth(t, buf.String(), 10)
}

func TestTabWriterMaxWidthTrimContent(t *testing.T) {
	var buf bytes.Buffer
	w := TabWriter(&buf, WithMaxWidth(10))

	input := `
Oranges	Bananas	Tail
Oranges	Bananas	Tail
`
	_, err := w.Write([]byte(input))
	require.NoError(t, err)
	err = w.Flush()
	require.NoError(t, err)

	expected := `
O… Ba… Ta…
O… Ba… Ta…
`
	require.Equal(t, expected, buf.String())
	requireMaxLineWidth(t, buf.String(), 10)
}

func TestTabWriterMaxWidthComplexTable(t *testing.T) {
	var buf bytes.Buffer
	w := TabWriter(&buf, WithMaxWidth(40))

	input := `
NAME	STATUS	IMAGE	ARCH	COMMAND
demo-app-123	STARTING	image-latest	arm64	start --port=8080 -v
api	RUNNING	img	arm64	run --port=80
`
	_, err := w.Write([]byte(input))
	require.NoError(t, err)
	err = w.Flush()
	require.NoError(t, err)

	expected := `
NAME    STATUS   IMAGE    ARCH  COMMAND
demo-a… STARTING image-l… arm64 start -…
api     RUNNING  img      arm64 run --p…
`
	require.Equal(t, expected, buf.String())
	requireMaxLineWidth(t, buf.String(), 40)
}

func TestTabWriterMaxWidthPreservesColor(t *testing.T) {
	var buf bytes.Buffer
	w := TabWriter(&buf, WithMaxWidth(12))

	input := fmt.Sprintf(`
NAME	STATUS
demo	%sverylongstatus%s
`, "\x1b[31m", "\x1b[0m")
	_, err := w.Write([]byte(input))
	require.NoError(t, err)
	err = w.Flush()
	require.NoError(t, err)

	expected := fmt.Sprintf(`
NAME STATUS
demo %sverylo…%s
`, "\x1b[31m", "\x1b[0m")
	require.Equal(t, expected, buf.String())
	require.Contains(t, buf.String(), "\x1b[31m")
	require.Contains(t, buf.String(), "\x1b[0m")
	requireMaxLineWidth(t, buf.String(), 12)
}

func requireMaxLineWidth(t *testing.T, output string, maxWidth int) {
	t.Helper()
	for line := range strings.SplitSeq(strings.TrimSuffix(output, "\n"), "\n") {
		width := ansi.StringWidth(line)
		require.LessOrEqual(t, width, maxWidth)
	}
}
