// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"

	"unikraft.com/cli/internal/config"
	cmd "unikraft.com/cli/internal/resource/cmd"
)

// defaultMetroFromProfile checks whether metro has already been set in args
// (via --metro flag or --set metro=...). If not, it looks at the current
// profile's metro list: when exactly one metro is configured it is used
// automatically; otherwise an error is returned telling the user the flag is
// required.
func defaultMetroFromProfile(ctx context.Context, args *cmd.SetArgs) error {
	// Check whether metro was already provided.
	for _, m := range args.Set {
		if _, ok := m["metro"]; ok {
			return nil
		}
	}

	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return err
	}

	switch len(profile.Metros) {
	case 0:
		return fmt.Errorf("--metro is required: no metros configured in profile %q", profile.Name)
	case 1:
		args.Set = append(args.Set, map[string]string{"metro": profile.Metros[0].Name})
		return nil
	default:
		names := make([]string, len(profile.Metros))
		for i, m := range profile.Metros {
			names[i] = m.Name
		}
		return fmt.Errorf("--metro is required: profile %q has multiple metros (%v)", profile.Name, names)
	}
}
