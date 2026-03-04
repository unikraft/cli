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

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/logs"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/muxreader"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
	xmaps "unikraft.com/cli/internal/x/maps"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
)

type RunCmd struct {
	Image string   `arg:"" help:"Image to run."`
	Args  []string `arg:"" optional:"" help:"Arguments to pass to the instance."`
	Env   []string `short:"e" help:"Set environment variables (KEY=VALUE)."`

	Name  string `short:"n" help:"Name of the instance."`
	Metro string `help:"Metro to deploy the instance in." required:"" placeholder:"metro"`

	Memory types.SizeMebibytes `short:"m" help:"Memory in IEC format (e.g., 128MiB, 1GiB, 1G)."`
	Vcpus  int                 `help:"Number of vCPUs."`

	Volume []string `short:"v" help:"Attach a volume (NAME:AT[:ro] or NAME:SIZE:AT[:ro] or UUID:AT[:ro])."`

	Service     string   `help:"Service group name."`
	Publish     []string `short:"p" help:"Publish a service port (SOURCE:DESTINATION[/HANDLERS])."`
	Domain      []string `help:"Add a service domain (FQDN)."`
	ScaleToZero []string `help:"Enable scale-to-zero."`

	Restart       string   `help:"Restart policy."`
	Autostart     bool     `help:"Start the instance automatically." default:"true"`
	Replicas      int64    `help:"Number of replicas."`
	WaitTimeoutMs int64    `help:"Wait timeout in milliseconds."`
	Features      []string `help:"Enable instance features."`

	DryRun bool `help:"Show the create preview without executing."`

	Follow bool `help:"Follow instance logs after creation."`

	cmd.FormatOpts
}

func (RunCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Deploy a new instance and expose a HTTPS service",
			Commands: []string{
				"unikraft run --metro=sfo -p 443:8080/http+tls nginx:latest",
			},
		},
		{
			Description: "Deploy a new instance and expose a HTTPS service and redirect from HTTP to HTTPS",
			Commands: []string{
				"unikraft run --metro=sfo -p 443:8080/http+tls -p 80:443/http+redirect nginx:latest",
			},
		},
		{
			Description: "Deploy and tail logs from a new instance",
			Commands: []string{
				"unikraft run --metro=fra --follow nginx:latest",
			},
		},
		{
			Description: "Preview instance creation without executing",
			Commands: []string{
				"unikraft run --metro=dal --dry-run nginx:latest",
			},
		},
		{
			Description: "Deploy a new instance with environment variables",
			Commands: []string{
				"unikraft run --metro=was -e KEY1=VALUE1 -e KEY2=VALUE2 my-app:latest",
			},
		},
		{
			Description: "Deploy a new instance with attached volume",
			Commands: []string{
				"unikraft run --metro=sin -v my-volume:/data my-app:latest",
			},
		},
		{
			Description: "Deploy a new instance with attached volume which is read-only",
			Commands: []string{
				"unikraft run --metro=sin -v my-volume:/data:ro my-app:latest",
			},
		},
		{
			Description: "Deploy a new instance with custom resource allocations",
			Commands: []string{
				"unikraft run --metro=sfo -m 512MiB --vcpus 2 my-app:latest",
			},
		},
		{
			Description: "Deploy a new instance with scale-to-zero enabled",
			Commands: []string{
				"unikraft run --metro=fra --scale-to-zero policy=on,cooldown-time=300 my-app:latest",
			},
		},
		{
			Description: "Deploy a new instance with specific restart policy",
			Commands: []string{
				"unikraft run --metro=dal --restart=on-failure my-app:latest",
			},
		},
	}
}

