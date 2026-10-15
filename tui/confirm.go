package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// exitConfirmation is independent of modal so canceling preserves editor drafts.
type exitConfirmation struct {
	action, title, message, dbName string
	accept                         bool
}

func (m *model) confirmExit(action string) {
	c := &exitConfirmation{action: action}
	switch action {
	case "editor":
		c.title = "Close editor?"
		c.message = "Return without saving this draft?"
	case "diff":
		c.title = "Close comparison?"
		c.message = "Discard the completed comparison and its temporary files?"
	case "cancel-diff":
		c.title = "Stop comparison?"
		c.message = "Stop comparing and discard its results?"
	case "database":
		name, _, err := m.app.Active()
		if err != nil {
			return
		}
		c.dbName = name
		c.title = "Close database?"
		c.message = "Close " + name + "? The database file will remain on disk."
	case "quit":
		c.title = "Quit BoltCLI?"
		c.message = "Close all open databases and leave the application?"
	default:
		return
	}
	m.confirm = c
}

func (m model) updateConfirmation(key string) (tea.Model, tea.Cmd) {
	c := m.confirm
	switch key {
	case "esc", "q", "n":
		m.confirm = nil
		return m, nil
	case "left", "right", "tab", "shift+tab":
		c.accept = !c.accept
		return m, nil
	case "y":
		c.accept = true
	case "enter":
		if !c.accept {
			m.confirm = nil
			return m, nil
		}
	default:
		return m, nil
	}
	m.confirm = nil
	switch c.action {
	case "editor":
		m.modal = nil
	case "diff":
		return m, m.closeDiff()
	case "cancel-diff":
		if m.diffRunning && m.cancel != nil {
			m.cancel()
			m.setStatus("Stopping comparison…", false)
		} else if m.diffOpen && m.diffStore != nil {
			return m, m.closeDiff()
		}
	case "database":
		name := c.dbName
		return m, m.operation(func(context.Context) tea.Msg { return opened{err: m.app.Close(name)} })
	case "quit":
		return m, tea.Quit
	}
	return m, nil
}

func (m model) confirmationView(background string) string {
	width, height := max(1, m.width), max(1, m.height)
	boxWidth := max(1, min(58, width-4))
	inner := max(1, boxWidth-6)
	accent := lipgloss.AdaptiveColor{Light: "#5B21B6", Dark: "#C4B5FD"}
	active := lipgloss.NewStyle().Bold(true).Background(accent).Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#171325"})
	stay, leave := " Stay ", " Close "
	switch m.confirm.action {
	case "editor":
		leave = " Discard "
	case "quit":
		leave = " Quit "
	case "cancel-diff":
		leave = " Stop "
	}
	if m.confirm.accept {
		leave = active.Render(leave)
	} else {
		stay = active.Render(stay)
	}
	lines := wrapLines(m.confirm.message, inner)
	if len(lines) > 3 {
		lines = lines[:3]
	}
	content := lipgloss.NewStyle().Bold(true).Render(clip(m.confirm.title, inner)) + "\n\n" + strings.Join(lines, "\n") + "\n\n" + stay + "   " + leave + "\n" + clip("←→ / Tab select · Enter · Esc stay", inner)
	popup := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2).Width(max(1, boxWidth-2)).Render(content)
	popupLines := strings.Split(popup, "\n")
	if len(popupLines) > height {
		popupLines = popupLines[:height]
	}
	popupWidth := min(width, lipgloss.Width(popup))
	x, y := max(0, (width-popupWidth)/2), max(0, (height-len(popupLines))/2)
	base := strings.Split(background, "\n")
	out := make([]string, height)
	for row := range out {
		line := ""
		if row < len(base) {
			line = base[row]
		}
		if row >= y && row < y+len(popupLines) {
			front := pad(ansi.Cut(line, 0, x), x)
			center := pad(ansi.Truncate(popupLines[row-y], popupWidth, ""), popupWidth)
			line = front + center + ansi.Cut(line, x+popupWidth, width)
		}
		out[row] = ansi.Truncate(line, width, "")
	}
	return strings.Join(out, "\n")
}
