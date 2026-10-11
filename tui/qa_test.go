package tui

import (
	"context"
	"encoding/json"
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

func TestScreenMatrixWithErrors(t *testing.T) {
	for _, size := range [][2]int{{45, 14}, {45, 15}, {60, 16}, {80, 24}, {120, 40}} {
		for _, screen := range []string{"browser", "search", "diff", "equal", "different", "dialog", "help", "detail", "error"} {
			for _, failed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/%s/error=%t", size[0], size[1], screen, failed), func(t *testing.T) {
					m := model{ctx: context.Background(), app: domain.NewApp(), workers: &workers{}, width: size[0], height: size[1], status: "Ready"}
					m.path = domain.Path{[]byte("корень"), []byte("nested")}
					m.buckets = [][]byte{[]byte("child")}
					m.page.Records = []domain.Record{{Key: []byte("ключ"), Value: []byte(strings.Repeat("界🌍", 100))}}
					switch screen {
					case "search":
						m.searchOpen = true
						m.searchKind = "record keys"
						m.searchPage.Hits = []domain.SearchHit{{Path: m.path, Key: []byte("ключ")}}
					case "diff", "equal", "different":
						m.diffOpen = true
						m.diffBase, m.diffOther = "base.db", "other.db"
						m.diffStore = &repository.DiffStore{Count: 1, Summary: domain.DiffSummary{Differences: 1, EqualRecords: 1, Records: 2}}
						m.diffEntries = []domain.DiffEntry{{Kind: "record", Path: m.path, Key: []byte("ключ"), BasePresent: true, OtherPresent: true, Base: []byte("old"), Other: []byte("new"), BaseBytes: 3, OtherBytes: 3}}
						if screen == "equal" {
							m.diffStore.Summary = domain.DiffSummary{}
						}
						if screen == "different" {
							m.diffStore.Summary.EqualRecords = 0
						}
					case "dialog":
						m.form("put", "Edit record", field{"Key", "ключ"}, field{"Value", strings.Repeat("界", 100)})
					case "help":
						m.helpOpen = true
					case "detail":
						m.showDetail(strings.Repeat("JSON value\n", 100))
					case "error":
						m.errorOpen = true
					}
					if failed {
						m.setStatus(strings.Repeat("Cannot open database: ", 100), true)
					}
					view := ansi.Strip(m.View())
					if lipgloss.Height(view) > m.height {
						t.Fatalf("height %d exceeds %d", lipgloss.Height(view), m.height)
					}
					if lipgloss.Width(view) > m.width {
						t.Fatalf("width %d exceeds %d", lipgloss.Width(view), m.width)
					}
				})
			}
		}
	}
}

func TestHelpInsideDialogCanCloseWithH(t *testing.T) {
	m := model{app: domain.NewApp(), width: 80, height: 24}
	m.form("open", "Open", field{"Path", ""})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyF1})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = next.(model)
	if m.helpOpen || m.modal == nil || m.modal.fields[0].value != "" {
		t.Fatal("h should close help and preserve the dialog")
	}
}

func TestReadOnlyHotkeysDoNotOpenMutationDialogs(t *testing.T) {
	for _, key := range []string{"b", "D", "r", "e", "d", "i"} {
		m := model{app: domain.NewApp(), width: 80, height: 24, info: domain.Info{Path: "db", ReadOnly: true}, path: domain.Path{[]byte("b")}, focus: 1}
		m.page.Records = []domain.Record{{Key: []byte("key"), Value: []byte("value")}}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = next.(model)
		if cmd != nil || m.modal != nil || !m.statusError || !strings.Contains(m.status, "read-only") {
			t.Fatalf("%s: mutation not blocked before opening its dialog", key)
		}
		if !strings.Contains(ansi.Strip(m.View()), "READ ONLY") {
			t.Fatal("missing read-only indicator")
		}
	}
}

func TestDeletingBucketClearsChildFiltersAndSelection(t *testing.T) {
	m := model{ctx: context.Background(), app: domain.NewApp(), width: 80, height: 24, path: domain.Path{[]byte("root"), []byte("child")}, bucketContains: []byte("child-only"), keyContains: []byte("child-key"), selected: 20, bucketSelected: 30, recordTop: 20}
	next, _ := m.Update(deletedBucket{})
	m = next.(model)
	if len(m.path) != 1 || len(m.bucketContains) != 0 || len(m.keyContains) != 0 || m.selected != 0 || m.bucketSelected != 0 || m.recordTop != 0 {
		t.Fatal("parent inherited the deleted bucket's filters or selection")
	}
}

