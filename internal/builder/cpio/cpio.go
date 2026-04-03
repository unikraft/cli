// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2024, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cpio

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/unikraft/go-cpio"
	"unikraft.com/x/log"
)

type createOptions struct {
	allRoot bool
}

type CpioCreateOption func(*createOptions)

// WithAllRoot toggles whether all files permissions should be set to root:root
// instead of the original file permissions.
func WithAllRoot(allRoot bool) CpioCreateOption {
	return func(co *createOptions) {
		co.allRoot = allRoot
	}
}

// CreateFSFromDirectory creates a CPIO filesystem from an existing directory.
func CreateFSFromDirectory(ctx context.Context, w io.Writer, source string, opts ...CpioCreateOption) error {
	c := createOptions{}
	for _, opt := range opts {
		opt(&c)
	}

	cw := cpio.NewWriter(w)

	type hardlinkKey struct {
		device int
		inode  int64
	}
	hardlinks := map[hardlinkKey]string{}

	// Recursively walk and serialize to the output
	if err := filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("received error before parsing path: %w", err)
		}

		internal := strings.TrimPrefix(path, filepath.Clean(source))
		if internal == "" {
			return nil // Do not archive empty paths
		}
		internal = "." + filepath.ToSlash(internal)

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("could not get directory entry info: %w", err)
		}

		if d.Type().IsDir() {
			header := &cpio.Header{
				Name:    internal,
				Mode:    cpio.FileMode(info.Mode().Perm()) | cpio.TypeDir,
				ModTime: info.ModTime(),
				Size:    0, // Directories have size 0 in cpio
			}

			// Populate platform specific information
			FileInfoToCPIOHeader(info, header)

			if err := cw.WriteHeader(header); err != nil {
				return fmt.Errorf("could not write CPIO header: %w", err)
			}
			return nil
		}

		log.G(ctx).
			Debug().
			Str("file", internal).
			Msg("archiving")

		header := &cpio.Header{
			Name:    internal,
			Mode:    cpio.FileMode(info.Mode().Perm()),
			ModTime: info.ModTime(),
			Size:    info.Size(),
		}

		// Populate platform specific information
		FileInfoToCPIOHeader(info, header)

		if c.allRoot {
			header.Uid = 0
			header.Guid = 0
		}

		var data []byte
		switch {
		case info.Mode().IsRegular():
			header.Mode |= cpio.TypeRegular
			isHardlink := header.Links > 1 && header.Inode != 0
			if isHardlink {
				key := hardlinkKey{device: header.DeviceID, inode: header.Inode}
				if _, ok := hardlinks[key]; ok {
					header.Size = 0
					break
				}
				hardlinks[key] = internal
			}

			data, err = os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("could not read file: %w", err)
			}
			header.Size = int64(len(data))

		case info.Mode()&fs.ModeSymlink != 0:
			targetLink, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("could not read symlink: %w", err)
			}
			data = []byte(targetLink)
			header.Mode |= cpio.TypeSymlink
			header.Linkname = targetLink
			header.Size = int64(len(data))

		default:
			log.G(ctx).Warn().Msgf("unsupported file: %s", path)
			return nil
		}

		if err := cw.WriteHeader(header); err != nil {
			return fmt.Errorf("writing cpio header for %q: %w", internal, err)
		}

		if len(data) > 0 {
			if _, err := cw.Write(data); err != nil {
				return fmt.Errorf("could not write CPIO data for %s: %w", internal, err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("could not walk output path: %w", err)
	}

	return cw.Close()
}

// IsCPIOFile reports whether path is an SVR4 CPIO archive by inspecting its
// magic bytes (newc: "070701", newcrc: "070702").
func IsCPIOFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	var magic [6]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	s := string(magic[:])
	return s == "070701" || s == "070702"
}

const (
	// msgMaxSize is the maximum number of bytes written per send to the
	// volimport service, matching its internal socket read buffer size.
	msgMaxSize = 64 * 1024 // 64 KiB
)

// startResponse is the message sent by the volimport service immediately after
// the auth token is accepted.
// Each field is an 8-byte little-endian unsigned integer.
type startResponse struct {
	Free   uint64
	Total  uint64
	Maxlen uint64
}

// stopResponse is the final message sent by the volimport service once all
// archive entries have been processed.  Its layout is identical to
// startResponse.
type stopResponse struct {
	Free   uint64
	Total  uint64
	Maxlen uint64
}

