// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
)

type ProxyCmd struct {
	Targets []string `arg:"" help:"Forward targets in format [LOCAL_PORT:]<INSTANCE|PRIVATE_IP|PRIVATE_FQDN>:DEST_PORT[/TYPE]." required:""`

	TunnelProxyPorts []uint16 `short:"p" name:"tunnel-proxy-port" help:"Remote port exposed by the tunnelling service(s). (default start port is 4444)"`
	ProxyControlPort uint16   `short:"P" name:"tunnel-control-port" help:"Command-and-control port used by the tunneling service(s)." default:"4443"`
	TunnelImage      string   `name:"tunnel-image" help:"Tunnel service image." default:"utils/tunnel:1.0"`
	Metro            string   `help:"Metro to deploy the tunnel service in." required:"" placeholder:"metro"`

	// parsedProxyPorts contains the parsed ProxyPorts converted from string to uint16
	parsedProxyPorts []uint16
	// instances (name/uuid/private-ip) gets turned into private-ip after fetching
	instances []string
	// localPorts to forward on the local machine
	localPorts []uint16
	// ctype is the connection type of the port to forward (tcp/udp)
	ctypes []string
	// instanceProxyPorts is the port to forward of the instance
	instanceProxyPorts []uint16
	// exposedProxyPorts is the port to expose the proxy on
	exposedProxyPorts []uint16
	// portIterator for when a single proxy port is provided
	portIterator uint16
}

const tunnelImageOld = "official/utils/tunnel:latest"

func (ProxyCmd) Help() string {
	return heredoc.Docf(`
		Forward a local port to an unexposed instance through an intermediate TLS
		tunnel service.

		When you need to access an instance on Unikraft Cloud which is not
		publicly exposed to the internet, you can use the
		%[1]sunikraft proxy%[1]s subcommand to forward from a local port to a
		port which the instance listens on.

		The %[1]sunikraft proxy%[1]s subcommand creates a secure tunnel
		between your local machine and the private instance(s). The tunnel is
		created using an intermediate TLS tunnel service which is another instance
		running as a sidecar along with the target instance in the same private
		network. The tunnel service listens on a publicly exposed port on the
		cloud and forwards the traffic to the private instance.

		When you run the %[1]sunikraft proxy%[1]s subcommand, you specify the
		local port to forward, the private instance to connect to, and the port on
		the private instance to connect to.

		It is also possible to customize the remote port which the tunnel service
		exposes and the command-and-control port used by the tunnel service. By
		default, the remote port is %[1]s4444%[1]s and the command-and-control
		port is %[1]s4443%[1]s.
	`, "`")
}

func (ProxyCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Forward to the TCP port 8080 of an unexposed instance by name",
			Commands:    []string{"unikraft proxy --metro=fra nginx:8080"},
		},
		{
			Description: "Forward to the TCP port 8080 of an unexposed instance by private FQDN",
			Commands:    []string{"unikraft proxy --metro=fra nginx.internal:8080"},
		},
		{
			Description: "Forward to the TCP port 8080 of an unexposed instance by private IP",
			Commands:    []string{"unikraft proxy --metro=fra 172.16.28.8:8080"},
		},
		{
			Description: "Forward to the UDP port 8123 of an unexposed instance",
			Commands:    []string{"unikraft proxy --metro=fra 172.16.22.2:8123/udp"},
		},
		{
			Description: "Forward with a custom local port",
			Commands:    []string{"unikraft proxy --metro=fra 8333:nginx:8080"},
		},
		{
			Description: "Forward multiple ports from multiple instances",
			Commands:    []string{"unikraft proxy --metro=fra 8080:my-instance1:8080/tcp 8443:my-instance2:8080/tcp"},
		},
		{
			Description: "Tunnel via custom intermediate port",
			Commands:    []string{"unikraft proxy --metro=fra -p 5500 my-instance:8080"},
		},
	}
}

