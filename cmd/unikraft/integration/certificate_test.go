// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

func TestCertificates(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		r := runner(t, true)
		certNameA := uniq()
		certNameB := uniq()
		certA := integ.GenerateCert(t)
		certB := integ.GenerateCert(t)

		out := r.Run(t, []string{"unikraft", "certificate", "list", "--output", "quiet"})
		assert.Empty(t, strings.TrimSpace(out))

		out = r.Run(t, []string{"unikraft", "certificate", "create", "--set", "name=test-" + certNameA, "--set", "cn=" + certA.CN, "--set", "chain=" + certA.Chain, "--set", "pkey=" + certA.Key, "--set", "metro=" + r.Config.MetroName})
		assert.Regexp(t, `name:\s+test-`, out)
		assert.Regexp(t, `state:\s+valid`, out)

		out = r.Run(t, []string{"unikraft", "certificate", "create", "--set", "name=test-" + certNameB, "--set", "cn=" + certB.CN, "--set", "chain=" + certB.Chain, "--set", "pkey=" + certB.Key, "--set", "metro=" + r.Config.MetroName})
		assert.Regexp(t, `name:\s+test-`, out)
		assert.Regexp(t, `state:\s+valid`, out)

		out = r.Run(t, []string{"unikraft", "certificate", "list"})
		assert.Regexp(t, `test-.*valid`, out)

		out = r.Run(t, []string{"unikraft", "certificate", "inspect", "test-" + certNameA, "test-" + certNameB})
		assert.Regexp(t, `state:\s+valid`, out)
		assert.Regexp(t, `common-name:`, out)

		out = r.Run(t, []string{"unikraft", "certificate", "delete", "test-" + certNameA, "test-" + certNameB})
		assert.Regexp(t, `test-`, out)
	})

	t.Run("serve", func(t *testing.T) {
		r := runner(t, true)
		certName := uniq()
		domainName := uniq()
		instName := uniq()

		cert := integ.GenerateCert(t)

		// Upload the certificate to Unikraft Cloud.
		r.Run(t, []string{
			"unikraft", "certificate", "create",
			"--set", "name=test-" + certName,
			"--set", "cn=" + cert.CN,
			"--set", "chain=" + cert.Chain,
			"--set", "pkey=" + cert.Key,
			"--set", "metro=" + r.Config.MetroName,
		})

		// Create an nginx instance whose inline service domain references the
		// certificate we just created.
		r.Run(t, []string{
			"unikraft", "instance", "create",
			"--set", "name=test-" + instName,
			"--set", "metro=" + r.Config.MetroName,
			"--set", "image=nginx:latest",
			"--set", "autostart=true",
			"--set", "resources.memory=128",
			"--set", "resources.vcpus=1",
			"--set", "service.services=443:8080/tls+http",
			"--set", "service.domains=name=" + domainName + ",certificate=test-" + certName,
		})

		out := r.Run(t, []string{
			"unikraft", "instance", "inspect", "test-" + instName,
			"--output", `template={{ (index .service.domains 0).fqdn }}`,
		})
		fqdn := strings.TrimSpace(out)
		require.NotEmpty(t, fqdn, "expected a non-empty FQDN from the service domain")

		r.Run(t, []string{
			"unikraft", "instance", "wait",
			"--until", "state==running",
			"--timeout", "30s",
			"test-" + instName,
		})

		tlsCerts := integ.HTTPGetTLSCerts(t, "https://"+fqdn)
		require.NotEmpty(t, tlsCerts, "TLS handshake returned no certificates")

		expectedCN := strings.TrimSuffix(cert.CN, ".")
		assert.Equal(t, expectedCN, tlsCerts[0].Subject.CommonName,
			"served TLS certificate should be the one uploaded, not the platform default")
	})
}
