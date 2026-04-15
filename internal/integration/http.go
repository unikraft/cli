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
	"net"
	"net/http"
	"net/url"
	"strings"
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

// HTTPPost sends a POST request with the given body and content type, retrying
// up to 10 times, and returns the response body. TLS verification is skipped so
// self-signed certificates work.
func HTTPPost(t *testing.T, url, contentType, body string) string {
	t.Helper()
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //#nosec G402 -- test code
		},
		Timeout: 10 * time.Second,
	}

	var lastErr error
	for range 10 {
		resp, err := client.Post(url, contentType, strings.NewReader(body)) //#nosec G107 -- test code, URL from test
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			time.Sleep(2 * time.Second)
			continue
		}
		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
			time.Sleep(2 * time.Second)
			continue
		}
		return string(respBody)
	}
	require.NoError(t, lastErr, "HTTP POST %s failed after retries", url)
	return ""
}

// HTTPGetTLSCerts dials the HTTPS URL and returns the peer's certificate chain.
// TLS verification is intentionally skipped so self-signed certificates work.
// It retries up to 10 times with a 2-second sleep between attempts.
//
// dialAddr overrides the TCP address to connect to (host:port). When empty the
// address is derived from the URL itself. This is useful when DNS for the URL
// hostname does not resolve but the load balancer is reachable at a known
// address (e.g. resolved from an internal FQDN) and SNI routing is used to
// select the right certificate.
func HTTPGetTLSCerts(t *testing.T, rawURL string, dialAddr string) []*x509.Certificate {
	t.Helper()

	u, err := url.Parse(rawURL)
	require.NoError(t, err, "invalid URL: %s", rawURL)
	require.NotEmpty(t, u.Host, "URL has no host: %s", rawURL)

	if dialAddr == "" {
		dialAddr = u.Host
		if u.Port() == "" {
			dialAddr = net.JoinHostPort(u.Hostname(), "443")
		}
	}

	cfg := &tls.Config{
		InsecureSkipVerify: true, //#nosec G402 -- test code
		ServerName:         u.Hostname(),
	}

	var lastErr error
	for range 10 {
		conn, err := tls.Dial("tcp", dialAddr, cfg)
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
