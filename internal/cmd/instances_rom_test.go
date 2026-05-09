// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import "testing"

func TestResolveROMFilePlacement(t *testing.T) {
	tests := []struct {
		name          string
		srcPath       string
		at            string
		wantMountAt   string
		wantInlineDst string
	}{
		{
			name:          "full destination file path",
			srcPath:       "console.yaml",
			at:            "/etc/consoled/config.yaml",
			wantMountAt:   "/etc/consoled",
			wantInlineDst: "/config.yaml",
		},
		{
			name:          "mountpoint without trailing slash",
			srcPath:       ".env",
			at:            "/rom",
			wantMountAt:   "/rom",
			wantInlineDst: "/.env",
		},
		{
			name:          "mountpoint with trailing slash",
			srcPath:       "config.env",
			at:            "/rom/",
			wantMountAt:   "/rom/",
			wantInlineDst: "/config.env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMountAt, gotInlineDst := resolveROMFilePlacement(tt.srcPath, tt.at)
			if gotMountAt != tt.wantMountAt {
				t.Fatalf("mountAt mismatch: got %q want %q", gotMountAt, tt.wantMountAt)
			}
			if gotInlineDst != tt.wantInlineDst {
				t.Fatalf("inline path mismatch: got %q want %q", gotInlineDst, tt.wantInlineDst)
			}
		})
	}
}
