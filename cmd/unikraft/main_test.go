// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/mitchellh/copystructure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/golden"
	"mvdan.cc/sh/v3/shell"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/integration"
	"unikraft.com/cli/internal/resource"
	resourcecmd "unikraft.com/cli/internal/resource/cmd"
)

const unikraftCmd = "unikraft"

// integrationRunner holds shared state for running integration tests.
type integrationRunner struct {
	t            *testing.T
	cfg          *integration.Config
	unikraftPath string
}

// command represents a single command to execute in a test.
type command struct {
	args       []string
	err        commandErr
	captureEnv string
	match      []string
}

type commandErr int

const (
	errNo    commandErr = iota // command must succeed
	errMaybe                   // command may fail (either outcome is acceptable)
	errYes                     // command must fail
)

// testBuilder provides a fluent interface for configuring and running tests.
type testBuilder struct {
	runner  *integrationRunner
	online  bool
	context map[string]string
}

// online returns a new testBuilder configured for online tests (requiring config).
func (r *integrationRunner) online() *testBuilder {
	return &testBuilder{runner: r, online: true}
}

// offline returns a new testBuilder configured for offline tests.
func (r *integrationRunner) offline() *testBuilder {
	return &testBuilder{runner: r, online: false}
}

// withContext adds context files to be created in the test directory.
func (b *testBuilder) withContext(context map[string]string) *testBuilder {
	b.context = context
	return b
}

func TestIntegration(t *testing.T) {
	integration.SkipUnlessIntegration(t)
	unikraftPath := buildUnikraftBinary(t)

	cfg, err := integration.LoadConfig(t)
	require.NoError(t, err)

	runner := &integrationRunner{
		t:            t,
		cfg:          cfg,
		unikraftPath: unikraftPath,
	}

	tests := []struct {
		name string
		fn   func(*testing.T, *integrationRunner)
	}{
		{"auth", authTests},
		{"instances", instancesTests},
		{"instance-templates", instanceTemplatesTests},
		{"volumes", volumesTests},
		{"volume-templates", volumeTemplatesTests},
		{"services", servicesTests},
		{"certificates", certificatesTests},
		{"images", imagesTests},
		{"resources", resourceTests},
		{"build", buildTests},
		{"config", configTests},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.fn(t, runner)
		})
	}
}

// run executes a test case with the given commands using the integrationRunner directly (for offline tests).
func (r *integrationRunner) run(t *testing.T, commands []command) {
	r.offline().run(t, commands)
}

// run executes a test case with the given commands and the builder's configuration.
func (b *testBuilder) run(t *testing.T, commands []command) {
	t.Helper()
	t.Parallel()

	r := b.runner

	if b.online && r.cfg == nil {
		t.Skip("online test requires config, but no config found")
	}

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	var testCfg *integration.Config
	if r.cfg != nil {
		cloned, err := copystructure.Copy(r.cfg)
		require.NoError(t, err)
		testCfg = cloned.(*integration.Config)
		testCfg.Config.Path = configPath
		require.NoError(t, testCfg.Config.Save())
	}

	ctx := t.Context()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.TraceLevel))

	assert.NotEmpty(t, commands, "no commands specified")

	sandboxPath := filepath.Join(t.TempDir(), "sandbox.json")
	t.Cleanup(func() {
		ctx := ctx
		if testCfg != nil {
			ctx = config.WithConfig(ctx, testCfg.Config)
		}

		sandbox, err := resource.LoadSandbox(sandboxPath, cmd.SandboxedResources...)
		require.NoError(t, err)
		require.NotNil(t, sandbox)

		require.NoError(t, sandbox.Teardown(context.WithoutCancel(ctx)))
	})

	dir := t.TempDir()
	for name, contents := range b.context {
		require.NotEmpty(t, name, "context filename cannot be empty")
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	}

	expander := &expander{}
	for i, command := range commands {
		require.NotEmpty(t, command.args, "no command specified")
		args := expander.expandArgs(command.args)

		log.G(ctx).Debug().
			Strs("args", args).
			Msg("executing command")

		var cmd *exec.Cmd
		if args[0] == unikraftCmd {
			cmd = exec.CommandContext(ctx, r.unikraftPath, args[1:]...)
			cmd.Args[0] = r.unikraftPath
		} else {
			cmd = exec.CommandContext(ctx, args[0], args[1:]...)
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		cmd.Dir = dir
		cmd.Env = os.Environ()
		cmd.Env = slices.DeleteFunc(cmd.Env, func(s string) bool {
			return strings.HasPrefix(s, "UNIKRAFT_")
		})
		cmd.Env = append(cmd.Env, "NO_COLOR=1")
		cmd.Env = append(cmd.Env, "UNIKRAFT_CONFIG="+configPath)
		cmd.Env = append(cmd.Env, "BUILDKIT_PROGRESS=quiet")
		cmd.Env = append(cmd.Env, resource.UnikraftSandboxEnv+"="+sandboxPath)

		if i > 0 {
			// HACK: this is awful, but the platform can take a moment for things to
			// get ready :(
			time.Sleep(500 * time.Millisecond)
		}

		err := cmd.Run()
		if command.captureEnv != "" {
			value := strings.TrimSpace(stdout.String())
			if value == "" {
				value = strings.TrimSpace(stderr.String())
			}
			if value != "" {
				if expander.env == nil {
					expander.env = make(map[string]string)
				}
				expander.env[command.captureEnv] = value
			}
		}
		var exitErr *exec.ExitError
		var exitCode int
		if errors.As(err, &exitErr) && command.err >= errMaybe {
			exitCode = exitErr.ExitCode()
			err = nil
		}
		require.NoError(t, err, "command %q failed\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "),
			stdout.String(),
			stderr.String(),
		)
		if command.err == errYes {
			require.NotZero(t, exitCode, "command %q was expected to fail but succeeded", strings.Join(args, " "))
		}

		combined := ansi.Strip(stdout.String() + stderr.String())
		for _, pattern := range command.match {
			assert.Regexp(t, pattern, combined,
				"command %q output did not match pattern %q\nstdout:\n%s\nstderr:\n%s",
				strings.Join(command.args, " "), pattern, ansi.Strip(stdout.String()), ansi.Strip(stderr.String()))
		}
	}
}

