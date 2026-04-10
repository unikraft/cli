// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSavePreservesCommentsAndDropsUnknownKeys(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")

	input := strings.TrimSpace(`
# global comment
profile: default
profiles:
  # default profile comment
  default:
    type: cloud
    token: oldtoken
    foobar: remove-me
`) + "\n"

	err := os.WriteFile(path, []byte(input), 0o600)
	require.NoError(t, err, "write config")

	config := &Config{
		Path:           path,
		DefaultProfile: "default",
		Profiles: map[string]Profile{
			"default": {
				Type:  ProfileTypeCloud,
				Token: "newtoken",
			},
		},
	}

	err = config.Save()
	require.NoError(t, err, "save config")

	output, err := os.ReadFile(path)
	require.NoError(t, err, "read config")
	content := string(output)

	assert.Contains(t, content, "# global comment", "expected global comment to be preserved")
	assert.Contains(t, content, "# default profile comment", "expected profile comment to be preserved")
	assert.Contains(t, content, "token: newtoken", "expected token to be updated")
	assert.NotContains(t, content, "foobar", "expected unknown profile key to be removed")
}

func TestLegacyProfileParsing(t *testing.T) {
	tests := []struct {
		name             string
		metroEnv         string
		expectedName     string
		expectedEndpoint string
	}{
		{
			name:             "full URL",
			metroEnv:         "https://api.fra.unikraft.cloud/v1",
			expectedName:     "fra",
			expectedEndpoint: "https://api.fra.unikraft.cloud",
		},
		{
			name:             "short name",
			metroEnv:         "fra",
			expectedName:     "fra",
			expectedEndpoint: "https://api.fra.unikraft.cloud",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("UKC_METRO", tt.metroEnv)
			t.Setenv("UKC_TOKEN", "test-token")

			root := t.TempDir()
			path := filepath.Join(root, "config.yaml")

			input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: legacy
`) + "\n"

			err := os.WriteFile(path, []byte(input), 0o600)
			require.NoError(t, err)

			config, err := Load(path)
			require.NoError(t, err)

			profile := config.Profiles["default"]
			assert.Equal(t, ProfileTypeLegacy, profile.Type)
			assert.Equal(t, "test-token", profile.Token)
			require.Len(t, profile.Metros, 1)

			metro := profile.Metros[0]
			assert.Equal(t, tt.expectedName, metro.Name)
			assert.Equal(t, tt.expectedEndpoint, metro.Endpoint)
		})
	}
}

func TestLegacyProfileSaveDoesNotPersistEnvVars(t *testing.T) {
	t.Setenv("UKC_METRO", "https://api.fra.unikraft.cloud/v1")
	t.Setenv("UKC_TOKEN", "test-token-secret")

	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")

	input := strings.TrimSpace(`
profile: default
profiles:
  default:
    type: legacy
`) + "\n"

	err := os.WriteFile(path, []byte(input), 0o600)
	require.NoError(t, err)

	config, err := Load(path)
	require.NoError(t, err)

	err = config.Save()
	require.NoError(t, err)

	output, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(output)

	assert.NotContains(t, content, "test-token-secret")
	assert.NotContains(t, content, "metros")
	assert.Contains(t, content, "type: legacy")
}

func TestProfile_GetDefaultMetro(t *testing.T) {
	tests := []struct {
		name     string
		profile  Profile
		expected string
	}{
		{
			name: "explicit default metro",
			profile: Profile{
				DefaultMetro: "fra",
				Metros: []Metro{
					{Name: "lhr"},
					{Name: "fra"},
				},
			},
			expected: "fra",
		},
		{
			name: "single configured metro",
			profile: Profile{
				Metros: []Metro{
					{Name: "lhr"},
				},
			},
			expected: "lhr",
		},
		{
			name: "multiple metros without explicit default",
			profile: Profile{
				Metros: []Metro{
					{Name: "lhr"},
					{Name: "fra"},
				},
			},
			expected: "",
		},
		{
			name:     "no metros",
			profile:  Profile{},
			expected: "",
		},
		{
			name: "explicit default takes precedence over single metro",
			profile: Profile{
				DefaultMetro: "fra",
				Metros: []Metro{
					{Name: "lhr"},
				},
			},
			expected: "fra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.profile.GetDefaultMetro()
			if got != tt.expected {
				t.Errorf("GetDefaultMetro() = %q, want %q", got, tt.expected)
			}
		})
	}
}