func (c *RunCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	if c.Image == "" {
		return fmt.Errorf("image is required")
	}
	if c.Metro == "" {
		return fmt.Errorf("metro is required")
	}

	env, err := value.Parse[map[string]string](c.Env)
	if err != nil {
		return err
	}
	volumes, err := value.Parse[[]*InstanceVolume](c.Volume)
	if err != nil {
		return err
	}
	services, err := value.Parse[[]*Service](c.Publish)
	if err != nil {
		return err
	}
	domains, err := value.Parse[[]Domain](c.Domain)
	if err != nil {
		return err
	}
	scaleToZero, err := value.Parse[*InstanceScaleToZero](c.ScaleToZero)
	if err != nil {
		return err
	}

	fields, err := Instance{}.Fields()
	if err != nil {
		return err
	}
	if err := c.applyCreatePatches(fields, env, volumes, services, domains, scaleToZero); err != nil {
		return err
	}

	if c.DryRun {
		return cmd.PrintPatches(stdio.Stdout, fields, true)
	}

	var creatable resource.CreatableResource = Instance{}
	if sandbox != nil {
		creatable = sandbox.WrapCreatable(creatable)
	}
	created, err := creatable.Create(ctx, fields)
	if err != nil {
		return err
	}
	if len(created) == 0 {
		return fmt.Errorf("no instances created")
	}
	err = c.Output.
		WithDefault(cmd.PrinterTypeKeyValue).
		Print(ctx, stdio.Stdout, c.Field, Instance{}, created...)
	if err != nil {
		return err
	}

	if c.Follow {
		fmt.Fprintln(stdio.Stdout)

		mux := muxreader.New()
		defer mux.Close()

		keys := make(multimetro.Keys, 0, len(created))
		for _, res := range created {
			instance, ok := res.(Instance)
			if !ok {
				return fmt.Errorf("unexpected resource type %T", res)
			}
			keys = append(keys, instance.key)
		}

		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		g, err := multimetro.NewClient(ctx)
		if err != nil {
			return err
		}
		err = group.DoRefs(ctx, g, keys.Refs(), func(_ context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
			for _, ref := range refs {
				key := multimetro.Key(ref)
				r, err := logs.InstanceLogs(ctx, c).Reader(ref.NameOrUUID(), 0, true)
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
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	return nil
}

func (c *RunCmd) applyCreatePatches(fields []resource.Field, env map[string]string, volumes []*InstanceVolume, services []*Service, domains []Domain, scaleToZero *InstanceScaleToZero) error {
	patches := c.runCreatePatches(env, volumes, services, domains, scaleToZero)
	for path, field := range resource.IterFields(fields) {
		if field.Create == nil {
			continue
		}
		field.Create = nil
		if value, ok := patches[path.String()]; ok {
			field.Create = &resource.Patch{Set: value}
			delete(patches, path.String())
		}
	}
	if len(patches) > 0 {
		return fmt.Errorf("unknown create patches: %v", xmaps.OrderedKeys(patches))
	}

	return nil
}

func (c *RunCmd) runCreatePatches(env map[string]string, volumes []*InstanceVolume, services []*Service, domains []Domain, scaleToZero *InstanceScaleToZero) map[string]any {
	patches := map[string]any{
		// FIXME: parse image key, don't require exact matches
		"image": c.Image,
		"metro": c.Metro,
	}
	if c.Name != "" {
		patches["name"] = c.Name
	}
	if len(c.Args) > 0 {
		patches["runtime.args"] = c.Args
	}
	if len(env) > 0 {
		patches["runtime.env"] = env
	}
	if c.Memory > 0 {
		patches["resources.memory"] = c.Memory
	}
	if c.Vcpus > 0 {
		patches["resources.vcpus"] = c.Vcpus
	}
	if c.Restart != "" {
		patches["restart.policy"] = c.Restart
	}
	if scaleToZero != nil {
		patches["scale-to-zero.policy"] = string(platform.InstanceScaleToZeroPolicyOn)
		if scaleToZero.Policy != "" {
			patches["scale-to-zero.policy"] = scaleToZero.Policy
		}
		if scaleToZero.Stateful {
			patches["scale-to-zero.stateful"] = scaleToZero.Stateful
		}
		if scaleToZero.CooldownTime > 0 {
			patches["scale-to-zero.cooldown-time"] = scaleToZero.CooldownTime
		}
	}
	if len(volumes) > 0 {
		patches["volumes"] = volumes
	}
	if c.Service != "" {
		key := multimetro.ParseKey(c.Service)
		if key.UUID != "" {
			patches["service.uuid"] = key.UUID
		} else {
			patches["service.name"] = key.Name
		}
	}
	if len(services) > 0 {
		patches["service.services"] = services
	}
	if len(domains) > 0 {
		patches["service.domains"] = domains
	}
	if c.Autostart {
		patches["autostart"] = c.Autostart
	}
	if c.Replicas > 0 {
		patches["replicas"] = c.Replicas
	}
	if c.WaitTimeoutMs > 0 {
		patches["wait_timeout_ms"] = c.WaitTimeoutMs
	}
	if len(c.Features) > 0 {
		patches["features"] = c.Features
	}
	return patches
}
