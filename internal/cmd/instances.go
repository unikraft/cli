// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/distribution/reference"
	"github.com/google/uuid"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/platform/stop"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/logs"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/muxreader"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
)

type InstancesCmd struct {
	cmd.ResourceCmd[Instance]
	cmd.GettableResourceCmd[Instance]
	cmd.WaitableResourceCmd[Instance]
	cmd.ListableResourceCmd[Instance]
	cmd.BulkDeletableResourceCmd[Instance]

	Create InstanceCreateCmd `cmd:"" help:"Create an instance."`
	Edit   InstanceEditCmd   `cmd:"" help:"Edit an instance."`

	Logs    InstancesLogsCmd    `cmd:"" help:"Fetch and display instance logs."`
	Start   InstancesStartCmd   `cmd:"" help:"Start one or more instances."`
	Stop    InstancesStopCmd    `cmd:"" help:"Stop one or more instances."`
	Restart InstancesRestartCmd `cmd:"" help:"Restart one or more instances."`
	Tunnel  InstancesTunnelCmd  `cmd:"" help:"Forward a local port to an unexposed instance."`
}

// InstanceCreateCmd extends the generic resource create command with shortcut
// flags for commonly used instance fields. Each field tagged with
// `shortcut:"<path>"` is translated into a --set <path>=<value> entry before
// the standard create pipeline runs.
type InstanceCreateCmd struct {
	cmd.ResourceCreateCmd[Instance]

	// Shortcut flags - ordered to match Instance struct layout.
	Metro string `group:"flag-create" shortcut:"metro" help:"Metro to deploy in." placeholder:"metro" example:"fra,sfo,nyc"`
	Name  string `group:"flag-create" shortcut:"name" short:"n" help:"Instance name." placeholder:"name"`

	Image string `group:"flag-create" shortcut:"image" help:"Image to deploy." placeholder:"<name>:<tag>" example:"nginx:latest,my-app:v1.2.3"`

	Args []string `group:"flag-create" shortcut:"runtime.args" help:"Arguments to pass to the instance." placeholder:"arg"`
	Env  []string `group:"flag-create" shortcut:"runtime.env" short:"e" help:"Environment variables." placeholder:"<key>=<value>" example:"DEBUG=true,PORT=8080"`

	Memory types.SizeMebibytes `group:"flag-create" shortcut:"resources.memory" short:"m" help:"Memory allocation." placeholder:"size" example:"128MiB,1GiB"`
	Vcpus  int                 `group:"flag-create" shortcut:"resources.vcpus" help:"Number of vCPUs." placeholder:"n" example:"1,2,4"`

	Volume []InstanceVolume `group:"flag-create" shortcut:"volumes" short:"v" help:"Attach volume." placeholder:"<name>:<path>[:<ro>]" example:"my-vol:/data,cache:/tmp:ro"`

	Service InstanceService `group:"flag-create" shortcut:"service" help:"Service group name or key." placeholder:"name"`
	Publish []Service       `group:"flag-create" shortcut:"service.services" short:"p" help:"Publish port." placeholder:"<src>:<dest>[/<handlers>]" example:"443:8080/http+tls,80:8080/http"`
	Domain  []Domain        `group:"flag-create" shortcut:"service.domains" help:"Service domain." placeholder:"fqdn" example:"example.com,api.example.com"`

	ScaleToZero InstanceScaleToZero `group:"flag-create" shortcut:"scale-to-zero" help:"Scale-to-zero options." placeholder:"<key>=<value>" example:"policy=on\\,cooldown-time=300"`

	Restart string `group:"flag-create" shortcut:"restart.policy" help:"Restart policy." placeholder:"policy" example:"always,on-failure,never"`

	Autostart *bool    `group:"flag-create" shortcut:"autostart" help:"Start instance automatically."`
	Replicas  int64    `group:"flag-create" shortcut:"replicas" help:"Number of replicas." placeholder:"n" example:"1,3"`
	Features  []string `group:"flag-create" shortcut:"features" help:"Instance features." placeholder:"feature"`
}

