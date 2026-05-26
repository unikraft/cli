// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2025, The buildx authors.
// Licensed under the Apache License, Version 2.0 (the "License").
// You may not use this file except in compliance with the License.

// original code imported from github.com/docker/buildx/util/buildflags/

package buildflags

import (
	"fmt"
	"strings"

	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/session/sshforward/sshprovider"
	"github.com/tonistiigi/go-csvvalue"
)

type Secret struct {
	ID       string
	FilePath string
	Env      string
}

func (s *Secret) UnmarshalText(text []byte) error {
	value := string(text)
	fields, err := csvvalue.Fields(value, nil)
	if err != nil {
		return fmt.Errorf("failed to parse csv secret: %w", err)
	}

	*s = Secret{}

	var typ string
	for _, field := range fields {
		key, val, ok := strings.Cut(field, "=")
		if !ok {
			return fmt.Errorf("invalid field %q must be a key=value pair", field)
		}
		key = strings.ToLower(key)
		switch key {
		case "type":
			if val != "file" && val != "env" {
				return fmt.Errorf("unsupported secret type %q", val)
			}
			typ = val
		case "id":
			s.ID = val
		case "source", "src":
			s.FilePath = val
		case "env":
			s.Env = val
		default:
			return fmt.Errorf("unexpected key %q in %q", key, field)
		}
	}
	if typ == "env" && s.Env == "" {
		s.Env = s.FilePath
		s.FilePath = ""
	}
	return nil
}

func ParseSecretSpecs(sl []string) ([]*Secret, error) {
	fs := make([]*Secret, 0, len(sl))
	for _, v := range sl {
		if v == "" {
			continue
		}
		s, err := parseSecret(v)
		if err != nil {
			return nil, err
		}
		fs = append(fs, s)
	}
	return fs, nil
}

func parseSecret(value string) (*Secret, error) {
	var s Secret
	if err := s.UnmarshalText([]byte(value)); err != nil {
		return nil, err
	}
	return &s, nil
}

type SSH struct {
	ID    string
	Paths []string
}

func (s *SSH) UnmarshalText(text []byte) error {
	parts := strings.SplitN(string(text), "=", 2)

	s.ID = parts[0]
	if len(parts) > 1 {
		s.Paths = strings.Split(parts[1], ",")
	} else {
		s.Paths = nil
	}
	return nil
}

func ParseSSHSpecs(sl []string) ([]*SSH, error) {
	var outs []*SSH
	if len(sl) == 0 {
		return nil, nil
	}

	for _, s := range sl {
		if s == "" {
			continue
		}

		var out SSH
		if err := out.UnmarshalText([]byte(s)); err != nil {
			return nil, err
		}
		outs = append(outs, &out)
	}
	return outs, nil
}

func CreateSecrets(secrets []*Secret) (session.Attachable, error) {
	fs := make([]secretsprovider.Source, 0, len(secrets))
	for _, secret := range secrets {
		if secret == nil {
			continue
		}
		fs = append(fs, secretsprovider.Source{
			ID:       secret.ID,
			FilePath: secret.FilePath,
			Env:      secret.Env,
		})
	}
	store, err := secretsprovider.NewStore(fs)
	if err != nil {
		return nil, err
	}
	return secretsprovider.NewSecretProvider(store), nil
}

func CreateSSH(ssh []*SSH) (session.Attachable, error) {
	configs := make([]sshprovider.AgentConfig, 0, len(ssh))
	for _, entry := range ssh {
		if entry == nil {
			continue
		}
		cfg := sshprovider.AgentConfig{
			ID:    entry.ID,
			Paths: append([]string(nil), entry.Paths...),
		}
		configs = append(configs, cfg)
	}
	return sshprovider.NewSSHAgentProvider(configs)
}
