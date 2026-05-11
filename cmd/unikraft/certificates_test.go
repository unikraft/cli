// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"testing"

	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/types"
)

func certificatesHelpTests(t *testing.T, unikraftPath string) {
	r := newTestEnv(t, unikraftPath)
	gild(t.Context(), t, r.cli,
		[]string{unikraftCmd, "certificate", "--help"},
		[]string{unikraftCmd, "certificate", "get", "--help"},
		[]string{unikraftCmd, "certificate", "list", "--help"},
		[]string{unikraftCmd, "certificate", "wait", "--help"},
		[]string{unikraftCmd, "certificate", "create", "--help"},
		[]string{unikraftCmd, "certificate", "delete", "--help"},
	)
}

func certificatesOutputTests(t *testing.T) {
	sample := cmd.Certificate{
		MetroName:    "fra",
		Name:         "my-cert",
		UUID:         "c3d4e5f6-a7b8-9012-cdef-123456789012",
		CommonName:   "example.unikraft.app",
		Subject:      "CN=example.unikraft.app",
		Issuer:       "CN=Test CA",
		SerialNumber: "1234567890",
		State:        types.CertificateState(platform.CertificateStateValid),
	}

	gild[resource.Resource](t.Context(), t, dumpResource, sample)
}

func certificatesTests(t *testing.T, r *integrationRunner) {
	metroName := ""
	if r.cfg != nil {
		metroName = r.cfg.MetroName
	}

	t.Run("create", func(t *testing.T) {
		r.
			online().
			run(t, []command{
				{args: []string{unikraftCmd, "certificate", "list"}, match: []string{`METRO\s+NAME`}},
				{args: []string{unikraftCmd, "certificate", "create", "--set", "name=test-$UNIQ_CERT_A", "--set", "cn=$CERT_A_CN", "--set", "chain=$CERT_A_CHAIN", "--set", "pkey=$CERT_A_KEY", "--set", "metro=" + metroName}, match: []string{`name:\s+test-`, `state:\s+valid`}},
				{args: []string{unikraftCmd, "certificate", "create", "--set", "name=test-$UNIQ_CERT_B", "--set", "cn=$CERT_B_CN", "--set", "chain=$CERT_B_CHAIN", "--set", "pkey=$CERT_B_KEY", "--set", "metro=" + metroName}, match: []string{`name:\s+test-`, `state:\s+valid`}},
				{args: []string{unikraftCmd, "certificate", "list"}, match: []string{`test-.*valid`}},
				{args: []string{unikraftCmd, "certificate", "inspect", "test-$UNIQ_CERT_A", "test-$UNIQ_CERT_B"}, match: []string{`state:\s+valid`}},
				{args: []string{unikraftCmd, "certificate", "delete", "test-$UNIQ_CERT_A", "test-$UNIQ_CERT_B"}, match: []string{`test-`}},
			})
	})
}
