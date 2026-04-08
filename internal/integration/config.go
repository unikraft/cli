// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"sync"
	"testing"

	"github.com/mitchellh/copystructure"
	"unikraft.com/cli/internal/config"
)

func LoadConfig(t *testing.T) (*Config, error) {
	return populate()
}

type Config struct {
	Config  *config.Config
	Profile *config.Profile

	Metro     *config.Metro
	MetroName string
}

var (
	cfg     *Config
	once    sync.Once
	onceErr error
)

func populate() (*Config, error) {
	once.Do(func() {
		path, err := config.ConfigFilePath()
		if err != nil {
			onceErr = err
			return
		}
		baseCfg, err := config.Load(path)
		if err != nil || baseCfg == nil {
			onceErr = err
			return
		}

		profile, err := baseCfg.CurrentProfile()
		if err != nil {
			onceErr = err
			return
		}

		profile.Name = "default"
		if len(profile.Metros) == 0 {
			return
		}
		profile.ControlPlane = ""
		profile.Metros = profile.Metros[:1]
		profile.Metros[0].Name = "test"
		profile.Metros[0].Country = "xx"

		config := &config.Config{
			DefaultProfile: profile.Name,
			Profiles:       map[string]config.Profile{profile.Name: *profile},
		}

		cfg = &Config{
			Config:    config,
			Profile:   profile,
			Metro:     &profile.Metros[0],
			MetroName: profile.Metros[0].Name,
		}
	})
	if onceErr != nil {
		return nil, onceErr
	}

	cloned, err := copystructure.Copy(cfg)
	if err != nil {
		return nil, err
	}
	return cloned.(*Config), nil
}