func formatArgs(args []string) []string {
	formatted := make([]string, 0, len(args))
	for _, arg := range args {
		formatted = append(formatted, quoteArg(arg))
	}
	return formatted
}

func quoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if strings.ContainsAny(arg, " \t\n{}()") {
		return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return arg
}

type cleaner struct {
	pattern *regexp.Regexp
	repl    string
}

func wordCleaner(word string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`)
}

func wordCleanerf(word string, args ...any) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(fmt.Sprintf(word, args...)) + `\b`)
}

type expander struct {
	uniq  map[string]string
	certs map[string]*generatedCert
	env   map[string]string
}

type generatedCert struct {
	cn    string
	chain string
	key   string
}

func (e *expander) expandArgs(args []string) []string {
	if e.uniq == nil {
		e.uniq = make(map[string]string)
	}
	if e.certs == nil {
		e.certs = make(map[string]*generatedCert)
	}
	if e.env == nil {
		e.env = make(map[string]string)
	}
	expanded := make([]string, 0, len(args))
	for _, arg := range args {
		arg, err := shell.Expand(arg, func(varname string) string {
			if val, ok := e.env[varname]; ok {
				return val
			}
			prefix, rest, ok := strings.Cut(varname, "_")
			if !ok {
				return ""
			}
			switch prefix {
			case "UNIQ":
				if val, ok := e.uniq[rest]; ok {
					return val
				}
				result := fmt.Sprintf("%x", rand.Text())[:12]
				e.uniq[rest] = result
				return result
			case "CERT":
				name, field, ok := strings.Cut(rest, "_")
				if !ok {
					return ""
				}
				cert, ok := e.certs[name]
				if !ok {
					cert = generateCert()
					e.certs[name] = cert
				}
				switch field {
				case "CN":
					return cert.cn
				case "CHAIN":
					return cert.chain
				case "KEY":
					return cert.key
				}
			}
			return ""
		})
		if err != nil {
			panic(err)
		}
		expanded = append(expanded, arg)
	}
	return expanded
}

func generateCert() *generatedCert {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	cn := fmt.Sprintf("test-%x.unikraft.io", rand.Text()[:12])
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		panic(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	return &generatedCert{
		cn:    cn + ".",
		chain: string(certPEM),
		key:   string(keyPEM),
	}
}

func buildUnikraftBinary(t *testing.T) string {
	t.Helper()
	binaryDir := t.TempDir()
	binaryName := unikraftCmd
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(binaryDir, binaryName)

	cmd := exec.CommandContext(t.Context(), "go", "build", "-buildvcs=false", "-o", binaryPath, ".")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "go build failed\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	return binaryPath
}

// testEnv holds per-test environment for running CLI commands deterministically.
type testEnv struct {
	unikraftPath string
	configPath   string
	sandboxPath  string
	dir          string
}

// newTestEnv creates a new isolated test environment.
func newTestEnv(t *testing.T, unikraftPath string) *testEnv {
	t.Helper()
	return &testEnv{
		unikraftPath: unikraftPath,
		configPath:   filepath.Join(t.TempDir(), "config.yaml"),
		sandboxPath:  filepath.Join(t.TempDir(), "sandbox.json"),
		dir:          t.TempDir(),
	}
}

// cli runs a CLI command and returns formatted output for golden comparison.
func (env *testEnv) cli(ctx context.Context, t *testing.T, args []string) string {
	t.Helper()

	var c *exec.Cmd
	if args[0] == unikraftCmd {
		c = exec.CommandContext(ctx, env.unikraftPath, args[1:]...)
		c.Args[0] = env.unikraftPath
	} else {
		c = exec.CommandContext(ctx, args[0], args[1:]...)
	}

	var output bytes.Buffer
	c.Stdout = &output
	c.Stderr = &output
	c.Dir = env.dir
	c.Env = os.Environ()
	c.Env = slices.DeleteFunc(c.Env, func(s string) bool {
		return strings.HasPrefix(s, "UNIKRAFT_")
	})
	c.Env = append(c.Env, "NO_COLOR=1")
	c.Env = append(c.Env, "UNIKRAFT_CONFIG="+env.configPath)
	c.Env = append(c.Env, "BUILDKIT_PROGRESS=quiet")
	c.Env = append(c.Env, resource.UnikraftSandboxEnv+"="+env.sandboxPath)

	err := c.Run()

	var exitCode int
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		require.NoError(t, err, "command %q failed: %s", strings.Join(args, " "), output.String())
	}

	var result strings.Builder
	result.WriteString("$ " + strings.Join(formatArgs(args), " ") + "\n")

	out := normalizeOutput(output.String())
	if out != "" {
		result.WriteString("\n" + out + "\n")
	}

	if exitCode != 0 {
		result.WriteString("\nexit code: " + strconv.Itoa(exitCode) + "\n")
	}

	return result.String()
}

// gild runs a callback for each arg, concatenates the outputs, and asserts
// against the golden file for the current test. Only use offline callbacks.
func gild[Arg any](ctx context.Context, t *testing.T, callback func(context.Context, *testing.T, Arg) string, args ...Arg) {
	t.Helper()
	var output strings.Builder
	for i, arg := range args {
		if i > 0 {
			output.WriteString("\n")
		}
		output.WriteString(callback(ctx, t, arg))
	}
	golden.Assert(t, output.String(), t.Name())
}

// normalizeOutput strips ANSI codes, normalizes line endings, and applies
// build-environment cleaners.
func normalizeOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = ansi.Strip(s)
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	if s == "" {
		return ""
	}
	for _, c := range offlineCleaners {
		s = c.pattern.ReplaceAllString(s, c.repl)
	}
	return s
}

// offlineCleaners normalize build-environment values that differ between
// machines.
var offlineCleaners = []cleaner{
	{
		pattern: wordCleaner(runtime.Version()),
		repl:    "goX.Y.Z",
	},
	{
		pattern: wordCleanerf("%s/%s", runtime.GOOS, runtime.GOARCH),
		repl:    "GOOS/GOARCH",
	},
	{
		pattern: regexp.MustCompile(`/tmp/Test[^/\s]+/`),
		repl:    "/tmp/Test/",
	},
	{
		pattern: regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|(\+\d{2}:\d{2}))?\b`),
		repl:    "YYYY-MM-DDTHH:MM:SSZ",
	},
}

