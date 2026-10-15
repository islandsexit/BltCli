package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository"
	"github.com/islandsexit/BltCli/utils"
)

type diffDocumentLoaded struct {
	doc   *repository.DiffDocument
	rows  []repository.DiffLine
	entry domain.DiffEntry
	err   error
}

func (m *model) reformatDiffDocument(hex bool) tea.Cmd {
	store, dir, e, old, limit := m.diffStore, m.diffDir, m.diffDocumentEntry, m.diffDocument, m.diffDocumentHeight()
	return m.operation(func(ctx context.Context) tea.Msg {
		var doc *repository.DiffDocument
		var err error
		if hex {
			doc, err = store.BuildDiffDocumentHex(ctx, dir, e)
		} else {
			doc, err = store.BuildDiffDocumentMode(ctx, dir, e, !old.Formatted)
		}
		result := diffDocumentLoaded{doc: doc, entry: e, err: err}
		if err == nil {
			result.rows, result.err = doc.Page(ctx, 0, limit)
		}
		if result.err != nil && doc != nil {
			result.err = errors.Join(result.err, doc.Remove())
			result.doc = nil
		}
		if result.err == nil {
			result.err = old.Remove()
			if result.err != nil {
				result.err = errors.Join(result.err, doc.Remove())
				result.doc = nil
			}
		}
		return result
	})
}

type diffDocumentPage struct {
	rows  []repository.DiffLine
	start int64
	err   error
}
type diffDocumentClosed struct{ err error }

func (m model) diffDocumentHeight() int { return max(1, min(200, m.height-12-m.statusExtraRows())) }

func (m *model) loadDiffDocument(e domain.DiffEntry) tea.Cmd {
	store, dir, limit := m.diffStore, m.diffDir, m.diffDocumentHeight()
	return m.operation(func(ctx context.Context) tea.Msg {
		doc, err := store.BuildDiffDocument(ctx, dir, e)
		result := diffDocumentLoaded{doc: doc, entry: e, err: err}
		if err == nil {
			result.rows, result.err = doc.Page(ctx, 0, limit)
		}
		if result.err != nil && doc != nil {
			result.err = errors.Join(result.err, doc.Remove())
			result.doc = nil
		}
		return result
	})
}

func (m *model) diffDocumentLoad(start int64, jump int) tea.Cmd {
	doc, limit := m.diffDocument, m.diffDocumentHeight()
	start = max(0, min(start, max(0, doc.Count-int64(limit))))
	return m.operation(func(ctx context.Context) tea.Msg {
		if jump != 0 {
			var err error
			start, err = doc.Change(ctx, start, jump > 0)
			if err != nil {
				return diffDocumentPage{err: err}
			}
		}
		rows, err := doc.Page(ctx, start, limit)
		return diffDocumentPage{rows: rows, start: start, err: err}
	})
}

func (m *model) clearDiffDocument() *repository.DiffDocument {
	doc := m.diffDocument
	m.diffDocument = nil
	m.diffDocumentEntry = domain.DiffEntry{}
	m.diffRows = nil
	m.diffRowStart = 0
	m.diffColumn = 0
	m.detail = ""
	m.detailLines = nil
	m.detailStyledLines = nil
	m.detailJSON = nil
	m.diffDetail = false
	return doc
}

func (m model) updateDiffDocument(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q", "enter":
		doc := m.clearDiffDocument()
		return m, m.operation(func(context.Context) tea.Msg { return diffDocumentClosed{err: doc.Remove()} })
	case "/", "g", "ctrl+f":
		m.form("diff-search", "Find differences by key", field{"Key (text:, hex:, base64:)", ""}, field{"Match: exact / contains", "exact"})
	case "f4":
		return m, m.reformatDiffDocument(false)
	case "f6":
		return m, m.reformatDiffDocument(true)
	case "up", "k":
		return m, m.diffDocumentLoad(m.diffRowStart-1, 0)
	case "down", "j":
		return m, m.diffDocumentLoad(m.diffRowStart+1, 0)
	case "pgup", "p":
		return m, m.diffDocumentLoad(m.diffRowStart-int64(m.diffDocumentHeight()), 0)
	case "pgdown", "n":
		return m, m.diffDocumentLoad(m.diffRowStart+int64(m.diffDocumentHeight()), 0)
	case "home", "ctrl+home":
		return m, m.diffDocumentLoad(0, 0)
	case "end", "ctrl+end":
		return m, m.diffDocumentLoad(m.diffDocument.Count, 0)
	case "]":
		return m, m.diffDocumentLoad(m.diffRowStart, 1)
	case "[":
		return m, m.diffDocumentLoad(m.diffRowStart, -1)
	case "left":
		m.diffColumn = max(0, m.diffColumn-8)
	case "right":
		m.diffColumn = min(32<<10, m.diffColumn+8)
	case "0":
		m.diffColumn = 0
	}
	return m, nil
}

