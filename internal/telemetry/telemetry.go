// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package telemetry provides basic analytics and crash reporting for the
// Unikraft CLI using PostHog.
package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/posthog/posthog-go"

	"unikraft.com/cli/internal/version"
	"unikraft.com/x/fingerprint"
)

var (
	// PostHogAPIKey is the default PostHog project API key for CLI telemetry.
	// This can be overridden via the UNIKRAFT_POSTHOG_API_KEY environment variable.
	PostHogAPIKey = ""

	// PostHogHost is the PostHog API host.
	// This can be overridden via the UNIKRAFT_POSTHOG_HOST environment variable.
	PostHogHost = ""
)

var (
	distinctID string
	enabled    bool
	mu         sync.Mutex

	// commandStart tracks when the current command started for duration calculation.
	commandStart time.Time

	// commandPath stores the current command path for crash reporting.
	commandPath string
)

// contextKey is a type for context keys used by this package.
type contextKey string

const (
	// commandPathKey is the context key for storing the command path.
	commandPathKey contextKey = "telemetry.commandPath"
	// commandStartKey is the context key for storing the command start time.
	commandStartKey contextKey = "telemetry.commandStart"
)

// EventPayload represents the data passed to the detached subprocess.
// Note: APIKey and Endpoint are intentionally excluded to avoid exposing
// them in process listings (ps/top). SendEvent reads them from package-level vars.
type EventPayload struct {
	Event      string         `json:"event"`
	DistinctID string         `json:"distinct_id"`
	Properties map[string]any `json:"properties"`
	Timestamp  time.Time      `json:"timestamp"`
}

// Init initializes the PostHog analytics client.
func Init() error {
	mu.Lock()
	defer mu.Unlock()

	enabled = true

	apiKey := os.Getenv("UNIKRAFT_POSTHOG_API_KEY")
	if apiKey == "" {
		apiKey = PostHogAPIKey
	}
	if apiKey == "" {
		enabled = false
		return fmt.Errorf("no API key set for PostHog; use UNIKRAFT_POSTHOG_API_KEY environment variable")
	}

	// Generate anonymous distinct ID from machine fingerprint
	distinctID = generateDistinctID()

	return nil
}

// generateDistinctID creates an anonymous distinct ID based on machine fingerprint.
// The ID is a SHA-256 hash to ensure privacy while maintaining consistency.
func generateDistinctID() string {
	fp, err := fingerprint.New()
	if err != nil {
		// Fallback to hostname-based ID
		hostname, _ := os.Hostname()
		hash := sha256.Sum256([]byte(hostname + "-unikraft-cli"))
		return hex.EncodeToString(hash[:16])
	}

	// Create a stable fingerprint string from machine characteristics
	fpStr := fmt.Sprintf("%s-%s-%s-%s-%t",
		fp.Hostname,
		fp.Os,
		fp.Goarch,
		fp.Goos,
		fp.Container,
	)
	hash := sha256.Sum256([]byte(fpStr))
	return hex.EncodeToString(hash[:16])
}

// SetCommandContext stores the command path and start time in context for tracking.
func SetCommandContext(ctx context.Context, cmdPath string) context.Context {
	mu.Lock()
	start := time.Now()
	commandPath = cmdPath
	commandStart = start
	mu.Unlock()
	ctx = context.WithValue(ctx, commandPathKey, cmdPath)
	ctx = context.WithValue(ctx, commandStartKey, start)
	return ctx
}

// TrackCommandStart records when a command starts executing.
func TrackCommandStart(cmdPath string) {
	mu.Lock()
	defer mu.Unlock()

	if !enabled {
		return
	}

	commandPath = cmdPath
	commandStart = time.Now()

	spawnDetachedAnalytics(posthog.Capture{
		DistinctId: distinctID,
		Event:      "command_started",
		Properties: posthog.NewProperties().Set("command", cmdPath),
	})
}

// TrackCommandSuccess records successful command completion with duration.
func TrackCommandSuccess(cmdPath string) {
	mu.Lock()
	defer mu.Unlock()

	if !enabled {
		return
	}

	duration := time.Since(commandStart)

	spawnDetachedAnalytics(posthog.Capture{
		DistinctId: distinctID,
		Event:      "command_succeeded",
		Properties: posthog.NewProperties().
			Set("command", cmdPath).
			Set("duration_ms", duration.Milliseconds()),
	})
}

