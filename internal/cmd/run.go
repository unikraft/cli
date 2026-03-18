// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/alecthomas/kong"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/logs"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/muxreader"
	"unikraft.com/cli/internal/resource"
	resourcecmd "unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
)

// RunCmd is a convenience wrapper around `instance create` that adds log
// following capability. All instance creation flags are inherited from
// InstanceCreateCmd via embedding.
type RunCmd struct {
	InstanceCreateCmd

	// Follow is the only run-specific flag; it tails logs after creation.
	Follow bool `help:"Follow instance logs after creation."`
}

func (RunCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Deploy a new instance and expose a HTTPS service",
			Commands: []string{
				"unikraft run --metro=sfo --image=nginx:latest -p 443:8080/http+tls",
			},
		},
		{
			Description: "Deploy a new instance and expose a HTTPS service and redirect from HTTP to HTTPS",
			Commands: []string{
				"unikraft run --metro=sfo --image=nginx:latest -p 443:8080/http+tls -p 80:443/http+redirect",
			},
		},
		{
			Description: "Deploy and tail logs from a new instance",
			Commands: []string{
				"unikraft run --metro=fra --image=nginx:latest --follow",
			},
		},
		{
			Description: "Preview instance creation without executing",
			Commands: []string{
				"unikraft run --metro=dal --image=nginx:latest --dry-run",
			},
		},
		{
			Description: "Deploy a new instance with environment variables",
			Commands: []string{
				"unikraft run --metro=was --image=my-app:latest -e KEY1=VALUE1 -e KEY2=VALUE2",
			},
		},
		{
			Description: "Deploy a new instance with attached volume",
			Commands: []string{
				"unikraft run --metro=sin --image=my-app:latest -v my-volume:/data",
			},
		},
		{
			Description: "Deploy a new instance with attached volume which is read-only",
			Commands: []string{
				"unikraft run --metro=sin --image=my-app:latest -v my-volume:/data:ro",
			},
		},
		{
			Description: "Deploy a new instance with custom resource allocations",
			Commands: []string{
				"unikraft run --metro=sfo --image=my-app:latest -m 512MiB --vcpus 2",
			},
		},
		{
			Description: "Deploy a new instance with scale-to-zero enabled",
			Commands: []string{
				"unikraft run --metro=fra --image=my-app:latest --scale-to-zero policy=on,cooldown-time=300",
			},
		},
		{
			Description: "Deploy a new instance with specific restart policy",
			Commands: []string{
				"unikraft run --metro=dal --image=my-app:latest --restart=on-failure",
			},
		},
	}
}

func (c *RunCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if c.Autostart == nil {
		c.Autostart = new(bool)
		*c.Autostart = true
	}
	if err := resourcecmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	created, err := c.RunResources(ctx, stdio, sandbox)
	if err != nil {
		return err
	}
	if !c.Follow || len(created) == 0 {
		return nil
	}

	fmt.Fprintln(stdio.Stdout)

	keys := make(multimetro.Keys, 0, len(created))
	for _, res := range created {
		instance, ok := res.(Instance)
		if !ok {
			return fmt.Errorf("unexpected resource type %T", res)
		}
		keys = append(keys, instance.key)
	}

	return streamInstanceLogs(ctx, stdio, keys, 0, true)
}

func streamInstanceLogs(ctx context.Context, stdio config.Stdio, keys multimetro.Keys, tail int, follow bool) error {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	mux := muxreader.New()
	defer mux.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = group.DoRefs(ctx, g, keys.Refs(), func(_ context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		for _, ref := range refs {
			key := multimetro.Key(ref)
			r, err := logs.InstanceLogs(ctx, c).Reader(ref.NameOrUUID(), tail, follow)
			if err != nil {
				return nil, err
			}
			mux.With(key.String(), r)
		}
		return refs, nil
	})
	if err != nil {
		return err
	}
	mux.Seal()

	_, err = io.Copy(stdio.Stdout, mux)
	if follow && errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