func changedText(a, b string) (int, int) {
	prefix := 0
	for prefix < min(len(a), len(b)) && a[prefix] == b[prefix] {
		prefix++
	}
	for prefix > 0 && prefix < len(a) && !utf8.RuneStart(a[prefix]) {
		prefix--
	}
	suffix := 0
	for suffix < min(len(a)-prefix, len(b)-prefix) && a[len(a)-suffix-1] == b[len(b)-suffix-1] {
		suffix++
	}
	for suffix > 0 && len(a)-suffix < len(a) && !utf8.RuneStart(a[len(a)-suffix]) {
		suffix--
	}
	return prefix, suffix
}

func diffDocumentCell(row repository.DiffLine, base, jsonMode bool, column, width int) string {
	text, number := row.Other, row.OtherLine
	if base {
		text, number = row.Base, row.BaseLine
	}
	if number == 0 {
		return strings.Repeat(" ", width)
	}
	text = safeDetailText(text)
	mark := " "
	content := text
	background := lipgloss.Style{}
	if row.Kind != "equal" {
		mark = "+"
		bg := lipgloss.AdaptiveColor{Light: "#DFF3E5", Dark: "#173528"}
		strong := lipgloss.AdaptiveColor{Light: "#A4DBB4", Dark: "#276647"}
		fg := lipgloss.AdaptiveColor{Light: "#155331", Dark: "#C2F2D3"}
		if base {
			mark = "-"
			bg = lipgloss.AdaptiveColor{Light: "#FCE2E4", Dark: "#402027"}
			strong = lipgloss.AdaptiveColor{Light: "#F3AEB6", Dark: "#813744"}
			fg = lipgloss.AdaptiveColor{Light: "#8B2033", Dark: "#FFD2D8"}
		}
		background = lipgloss.NewStyle().Background(bg).Foreground(fg)
		prefix, suffix := 0, 0
		if row.Kind == "replace" {
			prefix, suffix = changedText(safeDetailText(row.Base), safeDetailText(row.Other))
		}
		middle := len(text) - suffix
		content = background.Render(text[:prefix]) + background.Background(strong).Bold(true).Render(text[prefix:middle]) + background.Render(text[middle:])
	} else if jsonMode {
		content = highlightJSON(text)
	}
	gutter := fmt.Sprintf("%5d %s ", number, mark)
	gutter = ansi.Truncate(gutter, max(0, width-1), "")
	available := max(0, width-lipgloss.Width(gutter))
	content = ansi.Cut(content, column, column+available)
	line := gutter + content
	line += strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
	if row.Kind != "equal" {
		line = background.Render(line)
	}
	return line
}

func (m model) diffDocumentView(title lipgloss.Style) string {
	width := max(1, m.width-4)
	left := max(1, (width-3)/2)
	right := max(1, width-left-3)
	green, red := diffColors()
	var out strings.Builder
	e := m.diffDocumentEntry
	out.WriteString(title.Render(clip("VALUE DIFF · "+pathLabel(e.Path)+" / "+utils.Display(e.Key, 128), width)) + "\n")
	out.WriteString(red.Render(pad(clip("BASE − "+m.diffBaseFile, left), left)) + " │ " + green.Render(clip("OTHER + "+m.diffOtherFile, right)) + "\n")
	mode := "formatted"
	if !m.diffDocument.BaseJSON && !m.diffDocument.OtherJSON {
		mode = "text"
	}
	if !m.diffDocument.Formatted {
		mode = "raw"
	}
	if m.diffDocument.BaseHex && m.diffDocument.OtherHex {
		mode = "hex"
	} else if m.diffDocument.BaseHex || m.diffDocument.OtherHex {
		mode = "binary/text"
	}
	out.WriteString(clip(mode+" · − removed · + added · [ / ] change · F4 raw/JSON", width) + "\n")
	height := m.diffDocumentHeight()
	for i := 0; i < height; i++ {
		row := repository.DiffLine{}
		if i < len(m.diffRows) {
			row = m.diffRows[i]
		}
		out.WriteString(diffDocumentCell(row, true, m.diffDocument.BaseJSON, m.diffColumn, left) + " │ " + diffDocumentCell(row, false, m.diffDocument.OtherJSON, m.diffColumn, right) + "\n")
	}
	out.WriteString(clip(fmt.Sprintf("Rows %d–%d / %d · %d change blocks · column %d", min(m.diffRowStart+1, m.diffDocument.Count), m.diffRowStart+int64(len(m.diffRows)), m.diffDocument.Count, m.diffDocument.Changes, m.diffColumn+1), width))
	return out.String()
}
