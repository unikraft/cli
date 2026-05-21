// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"fmt"
	"image/color"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/x/term"
	"github.com/docker/go-units"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/kvwriter"
	"unikraft.com/cli/internal/tui/watcher"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"
)

// MetroQuotasCmd displays quota usage for all metros in an interactive TUI.
// By default it shows an aggregate of all metros. Tabs allow switching to
// individual metros. Use --watch for periodic refresh.
type MetroQuotasCmd struct {
	Watch         *time.Duration `short:"w" help:"Watch for changes and refresh output. Defaults to 2s." type:"optional" placeholder:"duration"`
	Metro         string         `short:"m" help:"Show quotas for a specific metro only." placeholder:"metro"`
	NoInteractive bool           `name:"no-interactive" short:"n" help:"Print stats once without entering the interactive TUI."`
}

func (cmd *MetroQuotasCmd) Run(ctx context.Context, stdio config.Stdio) error {
	if !cmd.NoInteractive && term.IsTerminal(os.Stdout.Fd()) {
		m := newQuotasModel(ctx, cmd.Watch, cmd.Metro)
		// Pass os.Stdout directly so Bubble Tea can obtain the terminal fd
		// (implements term.File). colorprofile.Writer wraps os.Stdout but
		// does not expose Fd(), which causes ttyOutput to be nil and the
		// renderer to use 0×0 dimensions, producing no output.
		p := tea.NewProgram(m, tea.WithInput(stdio.Stdin), tea.WithOutput(os.Stdout))
		_, err := p.Run()
		return err
	}

	render := func(out io.Writer) error { return cmd.renderOnce(ctx, out) }
	if cmd.Watch != nil {
		interval := cmp.Or(*cmd.Watch, 2*time.Second)
		return watcher.WatchOutput(ctx, interval, stdio.Stdout, render)
	}
	return render(stdio.Stdout)
}

func (cmd *MetroQuotasCmd) renderOnce(ctx context.Context, out io.Writer) error {
	result := fetchAllQuotas(ctx, cmd.Metro)
	data := make(map[string]*platform.Quotas)
	var firstErr error
	for _, e := range result.entries {
		if e.err != nil {
			if firstErr == nil {
				firstErr = e.err
			}
			continue
		}
		data[e.metro] = e.quotas
	}
	if len(data) == 0 && firstErr != nil {
		return firstErr
	}

	var q *platform.Quotas
	if cmd.Metro != "" {
		q = data[cmd.Metro]
	} else {
		q = aggregateQuotas(data)
	}
	if q == nil {
		return fmt.Errorf("no quota data available")
	}
	_, err := fmt.Fprint(out, renderQuotaView(q, result.userName))
	return err
}

// quotaEntry holds fetched quota information for a single metro.
type quotaEntry struct {
	metro  string
	quotas *platform.Quotas
	err    error
}

// quotasFetchResult is the result of fetchAllQuotas.
type quotasFetchResult struct {
	entries  []quotaEntry
	userName string
}

type quotasModel struct {
	ctx       context.Context
	metro     string
	tabs      []string
	activeTab int
	data      map[string]*platform.Quotas
	userName  string
	loading   bool
	err       error
	watch     *time.Duration
}

type quotasLoadedMsg = quotasFetchResult

type quotasTickMsg struct{}

func newQuotasModel(ctx context.Context, watch *time.Duration, metro string) quotasModel {
	tabs := []string{"All"}
	if metro != "" {
		tabs = []string{metro}
	}
	return quotasModel{
		ctx:     ctx,
		metro:   metro,
		tabs:    tabs,
		data:    make(map[string]*platform.Quotas),
		loading: true,
		watch:   watch,
	}
}

func (m quotasModel) Init() tea.Cmd {
	return m.fetchCmd()
}

func (m quotasModel) fetchCmd() tea.Cmd {
	ctx := m.ctx
	metro := m.metro
	return func() tea.Msg {
		return fetchAllQuotas(ctx, metro)
	}
}

