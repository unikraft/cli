// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/alecthomas/kong"
	kongcompletion "github.com/jotaen/kong-completion"
)

// registerCompletion wires up kong-completion, the same way
// kongcompletion.Register does, but additionally hides command aliases from
// the root completion listing.
func registerCompletion(parser *kong.Kong, opt ...kongcompletion.Option) *int {
	completed, bareLast, isCompletion := parseCompLine()
	if !isCompletion {
		kongcompletion.Register(parser, opt...)
		return nil
	}

	origStdout := parser.Stdout
	var buf bytes.Buffer
	parser.Stdout = &buf

	var exitCode int
	exited := false
	opt = append(slices.Clone(opt), kongcompletion.WithExitFunc(func(code int) {
		exited = true
		exitCode = code
	}))

	kongcompletion.Register(parser, opt...)

	parser.Stdout = origStdout

	if !exited {
		_, _ = io.Copy(origStdout, &buf)
		return nil
	}

	var hide map[string]bool
	if bareLast {
		// Walk down the command tree following the already-typed arguments
		// (which may themselves be aliases) to find the active node.
		node := parser.Model.Node
	argLoop:
		for _, arg := range completed {
			for _, child := range node.Children {
				if child != nil && (child.Name == arg || slices.Contains(child.Aliases, arg)) {
					node = child
					continue argLoop
				}
			}
			break
		}

		// Hide the active node's children's aliases, keeping canonical names
		hide = make(map[string]bool)
		for _, child := range node.Children {
			if child == nil {
				continue
			}
			for _, alias := range child.Aliases {
				if alias != child.Name {
					hide[alias] = true
				}
			}
		}
	}

	for line := range strings.SplitSeq(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" || hide[line] {
			continue
		}
		fmt.Fprintln(origStdout, line)
	}

	return &exitCode
}

// parseCompLine parses the COMP_LINE/COMP_POINT environment variables set by
// the shell during completion.
func parseCompLine() (completed []string, bareLast bool, isCompletion bool) {
	line, ok := os.LookupEnv("COMP_LINE")
	if !ok || line == "" {
		return nil, false, false
	}

	if pointStr, ok := os.LookupEnv("COMP_POINT"); ok {
		if point, err := strconv.Atoi(pointStr); err == nil && point >= 0 && point < len(line) {
			line = line[:point]
		}
	}

	parts := strings.Fields(line)
	if line != "" && unicode.IsSpace(rune(line[len(line)-1])) {
		parts = append(parts, "")
	}
	if len(parts) == 0 {
		return nil, true, true
	}

	bareLast = parts[len(parts)-1] == ""

	// Drop the binary name (first field) and the word currently being typed
	// (last field), leaving only the already-completed command path.
	body := parts[1:]
	if len(body) > 0 {
		body = body[:len(body)-1]
	}
	return body, bareLast, true
}
