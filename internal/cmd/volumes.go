// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/alecthomas/kong"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/patch"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
)

type VolumesCmd struct {
	cmd.ResourceCmd[Volume]
	cmd.GettableResourceCmd[Volume]
	cmd.WaitableResourceCmd[Volume]
	cmd.ListableResourceCmd[Volume]
	cmd.BulkDeletableResourceCmd[Volume]

	Create VolumeCreateCmd `cmd:"" help:"Create a volume."`
	Edit   VolumeEditCmd   `cmd:"" help:"Edit a volume."`

	Clone VolumesCloneCmd `cmd:"" help:"Clone a volume."`
}

// VolumeCreateCmd extends the generic resource create command with shortcut
// flags for commonly used volume fields. Each field tagged with
// `shortcut:"<path>"` is translated into a --set <path>=<value> entry before
// the standard create pipeline runs.
type VolumeCreateCmd struct {
	cmd.ResourceCreateCmd[Volume]

	Metro       string              `group:"flag-create" shortcut:"metro" help:"Metro to create in." placeholder:"metro" example:"fra,sfo,nyc"`
	Name        string              `group:"flag-create" shortcut:"name" short:"n" help:"Volume name." placeholder:"name"`
	Size        types.SizeMebibytes `group:"flag-create" shortcut:"size" help:"Volume size." placeholder:"size" example:"10GiB,100MiB"`
	Filesystem  string              `group:"flag-create" shortcut:"filesystem" help:"Volume filesystem." placeholder:"filesystem" example:"ext4"`
	QuotaPolicy string              `group:"flag-create" shortcut:"quota-policy" help:"Volume quota policy." placeholder:"quota-policy" example:"static,dynamic"`
}

func (c *VolumeCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	if err := defaultMetroFromProfile(ctx, &c.SetArgs); err != nil {
		return err
	}
	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

// VolumeEditCmd extends the generic resource edit command with shortcut
// flags for commonly used editable volume fields. Each field tagged with
// `shortcut:"<path>"` is translated into a --set <path>=<value> entry before
// the standard edit pipeline runs.
type VolumeEditCmd struct {
	cmd.ResourceEditCmd[Volume]

	Size        types.SizeMebibytes `group:"flag-edit" shortcut:"size" help:"Volume size." placeholder:"size" example:"20GiB,100MiB"`
	QuotaPolicy string              `group:"flag-edit" shortcut:"quota-policy" help:"Volume quota policy." placeholder:"quota-policy" example:"static,dynamic"`
}

func (c *VolumeEditCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceEditCmd.Run(ctx, stdio, sandbox)
}

type VolumesCloneCmd struct {
	Source string `arg:"" completion-predictor:"resource-key-volume" help:"Name or UUID of the volume to clone."`

	Name string   `group:"flag-clone" shortcut:"name" short:"n" help:"New volume name." placeholder:"name"`
	Tags []string `group:"flag-clone" shortcut:"tags" help:"Volume tags." placeholder:"tag" example:"env=prod,team=platform"`

	cmd.SetArgs

	cmd.FormatOpts
}

func (VolumesCloneCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Clone a volume with a new name",
			Commands: []string{
				// "unikraft volume clone demo-volume --set name=demo-volume-clone",
				"unikraft volume clone demo-volume --name demo-volume-clone",
			},
		},
	}
}

