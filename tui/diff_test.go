package tui

import (
	"context"
	"fmt"
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

func TestDiffDialogPaginationDetailsAndReturn(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	dir := t.TempDir()
	a, err := adapters.Open(filepath.Join(dir, "a.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("base", a); err != nil {
		t.Fatal(err)
	}
	b, err := adapters.Open(filepath.Join(dir, "b.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("other", b); err != nil {
		t.Fatal(err)
	}
	if err = app.Select("base"); err != nil {
		t.Fatal(err)
	}
	path := domain.Path{[]byte("root")}
	for _, db := range []*adapters.BoltDBAdapter{a, b} {
		if err = db.CreateBucket(path); err != nil {
			t.Fatal(err)
		}
		if err = db.Put(path, []byte("same"), []byte("same")); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 20; i++ {
		if err = a.Put(path, []byte(fmt.Sprintf("%03d", i)), []byte(`{"value":"base"}`)); err != nil {
			t.Fatal(err)
		}
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24, diffDir: dir, info: domain.Info{Path: filepath.Join(dir, "a.db")}, path: path}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = next.(model)
	if m.modal == nil || m.modal.kind != "diff" {
		t.Fatal("diff dialog missing")
	}
	m.modal.fields[0].value = "other"
	next, cmd := m.submit()
	m = completeLoad(t, next.(model), cmd)
	if !m.diffOpen || m.diffStore.Count != 20 || len(m.diffBrowse.Buckets) != 1 || len(m.diffEntries) != 0 {
		t.Fatal("diff did not open a bounded page")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = completeLoad(t, next.(model), cmd)
	if len(m.diffPath) != 1 || len(m.diffEntries) != m.diffPageSize() {
		t.Fatal("bucket did not open")
	}
	name, _, _ := app.Active()
	if name != "base" {
		t.Fatal("comparison switched active database")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "BASE · base") || !strings.Contains(view, "OTHER · other") || strings.Contains(view, " / \"same\"") {
		t.Fatal("legend missing or equal records visible")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = completeLoad(t, next.(model), cmd)
	if m.diffStart == 0 {
		t.Fatal("diff page did not advance")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = completeLoad(t, next.(model), cmd)
	if !m.diffDetail || !strings.Contains(m.detail, "BASE (red):") || !strings.Contains(m.detail, "OTHER (green):") {
		t.Fatal("diff detail missing")
	}
	if !strings.Contains(m.detail, "File: "+filepath.Join(dir, "a.db")) || !strings.Contains(m.detail, "File: "+filepath.Join(dir, "b.db")) {
		t.Fatal("actual file paths missing from diff detail")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = completeLoad(t, next.(model), cmd)
	if !m.diffOpen || m.detail != "" || len(m.path) != 1 {
		t.Fatal("Esc from detail changed underlying browser")
	}
	store := m.diffStore
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = completeLoad(t, next.(model), cmd)
	if len(m.diffPath) != 0 || len(m.diffBrowse.Buckets) != 1 {
		t.Fatal("parent navigation failed")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	m, cmd = acceptExit(t, m)
	commands := cmd().(tea.BatchMsg)
	next, reload := m.Update(commands[0]())
	m = completeLoad(t, next.(model), reload)
	if m.diffOpen || len(m.path) != 1 {
		t.Fatal("diff did not return to current bucket")
	}
	if _, err = os.Stat(store.Path); !os.IsNotExist(err) {
		t.Fatal("diff files not removed")
	}
}

func TestDiffSummaryAndLongErrorViews(t *testing.T) {
	for _, summary := range []domain.DiffSummary{{}, {Differences: 2, Records: 2}} {
		m := model{app: domain.NewApp(), width: 80, height: 24, diffOpen: true, diffBase: "a", diffOther: "b", diffStore: &repository.DiffStore{Summary: summary}}
		view := m.View()
		if summary.Equal() && !strings.Contains(view, "Databases are equal.") {
			t.Fatal(view)
		}
		if summary.CompletelyDifferent() && !strings.Contains(view, "Databases are completely different.") {
			t.Fatal(view)
		}
	}
	m := model{app: domain.NewApp(), width: 60, height: 24}
	m.setStatus(strings.Repeat("long error ", 70)+"END-OF-ERROR", true)
	if !strings.Contains(m.View(), "F2 opens the complete error") {
		t.Fatal("missing error shortcut")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	m = next.(model)
	if !m.errorOpen {
		t.Fatal("full error did not open")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(model)
	view := m.View()
	if !strings.Contains(view, "END-OF-ERROR") {
		t.Fatal("error tail inaccessible")
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatal("error view width overflow")
		}
	}
	if len(strings.Split(view, "\n")) > m.height {
		t.Fatal("error view height overflow")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(model).errorOpen {
		t.Fatal("error did not close")
	}
	for _, size := range [][2]int{{45, 14}, {60, 16}, {80, 24}} {
		m := model{app: domain.NewApp(), width: size[0], height: size[1]}
		m.setStatus(strings.Repeat("very long error ", 100), true)
		view := m.View()
		if len(strings.Split(view, "\n")) > m.height {
			t.Fatalf("footer height overflow at %v", size)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > m.width {
				t.Fatalf("footer width overflow at %v", size)
			}
		}
	}
}

func TestDiffCancelAndSameOpenFile(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.db")
	db, err := adapters.Open(path, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add(path, db); err != nil {
		t.Fatal(err)
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, diffDir: dir, width: 80, height: 24, diffBase: path, diffOther: path}
	m = completeLoad(t, m, m.startDiff())
	if !m.diffStore.Summary.Equal() {
		t.Fatal("same active file not equal")
	}
	if err = m.diffStore.Remove(); err != nil {
		t.Fatal(err)
	}
	m.diffOpen = false
	m.diffStore = nil
	cmd := m.startDiff()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	m, _ = acceptExit(t, m)
	m = completeLoad(t, m, cmd)
	if !m.statusError || m.diffOpen || !strings.Contains(m.status, "canceled") {
		t.Fatal("comparison not canceled")
	}
}

func TestDiffLaunchOpensReadOnlyAndReleasesFiles(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "base.db"), filepath.Join(dir, "other.db")}
	for i, path := range paths {
		db, err := adapters.Open(path, true, false)
		if err != nil {
			t.Fatal(err)
		}
		if err = db.CreateBucket(domain.Path{[]byte("root")}); err != nil {
			t.Fatal(err)
		}
		if err = db.Put(domain.Path{[]byte("root")}, []byte("key"), []byte(fmt.Sprintf("value %d", i))); err != nil {
			t.Fatal(err)
		}
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	m := model{ctx: context.Background(), app: domain.NewApp(), workers: &workers{}, diffDir: dir, width: 80, height: 24, initialDiff: paths, diffBase: paths[0], diffOther: paths[1], busy: true}
	next, cmd := m.Update(m.Init()())
	m = completeLoad(t, next.(model), cmd)
	if !m.diffOpen || !m.diffStore.Summary.CompletelyDifferent() || len(m.diffEntries) != 0 {
		t.Fatal("launch did not open completed comparison")
	}
	if _, err := os.Stat(m.diffStore.Path); err != nil {
		t.Fatal("completely different result lost searchable data", err)
	}
	if len(m.app.Names()) != 0 {
		t.Fatal("comparison added browser handles")
	}
	for _, path := range paths {
		db, err := adapters.Open(path, false, false)
		if err != nil {
			t.Fatalf("comparison retained a file lock: %v", err)
		}
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