func (m quotasModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case quotasLoadedMsg:
		m.loading = false
		m.userName = msg.userName
		m.data = make(map[string]*platform.Quotas)
		var tabs []string
		if m.metro == "" {
			tabs = []string{"All"}
		}
		var firstErr error
		for _, e := range msg.entries {
			if e.err != nil {
				if firstErr == nil {
					firstErr = e.err
				}
				continue
			}
			m.data[e.metro] = e.quotas
			tabs = append(tabs, e.metro)
		}
		m.tabs = tabs
		if m.activeTab >= len(m.tabs) {
			m.activeTab = 0
		}
		if len(m.data) == 0 && firstErr != nil {
			m.err = firstErr
		}
		if m.watch != nil {
			interval := cmp.Or(*m.watch, 2*time.Second)
			return m, tea.Tick(interval, func(time.Time) tea.Msg {
				return quotasTickMsg{}
			})
		}
		return m, nil

	case quotasTickMsg:
		m.loading = true
		return m, m.fetchCmd()

	case tea.KeyPressMsg:
		switch msg.Keystroke() {
		case "ctrl+c", "q", "escape":
			return m, tea.Quit
		case "tab", "right", "l":
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
		case "shift+tab", "left", "h":
			m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
		}
	}
	return m, nil
}

func (m quotasModel) View() tea.View {
	if m.err != nil {
		return tea.NewView(fmt.Sprintf("  Error: %v\n", m.err))
	}

	var b strings.Builder

	b.WriteString(m.renderTabs())
	b.WriteString("\n\n")

	if m.loading && len(m.data) == 0 {
		b.WriteString("  Loading quotas...\n")
		return tea.NewView(b.String())
	}

	var q *platform.Quotas
	if m.activeTab < len(m.tabs) {
		tab := m.tabs[m.activeTab]
		if tab == "All" {
			q = aggregateQuotas(m.data)
		} else {
			q = m.data[tab]
		}
	}

	if q == nil {
		b.WriteString("  No quota data available.\n")
	} else {
		b.WriteString(renderQuotaView(q, m.userName))
	}

	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Faint(true).Render("  Tab/←→: switch metro  q: quit"))
	if m.loading {
		b.WriteString(lipgloss.NewStyle().Faint(true).Italic(true).Render("  refreshing..."))
	}
	b.WriteString("\n")

	return tea.NewView(b.String())
}

var (
	quotaTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#7c3aed")).
			Padding(0, 1)
	quotaTabInactive = lipgloss.NewStyle().
				Faint(true).
				Padding(0, 1)
)

func (m quotasModel) renderTabs() string {
	var b strings.Builder
	b.WriteString("  ")
	for i, tab := range m.tabs {
		if i == m.activeTab {
			b.WriteString(quotaTabActive.Render(tab))
		} else {
			b.WriteString(quotaTabInactive.Render(tab))
		}
		if i < len(m.tabs)-1 {
			b.WriteString(" ")
		}
	}
	return b.String()
}

// aggregateQuotas sums Used and Hard values across all metros.
// Limits (size ranges) are taken from the first metro as they are
// account-level configuration identical across metros.
func aggregateQuotas(data map[string]*platform.Quotas) *platform.Quotas {
	if len(data) == 0 {
		return nil
	}
	agg := &platform.Quotas{}
	first := true
	for _, q := range data {
		agg.Used.Instances += q.Used.Instances
		agg.Used.LiveInstances += q.Used.LiveInstances
		agg.Used.LiveVcpus += q.Used.LiveVcpus
		agg.Used.LiveMemoryMb += q.Used.LiveMemoryMb
		agg.Used.ServiceGroups += q.Used.ServiceGroups
		agg.Used.Services += q.Used.Services
		agg.Used.Volumes += q.Used.Volumes
		agg.Used.TotalVolumeMb += q.Used.TotalVolumeMb
		agg.Hard.Instances += q.Hard.Instances
		agg.Hard.LiveInstances += q.Hard.LiveInstances
		agg.Hard.LiveVcpus += q.Hard.LiveVcpus
		agg.Hard.LiveMemoryMb += q.Hard.LiveMemoryMb
		agg.Hard.ServiceGroups += q.Hard.ServiceGroups
		agg.Hard.Services += q.Hard.Services
		agg.Hard.Volumes += q.Hard.Volumes
		agg.Hard.TotalVolumeMb += q.Hard.TotalVolumeMb
		if first {
			agg.Uuid = q.Uuid
			agg.Limits = q.Limits
			first = false
		}
	}
	return agg
}