func (c *VolumesCloneCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	spec := patch.PatchSpec{
		Set: make(map[string][]string),
	}
	if err := c.Apply(&spec); err != nil {
		return err
	}
	req := platform.CloneVolumesRequestItem{}
	var unknownFields []string
	for key, values := range spec.Set {
		switch key {
		case "name":
			name, err := value.Parse[string](values)
			if err != nil {
				return err
			}
			if name != "" {
				req.VolName = new(name)
			}
		case "tags":
			tags, err := value.Parse[[]string](values)
			if err != nil {
				return err
			}
			req.Tags = tags
		default:
			unknownFields = append(unknownFields, key)
		}
	}
	if len(unknownFields) > 0 {
		slices.Sort(unknownFields)
		return fmt.Errorf("unknown fields: %v", unknownFields)
	}

	gettable := sandbox.WrapGettable(Volume{})
	resources, err := gettable.Get(ctx, []string{c.Source})
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		return fmt.Errorf("volume not found: %s", c.Source)
	}
	if len(resources) > 1 {
		var keys []string
		for _, res := range resources {
			keys = append(keys, res.Key().String())
		}
		return fmt.Errorf("ambiguous volume: %s (found %v)", c.Source, keys)
	}

	volume, ok := resources[0].(Volume)
	if !ok {
		return fmt.Errorf("unexpected resource type %T", resources[0])
	}
	if volume.UUID == "" {
		return fmt.Errorf("volume %q is missing a UUID", volume.Name)
	}
	if volume.Metro == nil {
		return fmt.Errorf("volume %q has no metro information", volume.Name)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	req.Uuid = ptr.NilIfZero(volume.key.UUID)
	if req.Uuid == nil {
		req.Name = ptr.NilIfZero(volume.key.Name)
	}
	keys, opErr := group.CollectMetro(ctx, g, volume.Metro.Name, func(ctx context.Context, client multimetro.MetroClient) (multimetro.Keys, error) {
		log.G(ctx).Trace().Msg("cloning volume")
		resp, err := client.CloneVolumes(ctx, []platform.CloneVolumesRequestItem{req})
		if err != nil {
			return nil, err
		}
		if len(resp.Data.Volumes) == 0 {
			return nil, fmt.Errorf("no volumes cloned")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Volumes))
		for _, vol := range resp.Data.Volumes {
			created = append(created, multimetro.Key{
				Metro: client.Metro.Name,
				UUID:  ptr.ZeroIfNil(vol.Uuid),
				Name:  ptr.ZeroIfNil(vol.Name),
			})
		}
		return created, nil
	})
	if opErr != nil && len(keys) == 0 {
		return opErr
	}

	results, getErr := Volume{}.Get(ctx, keys.Strings())
	if getErr != nil && len(results) == 0 {
		return errors.Join(opErr, getErr)
	}
	if sandbox != nil {
		for _, res := range results {
			if err := sandbox.Add(ctx, res); err != nil {
				return err
			}
		}
	}

	printErr := c.Output.
		WithDefault(cmd.PrinterTypeKeyValue).
		Print(ctx, stdio.Stdout, c.Field, Volume{}, results...)
	if printErr != nil {
		return errors.Join(opErr, getErr, printErr)
	}
	return errors.Join(opErr, getErr)
}

