// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	jujuerrors "github.com/juju/errors"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
)

type APICmd struct {
	Endpoint string   `arg:"" help:"API endpoint path (e.g. /v1/instances). May be a full URL, in which case the metro endpoint is ignored."`
	Method   string   `short:"X" name:"method" help:"HTTP method to use. Defaults to GET, or POST if --data is set." placeholder:"method"`
	Metro    string   `help:"Metro to target. Defaults to the profile's default metro." placeholder:"name"`
	Header   []string `short:"H" name:"header" help:"Add an HTTP header in 'Key: Value' format. May be repeated."`
	Data     string   `short:"d" name:"data" help:"HTTP request body. Use @path to read from a file, or @- to read from stdin."`
	Insecure bool     `short:"k" name:"insecure" help:"Skip TLS certificate verification."`
}

func (c *APICmd) Run(ctx context.Context, stdio config.Stdio) error {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return err
	}

	var (
		reqURL   string
		insecure bool
		trusted  bool
	)
	if strings.HasPrefix(c.Endpoint, "http://") || strings.HasPrefix(c.Endpoint, "https://") {
		reqURL = c.Endpoint
		// Only attach credentials if the host matches a configured metro for
		// the current profile, to avoid leaking the bearer token to
		// arbitrary hosts.
		u, err := url.Parse(reqURL)
		if err != nil {
			return jujuerrors.Annotate(err, "parsing endpoint URL")
		}
		// XXX: show against proxy!
		for i := range profile.Metros {
			mu, err := url.Parse(profile.Metros[i].Endpoint)
			if err != nil {
				continue
			}
			if mu.Host == u.Host {
				trusted = true
				insecure = ptr.ZeroIfNil(profile.Metros[i].Insecure)
				break
			}
		}
	} else {
		metroName := c.Metro
		if metroName == "" {
			metroName = profile.GetDefaultMetro()
		}
		if metroName == "" {
			return jujuerrors.New("no metro specified and no default metro set for the current profile")
		}

		var metro *config.Metro
		for i := range profile.Metros {
			if profile.Metros[i].Name == metroName {
				metro = &profile.Metros[i]
				break
			}
		}

		path := c.Endpoint
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		reqURL = strings.TrimRight(metro.Endpoint, "/") + path
		insecure = ptr.ZeroIfNil(metro.Insecure)
		trusted = true
	}
	if c.Insecure {
		insecure = true
	}

	// Resolve the request body.
	var body io.Reader
	if c.Data != "" {
		switch {
		case c.Data == "@-":
			body = stdio.Stdin
		case strings.HasPrefix(c.Data, "@"):
			f, err := os.Open(c.Data[1:])
			if err != nil {
				return jujuerrors.Annotate(err, "reading request body")
			}
			defer f.Close()
			body = f
		default:
			body = strings.NewReader(c.Data)
		}
	}

	method := strings.ToUpper(c.Method)
	if method == "" {
		if c.Data != "" {
			method = http.MethodPost
		} else {
			method = http.MethodGet
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return err
	}
	if trusted {
		req.Header.Set("Authorization", "Bearer "+profile.Token)
	}
	req.Header.Set("Accept", "application/json")
	if c.Data != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, h := range c.Header {
		k, v, ok := strings.Cut(h, ":")
		if !ok {
			return jujuerrors.Errorf("invalid header %q: expected 'Key: Value'", h)
		}
		req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
	}

	log.G(ctx).
		Debug().
		Str("method", method).
		Str("url", reqURL).
		Msg("sending API request")

	resp, err := httpclient.GetClient(insecure).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if len(raw) > 0 {
		if json.Valid(raw) {
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, raw, "", "  "); err == nil {
				pretty.WriteByte('\n')
				if _, err := stdio.Stdout.Write(pretty.Bytes()); err != nil {
					return err
				}
			} else {
				if _, err := stdio.Stdout.Write(raw); err != nil {
					return err
				}
			}
		} else {
			if _, err := stdio.Stdout.Write(raw); err != nil {
				return err
			}
			if !bytes.HasSuffix(raw, []byte("\n")) {
				fmt.Fprintln(stdio.Stdout)
			}
		}
	}

	if resp.StatusCode >= 400 {
		return jujuerrors.Errorf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return nil
}

func (APICmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "List instances in the default metro",
			Commands: []string{
				"unikraft api /v1/instances",
			},
		},
		{
			Description: "Get the current user's quotas",
			Commands: []string{
				"unikraft api /v1/users/quotas",
			},
		},
		{
			Description: "Inspect a specific instance by UUID",
			Commands: []string{
				"unikraft api /v1/instances/abc123-...-def456",
			},
		},
		{
			Description: "Create a new 256MB volume in a specific metro",
			Commands: []string{
				`unikraft api /v1/volumes --metro=fra -d '{"name":"data","size_mb":256}'`,
			},
		},
		{
			Description: "Create resources from a JSON file",
			Commands: []string{
				"unikraft api /v1/volumes -d @volume.json",
			},
		},
		{
			Description: "Delete an instance by UUID",
			Commands: []string{
				"unikraft api /v1/instances/abc123-...-def456 -X DELETE",
			},
		},
		{
			Description: "Pipe a request body in from stdin",
			Commands: []string{
				"unikraft api /v1/volumes -d @-",
			},
		},
		{
			Description: "Check the health of the API",
			Commands: []string{
				"unikraft api /v1/healthz",
			},
		},
	}
}
