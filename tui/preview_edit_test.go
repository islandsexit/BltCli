package tui

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
	"github.com/islandsexit/BltCli/utils"
)

func TestEditViewedSearchRecordInsteadOfTableSelection(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, err := adapters.Open(filepath.Join(t.TempDir(), "view.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("db", db); err != nil {
		t.Fatal(err)
	}
	path := domain.Path{[]byte("root")}
	key := []byte{'z', 0, 255}
	if err = db.CreateBucket(path); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"s":"` + strings.Repeat("x", 100<<10) + `END"}`)
	if err = db.Put(path, key, original); err != nil {
		t.Fatal(err)
	}
	if err = db.Put(path, []byte("aa"), []byte("unrelated")); err != nil {
		t.Fatal(err)
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24, searchOpen: true, searchPage: domain.SearchPage{Hits: []domain.SearchHit{{Path: path, Key: key}}}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	commands := cmd().(tea.BatchMsg)
	next, reload := m.Update(commands[0]())
	m = completeLoad(t, next.(model), reload)
	if !bytes.Equal(m.detailKey, key) || string(m.page.Records[m.selected].Key) != "aa" {
		t.Fatal("fixture did not open a non-selected search record")
	}
	preview := m.detail
	next, cmd = m.Update(runeKey("у"))
	m = completeLoad(t, next.(model), cmd)
	editedKey, err := utils.Decode(m.modal.fields[0].value)
	if err != nil || !bytes.Equal(editedKey, key) || !strings.Contains(m.modal.fields[1].value, "END") || len(m.modal.fields[1].value) < 100<<10 {
		t.Fatal("editor loaded the wrong record or a truncated preview")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	m, _ = acceptExit(t, m)
	if m.modal != nil || m.detail != preview {
		t.Fatal("cancel did not restore preview")
	}
	next, cmd = m.Update(runeKey("e"))
	m = completeLoad(t, next.(model), cmd)
	m.modal.fields[1].value = "{\n  \"updated\": true\n}"
	m.modal.positions[1] = len(m.modal.fields[1].value)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	commands = cmd().(tea.BatchMsg)
	next, reload = m.Update(commands[0]())
	m = completeLoad(t, next.(model), reload)
	saved, err := db.Get(path, key)
	if err != nil || string(saved) != `{"updated":true}` {
		t.Fatalf("save viewed record: %s %v", saved, err)
	}
	other, err := db.Get(path, []byte("aa"))
	if err != nil || string(other) != "unrelated" {
		t.Fatal("save modified the table selection")
	}
	if m.modal != nil || !m.detailIsRecord || !bytes.Equal(m.detailKey, key) || !strings.Contains(m.detail, `"updated": true`) {
		t.Fatal("saved preview was not refreshed")
	}
}

func TestEditPreviewErrorsPreserveView(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, err := adapters.Open(filepath.Join(t.TempDir(), "view.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("db", db); err != nil {
		t.Fatal(err)
	}
	path := domain.Path{[]byte("root")}
	key := []byte("target")
	if err = db.CreateBucket(path); err != nil {
		t.Fatal(err)
	}
	if err = db.Put(path, key, bytes.Repeat([]byte("x"), editLimit+1)); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"read-only", "too-large", "canceled", "missing"} {
		m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24, path: path}
		m.showRecordDetail("Key: target\nValue:\n", []byte(`{"preview":true}`))
		m.detailKey = bytes.Clone(key)
		if kind == "read-only" {
			m.info.ReadOnly = true
		}
		if kind == "missing" {
			if err = db.Delete(path, key); err != nil {
				t.Fatal(err)
			}
		}
		before := m.detail
		next, cmd := m.Update(runeKey("e"))
		m = next.(model)
		if cmd != nil {
			if kind == "canceled" {
				m.cancel()
			}
			m = completeLoad(t, m, cmd)
		}
		if m.modal != nil || !m.statusError || m.detail != before {
			t.Fatalf("%s: error lost preview or opened editor", kind)
		}
	}
}

func TestDiffPreviewDoesNotOpenEditor(t *testing.T) {
	m := model{app: domain.NewApp(), width: 80, height: 24}
	m.showDiffDetail(domain.DiffEntry{Kind: "record", Key: []byte("k")})
	next, cmd := m.Update(runeKey("e"))
	m = next.(model)
	if cmd != nil || m.modal != nil || m.detail == "" {
		t.Fatal("read-only diff opened an editor")
	}
}
