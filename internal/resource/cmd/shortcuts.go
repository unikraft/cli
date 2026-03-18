// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"fmt"
	"reflect"

	"github.com/alecthomas/kong"

	"unikraft.com/cli/internal/resource/value"
)

// ApplyShortcutFlags scans Kong flags for those tagged with
// `shortcut:"<field-path>"` and appends their non-zero values to args as
// --set key=value entries.
//
// Pointer fields are considered "provided" when non-nil; value fields are
// considered "provided" when non-zero. Slice fields produce one entry per
// element.
func ApplyShortcutFlags(args *SetArgs, flags []*kong.Flag) error {
	for _, flag := range flags {
		if flag == nil || flag.Value == nil || flag.Tag == nil {
			continue
		}

		key := flag.Tag.Get("shortcut")
		if key == "" || key == "-" {
			continue
		}

		fv := flag.Target
		if fv.IsZero() {
			continue
		}

		// Dereference pointers to get the underlying value.
		for fv.Kind() == reflect.Ptr {
			fv = fv.Elem()
		}

		// Slice fields produce one --set entry per element.
		if fv.Kind() == reflect.Slice {
			for j := range fv.Len() {
				s, err := value.Format(fv.Index(j).Interface())
				if err != nil {
					return fmt.Errorf("shortcut %q[%d]: %w", key, j, err)
				}
				args.Set = append(args.Set, map[string]string{key: s})
			}
			continue
		}

		s, err := value.Format(fv.Interface())
		if err != nil {
			return fmt.Errorf("shortcut %q: %w", key, err)
		}
		args.Set = append(args.Set, map[string]string{key: s})
	}
	return nil
}
