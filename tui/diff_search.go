package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/islandsexit/BltCli/repository"
	"github.com/islandsexit/BltCli/utils"
)

type diffSearchLoaded struct {
	page  repository.DiffSearchPage
	after []byte
	last  bool
	err   error
}

func (m *model) loadDiffSearch(after []byte, last bool, remove *repository.DiffDocument) tea.Cmd {
	store, key, mode, limit := m.diffStore, m.diffSearchKey, m.diffSearchMode, m.diffPageSize()
	return m.operation(func(ctx context.Context) tea.Msg {
		if remove != nil {
			if err := remove.Remove(); err != nil {
				return diffSearchLoaded{err: err}
			}
		}
		page, err := store.Search(ctx, key, mode, after, limit)
		return diffSearchLoaded{page: page, after: after, last: last, err: err}
	})
}

func (m model) updateDiffSearch(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		m.diffSearchOpen = false
		m.diffSelected = 0
		return m, nil
	case "/", "g", "ctrl+f":
		m.form("diff-search", "Find differences by key", field{"Key (text:, hex:, base64:)", "base64:" + base64.StdEncoding.EncodeToString(m.diffSearchKey)}, field{"Match: exact / contains", m.diffSearchMode})
	case "down", "j":
		if m.diffSelected+1 < len(m.diffSearchPage.Entries) {
			m.diffSelected++
		} else {
			return m, m.diffSearchNextPage(false)
		}
	case "up", "k":
		if m.diffSelected > 0 {
			m.diffSelected--
		} else {
			return m, m.diffSearchPreviousPage(true)
		}
	case "pgdown", "n":
		return m, m.diffSearchNextPage(false)
	case "pgup", "p":
		return m, m.diffSearchPreviousPage(false)
	case "home":
		m.diffSearchHistory = nil
		return m, m.loadDiffSearch(nil, false, nil)
	case "enter", "right":
		if m.diffSelected < len(m.diffSearchPage.Entries) {
			e := m.diffSearchPage.Entries[m.diffSelected]
			m.showDiffDetail(e)
			return m, m.loadDiffDocument(e)
		}
	}
	return m, nil
}

func (m *model) diffSearchNextPage(last bool) tea.Cmd {
	if m.diffSearchPage.Next == nil {
		return nil
	}
	if len(m.diffSearchHistory) == 128 {
		copy(m.diffSearchHistory, m.diffSearchHistory[1:])
		m.diffSearchHistory[127] = nil
		m.diffSearchHistory = m.diffSearchHistory[:127]
	}
	m.diffSearchHistory = append(m.diffSearchHistory, m.diffSearchAfter)
	return m.loadDiffSearch(m.diffSearchPage.Next, last, nil)
}
func (m *model) diffSearchPreviousPage(last bool) tea.Cmd {
	if len(m.diffSearchHistory) == 0 {
		return nil
	}
	lastIndex := len(m.diffSearchHistory) - 1
	after := m.diffSearchHistory[lastIndex]
	m.diffSearchHistory[lastIndex] = nil
	m.diffSearchHistory = m.diffSearchHistory[:lastIndex]
	return m.loadDiffSearch(after, last, nil)
}

func (m model) submitDiffSearch() (tea.Model, tea.Cmd) {
	d := m.modal
	key, err := utils.Decode(d.fields[0].value)
	if err != nil {
		m.setStatus(err.Error(), true)
		return m, nil
	}
	mode := strings.ToLower(strings.TrimSpace(d.fields[1].value))
	if mode != "exact" && mode != "contains" {
		m.setStatus("Match must be exact or contains", true)
		return m, nil
	}
	if mode == "exact" && len(key) == 0 {
		m.setStatus("Enter a key for exact search", true)
		return m, nil
	}
	m.modal = nil
	m.diffSearchOpen = true
	m.diffSearchKey = key
	m.diffSearchMode = mode
	m.diffSearchHistory = nil
	m.diffSearchPage = repository.DiffSearchPage{}
	m.diffSearchAfter = nil
	m.diffSelected = 0
	var doc *repository.DiffDocument
	if m.diffDocument != nil {
		doc = m.clearDiffDocument()
	}
	return m, m.loadDiffSearch(nil, false, doc)
}

func (m model) diffSearchView(title lipgloss.Style) string {
	width := max(1, m.width-4)
	green, red := diffColors()
	var out strings.Builder
	out.WriteString(title.Render(clip("DIFF SEARCH · "+m.diffSearchMode+" · "+utils.Display(m.diffSearchKey, 128), width)) + "\n")
	out.WriteString(clip("All buckets · completed comparison", width) + "\n")
	out.WriteString(clip("Enter opens full value diff · / changes search", width) + "\n")
	if len(m.diffSearchPage.Entries) == 0 {
		out.WriteString("No recorded differences for this key.\n")
		return out.String()
	}
	top := max(0, m.diffSelected-m.diffPageSize()+1)
	for i := top; i < min(len(m.diffSearchPage.Entries), top+m.diffPageSize()); i++ {
		e := m.diffSearchPage.Entries[i]
		mark := "  "
		if i == m.diffSelected {
			mark = "> "
		}
		out.WriteString(clip(mark+pathLabel(e.Path)+" / "+utils.Display(e.Key, 128), width) + "\n")
		out.WriteString(red.Render(clip("  BASE − "+strings.ReplaceAll(diffValue(e, true), "\n", " "), width)) + "\n")
		out.WriteString(green.Render(clip("  OTHER + "+strings.ReplaceAll(diffValue(e, false), "\n", " "), width)) + "\n")
		if m.height >= 15+m.statusExtraRows() {
			out.WriteByte('\n')
		}
	}
	more := "end"
	if m.diffSearchPage.Next != nil {
		more = "more: n / PgDown"
	}
	out.WriteString(clip(fmt.Sprintf("%d matching differences on page · %s", len(m.diffSearchPage.Entries), more), width))
	return out.String()
}
