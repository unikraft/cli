// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package selector

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	jujuerrors "github.com/juju/errors"
	"unikraft.com/x/colors"
)

// Single is a utility method used in a CLI context to prompt the
// user to pick exactly one option from a slice of options based on the
// generic type.
func Single[T fmt.Stringer](question string, options ...T) (T, error) {
	var zero T

	mapped := make(map[string]T)
	items := make([]radioItem, 0, len(options))
	for _, option := range options {
		mapped[option.String()] = option
		items = append(items, radioItem{
			text: option.String(),
		})
	}

	p := tea.NewProgram(&singleSelectModel{
		question: question,
		options:  items,
		cursor:   0,
		selected: -1,
	})

	m, err := p.Run()
	if err != nil {
		return zero, jujuerrors.Annotate(err, "could not start single selection prompt")
	}

	mo := m.(*singleSelectModel)
	if mo.selected < 0 || mo.selected >= len(mo.options) {
		return zero, jujuerrors.New("no option selected")
	}

	return mapped[mo.options[mo.selected].text], nil
}

type singleSelectModel struct {
	question string
	options  []radioItem
	cursor   int
	selected int
	quitting bool
}

type radioItem struct {
	text string
}

func (m singleSelectModel) Init() tea.Cmd {
	return nil
}

func (m *singleSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		return m, m.handleKeyMsg(typed)
	}
	return m, nil
}

func (m *singleSelectModel) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.quitting = true
		return tea.Quit
	case "enter":
		// If nothing has been explicitly selected yet, select the
		// item currently under the cursor.
		if m.selected < 0 {
			m.selected = m.cursor
		}
		m.quitting = true
		return tea.Quit
	case "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down":
		if m.cursor+1 < len(m.options) {
			m.cursor++
		}
	}
	return nil
}

func (m *singleSelectModel) View() tea.View {
	out := colors.InfoFgBg("?") + " " + m.question + ":\n"
	out += colors.InfoFgBg(" ") + "\n"

	for i, item := range m.options {
		var text string
		var radio string

		if i == m.cursor && !m.quitting {
			radio = colors.PrimaryFgBg(" ")
			text = colors.PrimaryFg(item.text)
		} else if m.quitting && m.selected == i {
			radio = colors.SuccessFgBg("●")
			text = colors.SuccessFg(item.text)
		} else {
			radio = colors.InfoFgBg(" ")
			text = item.text
		}

		if i == m.cursor && !m.quitting {
			radio += " ▸" + colors.InfoFg("[")
		} else {
			radio += "   "
		}

		out += fmt.Sprintf("%s%s", radio, text)

		if i == m.cursor && !m.quitting {
			out += colors.InfoFg("]")
		}

		out += "\n"
	}

	out += colors.InfoFgBg(" ") + "\n"

	if !m.quitting {
		out += colors.InfoFgBg(" ") + " use arrow keys to navigate; enter to select; esc to cancel"
	}

	return tea.NewView(out)
}