const quotaBarWidth = 36

var quotaBarEmptyStyle = lipgloss.NewStyle().Background(lipgloss.Color("243"))

func renderQuotaBar(used, limit int64) string {
	if limit <= 0 {
		return quotaBarEmptyStyle.Render(strings.Repeat(" ", quotaBarWidth))
	}
	filled := int(math.Floor(float64(used) / float64(limit) * float64(quotaBarWidth)))
	filled = max(0, min(filled, quotaBarWidth))

	var c color.Color
	pct := float64(used) / float64(limit)
	switch {
	case pct > 0.83:
		// fuchsia: violet — darker on light terminals for contrast
		c = compat.AdaptiveColor{Light: lipgloss.Color("#a21caf"), Dark: lipgloss.Color("#e879f9")}
	case pct > 0.56:
		// purple: blue-violet
		c = compat.AdaptiveColor{Light: lipgloss.Color("#6d28d9"), Dark: lipgloss.Color("#c084fc")}
	default:
		// indigo: purplish-blue
		c = compat.AdaptiveColor{Light: lipgloss.Color("#4338ca"), Dark: lipgloss.Color("#818cf8")}
	}

	bar := lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("█", filled))
	empty := quotaBarEmptyStyle.Render(strings.Repeat(" ", quotaBarWidth-filled))
	return bar + empty
}

func quotaFormatSizeMB(mb int64) string {
	return units.BytesSize(float64(mb) * units.MiB)
}

type quotaRow struct {
	label string
	bar   bool
	used  int64
	limit int64
	value string
	isMB  bool
}

func renderQuotaView(q *platform.Quotas, userName string) string {
	sections := [][]quotaRow{
		{
			{label: "active instances", bar: true, used: q.Used.LiveInstances, limit: q.Hard.Instances},
			{label: "total instances", bar: true, used: q.Used.Instances, limit: q.Hard.Instances},
			{label: "active vcpus", bar: true, used: q.Used.LiveVcpus, limit: q.Hard.LiveVcpus},
			{label: "vcpu limit", value: fmt.Sprintf("%d-%d", q.Limits.MinVcpus, q.Limits.MaxVcpus)},
		},
		{
			{label: "active used memory", bar: true, used: q.Used.LiveMemoryMb, limit: q.Hard.LiveMemoryMb, isMB: true},
			{label: "memory size limits", value: fmt.Sprintf("%s-%s", quotaFormatSizeMB(q.Limits.MinMemoryMb), quotaFormatSizeMB(q.Limits.MaxMemoryMb))},
		},
		{
			{label: "exposed services", bar: true, used: q.Used.Services, limit: q.Hard.Services},
			{label: "services", bar: true, used: q.Used.ServiceGroups, limit: q.Hard.ServiceGroups},
		},
	}

	enabledStyle := lipgloss.NewStyle().Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#4338ca"), Dark: lipgloss.Color("#818cf8")})
	disabledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	if q.Limits.MaxVolumeMb > 0 {
		sections = append(sections, []quotaRow{
			{label: "volumes", value: enabledStyle.Render("enabled")},
			{label: "active volumes", bar: true, used: q.Used.Volumes, limit: q.Hard.Volumes},
			{label: "used volume space", bar: true, used: q.Used.TotalVolumeMb, limit: q.Hard.TotalVolumeMb, isMB: true},
			{label: "volume size limits", value: fmt.Sprintf("%s-%s", quotaFormatSizeMB(q.Limits.MinVolumeMb), quotaFormatSizeMB(q.Limits.MaxVolumeMb))},
		})
	} else {
		sections = append(sections, []quotaRow{
			{label: "volumes", value: disabledStyle.Render("disabled")},
		})
	}

	var autoscale []quotaRow
	if q.Limits.MaxAutoscaleSize > 1 {
		autoscale = append(autoscale,
			quotaRow{label: "autoscale", value: enabledStyle.Render("enabled")},
			quotaRow{label: "autoscale limit", value: fmt.Sprintf("%d-%d", q.Limits.MinAutoscaleSize, q.Limits.MaxAutoscaleSize)},
		)
	} else {
		autoscale = append(autoscale,
			quotaRow{label: "autoscale", value: disabledStyle.Render("disabled")},
		)
	}
	autoscale = append(autoscale, quotaRow{label: "scale-to-zero", value: enabledStyle.Render("enabled")})
	sections = append(sections, autoscale)

	var b strings.Builder
	kv := kvwriter.KeyValueWriter(&b)

	// Header: user UUID and name.
	if q.Uuid != "" {
		fmt.Fprintf(kv, "user uuid: %s\n", q.Uuid)
	}
	if userName != "" {
		fmt.Fprintf(kv, "user name: %s\n", userName)
	}
	if q.Uuid != "" || userName != "" {
		fmt.Fprintln(kv)
	}

	for i, section := range sections {
		for _, r := range section {
			if r.bar {
				bar := renderQuotaBar(r.used, r.limit)
				if r.isMB {
					fmt.Fprintf(kv, "%s: %s  %s/%s\n", r.label, bar, quotaFormatSizeMB(r.used), quotaFormatSizeMB(r.limit))
				} else {
					fmt.Fprintf(kv, "%s: %s  %d/%d\n", r.label, bar, r.used, r.limit)
				}
			} else {
				fmt.Fprintf(kv, "%s: %s\n", r.label, r.value)
			}
		}
		if i < len(sections)-1 {
			fmt.Fprintln(kv)
		}
	}
	_ = kv.Flush()
	return b.String()
}