type Volume struct {
	MetroName string `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string `mirror:"volume.name" field:",short" create:"set"`
	UUID      string `mirror:"volume.uuid" field:",long"`

	Tags []string `mirror:"volume.tags"`

	State       types.VolumeState   `mirror:"volume.state" field:",short"`
	Size        types.SizeMebibytes `mirror:"volume.size_mb" field:",short" create:"set,required" edit:"set"`
	Filesystem  string              `mirror:"volume.filesystem" field:",long" create:"set"`
	QuotaPolicy string              `mirror:"volume.quota_policy" field:"quota-policy,long" create:"set" edit:"set"`
	Persistent  bool                `mirror:"volume.persistent" field:",long"`

	Timestamps struct {
		Created types.RelativeTime `mirror:"volume.created_at" field:",short"`
	}

	AttachedTo []struct {
		Name string `mirror:"name" field:",long"`
		UUID string `mirror:"uuid" field:",long"`
	} `mirror:"volume.attached_to"`

	MountedBy []struct {
		Name     string `mirror:"name" field:",long"`
		UUID     string `mirror:"uuid" field:",long"`
		ReadOnly bool   `mirror:"read_only" field:",long"`
	} `mirror:"volume.mounted_by"`

	Volume platform.Volume `field:"-" json:"volume"`
	Metro  *config.Metro   `field:"-" json:"metro"`

	key multimetro.Key
}

func (Volume) Type() resource.Type {
	return resource.Type{
		Name:  "volume",
		Names: "volumes",
	}
}

func (i Volume) Key() resource.Key {
	return i.key
}

func (i Volume) Raw() any {
	return i.Volume
}

func (i Volume) Fields() ([]resource.Field, error) {
	result, err := resource.FieldsFromStruct(i)
	if err != nil {
		return nil, err
	}

	for key, field := range resource.IterFields(result) {
		switch {
		case key.String() == "metro":
			if i.MetroName != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "metro",
					Key:  i.MetroName,
				})
			}
		case key.MatchesString("attached-to.*"):
			// Add link to instance resource
			nameField, _ := field.Get("name")
			uuidField, _ := field.Get("uuid")
			name, _ := nameField.Value.(string)
			uuid, _ := uuidField.Value.(string)
			if name != "" || uuid != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "instance",
					Key: multimetro.Key{
						Metro: i.Metro.Name,
						Name:  name,
						UUID:  uuid,
					}.String(),
				})
			}
		case key.MatchesString("mounted-by.*"):
			// Add link to instance resource
			nameField, _ := field.Get("name")
			uuidField, _ := field.Get("uuid")
			name, _ := nameField.Value.(string)
			uuid, _ := uuidField.Value.(string)
			if name != "" || uuid != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "instance",
					Key: multimetro.Key{
						Metro: i.Metro.Name,
						Name:  name,
						UUID:  uuid,
					}.String(),
				})
			}
		}
	}

	return result, nil
}

func (Volume) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing volumes")
		resp, err := c.GetVolumes(ctx, nil, platform.GetVolumesOpts{Details: new(true)})
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		var errs []error
		for _, volume := range resp.Data.Volumes {
			result, err := Volume{}.load(nil, volume, &c.Metro)
			if err != nil {
				errs = append(errs, err)
			}
			results = append(results, result)
		}
		return results, errors.Join(errs...)
	})
}

func (Volume) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting volumes")
		resp, err := c.GetVolumes(ctx, refs.NameOrUUIDs(), platform.GetVolumesOpts{Details: new(true)})
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		var errs []error
		for i, volume := range resp.Data.Volumes {
			if volume.Status == nil || *volume.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			result, err := Volume{}.load(&refs[i], volume, &c.Metro)
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

func (Volume) load(ref *group.Ref, volume platform.Volume, metro *config.Metro) (Volume, error) {
	if ref == nil {
		ref = &group.Ref{
			Metro: metro.Name,
			Name:  ptr.ZeroIfNil(volume.Name),
			UUID:  ptr.ZeroIfNil(volume.Uuid),
		}
	} else {
		ref.Metro = cmp.Or(ref.Metro, metro.Name)
		ref.Name = cmp.Or(ref.Name, ptr.ZeroIfNil(volume.Name))
		ref.UUID = cmp.Or(ref.UUID, ptr.ZeroIfNil(volume.Uuid))
	}

	result := Volume{
		Volume: volume,
		Metro:  metro,
		key:    multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return Volume{}, fmt.Errorf("could not mirror volume data: %w", err)
	}
	return result, nil
}

func (Volume) Delete(ctx context.Context, targets []resource.Resource) error {
	keys := make(multimetro.Keys, 0, len(targets))
	for _, target := range targets {
		volume := target.(Volume)
		keys = append(keys, volume.key)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting volumes")
		resp, err := c.DeleteVolumes(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, volume := range resp.Data.Volumes {
			if volume.Status == nil || *volume.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name,
				Name:  ptr.ZeroIfNil(volume.Name),
				UUID:  ptr.ZeroIfNil(volume.Uuid),
			})
		}
		return deleted, nil
	})
}

func (Volume) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var req platform.CreateVolumeRequest
	var metro string
	for key, field := range resource.IterFields(fields) {
		if field.Create.Set != nil {
			switch key.String() {
			case "name":
				name := field.Create.Set.(string)
				req.Name = &name
			case "metro":
				metro = field.Create.Set.(string)
			case "size":
				size := field.Create.Set.(types.SizeMebibytes)
				sizeMb := uint64(size)
				req.SizeMb = &sizeMb
			case "filesystem":
				filesystem := field.Create.Set.(string)
				if filesystem != "" {
					data, err := json.Marshal(filesystem)
					if err != nil {
						return nil, err
					}
					// HACK: set on additional properties, since we need an updated SDK
					if req.AdditionalProperties == nil {
						req.AdditionalProperties = make(map[string]json.RawMessage)
					}
					req.AdditionalProperties["filesystem"] = data
				}
			case "quota-policy":
				quotaPolicy := field.Create.Set.(string)
				if quotaPolicy != "" {
					data, err := json.Marshal(quotaPolicy)
					if err != nil {
						return nil, err
					}
					// HACK: set on additional properties, since we need an updated SDK
					if req.AdditionalProperties == nil {
						req.AdditionalProperties = make(map[string]json.RawMessage)
					}
					req.AdditionalProperties["quota_policy"] = data
				}
			}
		}
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := group.CollectMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) (multimetro.Keys, error) {
		log.G(ctx).Trace().Msg("creating volume")
		resp, err := c.CreateVolume(ctx, req)
		if err != nil {
			return nil, err
		}
		if len(resp.Data.Volumes) == 0 {
			return nil, fmt.Errorf("no volumes created")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Volumes))
		for _, volume := range resp.Data.Volumes {
			key := multimetro.Key{
				Metro: c.Metro.Name,
				UUID:  ptr.ZeroIfNil(volume.Uuid),
				Name:  ptr.ZeroIfNil(volume.Name),
			}
			created = append(created, key)
		}
		return created, nil
	})
	if err != nil {
		return nil, err
	}
	results, err := Volume{}.Get(ctx, keys.Strings())
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (Volume) Edit(ctx context.Context, target resource.Resource, fields []resource.Field) (resource.Resource, error) {
	volume := target.(Volume)
	patches := patchRequests(fields, volumePatchSpec)
	reqs := make([]platform.UpdateVolumesRequestItem, 0, len(patches))
	for _, patch := range patches {
		reqs = append(reqs, platform.UpdateVolumesRequestItem{
			Uuid:  &volume.UUID,
			Op:    platform.UpdateVolumesRequestItemOp(patch.Op),
			Prop:  patch.Prop,
			Value: new(patch.Value),
		})
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	err = group.DoMetro(ctx, g, volume.key.Metro, func(ctx context.Context, c multimetro.MetroClient) error {
		log.G(ctx).Trace().Msg("updating volume")
		_, err := c.UpdateVolumes(ctx, reqs)
		return err
	})
	if err != nil {
		return nil, err
	}
	results, err := Volume{}.Get(ctx, []string{volume.Key().String()})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func (Volume) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect a volume by name or UUID",
				Commands:    []string{"unikraft volume get demo-volume"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all volumes",
				Commands:    []string{"unikraft volume list"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Create a new volume",
				Commands: []string{
					// `unikraft volume create \
					//   --set name=demo-volume \
					//   --set size=10 \
					//   --set metro=fra`,
					`unikraft volume create \
	  --name demo-volume \
	  --size 10 \
	  --metro fra`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Resize a volume",
				Commands: []string{
					// "unikraft volume edit demo-volume --set size=20",
					"unikraft volume edit demo-volume --size 20",
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a volume by name or UUID",
				Commands:    []string{"unikraft volume delete demo-volume"},
			},
		},
	}
}

func volumePatchSpec(path string, _ patchOp, value any) (platform.UpdateVolumesRequestItemProp, any) {
	var zero platform.UpdateVolumesRequestItemProp
	switch path {
	case "size":
		return platform.UpdateVolumesRequestItemPropSize_mb, int64(value.(types.SizeMebibytes))
	case "quota-policy":
		return platform.UpdateVolumesRequestItemPropQuota_policy, value.(string)
	default:
		return zero, nil
	}
}