func TestSearchPagingAndChangingSearchType(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, err := adapters.Open(filepath.Join(t.TempDir(), "search.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("db", db); err != nil {
		t.Fatal(err)
	}
	p := domain.Path{[]byte("root")}
	if err = db.CreateBucket(p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 37; i++ {
		if err = db.Put(p, []byte(fmt.Sprintf("key:%02d", i)), []byte("matched-value")); err != nil {
			t.Fatal(err)
		}
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 16, searchOpen: true, searchKind: "record keys", searchTerm: []byte("key:")}
	m = completeLoad(t, m, m.loadSearch())
	for i := 0; i < 37; i++ {
		if key := string(m.searchPage.Hits[m.searchSelected].Key); key != fmt.Sprintf("key:%02d", i) {
			t.Fatalf("forward %d: %s", i, key)
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(model)
		if cmd != nil {
			m = completeLoad(t, m, cmd)
		}
	}
	for i := 36; i >= 0; i-- {
		if key := string(m.searchPage.Hits[m.searchSelected].Key); key != fmt.Sprintf("key:%02d", i) {
			t.Fatalf("backward %d: %s", i, key)
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = next.(model)
		if cmd != nil {
			m = completeLoad(t, m, cmd)
		}
	}
	for _, query := range []struct {
		kind, term string
		hits       int
	}{{"record values", "matched-value", m.pageSize()}, {"buckets", "root", 1}, {"record keys", "absent", 0}, {"record values", "absent", 0}} {
		m.form("global-search", "Find", field{"Type", query.kind}, field{"Contains", query.term})
		next, cmd := m.submit()
		m = completeLoad(t, next.(model), cmd)
		if len(m.searchPage.Hits) != query.hits || m.searchSelected != 0 || m.searchTop != 0 || len(m.searchHistory) != 0 {
			t.Fatalf("%s %s: stale results or navigation", query.kind, query.term)
		}
	}
}

func TestInteractiveCRUDImportExportAndConfirmation(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, err := adapters.Open(filepath.Join(t.TempDir(), "workflow.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("db", db); err != nil {
		t.Fatal(err)
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24}
	settle := func(cmd tea.Cmd) {
		t.Helper()
		for steps := 0; cmd != nil; steps++ {
			if steps > 4 {
				t.Fatal("unexpected operation loop")
			}
			batch, ok := cmd().(tea.BatchMsg)
			if !ok || len(batch) == 0 {
				t.Fatal("expected worker operation")
			}
			next, follow := m.Update(batch[0]())
			m, cmd = next.(model), follow
		}
	}
	submit := func(kind string, fields ...field) {
		t.Helper()
		m.form(kind, kind, fields...)
		next, cmd := m.submit()
		m = next.(model)
		settle(cmd)
		if m.statusError {
			t.Fatalf("%s: %s", kind, m.status)
		}
	}
	settle(m.reload())
	submit("bucket", field{"Name", "users"})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	settle(cmd)
	submit("put", field{"Key", "hex:00ff"}, field{"Value", "hex:001bff"})
	if len(m.page.Records) != 1 || m.focus != 1 {
		t.Fatal("new record not shown or focused")
	}
	export := filepath.Join(t.TempDir(), "export.jsonl")
	submit("export", field{"Path", export})
	data, err := os.ReadFile(export)
	if err != nil {
		t.Fatal(err)
	}
	var rec domain.Record
	if err = json.Unmarshal(data, &rec); err != nil || string(rec.Key) != string([]byte{0, 255}) || string(rec.Value) != string([]byte{0, 27, 255}) {
		t.Fatalf("binary export changed bytes: %v", err)
	}
	submit("put", field{"Key", "hex:00ff"}, field{"Value", "updated"})
	if string(m.page.Records[0].Value) != "updated" {
		t.Fatal("edit not reflected")
	}
	m.form("delete", "Delete", field{"Confirm", "no"})
	next, cmd = m.submit()
	m = next.(model)
	if cmd != nil || m.modal == nil || !m.statusError {
		t.Fatal("unconfirmed deletion was accepted")
	}
	submit("delete", field{"Confirm", "yes"})
	if len(m.page.Records) != 0 {
		t.Fatal("deleted record remained visible")
	}
	submit("import", field{"Path", export}, field{"Duplicates", "overwrite"})
	if len(m.page.Records) != 1 || string(m.page.Records[0].Value) != string(rec.Value) {
		t.Fatal("import did not restore binary record")
	}
	m.form("export", "Export", field{"Path", export})
	next, cmd = m.submit()
	m = next.(model)
	settle(cmd)
	if !m.statusError {
		t.Fatal("export overwrote an existing file")
	}
	after, err := os.ReadFile(export)
	if err != nil || string(after) != string(data) {
		t.Fatal("failed export changed existing output")
	}
	submit("delete-bucket", field{"Confirm", "yes"})
	if len(m.path) != 0 || len(m.buckets) != 0 {
		t.Fatal("bucket deletion did not return to root")
	}
	submit("close", field{"Confirm", "yes"})
	if len(app.Names()) != 0 || m.info.Path != "" {
		t.Fatal("closed database remained in UI")
	}
}

func TestCancelledExportDoesNotCreateFile(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, err := adapters.Open(filepath.Join(t.TempDir(), "db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Add("db", db); err != nil {
		t.Fatal(err)
	}
	p := domain.Path{[]byte("b")}
	if err = db.CreateBucket(p); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := model{ctx: ctx, app: app, workers: &workers{}, width: 80, height: 24, path: p}
	output := filepath.Join(t.TempDir(), "cancelled.jsonl")
	m.form("export", "Export", field{"Path", output})
	next, cmd := m.submit()
	m = next.(model)
	batch := cmd().(tea.BatchMsg)
	result := batch[0]().(finished)
	if result.err != context.Canceled || strings.Contains(result.text, "completed") {
		t.Fatalf("misleading result: %+v", result)
	}
	if _, err = os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("cancelled operation created output: %v", err)
	}
}