func fetchAllQuotas(ctx context.Context, metro string) quotasFetchResult {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return quotasFetchResult{entries: []quotaEntry{{err: err}}}
	}

	// Use organization as user name, fall back to profile name.
	userName := profile.Organization
	if userName == "" {
		userName = profile.Name
	}

	metros := profile.Metros
	if metro != "" {
		for _, m := range profile.Metros {
			if m.Name == metro {
				metros = []config.Metro{m}
				break
			}
		}
		if len(metros) == len(profile.Metros) {
			return quotasFetchResult{
				entries:  []quotaEntry{{err: fmt.Errorf("metro %q not found in profile", metro)}},
				userName: userName,
			}
		}
	}
	if len(metros) == 0 {
		return quotasFetchResult{
			entries:  []quotaEntry{{err: fmt.Errorf("no metros configured in profile %q", profile.Name)}},
			userName: userName,
		}
	}

	entries := make([]quotaEntry, len(metros))
	var wg sync.WaitGroup
	for i, metro := range metros {
		wg.Go(func() {
			httpClient := httpclient.GetClient(ptr.ZeroIfNil(metro.Insecure))
			client := platform.NewClient(
				platform.WithHTTPClient(httpClient),
				platform.WithToken(profile.Token),
				platform.WithDefaultMetro(metro.Endpoint),
			)

			log.G(ctx).Trace().Str("metro", metro.Name).Msg("fetching metro quotas")
			resp, err := client.GetUser(ctx)
			if err != nil {
				entries[i] = quotaEntry{metro: metro.Name, err: fmt.Errorf("%s: %w", metro.Name, err)}
				return
			}
			if resp.Data == nil || len(resp.Data.Quotas) == 0 {
				entries[i] = quotaEntry{metro: metro.Name, err: fmt.Errorf("%s: no quota data", metro.Name)}
				return
			}
			entries[i] = quotaEntry{metro: metro.Name, quotas: &resp.Data.Quotas[0]}
		})
	}
	wg.Wait()
	return quotasFetchResult{entries: entries, userName: userName}
}
