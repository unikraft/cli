// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tunnel

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/log"
)

// proxyInfo holds the metadata for a running tunnel proxy instance.
type proxyInfo struct {
	uuid string
	fqdn string
	// closed indicates whether this proxy instance has been closed.
	closed bool
}

// ParseTargets parses raw target strings and proxy port specifications into
// structured target objects.
// proxyPorts configures which ports the tunnel service exposes:
// if a single port is given it is used as the starting port for sequential
// assignment; otherwise there must be exactly one port per target.
func ParseTargets(rawTargets []string, proxyPorts []string) ([]target, error) {
	if len(rawTargets) == 0 {
		return nil, fmt.Errorf("at least one target must be specified")
	}

	if len(proxyPorts) == 0 {
		return nil, fmt.Errorf("at least one proxy port must be specified")
	}

	targets := make([]target, len(rawTargets))
	for i, raw := range rawTargets {
		t, err := parseTarget(raw)
		if err != nil {
			return nil, fmt.Errorf("parsing target %q: %w", raw, err)
		}
		targets[i] = t
	}

	parsedPorts, err := parseProxyPorts(proxyPorts)
	if err != nil {
		return nil, err
	}

	if len(parsedPorts) > 1 && len(parsedPorts) != len(targets) {
		return nil, fmt.Errorf("number of proxy ports must be either 1 or equal to the number of targets")
	}

	// Assign exposed proxy ports.
	for i := range targets {
		if len(parsedPorts) == 1 {
			targets[i].exposedProxyPort = parsedPorts[0] + uint16(i)
		} else {
			targets[i].exposedProxyPort = parsedPorts[i]
		}
	}
	return targets, nil
}

func parseProxyPorts(ports []string) ([]uint16, error) {
	parsed := make([]uint16, 0, len(ports))
	for _, port := range ports {
		p, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid port number", port)
		}
		parsed = append(parsed, uint16(p))
	}
	return parsed, nil
}

// formatProxyArgs formats the arguments to pass to a tunnel service instance
// for the given set of targets.
// NOTE(craciunoiuc): Proxy arguments can be found at:
// https://github.com/unikraft-cloud/catalog/tree/prod-staging/library/official/utils/tunlr/1.0
func formatProxyArgs(targets []target, authStr string, proxyControlPort uint) []string {
	connections := make([]string, 0, len(targets))
	for _, tgt := range targets {
		connections = append(connections, fmt.Sprintf("TCP2%s:%s:%d:%d:%d",
			strings.ToUpper(tgt.network),
			tgt.host,
			tgt.dest,
			tgt.exposedProxyPort,
			27,
		))
	}
	return []string{
		// HEARTBEAT_PORT:CTLR_AUTH_TIMEOUT
		fmt.Sprintf("%d:%d", proxyControlPort, 5),
		// AUTH_TIMEOUT:AUTH_COOKIE
		fmt.Sprintf("%d:%s", 5, authStr),
		// EVS_TIMEOUT
		"600",
		// [CONNSTR0|CONNSTR1|...]
		"[" + strings.Join(connections, "|") + "]",
	}
}

// parseTarget parses a single CLI forwarding target string of the form
// [LOCAL_PORT:][METRO/]INSTANCE:DEST_PORT[/TYPE].
func parseTarget(raw string) (target, error) {
	rest := raw
	network := "tcp"

	// Split connection type from the last "/" — but only if the suffix looks
	// like a protocol name rather than a metro/instance path component.
	if idx := strings.LastIndex(rest, "/"); idx >= 0 {
		suffix := rest[idx+1:]
		if strings.EqualFold(suffix, "tcp") || strings.EqualFold(suffix, "udp") {
			network = strings.ToLower(suffix)
			rest = rest[:idx]
		}
	}

	if network != "tcp" {
		return target{}, fmt.Errorf("unsupported connection type %q: only tcp is supported", network)
	}

	segments := strings.SplitN(rest, ":", 3)
	switch len(segments) {
	case 2:
		// INSTANCE:DEST_PORT — no local port override, use 0 for random port.
		if _, parseErr := strconv.ParseUint(segments[0], 10, 16); parseErr == nil {
			return target{}, fmt.Errorf("%q is not a valid instance identifier", segments[0])
		}
		rport64, parseErr := strconv.ParseUint(segments[1], 10, 16)
		if parseErr != nil {
			return target{}, fmt.Errorf("%q is not a valid port number", segments[1])
		}
		return target{host: segments[0], source: 0, dest: uint16(rport64), network: network}, nil
	case 3:
		// LOCAL_PORT:INSTANCE:DEST_PORT
		lport64, parseErr := strconv.ParseUint(segments[0], 10, 16)
		if parseErr != nil {
			return target{}, fmt.Errorf("%q is not a valid port number", segments[0])
		}
		rport64, parseErr := strconv.ParseUint(segments[2], 10, 16)
		if parseErr != nil {
			return target{}, fmt.Errorf("%q is not a valid port number", segments[2])
		}
		return target{host: segments[1], source: uint16(lport64), dest: uint16(rport64), network: network}, nil
	default:
		return target{}, fmt.Errorf("%q is not a valid forwarding target (expected [LOCAL_PORT:]INSTANCE:DEST_PORT[/TYPE])", raw)
	}
}

// createProxy creates a single tunnel proxy instance in the given metro for
// the provided targets.
func createProxy(ctx context.Context, g *group.Group[multimetro.MetroClient], metro string, targets []target, authStr string, proxyControlPort uint, tunnelImage string) (proxyInfo, error) {
	args := formatProxyArgs(targets, authStr, proxyControlPort)

	services := make([]platform.Service, 0, len(targets)+1)
	for _, tgt := range targets {
		p := uint32(tgt.exposedProxyPort)
		services = append(services, platform.Service{
			Port:            p,
			DestinationPort: &p,
			Handlers:        []platform.ConnectionHandler{platform.ConnectionHandlerTls},
		})
	}
	ctrlPort := uint32(proxyControlPort)
	services = append(services, platform.Service{
		Port:            ctrlPort,
		DestinationPort: &ctrlPort,
		Handlers:        []platform.ConnectionHandler{platform.ConnectionHandlerTls},
	})

	image := tunnelImage
	req := platform.CreateInstanceRequest{
		Image:    &image,
		MemoryMb: new(int64(128)),
		Args:     args,
		ServiceGroup: &platform.CreateInstanceRequestServiceGroup{
			Services: services,
		},
		Autostart: new(bool(true)),
		TimeoutS:  new(int64(-1)),
		Features:  []platform.InstanceFeature{platform.InstanceFeatureDeleteOnStop},
	}

	return group.CollectMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) (proxyInfo, error) {
		log.G(ctx).Trace().Msg("creating tunnel proxy instance")
		resp, err := c.CreateInstance(ctx, req)
		if err != nil {
			return proxyInfo{}, fmt.Errorf("creating proxy instance: %w", err)
		}
		if resp.Data == nil || len(resp.Data.Instances) == 0 {
			return proxyInfo{}, fmt.Errorf("no instance returned after creation")
		}
		inst := resp.Data.Instances[0]
		uuid := inst.Uuid

		var fqdn string
		if inst.ServiceGroup != nil && len(inst.ServiceGroup.Domains) > 0 {
			fqdn = inst.ServiceGroup.Domains[0].Fqdn
		}
		if fqdn == "" {
			return proxyInfo{}, fmt.Errorf("tunnel proxy has no service group domain")
		}
		return proxyInfo{uuid: uuid, fqdn: fqdn}, nil
	})
}