func (cmd *ProxyCmd) Run(ctx context.Context) error {
	if cmd.TunnelImage == tunnelImageOld {
		return fmt.Errorf("the image %q is deprecated, please use the default and update the CLI to the latest version", tunnelImageOld)
	}

	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return fmt.Errorf("could not get current profile: %w", err)
	}

	metro := cmd.findMetro(profile)
	if metro == nil {
		return fmt.Errorf("metro %q not found in profile", cmd.Metro)
	}

	// If no proxy ports are provided, default to 4444
	if len(cmd.TunnelProxyPorts) == 0 {
		cmd.parsedProxyPorts = []uint16{4444}
	} else {
		cmd.parsedProxyPorts = cmd.TunnelProxyPorts
	}

	if len(cmd.parsedProxyPorts) > 1 && len(cmd.parsedProxyPorts) != len(cmd.Targets) {
		return fmt.Errorf("supplied number of proxy ports must match the number of ports to forward")
	}

	if err := cmd.parseArgs(ctx); err != nil {
		return fmt.Errorf("could not parse arguments: %w", err)
	}

	client := platform.NewClient(
		platform.WithHTTPClient(httpclient.GetClient(metro.Insecure)),
		platform.WithToken(profile.Token),
		platform.WithDefaultMetro(metro.Endpoint),
	)

	rawInstances := cmd.instances
	cmd.instances, err = cmd.populatePrivateIPs(ctx, client, cmd.instances)
	if err != nil {
		return fmt.Errorf("could not populate private IPs: %w", err)
	}

	authStr, err := genRandAuth()
	if err != nil {
		return fmt.Errorf("could not generate random authentication string: %w", err)
	}

	instArgs := cmd.formatProxyArgs(authStr)

	instID, sgFQDN, err := cmd.runProxy(ctx, client, instArgs)
	if err != nil {
		return fmt.Errorf("could not run proxy: %w", err)
	}

	defer func() {
		err := cmd.terminateProxy(context.Background(), client, instID)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("could not terminate proxy")
		}
	}()

	// Control relay used for keeping the connection up
	cr := relay{
		rAddr: net.JoinHostPort(sgFQDN, strconv.FormatUint(uint64(cmd.ProxyControlPort), 10)),
		auth:  authStr,
	}
	ready := make(chan struct{}, 1)
	go func() {
		err := cr.controlUp(ctx, ready)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("could not start control relay")
		}
	}()
	// Wait for the control relay to be ready to be able to connect
	<-ready

	r := relay{
		// TODO(antoineco): allow dual-stack by creating two separate listeners.
		// Alternatively, we could have defaulted to the address "::" to create a
		// tcp46 socket, but listening on all addresses is an insecure default.
		lAddr:    net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(cmd.localPorts[0]), 10)),
		rAddr:    net.JoinHostPort(sgFQDN, strconv.FormatUint(uint64(cmd.exposedProxyPorts[0]), 10)),
		ctype:    cmd.ctypes[0],
		auth:     authStr,
		name:     instID,
		nameAddr: fmt.Sprintf("%s:%d", rawInstances[0], cmd.instanceProxyPorts[0]),
	}

	for i := range cmd.localPorts {
		if i == 0 {
			continue
		}

		pr := relay{
			lAddr:    net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(cmd.localPorts[i]), 10)),
			rAddr:    net.JoinHostPort(sgFQDN, strconv.FormatUint(uint64(cmd.exposedProxyPorts[i]), 10)),
			ctype:    cmd.ctypes[i],
			auth:     authStr,
			name:     instID,
			nameAddr: fmt.Sprintf("%s:%d", rawInstances[i], cmd.instanceProxyPorts[i]),
		}

		go func() {
			err := pr.up(ctx)
			if err != nil {
				log.G(ctx).Error().Err(err).Msg("could not start relay")
			}
		}()
	}

	return r.up(ctx)
}

func (cmd *ProxyCmd) findMetro(profile *config.Profile) *config.Metro {
	for i := range profile.Metros {
		if profile.Metros[i].Name == cmd.Metro {
			return &profile.Metros[i]
		}
	}
	return nil
}

// generatePort generates a port number based on the startPort and the portIterator.
// This is used when a single proxy port is provided and multiple ports are to be forwarded.
func (cmd *ProxyCmd) generatePort(startPort uint16) uint16 {
	defer func() {
		cmd.portIterator++
	}()
	return startPort + cmd.portIterator
}

// parseArgs parses the command line arguments into the instance, local port, remote port and connection type.
func (cmd *ProxyCmd) parseArgs(ctx context.Context) error {
	for i, arg := range cmd.Targets {
		instance, lport, rport, ctype, err := parsePorts(ctx, arg)
		if err != nil {
			return err
		}

		cmd.instances = append(cmd.instances, instance)
		cmd.localPorts = append(cmd.localPorts, lport)
		cmd.instanceProxyPorts = append(cmd.instanceProxyPorts, rport)
		cmd.ctypes = append(cmd.ctypes, ctype)

		if len(cmd.parsedProxyPorts) == 1 {
			cmd.exposedProxyPorts = append(cmd.exposedProxyPorts, cmd.generatePort(cmd.parsedProxyPorts[0]))
		} else {
			cmd.exposedProxyPorts = append(cmd.exposedProxyPorts, cmd.parsedProxyPorts[i])
		}
	}

	return nil
}