func (c *InstanceCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

// InstanceEditCmd extends the generic resource edit command with shortcut
// flags for commonly used editable instance fields. Each field tagged with
// `shortcut:"<path>"` is translated into a --set <path>=<value> entry before
// the standard edit pipeline runs.
type InstanceEditCmd struct {
	cmd.ResourceEditCmd[Instance]

	// Shortcut flags - only fields that support editing.
	Image string `group:"flag-edit" shortcut:"image" help:"Image to deploy." placeholder:"<name>:<tag>" example:"nginx:latest,my-app:v1.2.3"`

	Args []string `group:"flag-edit" shortcut:"runtime.args" help:"Arguments to pass to the instance." placeholder:"arg"`
	Env  []string `group:"flag-edit" shortcut:"runtime.env" short:"e" help:"Environment variables." placeholder:"<key>=<value>" example:"DEBUG=true,PORT=8080"`

	Memory types.SizeMebibytes `group:"flag-edit" shortcut:"resources.memory" short:"m" help:"Memory allocation." placeholder:"size" example:"128MiB,1GiB"`
	Vcpus  int                 `group:"flag-edit" shortcut:"resources.vcpus" help:"Number of vCPUs." placeholder:"n" example:"1,2,4"`

	ScaleToZero InstanceScaleToZero `group:"flag-edit" shortcut:"scale-to-zero" help:"Scale-to-zero options." placeholder:"<key>=<value>" example:"policy=on\\,cooldown-time=300"`
}

func (c *InstanceEditCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceEditCmd.Run(ctx, stdio, sandbox)
}

type Instance struct {
	MetroName string `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string `mirror:"instance.name" field:",short" create:"set"`
	UUID      string `mirror:"instance.uuid" field:",long"`

	Tags []string `mirror:"instance.tags"`

	State types.InstanceState `mirror:"instance.state" field:",short" edit:"set"`

	Image types.ImageRef[reference.Named] `mirror:"instance.image" field:",short" create:"set,required" edit:"set"`

	Runtime struct {
		Args []string          `mirror:"instance.args" field:",short" create:"set" edit:"set"`
		Env  map[string]string `mirror:"instance.env" field:",long" create:"set" edit:"set,add,del=keys"`
	}

	Resources struct {
		Memory types.SizeMebibytes `mirror:"instance.memory_mb" field:",short" create:"set" edit:"set"`
		VCPUs  int                 `mirror:"instance.vcpus" field:"vcpus,short" create:"set" edit:"set"`
	}

	Service *InstanceService  `mirror:"instance.service_group" field:",embed" create:"set"`
	Volumes []*InstanceVolume `mirror:"instance.volumes" field:",embed" create:"set"`

	Networks []struct {
		UUID      string `mirror:"uuid" field:",long"`
		PrivateIP string `mirror:"private_ip" field:",long"`
		MAC       string `mirror:"mac" field:",long"`
	} `mirror:"instance.network_interfaces"`

	Timestamps struct {
		Created types.RelativeTime `mirror:"instance.created_at" field:",short"`
		Started types.RelativeTime `mirror:"instance.started_at"`
		Stopped types.RelativeTime `mirror:"instance.stopped_at"`
	}

	ScaleToZero InstanceScaleToZero `field:",embed" mirror:"instance.scale_to_zero" create:"set" edit:"set"`

	Timing struct {
		Uptime   types.DurationMS `mirror:"instance.uptime_ms"`
		BootTime types.DurationUS `mirror:"instance.boot_time_us"`
		NetTime  types.DurationUS `mirror:"instance.net_time_us"`
	}

	Restart struct {
		Policy       string `mirror:"instance.restart_policy" create:"set"`
		StartCount   int    `mirror:"instance.start_count"`
		RestartCount int    `mirror:"instance.restart_count"`
	}

	Autostart   bool            `field:"autostart,invisible,valueless" create:"set"`
	Replicas    int64           `field:"replicas,invisible,valueless" create:"set"`
	WaitTimeout types.DurationS `field:"wait-timeout,invisible,valueless" create:"set"`
	Features    []string        `field:"features,invisible,valueless" create:"set"`
	Vsock       bool            `field:"vsock,invisible,valueless" create:"set" edit:"set"`

	Stop struct {
		Reason string     `field:",long"`
		Origin string     `field:"origin,hidden"`
		Errno  stop.Errno `field:"errno,hidden"`

		ExitCode *uint32 `mirror:"instance.exit_code" field:"exit-code,long"`
	} `field:",long"`

	Instance platform.Instance `field:"-" json:"instance"`
	Metro    *config.Metro     `field:"-" json:"metro"`
	Profile  *config.Profile   `field:"-" json:"profile"`

	key multimetro.Key
}

type InstanceService struct {
	Metro     string     `field:"-"`
	UUID      string     `mirror:"uuid" field:",long"`
	Name      string     `mirror:"name" field:",long"`
	Services  []*Service `mirror:"services" field:",invisible,valueless" create:"set"`
	Domains   []Domain   `mirror:"domains" field:",short,embed" create:"set"`
	SoftLimit uint32     `field:"soft-limit,invisible,valueless" create:"set"`
	HardLimit uint32     `field:"hard-limit,invisible,valueless" create:"set"`
}

func (i *InstanceService) UnmarshalText(data []byte) error {
	str := strings.TrimSpace(string(data))
	if str == "" {
		return nil
	}
	if strings.Contains(str, "=") {
		type instanceServiceAlias InstanceService
		parsed, err := value.Parse[instanceServiceAlias]([]string{str})
		if err != nil {
			return err
		}
		*i = InstanceService(parsed)
		return nil
	}
	key := multimetro.ParseKey(str)
	i.Metro = key.Metro
	i.UUID = key.UUID
	i.Name = key.Name
	return nil
}

type InstanceVolume struct {
	UUID     string `name:"uuid" mirror:"uuid" json:"uuid,omitempty" field:",long"`
	Name     string `name:"name" mirror:"name" json:"name,omitempty" field:",long"`
	At       string `name:"at" mirror:"at" json:"at" field:",long"`
	Readonly bool   `name:"readonly" mirror:"readonly" json:"readonly,omitempty" field:",long"`

	Size types.SizeMebibytes `name:"size" field:"size,invisible,valueless" create:"set"`
}

func (v *InstanceVolume) MarshalText() ([]byte, error) {
	parts := []string{
		cmp.Or(v.Name, v.UUID),
		v.At,
	}
	if v.Readonly {
		parts = append(parts, "ro")
	}
	if v.Size > 0 {
		s, err := value.Format(v.Size)
		if err != nil {
			return nil, err
		}
		parts = append(parts, fmt.Sprintf("size=%s", s))
	}
	return []byte(strings.Join(parts, ":")), nil
}

// MarshalJSON outputs the struct form (not the short text form).
// This takes precedence over MarshalText for JSON/YAML serialization.
func (v *InstanceVolume) MarshalJSON() ([]byte, error) {
	type volumeJSON InstanceVolume // alias to avoid recursion
	return json.Marshal((*volumeJSON)(v))
}

// UnmarshalJSON parses both the struct form and the short text form.
// This takes precedence over UnmarshalText for JSON/YAML deserialization.
func (v *InstanceVolume) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return v.UnmarshalText([]byte(text))
	}
	type volumeJSON InstanceVolume // alias to avoid recursion
	return json.Unmarshal(data, (*volumeJSON)(v))
}

func (v *InstanceVolume) UnmarshalText(data []byte) error {
	str := string(data)
	parts := strings.Split(str, ":")
	if len(parts) < 2 {
		return fmt.Errorf("invalid volume format, expected NAME:AT, got %q", str)
	}

	name, at, parts := parts[0], parts[1], parts[2:]
	readonly := false
	size := types.SizeMebibytes(0)

	var err error
	for _, part := range parts {
		k, v, ok := strings.Cut(part, "=")
		switch k {
		case "ro":
			if ok {
				readonly, err = strconv.ParseBool(v)
				if err != nil {
					return err
				}
			} else {
				readonly = true
			}
		case "size":
			size, err = value.Parse[types.SizeMebibytes]([]string{v})
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid volume option: %q", k)
		}
	}

	if uuid.Validate(name) == nil {
		v.UUID = name
	} else if name != "" {
		v.Name = name
	}
	v.At = at
	v.Readonly = readonly
	v.Size = size

	return nil
}

type InstanceScaleToZero struct {
	Enabled      bool             `name:"-" json:"-" mirror:"enabled" field:",long"`
	Policy       string           `name:"policy" json:"policy,omitempty" mirror:"policy" field:",long"`
	Stateful     bool             `name:"stateful" json:"stateful,omitempty" mirror:"stateful" field:",long"`
	CooldownTime types.DurationMS `name:"cooldown-time" json:"cooldown-time,omitempty" mirror:"cooldown_time_ms" field:",long"`
	NotifyTime   types.DurationMS `name:"notify-time" json:"notify-time,omitempty" mirror:"notify_time_ms" field:",long"`
}

func (s *InstanceScaleToZero) UnmarshalText(data []byte) error {
	str := strings.TrimSpace(string(data))
	if str == "" {
		return nil
	}

	lower := strings.ToLower(str)
	if lower == "on" || lower == "off" {
		s.Policy = lower
		return nil
	}

	type scaleToZeroAlias InstanceScaleToZero
	parsed, err := value.Parse[scaleToZeroAlias]([]string{str})
	if err != nil {
		return err
	}
	*s = InstanceScaleToZero(parsed)
	return nil
}

func (s InstanceScaleToZero) MarshalText() ([]byte, error) {
	var parts []string
	if s.Policy != "" {
		parts = append(parts, fmt.Sprintf("policy=%s", s.Policy))
	}
	if s.Stateful {
		parts = append(parts, "stateful=true")
	}
	if s.CooldownTime > 0 {
		parts = append(parts, fmt.Sprintf("cooldown-time=%s", s.CooldownTime))
	}
	if s.NotifyTime > 0 {
		parts = append(parts, fmt.Sprintf("notify-time=%s", s.NotifyTime))
	}
	return []byte(strings.Join(parts, ",")), nil
}

func (s InstanceScaleToZero) MarshalJSON() ([]byte, error) {
	type scaleToZeroJSON InstanceScaleToZero
	return json.Marshal(scaleToZeroJSON(s))
}

func (Instance) Type() resource.Type {
	return resource.Type{
		Name:  "instance",
		Names: "instances",
	}
}

func (i Instance) Key() resource.Key {
	return i.key
}

func (i Instance) Raw() any {
	return i.Instance
}

func (i Instance) Fields() ([]resource.Field, error) {
	result, err := resource.FieldsFromStruct(i)
	if err != nil {
		return nil, err
	}

	for key, field := range resource.IterFields(result) {
		switch {
		case key.String() == "name":
			field.Hyperlink = i.hyperlink()
		case key.String() == "metro":
			if i.MetroName != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "metro",
					Key:  i.MetroName,
				})
			}
		case key.String() == "service":
			nameField, _ := field.Get("name")
			uuidField, _ := field.Get("uuid")
			name, _ := nameField.Value.(string)
			uuid, _ := uuidField.Value.(string)
			if name != "" || uuid != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "service",
					Key: multimetro.Key{
						Metro: i.Metro.Name,
						Name:  name,
						UUID:  uuid,
					}.String(),
				})
			}
		case key.MatchesString("volumes.*"):
			// Add link to volume resource
			nameField, _ := field.Get("name")
			uuidField, _ := field.Get("uuid")
			name, _ := nameField.Value.(string)
			uuid, _ := uuidField.Value.(string)
			if name != "" || uuid != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "volume",
					Key: multimetro.Key{
						Metro: i.Metro.Name,
						Name:  name,
						UUID:  uuid,
					}.String(),
				})
			}
		case key.String() == "image":
			if i.Image.Reference != nil {
				field.Links = append(field.Links, resource.Link{
					Type: "image",
					Key:  i.Image.Reference.String(),
				})
			}
		}
	}

	return result, nil
}

func (i Instance) hyperlink() string {
	if i.Profile == nil || i.Profile.ControlPlane == "" {
		return ""
	}
	if i.Name == "" || i.Profile.Organization == "" {
		return ""
	}
	return fmt.Sprintf(
		"https://console.unikraft.cloud/org/%s/instances/%s/%s",
		i.Profile.Organization,
		i.MetroName,
		i.Name,
	)
}

func (Instance) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing instances")
		resp, err := c.GetInstances(ctx, nil, new(true))
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		var errs []error
		for _, instance := range resp.Data.Instances {
			result, err := Instance{}.load(nil, instance, &c.Metro, profile)
			if err != nil {
				errs = append(errs, err)
			}
			results = append(results, result)
		}
		return results, errors.Join(errs...)
	})
}

func (Instance) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting instances")
		resp, err := c.GetInstances(ctx, refs.NameOrUUIDs(), new(true))
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		var errs []error
		for i, instance := range resp.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			result, err := Instance{}.load(&refs[i], instance, &c.Metro, profile)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			found = append(found, group.Ref{
				Metro: c.Metro.Name,
				Name:  result.Name,
				UUID:  result.UUID,
			})
			results = append(results, result)
		}
		return results, found, errors.Join(errs...)
	})
}

func (Instance) load(ref *group.Ref, instance platform.Instance, metro *config.Metro, profile *config.Profile) (Instance, error) {
	if ref == nil {
		ref = &group.Ref{
			Metro: metro.Name,
			Name:  ptr.ZeroIfNil(instance.Name),
			UUID:  ptr.ZeroIfNil(instance.Uuid),
		}
	} else {
		ref.Metro = cmp.Or(ref.Metro, metro.Name)
		ref.Name = cmp.Or(ref.Name, ptr.ZeroIfNil(instance.Name))
		ref.UUID = cmp.Or(ref.UUID, ptr.ZeroIfNil(instance.Uuid))
	}

	result := Instance{
		Instance: instance,
		Metro:    metro,
		Profile:  profile,
		key:      multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return Instance{}, fmt.Errorf("could not mirror instance data: %w", err)
	}
	if s := instance.Stop(); s != nil {
		result.Stop.Reason = s.String()
		result.Stop.Origin = s.Origin()
		if stopCode := s.KernelStopCode(); stopCode != nil {
			result.Stop.Errno = stop.Errno(stopCode.Errno())
		}
	}
	return result, nil
}

func (Instance) Delete(ctx context.Context, targets []resource.Resource) error {
	keys := make(multimetro.Keys, 0, len(targets))
	for _, target := range targets {
		instance := target.(Instance)
		keys = append(keys, instance.key)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting instances")
		instances, err := c.DeleteInstances(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, instance := range instances.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name,
				Name:  *instance.Name,
				UUID:  *instance.Uuid,
			})
		}
		return deleted, nil
	})
}

func (Instance) Edit(ctx context.Context, target resource.Resource, fields []resource.Field) (resource.Resource, error) {
	instance := target.(Instance)

	targetState := instance.State
	if fields := resource.GetFieldByPath(fields, resource.FieldPath{"state"}); len(fields) > 0 {
		field := fields[0]
		if field.Edit != nil && field.Edit.Set != nil {
			targetState = field.Edit.Set.(types.InstanceState)
			field.Edit = nil
		}
	}

	patches := patchRequests(fields, instancePatchSpec)
	reqs := make([]platform.UpdateInstancesRequestItem, 0, len(patches))
	for _, patch := range patches {
		reqs = append(reqs, platform.UpdateInstancesRequestItem{
			Uuid:  &instance.UUID,
			Op:    platform.UpdateInstancesRequestItemOp(patch.Op),
			Prop:  patch.Prop,
			Value: new(patch.Value),
		})
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	if instance.State.IsRunning() && !targetState.IsRunning() {
		_, err := stopInstances(ctx, g, multimetro.Keys{instance.key}, StopOpts{DrainTimeout: -1})
		if err != nil {
			return nil, err
		}
	}
	if len(reqs) > 0 {
		err = group.DoMetro(ctx, g, instance.key.Metro, func(ctx context.Context, c multimetro.MetroClient) error {
			log.G(ctx).Trace().Msg("updating instance")
			_, err := c.UpdateInstances(ctx, reqs)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	if !instance.State.IsRunning() && targetState.IsRunning() {
		_, err := startInstances(ctx, g, multimetro.Keys{instance.key})
		if err != nil {
			return nil, err
		}
	}
	results, err := Instance{}.Get(ctx, []string{instance.Key().String()})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func instancePatchSpec(path string, op patchOp, value any) (platform.UpdateInstancesRequestItemProp, any) {
	var zero platform.UpdateInstancesRequestItemProp
	switch path {
	case "image":
		return platform.UpdateInstancesRequestItemPropImage, value.(types.ImageRef[reference.Named]).Reference.String()
	case "runtime.args":
		return platform.UpdateInstancesRequestItemPropArgs, value.([]string)
	case "runtime.env":
		if op == patchOpDel {
			return platform.UpdateInstancesRequestItemPropEnv, value.([]string)
		}
		return platform.UpdateInstancesRequestItemPropEnv, value.(map[string]string)
	case "resources.memory":
		return platform.UpdateInstancesRequestItemPropMemory_mb, int64(value.(types.SizeMebibytes))
	case "resources.vcpus":
		return platform.UpdateInstancesRequestItemPropVcpus, value.(int)
	case "scale-to-zero":
		value := value.(InstanceScaleToZero)
		req := map[string]any{}
		if value.Policy != "" {
			req["policy"] = value.Policy
		}
		if value.Stateful {
			req["stateful"] = value.Stateful
		}
		if value.CooldownTime > 0 {
			req["cooldown_time_ms"] = int32(value.CooldownTime)
		}
		if value.NotifyTime > 0 {
			req["notify_time_ms"] = int32(value.NotifyTime)
		}
		return platform.UpdateInstancesRequestItemPropScale_to_zero, req
	case "vsock":
		return "vsock", value.(bool)
	default:
		return zero, nil
	}
}

func (Instance) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var req platform.CreateInstanceRequest
	var metro string
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		switch key.String() {
		case "name":
			name := field.Create.Set.(string)
			req.Name = &name
		case "metro":
			metro = field.Create.Set.(string)
		case "image":
			req.Image = new(field.Create.Set.(types.ImageRef[reference.Named]).Reference.String())
		case "runtime.args":
			req.Args = field.Create.Set.([]string)
		case "runtime.env":
			req.Env = field.Create.Set.(map[string]string)
		case "resources.memory":
			mem := int64(field.Create.Set.(types.SizeMebibytes))
			req.MemoryMb = &mem
		case "resources.vcpus":
			vcpus := int32(field.Create.Set.(int))
			req.Vcpus = &vcpus
		case "restart.policy":
			policy := platform.CreateInstanceRequestRestartPolicy(field.Create.Set.(string))
			req.RestartPolicy = &policy
		case "scale-to-zero":
			scale := field.Create.Set.(InstanceScaleToZero)
			req.ScaleToZero = &platform.CreateInstanceRequestScaleToZero{}
			if scale.Policy != "" {
				req.ScaleToZero.Policy = new(platform.CreateInstanceRequestScaleToZeroPolicy(scale.Policy))
			}
			if scale.Stateful {
				req.ScaleToZero.Stateful = &scale.Stateful
			}
			if scale.CooldownTime > 0 {
				cooldown := int32(scale.CooldownTime)
				req.ScaleToZero.CooldownTimeMs = &cooldown
			}
			if scale.NotifyTime > 0 {
				notify := int32(scale.NotifyTime)
				req.ScaleToZero.NotifyTimeMs = &notify
			}
		case "volumes":
			for _, vol := range field.Create.Set.([]*InstanceVolume) {
				reqVol := platform.CreateInstanceRequestVolume{
					At: vol.At,
				}
				if vol.UUID != "" {
					reqVol.Uuid = &vol.UUID
				}
				if vol.Name != "" {
					reqVol.Name = &vol.Name
				}
				if vol.Size > 0 {
					reqVol.SizeMb = new(int64(vol.Size))
				}
				if vol.Readonly {
					reqVol.Readonly = &vol.Readonly
				}
				req.Volumes = append(req.Volumes, reqVol)
			}
		case "service":
			svc := field.Create.Set.(*InstanceService)
			if req.ServiceGroup == nil {
				req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
			}
			if svc.Metro != "" && svc.Metro != metro {
				return nil, fmt.Errorf("cannot create instance: metro mismatch between service (%q) and instance (%q)", svc.Metro, metro)
			}
			req.ServiceGroup.Name = ptr.NilIfZero(svc.Name)
			req.ServiceGroup.Uuid = ptr.NilIfZero(svc.UUID)
		case "service.services":
			services := field.Create.Set.([]*Service)
			if len(services) > 0 {
				if req.ServiceGroup == nil {
					req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
				}
				for _, svc := range services {
					req.ServiceGroup.Services = append(req.ServiceGroup.Services, platform.Service{
						Port:            svc.Source,
						DestinationPort: &svc.Destination,
						Handlers:        svc.Handlers,
					})
				}
			}
		case "service.domains":
			domains := field.Create.Set.([]Domain)
			if len(domains) > 0 {
				if req.ServiceGroup == nil {
					req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
				}
				for _, domain := range domains {
					name := domain.Name
					if name == "" {
						name = domain.FQDN + "."
					}
					req.ServiceGroup.Domains = append(req.ServiceGroup.Domains, platform.CreateInstanceRequestDomain{
						Name: name,
					})
				}
			}
		case "service.soft-limit":
			limit := field.Create.Set.(uint32)
			if limit > 0 {
				if req.ServiceGroup == nil {
					req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
				}
				req.ServiceGroup.SoftLimit = &limit
			}
		case "service.hard-limit":
			limit := field.Create.Set.(uint32)
			if limit > 0 {
				if req.ServiceGroup == nil {
					req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
				}
				req.ServiceGroup.HardLimit = &limit
			}
		case "autostart":
			autostart := field.Create.Set.(bool)
			req.Autostart = &autostart
		case "replicas":
			replicas := field.Create.Set.(int64)
			req.Replicas = &replicas
		case "wait-timeout":
			timeout := field.Create.Set.(types.DurationS)
			req.TimeoutS = new(int64(timeout))
		case "features":
			features := field.Create.Set.([]string)
			for _, f := range features {
				req.Features = append(req.Features, platform.CreateInstanceRequestFeatures(f))
			}
		case "vsock":
			vsock := field.Create.Set.(bool)
			dt, err := json.Marshal(vsock)
			if err != nil {
				return nil, err
			}
			if req.AdditionalProperties == nil {
				req.AdditionalProperties = make(map[string]json.RawMessage)
			}
			req.AdditionalProperties["vsock"] = dt
		}
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := group.CollectMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) (multimetro.Keys, error) {
		log.G(ctx).Trace().Msg("creating instance")
		resp, err := c.CreateInstance(ctx, req)
		if err != nil {
			return nil, err
		}
		if len(resp.Data.Instances) == 0 {
			return nil, fmt.Errorf("no instances created")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Instances))
		for _, instance := range resp.Data.Instances {
			key := multimetro.Key{
				Metro: c.Metro.Name,
				UUID:  ptr.ZeroIfNil(instance.Uuid),
				Name:  ptr.ZeroIfNil(instance.Name),
			}
			created = append(created, key)
		}
		return created, nil
	})
	if err != nil {
		return nil, err
	}
	results, err := Instance{}.Get(ctx, keys.Strings())
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (Instance) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect an instance by name or UUID",
				Commands:    []string{"unikraft instance get demo-instance"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List instances across metros",
				Commands:    []string{"unikraft instance list"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Create a new instance",
				Commands: []string{
					// `unikraft instance create \
					//   --set name=demo-instance \
					//   --set metro=fra \
					//   --set image=nginx:latest \
					//   --set autostart=true \
					//   --set resources.memory=128 \
					//   --set resources.vcpus=1`,
					`unikraft instance create \
	  --name demo-instance \
	  --metro fra \
	  --image nginx:latest \
	  --autostart \
	  --memory 128 \
	  --vcpus 1`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Resize instance memory",
				Commands: []string{
					// "unikraft instance edit demo-instance --set resources.memory=256",
					"unikraft instance edit demo-instance --memory 256",
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete an instance by name or UUID",
				Commands:    []string{"unikraft instance delete demo-instance"},
			},
		},
	}
}

