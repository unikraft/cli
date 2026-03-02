// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package progdl provides a download progress bar component using bubbletea.
package progdl

import (
	"context"
	"fmt"
	"io"

	"charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"unikraft.com/x/colors"
)

const maxWidth = 0 // 0 means no max width limit

var helpStyle = lipgloss.NewStyle().Foreground(colors.Info)

// progressWriter wraps an io.Writer to track bytes written and send progress updates.
type progressWriter struct {
	total      int64
	downloaded int64
	file       io.Writer
	onProgress func(float64)
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.file.Write(p)
	pw.downloaded += int64(n)
	if pw.total > 0 && pw.onProgress != nil {
		pw.onProgress(float64(pw.downloaded) / float64(pw.total))
	}
	return n, err
}

// contextReader wraps an io.Reader to respect context cancellation.
// It closes the underlying reader (if it implements io.Closer) when the context is cancelled.
type contextReader struct {
	ctx    context.Context
	r      io.Reader
	closed bool
}

func (cr *contextReader) Read(p []byte) (int, error) {
	// Check context before read
	if err := cr.ctx.Err(); err != nil {
		cr.tryClose()
		return 0, err
	}

	// Do the read with context monitoring
	type readResult struct {
		n   int
		err error
	}
	resultCh := make(chan readResult, 1)

	go func() {
		n, err := cr.r.Read(p)
		resultCh <- readResult{n, err}
	}()

	select {
	case result := <-resultCh:
		return result.n, result.err
	case <-cr.ctx.Done():
		cr.tryClose()
		// Wait for the read to complete (it should error out after close)
		<-resultCh
		return 0, cr.ctx.Err()
	}
}

func (cr *contextReader) tryClose() {
	if cr.closed {
		return
	}
	cr.closed = true
	if closer, ok := cr.r.(io.Closer); ok {
		closer.Close()
	}
}

// progressMsg is sent when download progress is updated.
type progressMsg float64

// doneMsg is sent when the download is complete.
type doneMsg struct{}

// errMsg is sent when an error occurs.
type errMsg struct{ err error }

// Model represents the progress bar state.
type Model struct {
	progress progress.Model
	percent  float64
	done     bool
	err      error
	width    int
}

// New creates a new progress bar model.
func New() Model {
	return Model{
		progress: progress.New(
			progress.WithScaled(false),
			progress.WithColors(colors.Blue500, colors.Emerald500),
		),
	}
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		if maxWidth > 0 && m.width > maxWidth {
			m.width = maxWidth
		}
		m.progress.SetWidth(m.width)
		return m, nil

	case progressMsg:
		m.percent = float64(msg)
		if m.percent > 1.0 {
			m.percent = 1.0
		}
		cmd := m.progress.SetPercent(m.percent)
		return m, cmd

	case doneMsg:
		m.done = true
		m.percent = 1.0
		cmd := m.progress.SetPercent(1.0)
		return m, tea.Sequence(cmd, tea.Quit)

	case errMsg:
		m.err = msg.err
		return m, tea.Quit

	case progress.FrameMsg:
		var cmd tea.Cmd
		m.progress, cmd = m.progress.Update(msg)
		return m, cmd

	default:
		return m, nil
	}
}

// View renders the progress bar.
func (m Model) View() tea.View {
	if m.err != nil {
		return tea.NewView("error\n")
	}

	if m.done {
		return tea.View{}
	}

	return tea.NewView(fmt.Sprintf("%s %s\n",
		m.progress.View(),
		helpStyle.Render(fmt.Sprintf("%.0f%%", m.percent*100)),
	))
}
