// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tunnel

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/volimport"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"
)

// target describes a single forwarding destination parsed from the CLI
// argument format [LOCAL_PORT:][METRO/]INSTANCE:DEST_PORT[/TYPE].
type target struct {
	// host is the instance identifier (possibly including a metro prefix like
	// "fra/my-instance"). After resolution it is replaced by a private IP.
	host string
	// source is the local port to listen on. 0 means the OS picks a free port.
	source uint16
	// dest is the port on the remote instance to forward to.
	dest uint16
	// network is the connection type, e.g. "tcp" (like in net.Dial).
	network string
	// exposedProxyPort is the port exposed by the tunnel service for this
	// target. It is computed from the proxy port configuration.
	exposedProxyPort uint16
	// metro is the metro where this target's instance lives, set by resolve.
	metro string
}

type Tunnel struct {
	Targets []target

	auth string
	// proxies maps metro name to the running tunnel proxy in that metro.
	// Populated by Run, consumed by Start and Terminate.
	proxies map[string]proxyInfo
}

// New creates a tunnel structure. It sets targets, assigns proxy ports,
// and resolves instance names to private IPs via the platform API.
func New(ctx context.Context, targets []target) (*Tunnel, error) {
	authStr, err := volimport.GenRandAuth()
	if err != nil {
		return nil, fmt.Errorf("could not generate auth string: %w", err)
	}

	t := &Tunnel{Targets: targets, auth: authStr}
	if err := t.resolve(ctx); err != nil {
		return nil, err
	}
	return t, nil
}

// resolve looks up each target's instance on the platform, replacing the Host
// with its private IP and recording the metro.
func (t *Tunnel) resolve(ctx context.Context) error {
	keys := make(multimetro.Keys, len(t.Targets))
	for i, tgt := range t.Targets {
		keys[i] = multimetro.ParseKey(tgt.host)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	type instanceInfo struct {
		ip    string
		metro string
	}
	type resolvedInstance struct {
		ref  group.Ref
		info instanceInfo
	}

	resolved, err := group.CollectRefsSlices(ctx, g, keys.Refs(),
		func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resolvedInstance, group.Refs, error) {
			log.G(ctx).Trace().Msg("getting instances for tunnel")
			resp, err := c.GetInstances(ctx, refs.NameOrUUIDs(), platform.GetInstancesOpts{Details: new(true)})
			if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil, err
			}
			var found group.Refs
			var results []resolvedInstance
			for i, inst := range resp.Data.Instances {
				if inst.Status == nil || *inst.Status != platform.ResponseStatusSuccess {
					continue
				}
				if len(inst.NetworkInterfaces) == 0 || inst.NetworkInterfaces[0].PrivateIp == "" {
					return nil, nil, fmt.Errorf("instance %q has no private IP", refs[i].Display)
				}
				found = append(found, refs[i])
				results = append(results, resolvedInstance{
					ref: refs[i],
					info: instanceInfo{
						ip:    inst.NetworkInterfaces[0].PrivateIp,
						metro: c.Metro.Name,
					},
				})
			}
			return results, found, nil
		})
	if err != nil {
		return fmt.Errorf("could not resolve instances: %w", err)
	}

	// NOTE(craciunoiuc): if the same instance name appears in multiple metros
	// (no metro prefix given), the last-seen result wins. Users should include
	// a metro prefix to disambiguate (e.g. fra/my-instance:8080).
	infoByDisplay := make(map[string]instanceInfo, len(resolved))
	for _, r := range resolved {
		infoByDisplay[r.ref.Display] = r.info
	}

	for i := range t.Targets {
		info, ok := infoByDisplay[t.Targets[i].host]
		if !ok {
			return fmt.Errorf("could not determine metro for %q: include the metro prefix (e.g. fra/my-instance:8080)", t.Targets[i].host)
		}
		t.Targets[i].host = info.ip
		t.Targets[i].metro = info.metro
	}
	return nil
}

