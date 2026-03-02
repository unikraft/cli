// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
	jujuerrors "github.com/juju/errors"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/tui/progdl"
	"unikraft.com/cli/internal/version"
)

type UpgradeCmd struct {
	Channel string `help:"Release channel to upgrade from." default:"stable" enum:"stable,staging"`
	Force   bool   `short:"f" help:"Force upgrade even if already at latest version."`
	Use     string `short:"v" name:"use" help:"Upgrade to a specific version."`
	BinDir  string `help:"Directory where to install the binary. If empty, uses the current binary location."`
	BaseUrl string `help:"Base URL for fetching releases. Can also be set via UNIKRAFT_CLI_INSTALL_URL environment variable." default:"https://pkg.unikraft.com" hidden:"true"`
}

func (cmd *UpgradeCmd) Run(ctx context.Context, stdio config.Stdio) error {
	var targetVersion string
	if cmd.Use != "" {
		// Use the specified version
		targetVersion = cmd.Use
		log.G(ctx).
			Info().
			Str("version", targetVersion).
			Msg("upgrading to specified version")
	} else {
		// Fetch latest version from channel
		log.G(ctx).
			Info().
			Str("channel", cmd.Channel).
			Msg("checking for updates")

		latestVersion, err := fetchLatestVersion(ctx, cmd.BaseUrl, cmd.Channel)
		if err != nil {
			return jujuerrors.Annotate(err, "fetching latest version")
		}
		targetVersion = latestVersion

		currentVersion := version.Version
		cmp, err := compareSemver(currentVersion, targetVersion)
		if err != nil {
			log.G(ctx).
				Warn().
				Err(err).
				Msg("could not compare versions, proceeding with upgrade")
		} else if cmp >= 0 && !cmd.Force {
			log.G(ctx).
				Info().
				Str("version", currentVersion).
				Msg("already at latest version")
			return nil
		}

		if cmp < 0 {
			log.G(ctx).
				Info().
				Str("current", currentVersion).
				Str("latest", targetVersion).
				Msg("new version available")
		}
	}

	// Determine platform and architecture
	platform := runtime.GOOS
	arch := runtime.GOARCH

	// Determine destination directory
	var destDir string
	if cmd.BinDir != "" {
		destDir = cmd.BinDir
	} else {
		// Use current binary location
		execPath, err := os.Executable()
		if err != nil {
			return jujuerrors.Annotate(err, "getting current executable path")
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return jujuerrors.Annotate(err, "resolving symlinks")
		}
		destDir = filepath.Dir(execPath)
	}

	// Download and install
	if err := downloadAndInstall(ctx, stdio, cmd.Force, cmd.BaseUrl, targetVersion, platform, arch, destDir); err != nil {
		return jujuerrors.Annotate(err, "downloading and installing")
	}

	log.G(ctx).Info().
		Str("version", targetVersion).
		Msg("upgrade complete")

	return nil
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

// downloadAndInstall downloads the CLI binary and installs it.
func downloadAndInstall(ctx context.Context, stdio config.Stdio, force bool, baseURL, ver, plat, arch, destDir string) error {
	asset := fmt.Sprintf("unikraft-%s-%s.tar.gz", plat, arch)
	assetURL := fmt.Sprintf("%s/endpoints/cli/content/%s/%s", baseURL, ver, asset)
	checksumURL := assetURL + ".sha256"

	log.G(ctx).
		Debug().
		Str("url", assetURL).
		Msg("downloading binary")

	// Create temp directory for download
	tmpDir, err := os.MkdirTemp("", "unikraft-upgrade-*")
	if err != nil {
		return jujuerrors.Annotate(err, "creating temp directory")
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, asset)

	log.G(ctx).
		Info().
		Str("version", ver).
		Str("platform", plat).
		Str("arch", arch).
		Msg("downloading")

	// Download the archive with progress bar
	if err := downloadFile(ctx, stdio, assetURL, archivePath); err != nil {
		return jujuerrors.Annotate(err, "downloading archive")
	}

	// Check if context was cancelled during download
	if err := ctx.Err(); err != nil {
		return jujuerrors.Annotate(err, "download interrupted")
	}

	// Verify checksum
	log.G(ctx).
		Debug().
		Str("checksum_url", checksumURL).
		Msg("verifying checksum")

	if err := verifyChecksum(ctx, archivePath, checksumURL); err != nil {
		if !force {
			return jujuerrors.New("checksum verification failed")
		}
		// Checksum verification failure is a warning, not fatal
		log.G(ctx).
			Warn().
			Err(err).
			Msg("checksum verification failed, continuing anyway")
	} else {
		log.G(ctx).
			Debug().
			Msg("checksum verified successfully")
	}

	// Extract and get binary path
	log.G(ctx).
		Debug().
		Str("archive", archivePath).
		Str("dest", tmpDir).
		Msg("extracting binary")

	binaryPath, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		return jujuerrors.Annotate(err, "extracting binary")
	}

	log.G(ctx).
		Debug().
		Str("path", binaryPath).
		Msg("extracted binary")

	// Determine destination path
	binaryName := "unikraft"
	if runtime.GOOS == "windows" {
		binaryName = "unikraft.exe"
	}
	destPath := filepath.Join(destDir, binaryName)

	log.G(ctx).
		Debug().
		Str("bin", destPath).
		Msg("installing")

	// Install the new binary
	if err := installBinary(binaryPath, destPath); err != nil {
		return jujuerrors.Annotate(err, "installing binary")
	}

	return nil
}