// okResponse represents an acknowledgement frame from the volimport service.
//
// Status codes:
//
//	 1  — entry processed successfully (no payload)
//	 2  — final stop frame with 24-byte payload
//	 0  — server closed (clean end or error accumulation terminator)
//	<0  — server-side error message
type okResponse struct {
	status  int32
	msglen  uint32
	message []byte
}

func (r *okResponse) clear() {
	r.status = 0
	r.msglen = 0
	r.message = nil
}

func (r *okResponse) parseMetadata(raw []byte) error {
	r.clear()
	if err := binary.Read(bytes.NewReader(raw[:4]), binary.LittleEndian, &r.status); err != nil {
		return err
	}
	if r.status == 1 {
		return nil
	}
	return binary.Read(bytes.NewReader(raw[4:8]), binary.LittleEndian, &r.msglen)
}

func (r *okResponse) parse(raw []byte) error {
	if err := r.parseMetadata(raw); err != nil {
		return err
	}
	r.message = raw[8 : 8+r.msglen]
	return nil
}

// waitForOK blocks on conn, consuming acknowledgement frames until a terminal
// status is received.  It returns any payload bytes attached to the terminal
// frame, or an error if the server reported a failure.
func (r *okResponse) waitForOK(conn *tls.Conn, errMsg string) ([]byte, error) {
	retErr := fmt.Errorf("%s", errMsg)
	for {
		var headBuf, bodyBuf []byte
		headRaw := bytes.NewBuffer(headBuf)
		bodyRaw := bytes.NewBuffer(bodyBuf)

		if _, err := io.CopyN(headRaw, conn, 8); err != nil {
			return nil, fmt.Errorf("%w: reading header: %s", retErr, err)
		}
		if err := r.parseMetadata(headRaw.Bytes()); err != nil {
			return nil, fmt.Errorf("%w: parsing header: %s", retErr, err)
		}
		if r.msglen != 0 {
			if _, err := io.CopyN(bodyRaw, conn, int64(r.msglen)); err != nil {
				return nil, fmt.Errorf("%w: reading body: %s", retErr, err)
			}
		}
		raw := append(headRaw.Bytes(), bodyRaw.Bytes()...)
		if err := r.parse(raw); err != nil {
			return nil, fmt.Errorf("%w: parsing body: %s", retErr, err)
		}

		switch {
		case r.status == 0:
			// If retErr is still the initial sentinel, the server closed cleanly.
			if retErr.Error() == errMsg {
				return r.message, nil
			}
			return nil, retErr
		case r.status == 1:
			return nil, nil
		case r.status == 2:
			return r.message, nil
		case r.status < 0:
			var msg string
			if len(r.message) > 0 {
				msg = strings.TrimSuffix(string(r.message[:len(r.message)-1]), "\n")
			}
			retErr = fmt.Errorf("%w: %s", retErr, msg)
		default:
			return nil, fmt.Errorf("unexpected response status: %d", r.status)
		}
	}
}

// waitForOKs runs on a goroutine, consuming per-entry acknowledgement frames
// from the volimport service until the final stop frame arrives or an error
// occurs.  It signals completion by sending to result and waitErr.
func waitForOKs(conn *tls.Conn, auth string, result chan *stopResponse, waitErr chan *error) {
	var err error
	var final *stopResponse
	resp := okResponse{}

	defer func() {
		waitErr <- &err
		result <- final
	}()

	for {
		stopRespRaw, e := resp.waitForOK(conn, "transmission failed")
		if e != nil {
			if strings.Contains(e.Error(), "EOF") ||
				strings.Contains(e.Error(), "use of closed network connection") ||
				strings.Contains(e.Error(), "i/o timeout") ||
				strings.Contains(e.Error(), "broken pipe") {
				return
			}
			err = e
			// Signal the server to terminate.
			_, _ = io.Copy(conn, strings.NewReader(auth))
			_ = conn.SetWriteDeadline(immediateNetCancel)
			_ = conn.SetReadDeadline(immediateNetCancel)
			return
		}
		if len(stopRespRaw) > 0 {
			sr := &stopResponse{}
			if binErr := binary.Read(bytes.NewReader(stopRespRaw), binary.LittleEndian, sr); binErr != nil {
				err = fmt.Errorf("parsing stop response: %w", binErr)
				return
			}
			final = sr
			// Signal the server that we are done.
			_, _ = io.Copy(conn, strings.NewReader(auth))
			return
		}
	}
}

