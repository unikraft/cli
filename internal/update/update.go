// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package update provides persistent update checking for the Unikraft CLI.
// It spawns a detached subprocess to check for updates in the background,
// caches the result, and notifies the user when a new version is available.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	jujuerrors "github.com/juju/errors"

	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/version"
)

const (
	// DefaultChannel is the default release channel for update checks.
	DefaultChannel = "stable"

	// DefaultBaseURL is the default base URL for fetching releases.
	DefaultBaseURL = "https://pkg.unikraft.com"

	// cacheFilename is the name of the file where update info is cached.
	cacheFilename = "update-check.json"

	// checkInterval is the minimum time between update checks to avoid
	// excessive network requests.
	checkInterval = 12 * time.Hour
)

// CachedUpdate represents the cached update information.
type CachedUpdate struct {
	// LatestVersion is the latest version available.
	LatestVersion string `json:"latest_version"`

	// CurrentVersion is the version that was running when the check was made.
	CurrentVersion string `json:"current_version"`

	// CheckedAt is when the update check was performed.
	CheckedAt time.Time `json:"checked_at"`

	// Channel is the release channel that was checked.
	Channel string `json:"channel"`
}

// cacheFilePath returns the path to the update cache file.
func cacheFilePath() (string, error) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", jujuerrors.Annotate(err, "getting user config dir")
	}
	return filepath.Join(userConfigDir, "unikraft", cacheFilename), nil
}

// CheckAndCache checks for updates and caches the result.
// This is called by the hidden _check_updates subcommand.
func CheckAndCache(ctx context.Context, baseURL, channel string) error {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if channel == "" {
		channel = DefaultChannel
	}

	// Fetch latest version
	latestVersion, err := fetchLatestVersion(ctx, baseURL, channel)
	if err != nil {
		return jujuerrors.Annotate(err, "fetching latest version")
	}

	// Save to cache
	cached := CachedUpdate{
		LatestVersion:  latestVersion,
		CurrentVersion: version.Version,
		CheckedAt:      time.Now(),
		Channel:        channel,
	}

	return saveCache(&cached)
}

// LoadCache loads the cached update information.
// Returns nil, nil if no cache exists.
func LoadCache() (*CachedUpdate, error) {
	path, err := cacheFilePath()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, jujuerrors.Annotate(err, "opening cache file")
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, jujuerrors.Annotate(err, "reading cache file")
	}

	var cached CachedUpdate
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, jujuerrors.Annotate(err, "decoding cache file")
	}

	return &cached, nil
}

// saveCache saves the update information to the cache file.
func saveCache(cached *CachedUpdate) error {
	path, err := cacheFilePath()
	if err != nil {
		return err
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return jujuerrors.Annotate(err, "creating cache directory")
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return jujuerrors.Annotate(err, "encoding cache")
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return jujuerrors.Annotate(err, "writing cache file")
	}

	return nil
}

// ShouldCheck returns true if we should check for updates.
// It returns false if we've checked recently (within checkInterval).
func ShouldCheck() bool {
	cached, err := LoadCache()
	if err != nil || cached == nil {
		return true
	}

	// Don't check too frequently
	return time.Since(cached.CheckedAt) > checkInterval
}

// HasUpdate returns the available update information if a newer version
// is available. Returns nil if the current version is up to date or if
// no cache exists.
func HasUpdate() *CachedUpdate {
	cached, err := LoadCache()
	if err != nil || cached == nil {
		return nil
	}

	// Check if the latest version is newer than the current version
	isNewer, err := isNewerVersion(version.Version, cached.LatestVersion)
	if err != nil || !isNewer {
		return nil
	}

	return cached
}

// isNewerVersion returns true if latest is newer than current.
func isNewerVersion(current, latest string) (bool, error) {
	// Handle dev versions gracefully
	if current == "dev" || current == "" {
		return false, nil
	}

	vCurrent, err := semver.NewVersion(current)
	if err != nil {
		return false, jujuerrors.Annotate(err, "parsing current version")
	}

	vLatest, err := semver.NewVersion(latest)
	if err != nil {
		return false, jujuerrors.Annotate(err, "parsing latest version")
	}

	return vLatest.GreaterThan(vCurrent), nil
}

// fetchLatestVersion retrieves the latest version string from the channel file.
func fetchLatestVersion(ctx context.Context, baseURL, channel string) (string, error) {
	url := fmt.Sprintf("%s/endpoints/cli/content/%s.txt", baseURL, channel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", jujuerrors.Annotate(err, "creating request")
	}

	resp, err := httpclient.DefaultHTTPClient.Do(req)
	if err != nil {
		return "", jujuerrors.Annotate(err, "fetching version file")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", jujuerrors.Errorf("failed to fetch version (HTTP %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", jujuerrors.Annotate(err, "reading response body")
	}

	return strings.TrimSpace(string(body)), nil
}