// Run creates one tunnel service proxy instance per unique metro and stores
// their metadata. For each metro, a control relay is
// established to the proxy in that metro, then data relays are started for
// every target in that metro.
func (t *Tunnel) Run(ctx context.Context, g *group.Group[multimetro.MetroClient], proxyControlPort uint, tunnelImage string) error {
	// Group targets by metro.
	metroTargets := make(map[string][]target)
	for _, tgt := range t.Targets {
		metroTargets[tgt.metro] = append(metroTargets[tgt.metro], tgt)
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return fmt.Errorf("getting current profile: %w", err)
	}

	metroInsecureMap := make(map[string]bool)
	for _, m := range profile.Metros {
		metroInsecureMap[m.Name] = ptr.ZeroIfNil(m.Insecure)
	}

	t.proxies = make(map[string]proxyInfo, len(metroTargets))
	for metro, targets := range metroTargets {
		info, err := createProxy(ctx, g, metro, targets, t.auth, proxyControlPort, tunnelImage)
		if err != nil {
			return fmt.Errorf("creating proxy in metro %q: %w", metro, err)
		}
		t.proxies[metro] = info
	}

	// Start a control relay per metro proxy.
	for metro, proxy := range t.proxies {
		cr := tunnelRelay{
			remoteAddr: net.JoinHostPort(proxy.fqdn, strconv.FormatUint(uint64(proxyControlPort), 10)),
			auth:       t.auth,
			insecure:   metroInsecureMap[metro],
		}
		ready := make(chan struct{})
		go func() {
			defer close(ready)
			if err := cr.controlUp(ctx, ready); err != nil {
				log.G(ctx).Error().Err(err).Str("metro", metro).Msg("control relay error")
			}
		}()
		// Wait for this metro's control relay before starting its data relays.
		<-ready
	}

	eg, ctx := errgroup.WithContext(ctx)
	for _, tgt := range t.Targets {
		proxy := t.proxies[tgt.metro]
		r := tunnelRelay{
			// TODO(antoineco): allow dual-stack by creating two separate listeners.
			// Alternatively, we could default to "::" to create a tcp46 socket, but
			// listening on all addresses is an insecure default.
			localAddr:  net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(tgt.source), 10)),
			remoteAddr: net.JoinHostPort(proxy.fqdn, strconv.FormatUint(uint64(tgt.exposedProxyPort), 10)),
			// NOTE(craciunoiuc): Only TCP is supported at the moment. This refers to the
			// local listener; the remote side always uses TLS-over-TCP.
			connectionType: tgt.network,
			auth:           t.auth,
			name:           proxy.uuid,
			nameAddr:       fmt.Sprintf("%s:%d", tgt.host, tgt.dest),
			insecure:       metroInsecureMap[tgt.metro],
		}
		eg.Go(func() error {
			return r.up(ctx)
		})
	}

	return eg.Wait()
}

// Close removes all tunnel proxy instances across all metros.
func (t *Tunnel) Close(ctx context.Context, g *group.Group[multimetro.MetroClient]) error {
	var errs []error
	for metro, proxy := range t.proxies {
		if proxy.closed {
			continue
		}

		err := group.DoMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) error {
			log.G(ctx).Trace().Msg("deleting tunnel proxy instance")
			_, err := c.DeleteInstances(ctx, []platform.DeleteInstanceRequestItem{{Uuid: &proxy.uuid}})
			if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return fmt.Errorf("deleting proxy instance %q: %w", proxy.uuid, err)
			}
			proxy.closed = true
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// tunnelConnection represents an accepted local connection being relayed to a
// remote host through the tunnel service.
type tunnelConnection struct {
	relay *tunnelRelay
	conn  net.Conn
}

// handle relays data between the local connection and the remote host.
func (c *tunnelConnection) handle(ctx context.Context, auth []byte, instance, instanceRaw string) {
	defer func() {
		c.conn.Close()
		log.G(ctx).Info().Str("for", instanceRaw).Msg("closed connection")
	}()

	rc, err := c.relay.dialRemote(ctx)
	if err != nil {
		log.G(ctx).Error().Err(err).Msg("failed to connect to remote host")
		return
	}
	defer rc.Close()

	log.G(ctx).Debug().
		Str("for", c.conn.RemoteAddr().String()).
		Str("from", rc.LocalAddr().String()).
		Str("to", rc.RemoteAddr().String()).
		Msg("opened connection")
	log.G(ctx).Info().Str("to", instanceRaw).Msg("accepted connection")

	_ = rc.SetDeadline(tunnelNoNetTimeout)
	_ = c.conn.SetDeadline(tunnelNoNetTimeout)

	defer func() {
		_ = c.conn.SetDeadline(tunnelImmediateNetCancel)
	}()

	if len(auth) > 0 {
		_, err = rc.Write(auth)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to write auth to remote host")
			return
		}

		statusRaw := bytes.NewBuffer(nil)
		n, err := io.CopyN(statusRaw, rc, 2)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to read auth status from remote host")
			return
		}
		if n != 2 {
			log.G(ctx).Error().Msg("invalid auth status from remote host")
			return
		}

		var status int16
		if err = binary.Read(statusRaw, binary.LittleEndian, &status); err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to parse auth status from remote host")
			return
		}

		if status == 0 {
			log.G(ctx).Error().Msg("no available connections to remote host, try again later")
			return
		} else if status < 0 {
			log.G(ctx).Error().Msgf("internal tunnel error (C=%d), to view logs run:", status)
			log.G(ctx).Error().Msgf("    unikraft instance logs %s\n", instance)
			return
		}
	}

	writerDone := make(chan struct{})
	go func() {
		defer func() {
			_ = rc.SetDeadline(tunnelImmediateNetCancel)
			close(writerDone)
		}()
		_, err = io.Copy(rc, c.conn)
		if err != nil && !isNetClosedError(err) && !isNetTimeoutError(err) {
			log.G(ctx).Error().Err(err).Msg("failed to copy data from client to remote host")
		}
	}()

	_, err = io.Copy(c.conn, rc)
	if err != nil {
		if !isNetTimeoutError(err) && !isNetClosedError(err) {
			log.G(ctx).Error().Err(err).Msg("failed to copy data from remote host to client")
		}
	} else {
		// Remote closed the connection cleanly; return to close our side.
		return
	}

	<-writerDone
}

func isNetTimeoutError(err error) bool {
	var neterr net.Error
	return errors.As(err, &neterr) && neterr.Timeout()
}

func isNetClosedError(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		strings.Contains(err.Error(), "connection reset by peer")
}
