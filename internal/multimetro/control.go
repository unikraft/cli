// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package multimetro

import (
	"context"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cloud/sdk/controlplane"
)

func NewControlClient(ctx context.Context, opts ...controlplane.ClientOption) (controlplane.Client, error) {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}

	copts := []controlplane.ClientOption{
		controlplane.WithDefaultEndpoint(profile.ControlPlane),
		controlplane.WithToken(profile.Token),
		controlplane.WithHTTPClient(httpclient.DefaultHTTPClient),
	}
	copts = append(copts, opts...)

	return controlplane.NewClient(copts...), nil
}
