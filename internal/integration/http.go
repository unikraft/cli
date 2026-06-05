// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func HTTPGet(t *testing.T, url string) string {
	t.Helper()
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //#nosec G402 -- test code
		},
		Timeout: 10 * time.Second,
	}

	var lastErr error
	for range 10 {
		resp, err := client.Get(url) //#nosec G107 -- test code, URL from test
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			time.Sleep(2 * time.Second)
			continue
		}
		return string(body)
	}
	require.NoError(t, lastErr, "HTTP GET %s failed after retries", url)
	return ""
}

// HTTPGetTLSCerts dials the HTTPS URL and returns the peer's certificate chain.
// TLS verification is intentionally skipped so self-signed certificates work.
// It retries up to 10 times with a 2-second sleep between attempts.
func HTTPGetTLSCerts(t *testing.T, rawURL string) []*x509.Certificate {
	t.Helper()

	u, err := url.Parse(rawURL)
	require.NoError(t, err, "invalid URL: %s", rawURL)

	host := u.Host
	if u.Port() == "" {
		host = u.Hostname() + ":443"
	}

	cfg := &tls.Config{
		InsecureSkipVerify: true, //#nosec G402 -- test code
		ServerName:         u.Hostname(),
	}

	var lastErr error
	for range 10 {
		conn, err := tls.Dial("tcp", host, cfg)
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		certs := conn.ConnectionState().PeerCertificates
		conn.Close()
		return certs
	}
	require.NoError(t, lastErr, "TLS dial %s failed after retries", rawURL)
	return nil
}