type InstancesLogsCmd struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to fetch logs for."`

	Prefix bool `help:"Prefix log lines with instance name." negatable:"" default:"true"`
	Tail   int  `help:"Number of lines to show from the end of the logs."`
	Follow bool `short:"f" help:"Follow log output."`
}

func (cmd InstancesLogsCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Fetch logs from an instance",
			Commands: []string{
				"unikraft instance logs my-instance",
			},
		},
		{
			Description: "Fetch the last 100 lines of logs from an instance",
			Commands: []string{
				"unikraft instance logs my-instance --tail 100",
			},
		},
		{
			Description: "Follow logs from an instance in real-time",
			Commands: []string{
				"unikraft instance logs my-instance --follow",
			},
		},
	}
}

func (cmd *InstancesLogsCmd) Run(ctx context.Context, stdio config.Stdio) error {
	// HACK: we resolve the keys early, so that we can assume that all the
	// instances actually exist (this is a potential race condition, but it's
	// acceptable for now)
	instances, err := Instance{}.Get(ctx, cmd.Targets)
	if err != nil {
		return err
	}
	keys := make(multimetro.Keys, 0, len(instances))
	for _, instance := range instances {
		key := instance.(Instance).key
		if key.Metro == "" {
			return fmt.Errorf("key %q not fully resolved", key)
		}
		keys = append(keys, key)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	mux := muxreader.New()
	if !cmd.Prefix {
		mux.DisablePrefix()
	}
	defer mux.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = group.DoRefs(ctx, g, keys.Refs(), func(_ context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		for _, ref := range refs {
			key := multimetro.Key(ref)
			r, err := logs.InstanceLogs(ctx, c).Reader(ref.NameOrUUID(), cmd.Tail, cmd.Follow)
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
	if cmd.Follow && errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

type InstancesStartCmd struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to start."`

	cmd.FormatOpts
}

func (cmd InstancesStartCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Start an instance",
			Commands: []string{
				"unikraft instance start demo-instance",
			},
		},
	}
}

func (c *InstancesStartCmd) Run(ctx context.Context, stdio config.Stdio) error {
	keys := multimetro.ParseKeys(c.Targets)
	before, opErr := Instance{}.Get(ctx, keys.Strings())
	if opErr != nil && len(before) == 0 {
		return opErr
	}
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	var targetKeys multimetro.Keys
	for _, res := range before {
		targetKeys = append(targetKeys, res.(Instance).key)
	}
	if len(targetKeys) == 0 {
		targetKeys = keys
	}

	started, startErr := startInstances(ctx, g, targetKeys)
	opErr = errors.Join(opErr, startErr)
	if len(started) == 0 {
		return opErr
	}

	updated, getErr := Instance{}.Get(ctx, started.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		return opErr
	}

	keySet := make(map[string]struct{}, len(started))
	for _, k := range started {
		keySet[k.Canonical()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})

	diffErr := cmd.Diff(ctx, stdio.Stdout, c.FormatOpts, Instance{}, before, updated)
	return errors.Join(opErr, diffErr)
}

type InstancesStopCmd struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to stop."`
	StopOpts

	cmd.FormatOpts
}

func (cmd InstancesStopCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Stop an instance",
			Commands: []string{
				"unikraft instance stop demo-instance",
			},
		},
		{
			Description: "Stop with a drain timeout",
			Commands: []string{
				"unikraft instance stop demo-instance --drain-timeout 30000",
			},
		},
		{
			Description: "Force stop an instance",
			Commands: []string{
				"unikraft instance stop demo-instance --force",
			},
		},
	}
}