// dumpResource renders a resource using kv, table, and debug printers and
// returns the concatenated output.
func dumpResource(ctx context.Context, t *testing.T, res resource.Resource) string {
	t.Helper()
	var output strings.Builder

	for _, format := range []resourcecmd.PrinterType{
		resourcecmd.PrinterTypeKeyValue,
		resourcecmd.PrinterTypeTable,
		resourcecmd.PrintTypeDebug,
	} {
		printer := resourcecmd.Printer{Type: format}

		var buf bytes.Buffer
		err := printer.Print(ctx, &buf, nil, res, res)
		require.NoError(t, err)

		rendered := ansi.Strip(buf.String())
		rendered = strings.TrimRightFunc(rendered, unicode.IsSpace)

		output.WriteString("=== " + string(format) + " ===\n")
		output.WriteString(rendered)
		output.WriteString("\n\n")
	}

	return strings.TrimRightFunc(output.String(), unicode.IsSpace) + "\n"
}

// TestHelp runs --help tests for all resource types.
// These tests do NOT require the integration build tag and run on every
// "task test" invocation.
func TestHelp(t *testing.T) {
	unikraftPath := buildUnikraftBinary(t)

	tests := []struct {
		name string
		fn   func(*testing.T, string)
	}{
		{"general", generalHelpTests},
		{"auth", authHelpTests},
		{"instances", instancesHelpTests},
		{"volumes", volumesHelpTests},
		{"services", servicesHelpTests},
		{"certificates", certificatesHelpTests},
		{"images", imagesHelpTests},
		{"resources", resourceHelpTests},
		{"build", buildHelpTests},
		{"config", configHelpTests},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.fn(t, unikraftPath)
		})
	}
}

// TestOutput runs printer/output tests for all resource types.
// They construct static sample data and verify that rendering through
// kv/table/debug printers is stable.
func TestOutput(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*testing.T)
	}{
		{"instances", instancesOutputTests},
		{"volumes", volumesOutputTests},
		{"services", servicesOutputTests},
		{"certificates", certificatesOutputTests},
		{"images", imagesOutputTests},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.fn(t)
		})
	}
}
