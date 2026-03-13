// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package prettydiff

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/stretchr/testify/require"
)

func TestDiffPrettyText_SingleLineUnchanged(t *testing.T) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain("hello world", "hello world", false)

	result := Render(diffs)
	clean := ansi.Strip(result)
	require.Equal(t, "  hello world\n", clean)
}

func TestDiffPrettyText_MultiLineUnchanged(t *testing.T) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain("line1\nline2\nline3", "line1\nline2\nline3", false)

	result := Render(diffs)
	clean := ansi.Strip(result)
	require.Equal(t, []string{
		"  line1",
		"  line2",
		"  line3",
	}, strings.Split(strings.TrimSuffix(clean, "\n"), "\n"))
	require.Equal(t, "  line1\n  line2\n  line3\n", clean)
}

func TestDiffPrettyText_SingleLineAddition(t *testing.T) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain("foo\n", "foo\nbar\n", false)

	result := Render(diffs)
	clean := ansi.Strip(result)
	require.Equal(t, []string{
		"  foo",
		"+ bar",
	}, strings.Split(strings.TrimSuffix(clean, "\n"), "\n"))
}

func TestDiffPrettyText_SingleLineDeletion(t *testing.T) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain("foo\nbar\n", "foo\n", false)

	result := Render(diffs)
	clean := ansi.Strip(result)
	require.Equal(t, []string{
		"  foo",
		"- bar",
	}, strings.Split(strings.TrimSuffix(clean, "\n"), "\n"))
}

func TestDiffPrettyText_MultiLineChanges(t *testing.T) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain("line1\nline2\nline3\n", "line1\nline2 modified\nline4\n", false)

	result := Render(diffs)
	clean := ansi.Strip(result)
	require.Equal(t, []string{
		"  line1",
		"- line2",
		"- line3",
		// additions
		"+ line2 modified",
		"+ line4",
	}, strings.Split(strings.TrimSuffix(clean, "\n"), "\n"))
}

func TestDiffPrettyText_NoTrailingNewline(t *testing.T) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain("line1\nline2", "line1\nline3", false)

	result := Render(diffs)
	clean := ansi.Strip(result)
	require.Equal(t, []string{
		"  line1",
		"- line2",
		"+ line3",
	}, strings.Split(strings.TrimSuffix(clean, "\n"), "\n"))
}

func TestDiffPrettyText_EmptyStrings(t *testing.T) {
	dmp := diffmatchpatch.New()

	// Both empty
	diffs := dmp.DiffMain("", "", false)
	result := Render(diffs)
	clean := ansi.Strip(result)
	require.Empty(t, clean)

	// Old empty, new has content
	diffs = dmp.DiffMain("", "new line\n", false)
	result = Render(diffs)
	clean = ansi.Strip(result)
	require.Equal(t, "+ new line\n", clean)

	// Old has content, new empty
	diffs = dmp.DiffMain("old line\n", "", false)
	result = Render(diffs)
	clean = ansi.Strip(result)
	require.Equal(t, "- old line\n", clean)
}

func TestDiffPrettyText_InsertBlockBeforeSimilarPrefix(t *testing.T) {
	dmp := diffmatchpatch.New()
	before := strings.Join([]string{
		"image:        nginx",
		"resources:",
		"  memory:     128",
		"  vcpus:      1",
		"",
	}, "\n")
	after := strings.Join([]string{
		"image:        nginx",
		"runtime:",
		"  env:        {A:1 B:2}",
		"resources:",
		"  memory:     128",
		"  vcpus:      1",
		"",
	}, "\n")

	diffs := dmp.DiffMain(before, after, false)
	result := Render(diffs)
	clean := ansi.Strip(result)
	require.Equal(t, []string{
		"  image:        nginx",
		"+ runtime:",
		"+   env:        {A:1 B:2}",
		"  resources:",
		"    memory:     128",
		"    vcpus:      1",
	}, strings.Split(strings.TrimSuffix(clean, "\n"), "\n"))
}

func TestDiffPrettyText_EmptyLinesHandling(t *testing.T) {
	dmp := diffmatchpatch.New()
	before := strings.Join([]string{
		"line1",
		"",
		"line3",
	}, "\n")
	after := strings.Join([]string{
		"line1",
		"line2",
		"",
		"line4",
	}, "\n")

	diffs := dmp.DiffMain(before, after, false)
	result := Render(diffs)
	clean := ansi.Strip(result)
	require.Equal(t, []string{
		"  line1",
		"+ line2",
		"  ",
		"- line3",
		"+ line4",
	}, strings.Split(strings.TrimSuffix(clean, "\n"), "\n"))
}
