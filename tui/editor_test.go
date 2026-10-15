package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
)

func TestEditorJSONFormattingAndCompactSave(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, err := adapters.Open(filepath.Join(t.TempDir(), "edit.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("db", db); err != nil {
		t.Fatal(err)
	}
	path := domain.Path{[]byte("root")}
	if err = db.CreateBucket(path); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{ "ключ": "строка с пробелами", "n": 900719925474099312345, "n": 1.00e+99, "nested": [{"s":"a\\b\"c"}] }`)
	if err = db.Put(path, []byte("k"), original); err != nil {
		t.Fatal(err)
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24, path: path, focus: 1, page: domain.Page{Records: []domain.Record{{Key: []byte("k"), Value: original}}}}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = next.(model)
	if m.modal.editor.mode != "JSON" || !strings.Contains(m.modal.fields[1].value, "\n") {
		t.Fatal("JSON did not open formatted")
	}
	unchanged, err := db.Get(path, []byte("k"))
	if err != nil || !bytes.Equal(unchanged, original) {
		t.Fatal("opening editor changed database")
	}
	var expected bytes.Buffer
	if err = json.Compact(&expected, original); err != nil {
		t.Fatal(err)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	commands := cmd().(tea.BatchMsg)
	next, reload := m.Update(commands[0]())
	m = completeLoad(t, next.(model), reload)
	actual, err := db.Get(path, []byte("k"))
	if err != nil || !bytes.Equal(actual, expected.Bytes()) {
		t.Fatalf("saved %s: %v", actual, err)
	}
	if m.modal != nil {
		t.Fatal("successful save left editor open")
	}
}

func TestEditorInvalidJSONAndFailedSaveKeepDraft(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, err := adapters.Open(filepath.Join(t.TempDir(), "edit.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("db", db); err != nil {
		t.Fatal(err)
	}
	path := domain.Path{[]byte("b")}
	if err = db.CreateBucket(path); err != nil {
		t.Fatal(err)
	}
	if err = db.Put(path, []byte("k"), []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24, path: path}
	m.form("put", "Edit", field{"Key", "k"}, field{"Value", `{"a":1}`})
	m.modal.fields[1].value = "{\n invalid"
	m.modal.positions[1] = len(m.modal.fields[1].value)
	next, cmd := m.submit()
	m = next.(model)
	if cmd != nil || m.modal == nil || !strings.Contains(m.modal.editor.message, "line 2") {
		t.Fatal("invalid JSON was accepted")
	}
	actual, err := db.Get(path, []byte("k"))
	if err != nil || string(actual) != `{"a":1}` {
		t.Fatal("invalid edit changed data")
	}
	m.modal.fields[1].value = `{"a":2}`
	m.modal.positions[1] = len(m.modal.fields[1].value)
	next, cmd = m.submit()
	m = next.(model)
	m.cancel()
	m = completeLoad(t, m, cmd)
	if m.modal == nil || m.modal.fields[1].value != `{"a":2}` || !m.statusError {
		t.Fatal("canceled save lost draft")
	}
	actual, err = db.Get(path, []byte("k"))
	if err != nil || string(actual) != `{"a":1}` {
		t.Fatal("canceled save committed")
	}
}

func TestEditorNavigationFormattingAndBinary(t *testing.T) {
	m := model{app: domain.NewApp(), width: 80, height: 24}
	m.form("put", "Edit", field{"Key", "k"}, field{"Value", "text:привет\n世界"})
	m.modal.selected = 1
	m.modal.positions[1] = len("привет")
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyRunes, tea.KeyUp, tea.KeyDown, tea.KeyHome, tea.KeyEnd} {
		msg := tea.KeyMsg{Type: key}
		if key == tea.KeyRunes {
			msg.Runes = []rune("界")
		}
		next, _ := m.Update(msg)
		m = next.(model)
		if !utf8.ValidString(m.modal.fields[1].value) || m.modal.positions[1] > len(m.modal.fields[1].value) {
			t.Fatal("invalid cursor")
		}
	}
	if !strings.Contains(m.modal.fields[1].value, "привет\n界\n世界") {
		t.Fatal(m.modal.fields[1].value)
	}
	m.form("put", "Edit", field{"Key", "k"}, field{"Value", "hex:001bff00"})
	data, err := m.modal.editorBytes()
	if err != nil || !bytes.Equal(data, []byte{0, 27, 255, 0}) {
		t.Fatal("binary value changed")
	}
	m.form("put", "Edit", field{"Key", "k"}, field{"Value", `text: { "a": 1 }`})
	data, err = m.modal.editorBytes()
	if err != nil || string(data) != `{"a":1}` {
		t.Fatal("JSON did not compact")
	}
	m.modal.fields[1].value = `hex:ff`
	m.modal.editor.mode = "text"
	data, err = m.modal.editorBytes()
	if err != nil || string(data) != "hex:ff" {
		t.Fatal("literal text interpreted as binary")
	}
}

func TestEditorFormattingBoundsAndSafeViewport(t *testing.T) {
	deep := strings.Repeat("[", 1100) + "0" + strings.Repeat("]", 1100)
	if _, err := formatEditorJSON(deep); err == nil {
		t.Fatal("unbounded indentation")
	}
	m := model{app: domain.NewApp(), width: 60, height: 16}
	m.form("put", "Edit", field{"Key", "k"}, field{"Value", `{"ключ":"界","v":1}`})
	m.modal.fields[1].value = strings.Repeat("long line 界", 10000) + "\x1b[2J\nEND"
	m.modal.positions[1] = len(m.modal.fields[1].value)
	m.modal.selected = 1
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "END") || strings.Contains(view, "\x1b") {
		t.Fatal("unsafe or inaccessible viewport")
	}
	if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
		t.Fatal("editor overflow")
	}
	next, _ := m.Update(editorSaved{err: errors.New("write failed")})
	if next.(model).modal == nil {
		t.Fatal("write failure lost draft")
	}
}

func FuzzEditorJSONRoundTrip(f *testing.F) {
	for _, s := range []string{`{"n":1.00e9,"n":9007199254740993}`, `{"x":"строка\\\""}`, `[{},[],true,null]`, "invalid"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 16<<10 || !utf8.ValidString(s) {
			return
		}
		pretty, err := formatEditorJSON(s)
		if err != nil {
			return
		}
		if len(pretty) > editLimit || !json.Valid([]byte(pretty)) {
			t.Fatal("invalid formatted JSON")
		}
		var a, b bytes.Buffer
		if json.Compact(&a, []byte(s)) != nil || json.Compact(&b, []byte(pretty)) != nil || !bytes.Equal(a.Bytes(), b.Bytes()) {
			t.Fatal("format changed JSON tokens")
		}
	})
}

func BenchmarkEditorViewport(b *testing.B) {
	value := strings.Repeat(`{"key":"value","n":123}`+"\n", 40000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		editorViewport(value, len(value)-10, 100, 20, true, true)
	}
}