func (c *InstancesStopCmd) Run(ctx context.Context, stdio config.Stdio) error {
	keys := multimetro.ParseKeys(c.Targets)
	before, opErr := Instance{}.Get(ctx, keys.Strings())
	if opErr != nil && len(before) == 0 {
		return opErr
	}
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	var targetKeys multimetro.Keys
	for _, res := range before {
		targetKeys = append(targetKeys, res.(Instance).key)
	}
	if len(targetKeys) == 0 {
		targetKeys = keys
	}
	stopped, stopErr := stopInstances(ctx, g, targetKeys, c.StopOpts)
	opErr = errors.Join(opErr, stopErr)
	if len(stopped) == 0 {
		return opErr
	}

	updated, getErr := Instance{}.Get(ctx, stopped.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		return opErr
	}

	keySet := make(map[string]struct{}, len(stopped))
	for _, k := range stopped {
		keySet[k.Canonical()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})

	diffErr := cmd.Diff(ctx, stdio.Stdout, c.FormatOpts, Instance{}, before, updated)
	return errors.Join(opErr, diffErr)
}

type InstancesRestartCmd struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to restart."`
	StopOpts

	cmd.FormatOpts
}

func (cmd InstancesRestartCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Restart an instance",
			Commands: []string{
				"unikraft instance restart demo-instance",
			},
		},
		{
			Description: "Force restart an instance",
			Commands: []string{
				"unikraft instance restart demo-instance --force",
			},
		},
	}
}