// runProxy runs a proxy instance with the given arguments.
// Information related to the proxy instance is hardcoded, but the UUID is returned.
func (cmd *ProxyCmd) runProxy(ctx context.Context, cli platform.Client, args []string) (string, string, error) {
	var services []platform.Service
	for i := range cmd.exposedProxyPorts {
		services = append(services, platform.Service{
			Port:            uint32(cmd.exposedProxyPorts[i]),
			DestinationPort: new(uint32(cmd.exposedProxyPorts[i])),
			Handlers:        []platform.ServiceHandlers{platform.ServiceHandlersTls},
		})
	}
	services = append(services, platform.Service{
		Port:            uint32(cmd.ProxyControlPort),
		DestinationPort: new(uint32(cmd.ProxyControlPort)),
		Handlers:        []platform.ServiceHandlers{platform.ServiceHandlersTls},
	})

	resp, err := cli.CreateInstance(ctx, platform.CreateInstanceRequest{
		Image:    &cmd.TunnelImage,
		MemoryMb: new(int64(64)),
		Args:     args,
		ServiceGroup: &platform.CreateInstanceRequestServiceGroup{
			Services: services,
		},
		Autostart: new(true),
		TimeoutS:  new(int64(3)),
		Features:  []platform.CreateInstanceRequestFeatures{platform.CreateInstanceRequestFeatures("delete-on-stop")},
	})
	if err != nil {
		return "", "", fmt.Errorf("creating proxy instance: %w", err)
	}
	if len(resp.Data.Instances) == 0 {
		return "", "", fmt.Errorf("no instances created")
	}

	inst := resp.Data.Instances[0]
	uuid := ptr.ZeroIfNil(inst.Uuid)
	if inst.ServiceGroup == nil || len(inst.ServiceGroup.Domains) == 0 {
		return "", "", fmt.Errorf("proxy instance has no service group domains")
	}

	fqdn := ptr.ZeroIfNil(inst.ServiceGroup.Domains[0].Fqdn)
	return uuid, fqdn, nil
}

// parsePorts parses a command line argument in the format [lport:]instance:rport[/ctype] into
// two port numbers lport and rport. If lport isn't set, a random port will be
// used by the relay. If ctype isn't set, the connection will be assumed to be TCP.
func parsePorts(ctx context.Context, portsArg string) (instance string, lport, rport uint16, ctype string, err error) {
	types := strings.SplitN(portsArg, "/", 2)
	if len(types) == 2 {
		ctype = types[1]
	} else {
		ctype = "tcp"
	}

	if !strings.EqualFold(ctype, "tcp") {
		log.G(ctx).Warn().Msg("only TCP connections are supported at the moment")
	}

	ports := strings.SplitN(types[0], ":", 3)

	if len(ports) == 2 {
		if _, err := strconv.ParseUint(ports[0], 10, 16); err == nil {
			return "", 0, 0, "", fmt.Errorf("%q is not a valid instance", ports[0])
		}

		rport64, err := strconv.ParseUint(ports[1], 10, 16)
		if err != nil {
			return "", 0, 0, "", fmt.Errorf("%q is not a valid port number", ports[1])
		}
		return ports[0], uint16(rport64), uint16(rport64), ctype, nil
	}

	lport64, err := strconv.ParseUint(ports[0], 10, 16)
	if err != nil {
		return "", 0, 0, "", fmt.Errorf("%q is not a valid port number", ports[0])
	}

	rport64, err := strconv.ParseUint(ports[2], 10, 16)
	if err != nil {
		return "", 0, 0, "", fmt.Errorf("%q is not a valid port number", ports[2])
	}

	return ports[1], uint16(lport64), uint16(rport64), ctype, nil
}