// downloadFile downloads a file from a URL to the specified path with a progress bar.
func downloadFile(ctx context.Context, stdio config.Stdio, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return jujuerrors.Annotate(err, "creating request")
	}

	resp, err := httpclient.DefaultHTTPClient.Do(req)
	if err != nil {
		return jujuerrors.Annotate(err, "downloading file")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return jujuerrors.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return jujuerrors.Annotate(err, "creating output file")
	}
	defer out.Close()

	// Use progress bar for download
	if err := progdl.DownloadWithProgress(ctx, stdio.Stdout, resp.Body, out, resp.ContentLength); err != nil {
		return err
	}

	return nil
}

// verifyChecksum downloads and verifies the SHA256 checksum of a file.
func verifyChecksum(ctx context.Context, filePath, checksumURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return jujuerrors.Annotate(err, "creating checksum request")
	}

	resp, err := httpclient.DefaultHTTPClient.Do(req)
	if err != nil {
		return jujuerrors.Annotate(err, "fetching checksum")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return jujuerrors.Errorf("checksum file not available (HTTP %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return jujuerrors.Annotate(err, "reading checksum response")
	}

	// Parse checksum - first field if space-separated
	expectedStr := strings.TrimSpace(string(body))
	if idx := strings.Index(expectedStr, " "); idx != -1 {
		expectedStr = expectedStr[:idx]
	}

	// Calculate actual checksum
	f, err := os.Open(filePath)
	if err != nil {
		return jujuerrors.Annotate(err, "opening file for checksum")
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return jujuerrors.Annotate(err, "calculating checksum")
	}
	actual := hex.EncodeToString(hash.Sum(nil))

	if actual != expectedStr {
		return jujuerrors.Errorf("checksum mismatch: expected %s, got %s", expectedStr, actual)
	}

	return nil
}

// extractBinary extracts the unikraft binary from a tar.gz archive.
func extractBinary(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", jujuerrors.Annotate(err, "opening archive")
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", jujuerrors.Annotate(err, "creating gzip reader")
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	binaryName := "unikraft"
	if runtime.GOOS == "windows" {
		binaryName = "unikraft.exe"
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", jujuerrors.Annotate(err, "reading tar entry")
		}

		// Look for the unikraft binary
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == binaryName {
			destPath := filepath.Join(destDir, binaryName)
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY, 0o755)
			if err != nil {
				return "", jujuerrors.Annotate(err, "creating output file")
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", jujuerrors.Annotate(err, "extracting binary")
			}
			outFile.Close()
			return destPath, nil
		}
	}

	return "", jujuerrors.New("binary not found in archive")
}

// installBinary copies the new binary to the destination, handling the case
// where the destination is the currently running executable.
func installBinary(srcPath, destPath string) error {
	// Read the new binary
	newBinary, err := os.ReadFile(srcPath)
	if err != nil {
		return jujuerrors.Annotate(err, "reading new binary")
	}

	// Get the original file info to preserve permissions
	info, err := os.Stat(destPath)
	if err != nil {
		return jujuerrors.Annotate(err, "getting destination info")
	}
	mode := info.Mode()

	// On Unix-like systems, we can't write to a running executable directly.
	// Instead, we rename the old binary and write the new one.
	destDir := filepath.Dir(destPath)
	destBase := filepath.Base(destPath)

	// Create a backup path
	backupPath := filepath.Join(destDir, "."+destBase+".old")

	// Remove any existing backup
	_ = os.Remove(backupPath)

	// Rename current executable to backup
	if err := os.Rename(destPath, backupPath); err != nil {
		return jujuerrors.Annotate(err, "backing up current binary")
	}

	// Write new binary
	if err := os.WriteFile(destPath, newBinary, mode); err != nil {
		// Try to restore backup
		_ = os.Rename(backupPath, destPath)
		return jujuerrors.Annotate(err, "writing new binary")
	}

	// Remove backup
	_ = os.Remove(backupPath)

	return nil
}

// compareSemver compares two semantic version strings.
// Returns:
//
//	-1 if a < b
//	 0 if a == b
//	 1 if a > b
func compareSemver(a, b string) (int, error) {
	va, err := semver.NewVersion(a)
	if err != nil {
		return 0, jujuerrors.Annotate(err, "parsing version")
	}

	vb, err := semver.NewVersion(b)
	if err != nil {
		return 0, jujuerrors.Annotate(err, "parsing version")
	}

	return va.Compare(vb), nil
}