func (c *InstancesRestartCmd) Run(ctx context.Context, stdio config.Stdio) error {
	keys := multimetro.ParseKeys(c.Targets)
	before, opErr := Instance{}.Get(ctx, keys.Strings())
	if opErr != nil && len(before) == 0 {
		return opErr
	}
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	var targetKeys multimetro.Keys
	for _, res := range before {
		targetKeys = append(targetKeys, res.(Instance).key)
	}

	stopped, stopErr := stopInstances(ctx, g, targetKeys, c.StopOpts)
	opErr = errors.Join(opErr, stopErr)
	if len(stopped) == 0 {
		return opErr
	}

	started, startErr := startInstances(ctx, g, stopped)
	opErr = errors.Join(opErr, startErr)
	if len(started) == 0 {
		return opErr
	}

	updated, getErr := Instance{}.Get(ctx, started.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		return opErr
	}

	keySet := make(map[string]struct{}, len(started))
	for _, k := range started {
		keySet[k.Canonical()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})

	diffErr := cmd.Diff(ctx, stdio.Stdout, c.FormatOpts, Instance{}, before, updated)
	return errors.Join(opErr, diffErr)
}

type StopOpts struct {
	Force        bool             `help:"Force stop the instance immediately."`
	DrainTimeout types.DurationMS `help:"Timeout in milliseconds for draining connections before stopping." default:"-1"`
}

func (args *StopOpts) toReq(nameOrUUID platform.NameOrUUID) platform.StopInstancesRequestItem {
	req := platform.StopInstancesRequestItem{
		Uuid: nameOrUUID.Uuid,
		Name: nameOrUUID.Name,
	}
	if args.Force {
		req.Force = &args.Force
	}
	if args.DrainTimeout >= 0 {
		timeout := uint64(args.DrainTimeout)
		req.DrainTimeoutMs = &timeout
	}
	return req
}

func startInstances(ctx context.Context, g *group.Group[multimetro.MetroClient], keys multimetro.Keys) (multimetro.Keys, error) {
	started, err := group.CollectRefsSlices(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]multimetro.Key, group.Refs, error) {
		log.G(ctx).Trace().Msg("starting instances")
		resp, err := c.StartInstances(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var started multimetro.Keys
		for _, instance := range resp.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			started = append(started, multimetro.Key{
				Metro: c.Metro.Name,
				Name:  *instance.Name,
				UUID:  *instance.Uuid,
			})
		}
		return started, started.Refs(), nil
	})
	return multimetro.Keys(started), err
}

func stopInstances(ctx context.Context, g *group.Group[multimetro.MetroClient], keys multimetro.Keys, opts StopOpts) (multimetro.Keys, error) {
	stopped, err := group.CollectRefsSlices(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]multimetro.Key, group.Refs, error) {
		log.G(ctx).Trace().Msg("stopping instances")
		reqs := make([]platform.StopInstancesRequestItem, 0, len(refs))
		for _, key := range refs {
			reqs = append(reqs, opts.toReq(key.NameOrUUID()))
		}
		resp, err := c.StopInstances(ctx, reqs)
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var stopped multimetro.Keys
		for _, instance := range resp.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			stopped = append(stopped, multimetro.Key{
				Metro: c.Metro.Name,
				Name:  *instance.Name,
				UUID:  *instance.Uuid,
			})
		}
		return stopped, stopped.Refs(), nil
	})
	return multimetro.Keys(stopped), err
}