// formatProxyArgs formats the arguments to be passed to the proxy instance.
func (cmd *ProxyCmd) formatProxyArgs(authStr string) []string {
	var connections []string

	for i := range cmd.instances {
		connections = append(connections,
			fmt.Sprintf("TCP2%s:%s:%d:%d:%d",
				strings.ToUpper(cmd.ctypes[i]),
				cmd.instances[i],
				cmd.instanceProxyPorts[i],
				cmd.exposedProxyPorts[i],
				27,
			),
		)
	}

	var allConnections string
	for _, conn := range connections {
		allConnections += conn + "|"
	}
	allConnections = "[" + strings.TrimSuffix(allConnections, "|") + "]"

	return []string{
		// HEARTBEAT_PORT:CTLR_AUTH_TIMEOUT
		fmt.Sprintf("%d:%d", cmd.ProxyControlPort, 5),
		// AUTH_TIMEOUT:AUTH_COOKIE
		fmt.Sprintf("%d:%s", 5, authStr),
		// EVS_TIMEOUT
		fmt.Sprintf("%d", 600),
		// [HOOKSTR0:<HOOKSTR0_ARGS>|HOOKSTR1:<HOOKSTR1_ARGS>...]
		allConnections,
	}
}

// populatePrivateIPs fetches the private IPs of the instances and replaces the instance names/uuids with the Private IPs.
func (cmd *ProxyCmd) populatePrivateIPs(ctx context.Context, cli platform.Client, targets []string) ([]string, error) {
	ips := make([]string, len(targets))
	copy(ips, targets)

	var instancesToGet []platform.NameOrUUID
	var indexesToGet []int
	for i := range targets {
		// If instance is not an IP (PrivateIP) or PrivateFQDN
		// assume it is a name/UUID and fetch the IP
		target := targets[i]
		if net.ParseIP(target) == nil && !strings.HasSuffix(target, ".internal") {
			instancesToGet = append(instancesToGet, platform.NameOrUUID{Name: &target})
			indexesToGet = append(indexesToGet, i)
		}
	}

	if len(instancesToGet) > 0 {
		resp, err := cli.GetInstances(ctx, instancesToGet, nil)
		if err != nil {
			return nil, fmt.Errorf("getting instances: %w", err)
		}

		for i, inst := range resp.Data.Instances {
			if inst.Status == nil || *inst.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			if len(inst.NetworkInterfaces) == 0 || inst.NetworkInterfaces[0].PrivateIp == nil {
				return nil, fmt.Errorf("instance %q has no private IP", ptr.ZeroIfNil(instancesToGet[i].Name))
			}
			ips[indexesToGet[i]] = *inst.NetworkInterfaces[0].PrivateIp
		}
	}

	return ips, nil
}

// terminateProxy terminates the proxy instance with the given UUID.
func (cmd *ProxyCmd) terminateProxy(ctx context.Context, cli platform.Client, instID string) error {
	resp, err := cli.DeleteInstances(ctx, []platform.NameOrUUID{{Uuid: &instID}})
	if err != nil {
		return fmt.Errorf("deleting proxy instance %q: %w", instID, err)
	}
	if len(resp.Data.Instances) == 0 {
		return fmt.Errorf("no response when deleting proxy instance %q", instID)
	}
	return nil
}

// genRandAuth generates a random authentication string.
func genRandAuth() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random auth: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// relay relays connections from a local listener to a remote host over TLS.
type relay struct {
	lAddr    string
	rAddr    string
	ctype    string
	auth     string
	name     string
	nameAddr string
}

const heartbeat = "\xf0\x9f\x91\x8b\xf0\x9f\x90\x92\x00"

func (r *relay) up(ctx context.Context) error {
	l, err := r.listenLocal(ctx)
	if err != nil {
		return err
	}
	defer l.Close()

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	log.G(ctx).
		Info().
		Str("from", l.Addr().String()).
		Str("to", r.nameAddr).
		Msg("tunnelling")
	log.G(ctx).
		Debug().
		Str("via", r.rAddr).
		Msg("tunnelling")

	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accepting incoming connection: %w", err)
		}

		c := r.newConnection(conn)
		go c.handle(ctx, []byte(r.auth), r.name, r.nameAddr)
	}
}

func (r *relay) controlUp(ctx context.Context, ready chan struct{}) error {
	rc, err := r.dialRemote(ctx)
	if err != nil {
		return err
	}
	defer rc.Close()

	go func() {
		<-ctx.Done()
		rc.Close()
	}()

	ready <- struct{}{}
	close(ready)

	// Heartbeat every minute to keep the connection alive
	_, err = io.CopyN(rc, bytes.NewReader([]byte(r.auth+heartbeat)), int64(len(r.auth)+9))
	if err != nil {
		return err
	}
	for {
		time.Sleep(time.Minute)
		_, err := io.CopyN(rc, bytes.NewReader([]byte(heartbeat)), 9)
		if err != nil {
			return err
		}
	}
}

