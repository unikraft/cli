// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"slices"
	"sort"

	jujuerrors "github.com/juju/errors"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/tui/selector"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
)

type ProfileCmd struct {
	cmd.ResourceCmd[Profile]
	cmd.GettableResourceCmd[Profile]
	cmd.ListableResourceCmd[Profile]

	Use UseCmd `cmd:"" help:"Switch between profiles."`
}

type Profile struct {
	Name   string `field:",short" json:"name"`
	Active bool   `field:",short" json:"active"`

	Metros []string `field:",short" json:"metros"`
}

func (Profile) Type() resource.Type {
	return resource.Type{
		Name:  "profile",
		Names: "profiles",
	}
}

func (i Profile) Key() resource.Key {
	return staticKey(i.Name)
}

func (i Profile) Raw() any {
	return i
}

func (i Profile) Fields() ([]resource.Field, error) {
	return resource.FieldsFromStruct(i)
}

func (Profile) List(ctx context.Context) ([]resource.Resource, error) {
	cfg := config.G(ctx)
	profiles := cfg.Profiles

	var results []resource.Resource
	for _, profile := range profiles {
		metroNames := make([]string, 0, len(profile.Metros))
		for _, metro := range profile.Metros {
			metroNames = append(metroNames, metro.Name)
		}

		result := Profile{
			Name:   profile.Name,
			Active: profile.Name == cfg.DefaultProfile,
			Metros: metroNames,
		}
		results = append(results, result)
	}
	slices.SortFunc(results, func(a, b resource.Resource) int {
		return cmp.Compare(a.(Profile).Name, b.(Profile).Name)
	})
	return results, nil
}

func (Profile) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	return getFromListable(ctx, Profile{}, keys)
}

func (Profile) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect a profile by name",
				Commands:    []string{"unikraft profile get default"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all profiles",
				Commands:    []string{"unikraft profile list"},
			},
		},
	}
}

type UseCmd struct {
	Name string `arg:"" optional:"" help:"Target profile to switch to."`
}

// profileName is a simple wrapper to satisfy fmt.Stringer for the single selector.
type profileName string

func (p profileName) String() string { return string(p) }

func (cmd *UseCmd) Run(ctx context.Context, cfg *config.Config) error {
	name := cmd.Name

	if name == "" {
		names := make([]profileName, 0, len(cfg.Profiles))
		for k := range cfg.Profiles {
			names = append(names, profileName(k))
		}
		sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })

		selected, err := selector.Single("Select a profile", names...)
		if err != nil {
			return jujuerrors.Annotate(err, "selecting profile")
		}
		name = string(selected)
	}

	if _, ok := cfg.Profiles[name]; !ok {
		return config.ErrProfileNotFound{Name: name}
	}
	cfg.DefaultProfile = name

	if err := cfg.Save(); err != nil {
		return jujuerrors.Annotate(err, "saving profile")
	}

	log.G(ctx).Info().
		Str("profile", name).
		Msg("switched")
	return nil
}