const tunnelDeprecatedImage = "official/utils/tunnel:latest"

type InstancesTunnelCmd struct {
	Targets []string `arg:"" name:"target" min:"1" help:"Forwarding target(s) in the form [LOCAL_PORT:]<INSTANCE|PRIVATE_IP|PRIVATE_FQDN>:DEST_PORT[/TYPE]."`

	TunnelProxyPorts   []string `short:"p" name:"tunnel-proxy-port"   help:"Remote port(s) exposed by the tunnel service. When a single value is given it is used as the starting port for multiple targets. (default: 4444)"`
	ProxyControlPort   uint     `short:"P" name:"tunnel-control-port" help:"Command-and-control port of the tunnel service." default:"4443"`
	TunnelServiceImage string   `          name:"tunnel-image"         help:"Image to use for the tunnel service." default:"official/utils/tunnel:1.0"`

	// parsedProxyPorts holds the validated proxy port numbers.
	parsedProxyPorts []uint16
	// instances holds the target instance identifiers; entries are replaced by
	// their private IPs once resolved.
	instances []string
	// localPorts to listen on locally.
	localPorts []uint16
	// ctypes holds the connection type ("tcp"/"udp") per target.
	ctypes []string
	// instanceProxyPorts holds the port on the instance to forward to.
	instanceProxyPorts []uint16
	// exposedProxyPorts holds the port exposed by the tunnel service per target.
	exposedProxyPorts []uint16
	// portIterator tracks the next port offset when a single proxy port is given.
	portIterator uint16
}

func (cmd InstancesTunnelCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Forward local port 8080 to instance \"nginx\" port 8080",
			Commands:    []string{"unikraft instance tunnel nginx:8080"},
		},
		{
			Description: "Forward to an instance identified by its private FQDN",
			Commands:    []string{"unikraft instance tunnel nginx.internal:8080"},
		},
		{
			Description: "Forward local port 8333 to instance \"nginx\" port 8080",
			Commands:    []string{"unikraft instance tunnel 8333:nginx:8080"},
		},
		{
			Description: "Forward multiple ports from multiple instances",
			Commands:    []string{"unikraft instance tunnel 8080:my-instance1:8080/tcp 8443:my-instance2:8080/tcp"},
		},
		{
			Description: "Forward local port 8080 to instance \"my-instance1\" port 8080 on fra metro using TCP",
			Commands:    []string{"unikraft instance tunnel 8080:fra/my-instance1:8080/tcp"},
		},
		{
			Description: "Use a custom relay port to avoid collisions",
			Commands:    []string{"unikraft instance tunnel -p 5500 my-instance:8080"},
		},
	}
}

func (cmd *InstancesTunnelCmd) Run(ctx context.Context, stdio config.Stdio) error {
	if cmd.TunnelServiceImage == tunnelDeprecatedImage {
		return fmt.Errorf("the image %q is deprecated, please use the default image", tunnelDeprecatedImage)
	}

	// Default to proxy port 4444 if none given.
	if len(cmd.TunnelProxyPorts) == 0 {
		cmd.TunnelProxyPorts = []string{"4444"}
	}

	for _, port := range cmd.TunnelProxyPorts {
		parsed, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return fmt.Errorf("%q is not a valid port number", port)
		}
		cmd.parsedProxyPorts = append(cmd.parsedProxyPorts, uint16(parsed))
	}

	if len(cmd.TunnelProxyPorts) > 1 && len(cmd.TunnelProxyPorts) != len(cmd.Targets) {
		return fmt.Errorf("number of proxy ports (%d) must match the number of forwarding targets (%d)", len(cmd.TunnelProxyPorts), len(cmd.Targets))
	}

	if err := cmd.tunnelParseArgs(ctx, cmd.Targets); err != nil {
		return fmt.Errorf("could not parse targets: %w", err)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	rawInstances := slices.Clone(cmd.instances)

	// Resolve instance names/UUIDs to private IPs, inferring the metro when needed.
	// Targets that are already IP addresses or private FQDNs (*.internal) are left as-is.
	var namesToResolve []string
	var indexesToResolve []int
	var metro string
	for i, inst := range cmd.instances {
		if net.ParseIP(inst) == nil && !strings.HasSuffix(inst, ".internal") {
			namesToResolve = append(namesToResolve, inst)
			indexesToResolve = append(indexesToResolve, i)
		}
	}
	if len(namesToResolve) > 0 {
		resolved, err := Instance{}.Get(ctx, namesToResolve)
		if err != nil {
			return fmt.Errorf("could not resolve instances: %w", err)
		}
		for i, r := range resolved {
			inst := r.(Instance)
			metro = inst.key.Metro
			if len(inst.Instance.NetworkInterfaces) == 0 || inst.Instance.NetworkInterfaces[0].PrivateIp == nil {
				return fmt.Errorf("instance %q has no private IP", namesToResolve[i])
			}
			cmd.instances[indexesToResolve[i]] = *inst.Instance.NetworkInterfaces[0].PrivateIp
		}
	}

	if metro == "" {
		return fmt.Errorf("could not determine target metro: include the metro prefix in the instance name (e.g. fra/my-instance:8080)")
	}

	authStr, err := tunnelGenRandAuth()
	if err != nil {
		return fmt.Errorf("could not generate auth string: %w", err)
	}

	instArgs := cmd.tunnelFormatProxyArgs(authStr)

	instUUID, sgFQDN, err := cmd.tunnelRunProxy(ctx, g, metro, instArgs)
	if err != nil {
		return fmt.Errorf("could not start tunnel proxy: %w", err)
	}

	defer func() {
		if err := tunnelTerminateProxy(context.WithoutCancel(ctx), g, metro, instUUID); err != nil {
			log.G(ctx).Error().Err(err).Msg("could not terminate tunnel proxy")
		}
	}()

	// Start the control relay to keep the tunnel connection alive.
	cr := tunnelRelay{
		rAddr:  net.JoinHostPort(sgFQDN, strconv.FormatUint(uint64(cmd.ProxyControlPort), 10)),
		auth:   authStr,
		stderr: stdio.Stderr,
	}
	ready := make(chan struct{}, 1)
	go func() {
		if err := cr.controlUp(ctx, ready); err != nil {
			log.G(ctx).Error().Err(err).Msg("control relay error")
		}
	}()
	// Wait for the control relay to establish its connection before sending traffic.
	<-ready

	r := tunnelRelay{
		// TODO(antoineco): allow dual-stack by creating two separate listeners.
		// Alternatively, we could default to "::" to create a tcp46 socket, but
		// listening on all addresses is an insecure default.
		lAddr: net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(cmd.localPorts[0]), 10)),
		rAddr: net.JoinHostPort(sgFQDN, strconv.FormatUint(uint64(cmd.exposedProxyPorts[0]), 10)),
		// NOTE(craciunoiuc): Only TCP is supported at the moment. This refers to the
		// local listener; the remote side always uses TLS-over-TCP.
		ctype:    cmd.ctypes[0],
		auth:     authStr,
		name:     instUUID,
		nameAddr: fmt.Sprintf("%s:%d", rawInstances[0], cmd.instanceProxyPorts[0]),
		stderr:   stdio.Stderr,
	}

	for i := range cmd.localPorts {
		if i == 0 {
			continue
		}
		pr := tunnelRelay{
			lAddr:    net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(cmd.localPorts[i]), 10)),
			rAddr:    net.JoinHostPort(sgFQDN, strconv.FormatUint(uint64(cmd.exposedProxyPorts[i]), 10)),
			ctype:    cmd.ctypes[i],
			auth:     authStr,
			name:     instUUID,
			nameAddr: fmt.Sprintf("%s:%d", rawInstances[i], cmd.instanceProxyPorts[i]),
			stderr:   stdio.Stderr,
		}
		go func() {
			if err := pr.up(ctx); err != nil {
				log.G(ctx).Error().Err(err).Msg("relay error")
			}
		}()
	}

	return r.up(ctx)
}

