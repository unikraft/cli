// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package logs

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"slices"
	"time"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"
)

// TODO: batch requests for multiple instances together:
// - we can batch the polling to avoid request flood
// - we can batch rewind fairly easily
// - it's a bit harder to batch arbitrary ReadAt calls

func InstanceLogs(ctx context.Context, client platform.Client) *LogsReader {
	return &LogsReader{
		ctx:    ctx,
		client: client,
	}
}

type LogsReader struct {
	ctx    context.Context
	client platform.Client
}

func (lr LogsReader) Reader(id platform.NameOrUUID, tail int, follow bool) (io.Reader, error) {
	r := &logsReader{
		LogsReader: lr,
		id:         id,
		follow:     follow,
	}
	offset, err := r.rewind(tail)
	if err != nil {
		return nil, err
	}
	// section reader needs a max size, but we don't know it here, so just use
	// max int - we'll handle returning EOF ourselves in ReadAt
	return io.NewSectionReader(r, offset, math.MaxInt64), nil
}

type logsReader struct {
	LogsReader
	id platform.NameOrUUID

	follow bool

	start *int64
	end   *int64

	cache       []byte
	cacheOffset int64
}

const (
	// logPageSize is the max size of a single page of logs can be read in a
	// request. This is determined by the underlying platform API.
	logPageSize        = 4096*4 - 1
	logBackoffDuration = 500 * time.Millisecond
)

// rewind rewinds the reader to tail newlines from the end.
func (r *logsReader) rewind(tail int) (n int64, err error) {
	if tail <= 0 {
		return 0, nil
	}

	total := 0

	chunk := make([]byte, logPageSize)
	chunkOffset := ptr.ZeroIfNil(r.end)
	for {
		requestOffset := chunkOffset - logPageSize
		if r.start != nil && requestOffset <= *r.start {
			requestOffset = *r.start
		}
		chunk := chunk[:chunkOffset-requestOffset]

		var chunkSize int
		chunkOffset, chunkSize, err = r.readChunk(chunk, requestOffset)
		if err != nil {
			return 0, err
		}
		done := chunkOffset <= ptr.ZeroIfNil(r.start)
		if chunkSize != len(chunk) && !done {
			return 0, fmt.Errorf("could not rewind logs: incomplete chunk read at offset %d", chunkOffset)
		}

		r.cache = slices.Concat(chunk[:chunkSize], r.cache)
		r.cacheOffset = chunkOffset

		for i := chunkSize - 1; i >= 0; i-- {
			if chunk[i] == '\n' {
				total++
				if total >= tail+1 {
					offset := chunkOffset + int64(i+1)
					log.G(r.ctx).Trace().
						Int64("offset", offset).
						Msg("starting log output from offset")
					return chunkOffset + int64(i+1), nil
				}
			}
		}
		if done {
			log.G(r.ctx).Trace().
				Msg("starting log output from start")
			return 0, nil
		}
	}
}

func (r *logsReader) ReadAt(p []byte, off int64) (n int, err error) {
	if len(r.cache) > 0 && off >= r.cacheOffset && off < r.cacheOffset+int64(len(r.cache)) {
		n = copy(p, r.cache[off-r.cacheOffset:])
		return n, nil
	}

	if r.start != nil && off < *r.start {
		return 0, fmt.Errorf("cannot read before start of logs")
	}
	if r.end != nil && off >= *r.end {
		if !r.follow {
			return 0, io.EOF
		}
	}

	for {
		_, n, err = r.readChunk(p, off)
		if err != nil {
			return n, err
		}
		if r.end != nil && *r.end <= off+int64(n) && !r.follow {
			return n, io.EOF
		}
		if n > 0 {
			return n, nil
		}

		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case <-time.After(logBackoffDuration):
		}
	}
}

func (r *logsReader) readChunk(p []byte, off int64) (actualOffset int64, n int, err error) {
	limit := min(int64(len(p)), logPageSize)

	log.G(r.ctx).Trace().
		Int64("offset", off).
		Int64("limit", limit).
		Msg("fetching logs chunk")

	req := platform.GetInstancesLogsRequestItem{
		Offset: new(off),
		Limit:  new(limit),
	}
	if r.id.Name != nil {
		req.Name = r.id.Name
	}
	if r.id.Uuid != nil {
		req.Uuid = r.id.Uuid
	}
	resp, err := r.client.GetInstanceLogs(r.ctx, []platform.GetInstancesLogsRequestItem{req})
	if err != nil {
		return 0, 0, err
	}
	if len(resp.Data.Instances) == 0 {
		return 0, 0, fmt.Errorf("no data returned")
	}
	data := resp.Data.Instances[0]

	dataRange := ptr.ZeroIfNil(data.Range)
	dataAvailable := ptr.ZeroIfNil(data.Available)

	// HACK: if you try reading out-of-range, the API seems to return a 0+0 available response
	if r.start == nil || r.end == nil || ptr.ZeroIfNil(dataAvailable.Start) != ptr.ZeroIfNil(dataAvailable.End) {
		r.start = data.Available.Start
		r.end = data.Available.End
	}

	n, err = base64.StdEncoding.Decode(p, []byte(ptr.ZeroIfNil(data.Output)))
	if err != nil {
		return 0, 0, err
	}
	dataRangeSize := ptr.ZeroIfNil(dataRange.End) - ptr.ZeroIfNil(dataRange.Start)
	if int64(n) != dataRangeSize {
		return 0, 0, fmt.Errorf("expected to read %d bytes but got %d", dataRangeSize, n)
	}
	return ptr.ZeroIfNil(dataRange.Start), n, nil
}
