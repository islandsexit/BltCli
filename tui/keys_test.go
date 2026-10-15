package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository"
	"github.com/islandsexit/BltCli/repository/adapters"
)

func runeKey(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestCommandKeyLayoutsAndModifiers(t *testing.T) {
	for _, pair := range [][2]string{{"й", "q"}, {"ц", "w"}, {"у", "e"}, {"к", "r"}, {"е", "t"}, {"н", "y"}, {"г", "u"}, {"ш", "i"}, {"щ", "o"}, {"з", "p"}, {"х", "["}, {"ъ", "]"}, {"ф", "a"}, {"ы", "s"}, {"в", "d"}, {"а", "f"}, {"п", "g"}, {"р", "h"}, {"о", "j"}, {"л", "k"}, {"д", "l"}, {"я", "z"}, {"ч", "x"}, {"с", "c"}, {"м", "v"}, {"и", "b"}, {"т", "n"}, {"ь", "m"}, {".", "/"}, {"А", "F"}, {"В", "D"}, {"У", "E"}, {"З", "P"}, {"і", "s"}, {"ї", "]"}, {"є", "'"}, {"ґ", "\\"}, {"ў", "o"}, {"І", "S"}, {"Ї", "}"}, {"Є", "\""}, {"Ґ", "|"}, {"Ў", "O"}} {
		key := runeKey(pair[0])
		if got := commandKey(key); got != pair[1] {
			t.Errorf("%s: %s, want %s", pair[0], got, pair[1])
		}
		if string(key.Runes) != pair[0] {
			t.Fatal("original input was mutated")
		}
	}
	for _, key := range []tea.KeyMsg{runeKey("q"), runeKey("Q"), runeKey("привет"), runeKey("界"), {Type: tea.KeyCtrlS}, {Type: tea.KeyRunes, Runes: []rune("й"), Paste: true}, {Type: tea.KeyRunes, Runes: []rune("й"), Alt: true}} {
		if got := commandKey(key); got != key.String() {
			t.Errorf("altered special input %q: %q", key.String(), got)
		}
	}
}

func TestCyrillicBrowserCommands(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, err := adapters.Open(filepath.Join(t.TempDir(), "keys.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("db", db); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, kind string }{{"щ", "open"}, {"с", "create-db"}, {"и", "bucket"}, {"к", "put"}, {"у", "put"}, {"в", "delete"}, {"В", "delete-bucket"}, {"А", "diff"}, {"п", "global-search"}, {".", "record-filter"}, {"м", "value-filter"}, {"З", "filter"}, {"а", "range"}, {"ш", "import"}, {"У", "export"}, {"ч", "close"}} {
		t.Run(tc.key, func(t *testing.T) {
			m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24, focus: 1, info: domain.Info{Path: "db"}, path: domain.Path{[]byte("root")}, page: domain.Page{Records: []domain.Record{{Key: []byte("k"), Value: []byte(`{"a":1}`)}}}}
			next, _ := m.Update(runeKey(tc.key))
			m = next.(model)
			if tc.kind == "close" {
				if m.confirm == nil || m.confirm.action != "database" {
					t.Fatal("Cyrillic close did not confirm")
				}
				return
			}
			if m.modal == nil || m.modal.kind != tc.kind {
				t.Fatalf("%s did not open %s", tc.key, tc.kind)
			}
		})
	}
	m := model{app: app, width: 80, height: 24, info: domain.Info{Path: "db", ReadOnly: true}, focus: 1}
	next, _ := m.Update(runeKey("у"))
	m = next.(model)
	if m.modal != nil || !m.statusError || !strings.Contains(m.status, "read-only") {
		t.Fatal("Cyrillic edit bypassed read-only guard")
	}
}

func TestCyrillicNavigationAndOverlays(t *testing.T) {
	m := model{app: domain.NewApp(), width: 80, height: 24, buckets: [][]byte{[]byte("a"), []byte("b")}}
	next, _ := m.Update(runeKey("о"))
	m = next.(model)
	if m.bucketSelected != 1 {
		t.Fatal("j navigation")
	}
	next, _ = m.Update(runeKey("л"))
	m = next.(model)
	if m.bucketSelected != 0 {
		t.Fatal("k navigation")
	}
	next, _ = m.Update(runeKey("р"))
	m = next.(model)
	if !m.helpOpen {
		t.Fatal("help did not open")
	}
	next, _ = m.Update(runeKey("р"))
	m = next.(model)
	if m.helpOpen {
		t.Fatal("help did not close")
	}
	m.setStatus("error", true)
	m.errorOpen = true
	next, _ = m.Update(runeKey("й"))
	m = next.(model)
	if m.errorOpen {
		t.Fatal("error did not close")
	}
	m.showDetail("value")
	next, _ = m.Update(runeKey("й"))
	m = next.(model)
	if m.detail != "" {
		t.Fatal("detail did not close")
	}
	m.ctx = context.Background()
	m.workers = &workers{}
	m.diffOpen = true
	m.diffStore = &repository.DiffStore{Count: 10, Summary: domain.DiffSummary{Differences: 10, EqualRecords: 1}}
	m.diffBrowse = repository.DiffBrowsePage{Count: 10}
	m.diffEntries = []domain.DiffEntry{{Key: []byte("k")}}
	next, cmd := m.Update(runeKey("т"))
	m = next.(model)
	if cmd == nil || !m.busy || m.diffStart != 0 {
		t.Fatal("Cyrillic n did not request next diff page")
	}
	m.cancel()
}

func TestCyrillicTextAndPasteStayLiteral(t *testing.T) {
	for _, kind := range []string{"open", "record-filter", "put"} {
		m := model{app: domain.NewApp(), width: 80, height: 24}
		if kind == "put" {
			m.form(kind, "Edit", field{"Key", ""}, field{"Value", ""})
			m.modal.selected = 1
		} else {
			m.form(kind, "Input", field{"Text", ""})
		}
		for _, r := range "привет.ХЪАВй" {
			next, _ := m.Update(runeKey(string(r)))
			m = next.(model)
		}
		if m.helpOpen || m.modal.fields[m.modal.selected].value != "привет.ХЪАВй" {
			t.Fatalf("%s: text changed", kind)
		}
		paste := runeKey("р")
		paste.Paste = true
		next, _ := m.Update(paste)
		m = next.(model)
		if m.helpOpen || !strings.HasSuffix(m.modal.fields[m.modal.selected].value, "р") {
			t.Fatal("paste became a command")
		}
	}
	m := model{app: domain.NewApp(), width: 80, height: 24}
	m.form("global-search", "Search", field{"Type", "record keys"}, field{"Query", ""})
	next, _ := m.Update(runeKey("м"))
	m = next.(model)
	if m.modal.fields[0].value != "record values" {
		t.Fatal("selector alias missing")
	}
	next, _ = m.Update(runeKey("и"))
	m = next.(model)
	if m.modal.fields[0].value != "buckets" {
		t.Fatal("bucket selector alias missing")
	}
	m.modal.selected = 1
	next, _ = m.Update(runeKey("и"))
	m = next.(model)
	if m.modal.fields[1].value != "и" {
		t.Fatal("query input was translated")
	}
}
