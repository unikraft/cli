// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tunnel

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"unikraft.com/x/log"
)

// tunnelRelay relays connections from a local listener to a remote host over TLS.
// NOTE(craciunoiuc): Protocol and heartbeat encoding can be found at:
// https://github.com/unikraft-cloud/catalog/tree/prod-staging/library/official/utils/tunlr/1.0
type tunnelRelay struct {
	localAddr      string
	remoteAddr     string
	connectionType string
	auth           string
	name           string
	nameAddr       string
	insecure       bool
}

const tunnelHeartbeat = "\xf0\x9f\x91\x8b\xf0\x9f\x90\x92\x00"

var (
	tunnelNoNetTimeout       = time.Time{}
	tunnelImmediateNetCancel = time.Unix(1, 0)
)

// up starts a local listener and relays accepted connections to the remote host.
func (r *tunnelRelay) up(ctx context.Context) error {
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, r.connectionType+"4", r.localAddr)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { l.Close() })
	defer func() {
		stop()
		l.Close()
	}()

	log.G(ctx).Info().Str("from", l.Addr().String()).Str("to", r.nameAddr).Msg("tunnelling")
	log.G(ctx).Debug().Str("via", r.remoteAddr).Msg("tunnelling")

	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accepting incoming connection: %w", err)
		}
		c := &tunnelConnection{relay: r, conn: conn}
		go c.handle(ctx, []byte(r.auth), r.name, r.nameAddr)
	}
}

// controlUp dials the remote control port, signals ready, then sends periodic
// heartbeats to keep the tunnel service alive.
func (r *tunnelRelay) controlUp(ctx context.Context, ready chan struct{}) error {
	rc, err := r.dialRemote(ctx)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { rc.Close() })
	defer func() {
		stop()
		rc.Close()
	}()

	ready <- struct{}{} // signal that the control connection is established

	// Send auth and initial heartbeat.
	_, err = io.CopyN(rc, bytes.NewReader([]byte(r.auth+tunnelHeartbeat)), int64(len(r.auth)+len(tunnelHeartbeat)))
	if err != nil {
		return err
	}
	// Send a heartbeat every minute to keep the connection alive.
	for {
		time.Sleep(time.Minute)
		_, err := io.CopyN(rc, bytes.NewReader([]byte(tunnelHeartbeat)), int64(len(tunnelHeartbeat)))
		if err != nil {
			return err
		}
	}
}

func (r *tunnelRelay) dialRemote(ctx context.Context) (net.Conn, error) {
	var d tls.Dialer
	d.Config = &tls.Config{
		InsecureSkipVerify: r.insecure, //nolint:gosec // insecure connections are allowed if wanted
	}
	return d.DialContext(ctx, "tcp4", r.remoteAddr)
}
