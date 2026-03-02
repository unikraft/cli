// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/update"
)

type CheckUpdatesCmd struct {
	Channel string `help:"Release channel to check." default:"stable" enum:"stable,staging"`
	BaseUrl string `help:"Base URL for fetching releases." default:"https://pkg.unikraft.com" hidden:"true"`
}

func (cmd *CheckUpdatesCmd) Run(ctx context.Context, _ *config.Config) error {
	return update.CheckAndCache(ctx, cmd.BaseUrl, cmd.Channel)
}
