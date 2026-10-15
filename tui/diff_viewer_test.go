package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository"
	"github.com/islandsexit/BltCli/repository/adapters"
)

func TestFullDiffViewerAndSearchWorkflow(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "base.db"), filepath.Join(dir, "other.db")}
	for side, path := range paths {
		db, err := adapters.Open(path, true, false)
		if err != nil {
			t.Fatal(err)
		}
		p := domain.Path{[]byte("root")}
		if err = db.CreateBucket(p); err != nil {
			t.Fatal(err)
		}
		value := []byte(`{"same":true,"value":"base"}`)
		if side == 1 {
			value = []byte(`{"same":true,"value":"other","added":1}`)
		}
		if err = db.Put(p, []byte("target"), value); err != nil {
			t.Fatal(err)
		}
		if err = db.Put(p, []byte("equal"), []byte("same")); err != nil {
			t.Fatal(err)
		}
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	m := model{ctx: context.Background(), app: domain.NewApp(), workers: &workers{}, width: 100, height: 30, diffDir: dir, diffBase: paths[0], diffOther: paths[1]}
	m = completeLoad(t, m, m.startDiff())
	defer m.diffStore.Remove()
	next, _ := m.Update(runeKey("/"))
	m = next.(model)
	if m.modal == nil || m.modal.kind != "diff-search" {
		t.Fatal("diff search shortcut missing")
	}
	m.modal.fields[0].value = "target"
	next, cmd := m.submit()
	m = completeLoad(t, next.(model), cmd)
	if !m.diffSearchOpen || len(m.diffSearchPage.Entries) != 1 {
		t.Fatal("changed key not found")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = completeLoad(t, next.(model), cmd)
	if m.diffDocument == nil || len(m.diffRows) == 0 {
		t.Fatal("full diff did not open")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "BASE −") || !strings.Contains(view, "OTHER +") || !strings.Contains(view, "same") {
		t.Fatal("side-by-side context missing")
	}
	firstDoc := m.diffDocument.Path
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyF4})
	m = completeLoad(t, next.(model), cmd)
	if m.diffDocument.Formatted {
		t.Fatal("raw mode did not activate")
	}
	if _, err := os.Stat(firstDoc); !os.IsNotExist(err) {
		t.Fatal("reformatted document leaked")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = completeLoad(t, next.(model), cmd)
	if m.diffDocument != nil || !m.diffSearchOpen || m.detail != "" {
		t.Fatal("close did not restore search results")
	}
	next, _ = m.Update(runeKey("/"))
	m = next.(model)
	m.modal.fields[0].value = "equal"
	next, cmd = m.submit()
	m = completeLoad(t, next.(model), cmd)
	if len(m.diffSearchPage.Entries) != 0 || !strings.Contains(ansi.Strip(m.View()), "No recorded differences") {
		t.Fatal("unchanged key falsely reported changed")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.diffSearchOpen || !m.diffOpen {
		t.Fatal("search escape closed comparison")
	}
}

func TestDiffViewerColorsAndResponsiveLayout(t *testing.T) {
	previewColors(t)
	row := repository.DiffLine{Base: `  "value": "old"`, Other: `  "value": "new"`, BaseLine: 100000, OtherLine: 100001, Kind: "replace"}
	for _, base := range []bool{false, true} {
		cell := diffDocumentCell(row, base, true, 0, 40)
		if !(strings.Contains(cell, "[48;") || strings.Contains(cell, ";48;")) || lipgloss.Width(cell) > 40 {
			t.Fatalf("changed line lacks a bounded background: %q width=%d", cell, lipgloss.Width(cell))
		}
	}
	equal := row
	equal.Kind = "equal"
	equal.Other = equal.Base
	if cell := diffDocumentCell(equal, true, false, 0, 40); strings.Contains(cell, "[48;") || strings.Contains(cell, ";48;") {
		t.Fatal("unchanged line has change background")
	}
	for _, size := range [][2]int{{45, 14}, {60, 16}, {80, 24}, {120, 40}} {
		for _, failed := range []bool{false, true} {
			m := model{app: domain.NewApp(), width: size[0], height: size[1], diffOpen: true, diffDocument: &repository.DiffDocument{Count: 100, Changes: 2}, diffRows: []repository.DiffLine{row}, diffBaseFile: strings.Repeat("base", 100), diffOtherFile: strings.Repeat("other", 100)}
			if failed {
				m.setStatus("Some operation failed", true)
			}
			view := m.View()
			if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
				t.Fatalf("viewer overflow %v error=%v: %s", size, failed, ansi.Strip(view))
			}
			m.diffDocument = nil
			m.diffSearchOpen = true
			m.diffSearchMode = "exact"
			m.diffSearchKey = []byte("target")
			m.diffSearchPage.Entries = []domain.DiffEntry{{Key: []byte("target"), Kind: "record"}}
			view = m.View()
			if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
				t.Fatalf("search overflow %v", size)
			}
		}
	}
}