// TrackCommandError records command failures with error information.
func TrackCommandError(cmdPath string, err error) {
	mu.Lock()
	defer mu.Unlock()

	if !enabled {
		return
	}

	duration := time.Since(commandStart)

	props := posthog.NewProperties().
		Set("command", cmdPath).
		Set("duration_ms", duration.Milliseconds())

	if err != nil {
		// Only capture error type, not the full message which may contain sensitive data
		props.Set("error_type", fmt.Sprintf("%T", err))
		// Capture a sanitized error message (first line only, truncated)
		errMsg := sanitizeErrorMessage(err.Error())
		props.Set("error_message", errMsg)
	}

	spawnDetachedAnalytics(posthog.Capture{
		DistinctId: distinctID,
		Event:      "command_failed",
		Properties: props,
	})
}

// TrackCrash records panic/crash information for debugging.
func TrackCrash(panicValue any, stack []byte) {
	mu.Lock()
	defer mu.Unlock()

	if !enabled {
		return
	}

	duration := time.Since(commandStart)

	props := posthog.NewProperties().
		Set("command", commandPath).
		Set("duration_ms", duration.Milliseconds()).
		Set("panic_type", fmt.Sprintf("%T", panicValue)).
		Set("panic_value", sanitizeErrorMessage(fmt.Sprintf("%v", panicValue)))

	// Include a truncated stack trace (first 2KB)
	stackStr := string(stack)
	if len(stackStr) > 2048 {
		stackStr = stackStr[:2048] + "...[truncated]"
	}
	props.Set("stack_trace", stackStr)

	spawnDetachedAnalytics(posthog.Capture{
		DistinctId: distinctID,
		Event:      "cli_crash",
		Properties: props,
	})
}

// RecoverAndReport should be deferred at the top of main() to catch panics,
// report them to PostHog, and then re-panic.
func RecoverAndReport() {
	if r := recover(); r != nil {
		// Capture stack trace
		stack := make([]byte, 4096)
		n := runtime.Stack(stack, false)
		stack = stack[:n]

		TrackCrash(r, stack)

		// Re-panic to preserve the original behavior
		panic(r)
	}
}

// sanitizeErrorMessage removes potentially sensitive information from error
// messages.
func sanitizeErrorMessage(msg string) string {
	// Take only the first line
	if idx := strings.Index(msg, "\n"); idx != -1 {
		msg = msg[:idx]
	}

	// Truncate long messages
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}

	return msg
}

// Enabled returns whether telemetry is currently enabled.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enabled
}

// SendEvent processes an event payload in the detached subprocess.
// This is called by the hidden send-analytics subcommand.
func SendEvent(payloadJSON string) error {
	var payload EventPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return err
	}

	apiKey := os.Getenv("UNIKRAFT_POSTHOG_API_KEY")
	if apiKey == "" {
		apiKey = PostHogAPIKey
	}
	if apiKey == "" {
		return nil // No API key, so just skip sending analytics
	}

	host := os.Getenv("UNIKRAFT_POSTHOG_HOST")
	if host == "" {
		host = PostHogHost
	}

	// Create PostHog client - no need for fast timeouts since we're detached
	// Read API key and endpoint from package-level vars (not passed via argv for security)
	client, err := posthog.NewWithConfig(apiKey, posthog.Config{
		Endpoint: host,
		// Use a short batch interval for CLI tools since they exit quickly
		BatchSize: 1,
		Interval:  100 * time.Millisecond,
		DefaultEventProperties: posthog.NewProperties().
			Set("cli_version", version.Version).
			Set("cli_commit", version.Commit).
			Set("cli_build_time", version.BuildTime).
			Set("os", runtime.GOOS).
			Set("arch", runtime.GOARCH).
			Set("ci", os.Getenv("CI") != "").
			Set("go_version", runtime.Version()),
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = client.Close()
	}()

	// Build properties
	props := posthog.NewProperties()
	for k, v := range payload.Properties {
		props.Set(k, v)
	}

	return client.Enqueue(posthog.Capture{
		DistinctId: payload.DistinctID,
		Event:      payload.Event,
		Properties: props,
		Timestamp:  payload.Timestamp,
	})
}
