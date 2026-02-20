// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/telemetry"
)

type SendAnalyticsCmd struct {
	Payload string `arg:"" help:"Analytics payload to send."`
}

func (cmd *SendAnalyticsCmd) Run(_ context.Context, _ *config.Config) error {
	return telemetry.SendEvent(cmd.Payload)
}
