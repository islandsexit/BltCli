package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository"
	"github.com/islandsexit/BltCli/repository/adapters"
)

func acceptExit(t *testing.T, m model) (model, tea.Cmd) {
	t.Helper()
	if m.confirm == nil || m.confirm.accept {
		t.Fatal("confirmation missing or defaults to Close")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(model), cmd
}

func TestEditorExitConfirmationPreservesDraftAndPreview(t *testing.T) {
	m := model{app: domain.NewApp(), width: 80, height: 24}
	m.showRecordDetail("Key: k\nValue:\n", []byte(`{"n":1}`))
	preview := m.detail
	m.form("put", "Edit", field{"Key", "k"}, field{"Value", `{"n":1}`})
	m.modal.selected = 1
	m.modal.fields[1].value = "{\n \"n\": 2\n}"
	m.modal.positions[1] = 4
	for _, cancel := range []tea.KeyType{tea.KeyEnter, tea.KeyEsc} {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		m = next.(model)
		if cmd != nil || m.confirm == nil || m.modal == nil {
			t.Fatal("editor closed before confirmation")
		}
		next, _ = m.Update(tea.KeyMsg{Type: cancel})
		m = next.(model)
		if m.confirm != nil || m.modal == nil || m.modal.fields[1].value != "{\n \"n\": 2\n}" || m.modal.positions[1] != 4 {
			t.Fatal("cancel lost draft or cursor")
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	m, cmd := acceptExit(t, m)
	if cmd != nil || m.modal != nil || m.detail != preview {
		t.Fatal("confirmed editor exit lost underlying preview")
	}
}

func TestDatabaseCloseConfirmationAndQuit(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	dir := t.TempDir()
	db, err := adapters.Open(filepath.Join(dir, "db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("db", db); err != nil {
		t.Fatal(err)
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24, info: domain.Info{Path: "db"}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if cmd != nil || m.confirm == nil || len(app.Names()) != 1 {
		t.Fatal("root Esc closed database directly")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if _, err = db.Info(); err != nil {
		t.Fatal("canceled close released handle")
	}
	next, cmd = m.Update(runeKey("q"))
	m = next.(model)
	if cmd != nil || m.confirm == nil || m.confirm.action != "quit" {
		t.Fatal("quit did not confirm open databases")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	next, _ = m.Update(runeKey("x"))
	m = next.(model)
	m, cmd = acceptExit(t, m)
	commands := cmd().(tea.BatchMsg)
	next, reload := m.Update(commands[0]())
	m = next.(model)
	if reload != nil {
		m = completeLoad(t, m, reload)
	}
	if len(app.Names()) != 0 {
		t.Fatal("confirmed database close did not execute")
	}
	if _, err = db.Info(); !errors.Is(err, domain.ErrClosed) {
		t.Fatal("handle still open", err)
	}
	if _, err = os.Stat(filepath.Join(dir, "db")); err != nil {
		t.Fatal("database file was removed")
	}
}

func TestDiffExitConfirmationDoesNotRemoveResultsOnCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "result")
	if err := os.WriteFile(path, []byte("result"), 0600); err != nil {
		t.Fatal(err)
	}
	m := model{ctx: context.Background(), app: domain.NewApp(), workers: &workers{}, width: 80, height: 24, diffOpen: true, diffStore: &repository.DiffStore{Path: path, Index: path + ".index"}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if cmd != nil || m.confirm == nil || !m.diffOpen {
		t.Fatal("diff closed directly")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if _, err := os.Stat(path); err != nil || !m.diffOpen {
		t.Fatal("cancel discarded diff")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	m, cmd = acceptExit(t, m)
	commands := cmd().(tea.BatchMsg)
	next, _ = m.Update(commands[0]())
	m = next.(model)
	if m.diffOpen || m.diffStore != nil {
		t.Fatal("confirmed comparison remained open")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("comparison files not removed")
	}
}

func TestConfirmationOverlayFitsAndBlocksOtherShortcuts(t *testing.T) {
	for _, size := range [][2]int{{45, 14}, {60, 16}, {100, 30}} {
		m := model{app: domain.NewApp(), width: size[0], height: size[1]}
		m.form("put", "Edit", field{"Key", "k"}, field{"Value", `{"text":"draft"}`})
		m.confirmExit("editor")
		m.confirm.message = strings.Repeat("Close the current editor? ", 20)
		view := m.View()
		if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height || !strings.Contains(view, "Close editor?") {
			t.Fatal("popup does not fit")
		}
		next, cmd := m.Update(runeKey("h"))
		m = next.(model)
		if cmd != nil || m.helpOpen || m.confirm == nil {
			t.Fatal("shortcut leaked through popup")
		}
	}
}

func TestComparisonCancellationNeedsConfirmation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := model{app: domain.NewApp(), width: 80, height: 24, busy: true, diffRunning: true, cancel: cancel}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if ctx.Err() != nil || m.confirm == nil {
		t.Fatal("comparison canceled before confirmation")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if ctx.Err() != nil || m.confirm != nil {
		t.Fatal("Stay canceled comparison")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	m, _ = acceptExit(t, m)
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("confirmed Stop did not cancel")
	}
}
