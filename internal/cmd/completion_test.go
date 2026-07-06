// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

func TestParseCompLine(t *testing.T) {
	tests := []struct {
		name          string
		unset         bool
		compLine      string
		compPoint     string
		wantCompleted []string
		wantBareLast  bool
		wantIsComp    bool
	}{
		{
			name:  "no COMP_LINE",
			unset: true,
		},
		{
			name:       "empty COMP_LINE",
			compLine:   "",
			wantIsComp: false,
		},
		{
			name:          "bare tab at root",
			compLine:      "unikraft ",
			wantCompleted: nil,
			wantBareLast:  true,
			wantIsComp:    true,
		},
		{
			name:          "typing a prefix",
			compLine:      "unikraft cr",
			wantCompleted: nil,
			wantBareLast:  false,
			wantIsComp:    true,
		},
		{
			name:          "bare tab after nested command",
			compLine:      "unikraft instances get ",
			wantCompleted: []string{"instances", "get"},
			wantBareLast:  true,
			wantIsComp:    true,
		},
		{
			name:          "typing a nested prefix",
			compLine:      "unikraft instances i",
			wantCompleted: []string{"instances"},
			wantBareLast:  false,
			wantIsComp:    true,
		},
		{
			name:          "COMP_POINT truncates the line",
			compLine:      "unikraft instances get extra-stuff",
			compPoint:     "23", // "unikraft instances get "
			wantCompleted: []string{"instances", "get"},
			wantBareLast:  true,
			wantIsComp:    true,
		},
		{
			name:          "COMP_POINT out of range is ignored",
			compLine:      "unikraft cr",
			compPoint:     "999",
			wantCompleted: nil,
			wantBareLast:  false,
			wantIsComp:    true,
		},
		{
			name:          "COMP_POINT not a number is ignored",
			compLine:      "unikraft cr",
			compPoint:     "not-a-number",
			wantCompleted: nil,
			wantBareLast:  false,
			wantIsComp:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.unset {
				old, had := os.LookupEnv("COMP_LINE")
				os.Unsetenv("COMP_LINE")
				if had {
					t.Cleanup(func() { os.Setenv("COMP_LINE", old) })
				}
			} else {
				t.Setenv("COMP_LINE", tt.compLine)
			}

			os.Unsetenv("COMP_POINT")
			if tt.compPoint != "" {
				t.Setenv("COMP_POINT", tt.compPoint)
			}

			completed, bareLast, isComp := parseCompLine()

			if isComp != tt.wantIsComp {
				t.Fatalf("isCompletion = %v, want %v", isComp, tt.wantIsComp)
			}
			if !slices.Equal(completed, tt.wantCompleted) {
				t.Errorf("completed = %v, want %v", completed, tt.wantCompleted)
			}
			if bareLast != tt.wantBareLast {
				t.Errorf("bareLast = %v, want %v", bareLast, tt.wantBareLast)
			}
		})
	}
}

// testCLI mirrors the structure of the real CLI closely enough to exercise
// registerCompletion's alias-hiding logic: a top-level command with several
// aliases (some of which redundantly re-list its own canonical name), and a
// nested subcommand that also has aliases.
type testCLI struct {
	Version      testLeafCmd `cmd:"" aliases:"version,ver,v" help:"Show version."`
	Certificates testCertCmd `cmd:"" aliases:"certificate,certificates,crt,crts,cert,certs" help:"Manage certificates."`
}

type testCertCmd struct {
	Get testLeafCmd `cmd:"" aliases:"inspect,show" help:"Get a certificate."`
}

type testLeafCmd struct{}

func newTestParser(t *testing.T) *kong.Kong {
	t.Helper()
	parser, err := kong.New(&testCLI{}, kong.Name("unikraft"))
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	return parser
}

// completionLines runs registerCompletion against a fresh parser for the
// given COMP_LINE, capturing the printed output and the returned exit code.
func completionLines(t *testing.T, compLine string) (lines []string, code *int) {
	t.Helper()

	t.Setenv("COMP_LINE", compLine)
	os.Unsetenv("COMP_POINT")

	parser := newTestParser(t)
	var out bytes.Buffer
	parser.Stdout = &out

	code = registerCompletion(parser)

	for line := range strings.SplitSeq(strings.TrimRight(out.String(), "\n"), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return lines, code
}

func TestRegisterCompletion(t *testing.T) {
	t.Run("not a completion request", func(t *testing.T) {
		os.Unsetenv("COMP_LINE")
		os.Unsetenv("COMP_POINT")

		parser := newTestParser(t)
		var out bytes.Buffer
		parser.Stdout = &out

		code := registerCompletion(parser)

		if code != nil {
			t.Errorf("code = %v, want nil", *code)
		}
		if out.String() != "" {
			t.Errorf("unexpected output: %q", out.String())
		}
	})

	t.Run("bare tab at root hides aliases", func(t *testing.T) {
		lines, code := completionLines(t, "unikraft ")

		if code == nil || *code != 0 {
			t.Fatalf("code = %v, want pointer to 0", code)
		}
		want := []string{"certificates", "version"}
		if !slices.Equal(lines, want) {
			t.Errorf("lines = %v, want %v", lines, want)
		}
	})

	t.Run("prefix matches only an alias", func(t *testing.T) {
		lines, _ := completionLines(t, "unikraft cr")

		want := []string{"crt", "crts"}
		if !slices.Equal(lines, want) {
			t.Errorf("lines = %v, want %v", lines, want)
		}
	})

	t.Run("bare tab in nested command hides aliases", func(t *testing.T) {
		lines, _ := completionLines(t, "unikraft certificates ")

		want := []string{"get"}
		if !slices.Equal(lines, want) {
			t.Errorf("lines = %v, want %v", lines, want)
		}
	})

	t.Run("bare tab reached via alias still descends", func(t *testing.T) {
		lines, _ := completionLines(t, "unikraft crt ")

		want := []string{"get"}
		if !slices.Equal(lines, want) {
			t.Errorf("lines = %v, want %v", lines, want)
		}
	})

	t.Run("nested prefix matches only an alias", func(t *testing.T) {
		lines, _ := completionLines(t, "unikraft certificates i")

		want := []string{"inspect"}
		if !slices.Equal(lines, want) {
			t.Errorf("lines = %v, want %v", lines, want)
		}
	})
}
