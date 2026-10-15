package tui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/utils"
)

type searchLoaded struct {
	page       domain.SearchPage
	err        error
	generation int
}
type searchDetail struct {
	key        []byte
	text       string
	jsonRanges []detailJSONRange
	path       domain.Path
	err        error
}

func pathLabel(path domain.Path) string {
	var out strings.Builder
	for _, part := range path {
		if out.Len() > 1024 {
			out.WriteString(" / …")
			break
		}
		if out.Len() > 0 {
			out.WriteString(" / ")
		}
		out.WriteString(utils.Display(part, 128))
	}
	return out.String()
}
func (m *model) loadSearch() tea.Cmd {
	m.searchPage = domain.SearchPage{}
	_, db, e := m.app.Active()
	if e != nil {
		m.setStatus(e.Error(), true)
		return nil
	}
	q := domain.SearchQuery{Kind: "records", Contains: m.searchTerm, Limit: m.pageSize(), After: m.searchAfter}
	if m.searchKind == "buckets" {
		q.Kind = "buckets"
	} else if m.searchKind == "record values" {
		q.Contains = nil
		q.ValueContains = m.searchTerm
	}
	generation := m.generation + 1
	return m.operation(func(ctx context.Context) tea.Msg {
		page, e := db.Search(ctx, q)
		return searchLoaded{page: page, err: e, generation: generation}
	})
}
func (m *model) searchChangePage(direction int, last bool) tea.Cmd {
	if direction > 0 {
		if m.searchPage.Next == nil {
			return nil
		}
		if len(m.searchHistory) == 128 {
			copy(m.searchHistory, m.searchHistory[1:])
			m.searchHistory[127] = nil
			m.searchHistory = m.searchHistory[:127]
		}
		m.searchHistory = append(m.searchHistory, m.searchAfter)
		m.searchAfter = m.searchPage.Next
	} else {
		if len(m.searchHistory) == 0 {
			return nil
		}
		i := len(m.searchHistory) - 1
		m.searchAfter = m.searchHistory[i]
		m.searchHistory[i] = nil
		m.searchHistory = m.searchHistory[:i]
	}
	m.searchSelected = 0
	m.searchTop = 0
	m.searchSelectLast = direction < 0 && last
	return m.loadSearch()
}
func (m model) updateSearch(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		m.searchOpen = false
		m.searchPage = domain.SearchPage{}
		return m, nil
	case "up", "k":
		if m.searchSelected > 0 {
			m.searchSelected--
			m.keepSearchVisible()
		} else {
			return m, m.searchChangePage(-1, true)
		}
	case "down", "j":
		if m.searchSelected+1 < len(m.searchPage.Hits) {
			m.searchSelected++
			m.keepSearchVisible()
		} else {
			return m, m.searchChangePage(1, false)
		}
	case "n", "pgdown":
		return m, m.searchChangePage(1, false)
	case "p", "pgup":
		return m, m.searchChangePage(-1, false)
	case "ctrl+r":
		return m, m.loadSearch()
	case "g", "ctrl+f":
		m.form("global-search", "Find in entire database", field{"Type (←→ choose records, values, buckets)", m.searchKind}, field{"Contains (empty lists all)", ""})
	case "enter", "o":
		if len(m.searchPage.Hits) == 0 {
			return m, nil
		}
		hit := m.searchPage.Hits[m.searchSelected]
		if hit.Key == nil || key == "o" {
			m.path = hit.Path
			m.resetQueries()
			m.searchOpen = false
			m.searchPage = domain.SearchPage{}
			m.focus = 0
			if hit.Key != nil {
				m.focus = 1
			}
			return m, m.reload()
		}
		_, db, e := m.app.Active()
		if e != nil {
			m.setStatus(e.Error(), true)
			return m, nil
		}
		return m, m.operation(func(ctx context.Context) tea.Msg {
			if e := ctx.Err(); e != nil {
				return searchDetail{err: e}
			}
			result := searchDetail{path: hit.Path}
			found := false
			e := db.Walk(ctx, hit.Path, domain.Query{Start: hit.Key, Prefix: hit.Key, Limit: 1}, func(key, value []byte) error {
				if !bytes.Equal(key, hit.Key) {
					return domain.ErrKey
				}
				found = true
				result.key = bytes.Clone(key)
				result.text, result.jsonRanges = recordDetailText(fmt.Sprintf("Bucket: %s\nKey: %s\nValue (%d bytes):\n", pathLabel(hit.Path), utils.Display(key, len(key)), len(value)), value)
				return nil
			})
			if e != nil {
				result.err = e
			} else if !found {
				result.err = domain.ErrKey
			}
			return result
		})
	}
	return m, nil
}

func searchHitLines(hit domain.SearchHit, width int) []string {
	var lines []string
	if hit.Key != nil {
		keys := wrapLines(utils.Display(hit.Key, width*3*utf8.UTFMax), width)
		if len(keys) > 3 {
			keys = keys[:3]
			keys[2] = ansi.Truncate(keys[2], width-1, "") + "…"
		}
		lines = append(lines, keys...)
	}
	location := wrapLines("Bucket: "+pathLabel(hit.Path), width)
	if len(location) > 2 {
		location = location[:2]
		location[1] = ansi.Truncate(location[1], width-1, "") + "…"
	}
	lines = append(lines, location...)
	return lines
}
func (m model) searchHeight() int { return max(1, m.height-9-m.statusExtraRows()) }
func (m *model) keepSearchVisible() {
	m.searchTop = max(0, min(m.searchTop, m.searchSelected))
	width := max(1, m.width-6)
	rows := 0
	for i := m.searchTop; i <= m.searchSelected && i < len(m.searchPage.Hits); i++ {
		rows += len(searchHitLines(m.searchPage.Hits[i], width))
	}
	for rows > m.searchHeight() && m.searchTop < m.searchSelected {
		rows -= len(searchHitLines(m.searchPage.Hits[m.searchTop], width))
		m.searchTop++
	}
}
func (m model) searchView(title lipgloss.Style) string {
	width := max(1, m.width-4)
	var out strings.Builder
	out.WriteString(title.Render(clip("DATABASE SEARCH · "+m.searchKind, width)) + "\n")
	rows := 0
	for i := m.searchTop; i < len(m.searchPage.Hits) && rows < m.searchHeight(); i++ {
		for j, line := range searchHitLines(m.searchPage.Hits[i], width-2) {
			if rows >= m.searchHeight() {
				break
			}
			marker := "  "
			if i == m.searchSelected && j == 0 {
				marker = "› "
			}
			line = marker + line
			if i == m.searchSelected {
				line = title.Render(line)
			}
			out.WriteString(line)
			out.WriteByte('\n')
			rows++
		}
	}
	if rows == 0 {
		out.WriteString("No matching results\n")
		rows++
	}
	for rows < m.searchHeight() {
		out.WriteByte('\n')
		rows++
	}
	return strings.TrimSuffix(out.String(), "\n")
}
