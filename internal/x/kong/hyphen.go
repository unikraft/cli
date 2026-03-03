// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kong

import (
	"fmt"

	"github.com/alecthomas/kong"
)

// HyphenString is a string type that allows values starting with '-'.
type HyphenString string

// Decode implements kong.MapperValue.
func (h *HyphenString) Decode(ctx *kong.DecodeContext) error {
	t := ctx.Scan.Pop()
	if t.IsEOL() {
		return fmt.Errorf("missing value")
	}

	switch v := t.Value.(type) {
	case string:
		*h = HyphenString(v)
	default:
		*h = HyphenString(fmt.Sprint(v))
	}

	return nil
}

// HyphenStrings is a slice of strings that allows values starting with '-'.
type HyphenStrings []string

// Decode implements kong.MapperValue.
func (h *HyphenStrings) Decode(ctx *kong.DecodeContext) error {
	sep := ctx.Value.Tag.Sep
	tail := ""
	if sep != -1 {
		tail = string(sep) + "..."
	}

	t := ctx.Scan.Pop()
	if t.IsEOL() {
		return fmt.Errorf("missing value, expecting \"<arg>%s\"", tail)
	}

	appendValue := func(v string) {
		*h = append(*h, v)
	}

	switch v := t.Value.(type) {
	case string:
		if sep == -1 {
			appendValue(v)
			return nil
		}
		for _, part := range kong.SplitEscaped(v, sep) {
			appendValue(part)
		}
	case []any:
		for _, part := range v {
			appendValue(fmt.Sprint(part))
		}
	default:
		appendValue(fmt.Sprint(v))
	}

	return nil
}