// tunnelGeneratePort returns the next sequential proxy port for single-port mode.
func (cmd *InstancesTunnelCmd) tunnelGeneratePort(startPort uint16) uint16 {
	defer func() { cmd.portIterator++ }()
	return startPort + cmd.portIterator
}

// tunnelParseArgs parses the positional forwarding target arguments and populates
// the cmd fields (instances, localPorts, ctypes, instanceProxyPorts, exposedProxyPorts).
func (cmd *InstancesTunnelCmd) tunnelParseArgs(ctx context.Context, args []string) error {
	for i, arg := range args {
		instance, lport, rport, ctype, err := tunnelParsePorts(ctx, arg)
		if err != nil {
			return err
		}
		cmd.instances = append(cmd.instances, instance)
		cmd.localPorts = append(cmd.localPorts, lport)
		cmd.instanceProxyPorts = append(cmd.instanceProxyPorts, rport)
		cmd.ctypes = append(cmd.ctypes, ctype)
		if len(cmd.parsedProxyPorts) == 1 {
			cmd.exposedProxyPorts = append(cmd.exposedProxyPorts, cmd.tunnelGeneratePort(cmd.parsedProxyPorts[0]))
		} else {
			cmd.exposedProxyPorts = append(cmd.exposedProxyPorts, cmd.parsedProxyPorts[i])
		}
	}
	return nil
}

// tunnelFormatProxyArgs formats the arguments to pass to the tunnel service instance.
func (cmd *InstancesTunnelCmd) tunnelFormatProxyArgs(authStr string) []string {
	connections := make([]string, 0, len(cmd.instances))
	for i := range cmd.instances {
		connections = append(connections, fmt.Sprintf("TCP2%s:%s:%d:%d:%d",
			strings.ToUpper(cmd.ctypes[i]),
			cmd.instances[i],
			cmd.instanceProxyPorts[i],
			cmd.exposedProxyPorts[i],
			27,
		))
	}
	return []string{
		// HEARTBEAT_PORT:CTLR_AUTH_TIMEOUT
		fmt.Sprintf("%d:%d", cmd.ProxyControlPort, 5),
		// AUTH_TIMEOUT:AUTH_COOKIE
		fmt.Sprintf("%d:%s", 5, authStr),
		// EVS_TIMEOUT
		"600",
		// [CONNSTR0|CONNSTR1|...]
		"[" + strings.Join(connections, "|") + "]",
	}
}

// tunnelRunProxy creates the tunnel service instance and returns its UUID and
// service group FQDN.
func (cmd *InstancesTunnelCmd) tunnelRunProxy(ctx context.Context, g *group.Group[multimetro.MetroClient], metro string, args []string) (instUUID, sgFQDN string, err error) {
	services := make([]platform.Service, 0, len(cmd.exposedProxyPorts)+1)
	for _, port := range cmd.exposedProxyPorts {
		p := uint32(port)
		services = append(services, platform.Service{
			Port:            p,
			DestinationPort: &p,
			Handlers:        []platform.ServiceHandlers{platform.ServiceHandlersTls},
		})
	}
	ctrlPort := uint32(cmd.ProxyControlPort)
	services = append(services, platform.Service{
		Port:            ctrlPort,
		DestinationPort: &ctrlPort,
		Handlers:        []platform.ServiceHandlers{platform.ServiceHandlersTls},
	})

	image := cmd.TunnelServiceImage
	memMB := int64(64)
	autostart := true
	timeoutS := int64(3)
	req := platform.CreateInstanceRequest{
		Image:    &image,
		MemoryMb: &memMB,
		Args:     args,
		ServiceGroup: &platform.CreateInstanceRequestServiceGroup{
			Services: services,
		},
		Autostart: &autostart,
		TimeoutS:  &timeoutS,
		Features:  []platform.CreateInstanceRequestFeatures{
			// TODO(craciunoiuc): Enable back when sdk is updated
			// platform.CreateInstanceRequestFeaturesDelete_on_stop,
		},
	}

	type proxyInfo struct{ uuid, fqdn string }
	info, err := group.CollectMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) (proxyInfo, error) {
		log.G(ctx).Trace().Msg("creating tunnel proxy instance")
		resp, err := c.CreateInstance(ctx, req)
		if err != nil {
			return proxyInfo{}, fmt.Errorf("creating proxy instance: %w", err)
		}
		if resp.Data == nil || len(resp.Data.Instances) == 0 {
			return proxyInfo{}, fmt.Errorf("no instance returned after creation")
		}
		inst := resp.Data.Instances[0]
		uuid := ptr.ZeroIfNil(inst.Uuid)

		var fqdn string
		if inst.ServiceGroup != nil && len(inst.ServiceGroup.Domains) > 0 {
			fqdn = ptr.ZeroIfNil(inst.ServiceGroup.Domains[0].Fqdn)
		}
		if fqdn == "" {
			return proxyInfo{}, fmt.Errorf("tunnel proxy has no service group domain")
		}
		return proxyInfo{uuid: uuid, fqdn: fqdn}, nil
	})
	if err != nil {
		return "", "", err
	}
	return info.uuid, info.fqdn, nil
}

// tunnelParsePorts parses a single forwarding target of the form
// [LOCAL_PORT:]INSTANCE:DEST_PORT[/TYPE] and returns the components.
func tunnelParsePorts(ctx context.Context, portsArg string) (instance string, lport, rport uint16, ctype string, err error) {
	var rest string
	parts := strings.SplitN(portsArg, "/", 3)
	if len(parts) == 3 {
		rest = parts[0] + "/" + parts[1]
		ctype = parts[2]
	} else if len(parts) == 2 {
		if strings.EqualFold(parts[1], "tcp") || strings.EqualFold(parts[1], "udp") {
			// It's missing the metro
			rest = parts[0]
			ctype = parts[1]
		} else {
			// It's missing the ctype
			rest = parts[0] + "/" + parts[1]
			ctype = "tcp"
		}
	} else {
		rest = parts[0]
		ctype = "tcp"
	}
	if strings.ToLower(ctype) != "tcp" {
		log.G(ctx).Warn().Msg("only TCP connections are supported at the moment")
	}

	segments := strings.SplitN(rest, ":", 3)
	switch len(segments) {
	case 2:
		// INSTANCE:DEST_PORT — no local port override
		if _, parseErr := strconv.ParseUint(segments[0], 10, 16); parseErr == nil {
			return "", 0, 0, "", fmt.Errorf("%q is not a valid instance identifier", segments[0])
		}
		rport64, parseErr := strconv.ParseUint(segments[1], 10, 16)
		if parseErr != nil {
			return "", 0, 0, "", fmt.Errorf("%q is not a valid port number", segments[1])
		}
		return segments[0], uint16(rport64), uint16(rport64), ctype, nil
	case 3:
		// LOCAL_PORT:INSTANCE:DEST_PORT
		lport64, parseErr := strconv.ParseUint(segments[0], 10, 16)
		if parseErr != nil {
			return "", 0, 0, "", fmt.Errorf("%q is not a valid port number", segments[0])
		}
		rport64, parseErr := strconv.ParseUint(segments[2], 10, 16)
		if parseErr != nil {
			return "", 0, 0, "", fmt.Errorf("%q is not a valid port number", segments[2])
		}
		return segments[1], uint16(lport64), uint16(rport64), ctype, nil
	default:
		return "", 0, 0, "", fmt.Errorf("%q is not a valid forwarding target (expected [LOCAL_PORT:]INSTANCE:DEST_PORT[/TYPE])", portsArg)
	}
}