var (
	// noNetTimeout disables I/O deadlines on the connection (zero time = no deadline).
	noNetTimeout = time.Time{}
	// immediateNetCancel is set to a time far in the past to immediately
	// cancel pending network I/O when applied as a deadline.
	immediateNetCancel = time.Unix(1, 0)
)

// Copy streams the CPIO archive at path to the volimport service over conn
// capacity check.  Returns the free and total volume bytes as reported by the
// service after the import completes.
func Copy(ctx context.Context, conn *tls.Conn, auth, path string, force bool, size uint64) (free, total uint64, err error) {
	var resp okResponse

	// Allow context cancellation to interrupt pending network I/O.
	_ = conn.SetWriteDeadline(noNetTimeout)
	_ = conn.SetReadDeadline(noNetTimeout)
	go func() {
		<-ctx.Done()
		_ = conn.SetWriteDeadline(immediateNetCancel)
		_ = conn.SetReadDeadline(immediateNetCancel)
	}()

	// Authenticate.
	if _, err := io.Copy(conn, strings.NewReader(auth)); err != nil {
		return 0, 0, fmt.Errorf("sending auth token: %w", err)
	}

	startRespRaw, err := resp.waitForOK(conn, "authentication failed")
	if err != nil {
		return 0, 0, err
	}

	var sr startResponse
	if err := binary.Read(bytes.NewReader(startRespRaw), binary.LittleEndian, &sr); err != nil {
		return 0, 0, fmt.Errorf("parsing start response: %w", err)
	}

	if size > sr.Free {
		if force {
			log.G(ctx).Warn().
				Str("free", units.BytesSize(float64(sr.Free))).
				Str("required", units.BytesSize(float64(size))).
				Str("total", units.BytesSize(float64(sr.Total))).
				Msg("import might exceed volume capacity")
		} else {
			return 0, 0, fmt.Errorf("not enough free space on volume: need %s, have %s free",
				units.BytesSize(float64(size)),
				units.BytesSize(float64(sr.Free)))
		}
	}

	// Open the archive before starting the background goroutine so that early
	// failures do not leak the goroutine.
	fi, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("opening CPIO archive: %w", err)
	}
	defer fi.Close()

	resultCh := make(chan *stopResponse, 1)
	waitErrCh := make(chan *error, 1)
	go waitForOKs(conn, auth, resultCh, waitErrCh)
	defer func() {
		if retErr := <-waitErrCh; retErr != nil && *retErr != nil {
			err = *retErr
		}
	}()

	reader := cpio.NewReader(fi)
	shouldStop := false

cpioLoop:
	for {
		hdr, raw, nextErr := reader.NextRaw()
		if nextErr == io.EOF {
			if raw == nil {
				break cpioLoop
			}
			shouldStop = true
		} else if nextErr != nil {
			return 0, 0, fmt.Errorf("reading CPIO entry: %w", nextErr)
		}

		// Send the raw 110-byte CPIO header.
		rawBytes := raw.Bytes()
		if _, err := io.CopyN(conn, bytes.NewReader(rawBytes), int64(len(rawBytes))); err != nil {
			break cpioLoop
		}

		// Send the NUL-terminated entry name.
		name := append([]byte(hdr.Name), 0x00)
		if _, err := io.CopyN(conn, bytes.NewReader(name), int64(len(name))); err != nil {
			break cpioLoop
		}

		// TRAILER!!! signals the end of the archive; nothing more to send.
		if shouldStop {
			break cpioLoop
		}

		// Send file content or symlink/hardlink destination.
		if hdr.Linkname == "" {
			// Regular file: stream in msgMaxSize chunks.
			for {
				toSend := msgMaxSize
				if hdr.Size < int64(toSend) {
					toSend = int(hdr.Size)
				}
				buf := make([]byte, toSend)
				n, readErr := reader.Read(buf)
				if readErr == io.EOF {
					break
				} else if readErr != nil {
					return 0, 0, fmt.Errorf("reading CPIO content for %q: %w", hdr.Name, readErr)
				}
				if _, sendErr := io.CopyN(conn, bytes.NewReader(buf[:n]), int64(n)); sendErr != nil {
					break cpioLoop
				}
			}
		} else {
			// Symlink or hardlink: send the link target as content.
			link := []byte(hdr.Linkname)
			if _, err := io.CopyN(conn, bytes.NewReader(link), int64(len(link))); err != nil {
				break cpioLoop
			}
		}
	}

	final := <-resultCh
	if final == nil {
		return 0, 0, fmt.Errorf("no stop response received from import service")
	}
	return final.Free, final.Total, nil
}