// newConnection creates a new connection from the given net.Conn.
func (r *relay) newConnection(conn net.Conn) *connection {
	return &connection{
		relay: r,
		conn:  conn,
	}
}

func (r *relay) dialRemote(ctx context.Context) (net.Conn, error) {
	var d tls.Dialer
	return d.DialContext(ctx, "tcp4", r.rAddr)
}

func (r *relay) listenLocal(ctx context.Context) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, r.ctype+"4", r.lAddr)
}

// connection represents the server side of a connection to a local TCP socket.
type connection struct {
	// relay is the relay on which the connection arrived.
	relay *relay
	// conn is the underlying network connection.
	conn net.Conn
}

// handle handles the client connection by relaying reads and writes from/to
// the remote host.
func (c *connection) handle(ctx context.Context, auth []byte, instance, instanceRaw string) {
	defer func() {
		c.conn.Close()
		log.G(ctx).
			Info().
			Str("for", instanceRaw).
			Msg("closed connection")
	}()

	rc, err := c.relay.dialRemote(ctx)
	if err != nil {
		log.G(ctx).Error().Err(err).Msg("failed to connect to remote host")
		return
	}
	defer rc.Close()

	log.G(ctx).
		Debug().
		Str("for", c.conn.RemoteAddr().String()).
		Str("from", rc.LocalAddr().String()).
		Str("to", rc.RemoteAddr().String()).
		Msg("opened connection")
	log.G(ctx).
		Info().
		Str("to", instanceRaw).
		Msg("accepted connection")

	// NOTE(antoineco): these calls are critical as they allow reads/writes to be
	// later cancelled, because the deadline applies to all future and pending
	// I/O and can be dynamically extended or reduced.
	_ = rc.SetDeadline(noNetTimeout)
	_ = c.conn.SetDeadline(noNetTimeout)

	defer func() {
		_ = c.conn.SetDeadline(immediateNetCancel)
	}()

	if len(auth) > 0 {
		_, err = rc.Write(auth)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to write auth to remote host")
			return
		}

		var status []byte
		statusRaw := bytes.NewBuffer(status)
		n, err := io.CopyN(statusRaw, rc, 2)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to read auth status from remote host")
			return
		}

		if n != 2 {
			log.G(ctx).Error().Msg("invalid auth status from remote host")
			return
		}

		var statusParsed int16
		err = binary.Read(statusRaw, binary.LittleEndian, &statusParsed)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to parse auth status from remote host")
			return
		}

		if statusParsed == 0 {
			log.G(ctx).Error().Msg("no more available connections to remote host. Try again later")
			return
		} else if statusParsed < 0 {
			log.G(ctx).Error().Msgf("internal tunnel error (C=%d), to view logs run:", statusParsed)
			fmt.Printf("\n    unikraft instance logs %s\n\n", instance)
			return
		}
	}

	writerDone := make(chan struct{})
	go func() {
		defer func() {
			_ = rc.SetDeadline(immediateNetCancel)
			writerDone <- struct{}{}
		}()

		_, err = io.Copy(rc, c.conn)
		if err != nil {
			if isNetClosedError(err) {
				return
			}
			if !isNetTimeoutError(err) {
				log.G(ctx).Error().Err(err).Msg("failed to copy data from client to remote host")
			}
		}
	}()

	_, err = io.Copy(c.conn, rc)
	if err != nil {
		if !isNetTimeoutError(err) {
			log.G(ctx).Error().Err(err).Msg("failed to copy data from remote host to client")
		}
	} else {
		// Connection was closed remote so we just return to close our side
		return
	}

	<-writerDone
}

var (
	// zero time value used to prevent network operations from timing out.
	noNetTimeout = time.Time{}
	// non-zero time far in the past used for immediate cancellation of network operations.
	immediateNetCancel = time.Unix(1, 0)
)

// isNetTimeoutError reports whether err is a network timeout error.
func isNetTimeoutError(err error) bool {
	var neterr net.Error
	if errors.As(err, &neterr) {
		return neterr.Timeout()
	}
	return false
}

// isNetClosedError reports whether err is a network closed error.
// - first error is for the case when the writer tries to write but the main
// thread already closed the connection.
// - second error is for when reader is still reading but the remote closed
// the connection.
func isNetClosedError(err error) bool {
	return strings.Contains(err.Error(), "use of closed network connection") ||
		strings.Contains(err.Error(), "connection reset by peer")
}