// tunnelGenRandAuth generates a 32-character random alphanumeric string used to
// authenticate connections to the tunnel service.
func tunnelGenRandAuth() (string, error) {
	chars := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
	max := big.NewInt(int64(len(chars)))
	var sb strings.Builder
	sb.Grow(32)
	for range 32 {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		sb.WriteByte(chars[idx.Int64()])
	}
	return sb.String(), nil
}

// tunnelTerminateProxy deletes the tunnel proxy instance.
func tunnelTerminateProxy(ctx context.Context, g *group.Group[multimetro.MetroClient], metro, uuid string) error {
	return group.DoMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) error {
		log.G(ctx).Trace().Msg("deleting tunnel proxy instance")
		_, err := c.DeleteInstances(ctx, []platform.NameOrUUID{{Uuid: &uuid}})
		if err != nil {
			return fmt.Errorf("deleting proxy instance %q: %w", uuid, err)
		}
		return nil
	})
}

// tunnelRelay relays connections from a local listener to a remote host over TLS.
type tunnelRelay struct {
	lAddr    string
	rAddr    string
	ctype    string
	auth     string
	name     string
	nameAddr string
	stderr   io.Writer
}

const tunnelHeartbeat = "\xf0\x9f\x91\x8b\xf0\x9f\x90\x92\x00"

var (
	tunnelNoNetTimeout       = time.Time{}
	tunnelImmediateNetCancel = time.Unix(1, 0)
)

// up starts a local listener and relays accepted connections to the remote host.
func (r *tunnelRelay) up(ctx context.Context) error {
	l, err := r.listenLocal(ctx)
	if err != nil {
		return err
	}
	defer l.Close()
	go func() { <-ctx.Done(); l.Close() }()

	log.G(ctx).Info().Str("from", l.Addr().String()).Str("to", r.nameAddr).Msg("tunnelling")
	log.G(ctx).Debug().Str("via", r.rAddr).Msg("tunnelling")

	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accepting incoming connection: %w", err)
		}
		c := &tunnelConnection{relay: r, conn: conn}
		go c.handle(ctx, []byte(r.auth), r.name, r.nameAddr)
	}
}

// controlUp dials the remote control port, signals ready, then sends periodic
// heartbeats to keep the tunnel service alive.
func (r *tunnelRelay) controlUp(ctx context.Context, ready chan struct{}) error {
	rc, err := r.dialRemote(ctx)
	if err != nil {
		return err
	}
	defer rc.Close()
	go func() { <-ctx.Done(); rc.Close() }()

	ready <- struct{}{}
	close(ready)

	// Send auth and initial heartbeat.
	_, err = io.CopyN(rc, bytes.NewReader([]byte(r.auth+tunnelHeartbeat)), int64(len(r.auth)+9))
	if err != nil {
		return err
	}
	// Send a heartbeat every minute to keep the connection alive.
	for {
		time.Sleep(time.Minute)
		_, err := io.CopyN(rc, bytes.NewReader([]byte(tunnelHeartbeat)), 9)
		if err != nil {
			return err
		}
	}
}

func (r *tunnelRelay) dialRemote(ctx context.Context) (net.Conn, error) {
	var d tls.Dialer
	return d.DialContext(ctx, "tcp4", r.rAddr)
}

func (r *tunnelRelay) listenLocal(ctx context.Context) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, r.ctype+"4", r.lAddr)
}

// tunnelConnection represents an accepted local connection being relayed to a
// remote host through the tunnel service.
type tunnelConnection struct {
	relay *tunnelRelay
	conn  net.Conn
}

// handle relays data between the local connection and the remote host.
func (c *tunnelConnection) handle(ctx context.Context, auth []byte, instance, instanceRaw string) {
	defer func() {
		c.conn.Close()
		log.G(ctx).Info().Str("for", instanceRaw).Msg("closed connection")
	}()

	rc, err := c.relay.dialRemote(ctx)
	if err != nil {
		log.G(ctx).Error().Err(err).Msg("failed to connect to remote host")
		return
	}
	defer rc.Close()

	log.G(ctx).Debug().
		Str("for", c.conn.RemoteAddr().String()).
		Str("from", rc.LocalAddr().String()).
		Str("to", rc.RemoteAddr().String()).
		Msg("opened connection")
	log.G(ctx).Info().Str("to", instanceRaw).Msg("accepted connection")

	_ = rc.SetDeadline(tunnelNoNetTimeout)
	_ = c.conn.SetDeadline(tunnelNoNetTimeout)

	defer func() {
		_ = c.conn.SetDeadline(tunnelImmediateNetCancel)
	}()

	if len(auth) > 0 {
		_, err = rc.Write(auth)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to write auth to remote host")
			return
		}

		statusRaw := bytes.NewBuffer(nil)
		n, err := io.CopyN(statusRaw, rc, 2)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to read auth status from remote host")
			return
		}
		if n != 2 {
			log.G(ctx).Error().Msg("invalid auth status from remote host")
			return
		}

		var status int16
		if err = binary.Read(statusRaw, binary.LittleEndian, &status); err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to parse auth status from remote host")
			return
		}

		if status == 0 {
			log.G(ctx).Error().Msg("no available connections to remote host, try again later")
			return
		} else if status < 0 {
			log.G(ctx).Error().Msgf("internal tunnel error (C=%d), to view logs run:", status)
			fmt.Fprintf(c.relay.stderr, "\n    unikraft instance logs %s\n\n", instance)
			return
		}
	}

	writerDone := make(chan struct{})
	go func() {
		defer func() {
			_ = rc.SetDeadline(tunnelImmediateNetCancel)
			writerDone <- struct{}{}
		}()
		_, err = io.Copy(rc, c.conn)
		if err != nil && !tunnelIsNetClosedError(err) && !tunnelIsNetTimeoutError(err) {
			log.G(ctx).Error().Err(err).Msg("failed to copy data from client to remote host")
		}
	}()

	_, err = io.Copy(c.conn, rc)
	if err != nil {
		if !tunnelIsNetTimeoutError(err) {
			log.G(ctx).Error().Err(err).Msg("failed to copy data from remote host to client")
		}
	} else {
		// Remote closed the connection cleanly; return to close our side.
		return
	}

	<-writerDone
}

func tunnelIsNetTimeoutError(err error) bool {
	var neterr net.Error
	return errors.As(err, &neterr) && neterr.Timeout()
}

func tunnelIsNetClosedError(err error) bool {
	return strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), "connection reset by peer")
}
