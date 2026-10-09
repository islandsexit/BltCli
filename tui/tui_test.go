package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
	"github.com/islandsexit/BltCli/utils"
	"path/filepath"
	"strings"
	"testing"
)

func TestErrorNoticeSurvivesReloadAndClearsAfterSuccess(t *testing.T) {
	m := model{ctx: context.Background(), app: domain.NewApp(), workers: &workers{}, width: 80, height: 24}
	next, _ := m.Update(finished{text: "Import failed", err: errors.New("invalid input")})
	m = next.(model)
	if !m.statusError || !strings.Contains(m.View(), "Error:") || !strings.Contains(m.status, "invalid input") {
		t.Fatalf("missing error notice: %q", m.status)
	}
	next, _ = m.Update(loaded{})
	m = next.(model)
	if m.statusError || strings.Contains(m.View(), "Error:") {
		t.Fatalf("successful load retained error status: %q", m.status)
	}
}

func TestNavigationAndDialogs(t *testing.T) {
	m := model{ctx: context.Background(), app: domain.NewApp(), workers: &workers{}, width: 80, height: 24, buckets: [][]byte{[]byte("users")}, info: domain.Info{Path: "test"}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if len(m.path) != 1 || string(m.path[0]) != "users" {
		t.Fatal("bucket navigation failed")
	}
	_ = cmd
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(model)
	if m.modal == nil || m.modal.kind != "put" {
		t.Fatal("record dialog missing")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(model)
	if m.modal.fields[0].value != "a" || m.modal.fields[1].value != "v" {
		t.Fatal("form editing failed")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.modal != nil {
		t.Fatal("dialog did not close")
	}
}
func TestAsyncOpenCloseMessages(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, e := adapters.Open(filepath.Join(t.TempDir(), "db"), true, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = app.Add("db", db); e != nil {
		t.Fatal(e)
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, path: domain.Path{[]byte("old")}, busy: true, height: 24, width: 80}
	next, cmd := m.Update(opened{})
	m = next.(model)
	if m.path != nil || cmd == nil || !m.busy {
		t.Fatal("open completion did not reload root")
	}
	next, _ = m.Update(loaded{info: domain.Info{Path: "db"}})
	m = next.(model)
	if m.busy {
		t.Fatal("load did not clear busy")
	}
	m.path = domain.Path{[]byte("parent"), []byte("child")}
	next, _ = m.Update(deletedBucket{})
	m = next.(model)
	if len(m.path) != 1 {
		t.Fatal("deleted bucket navigation failed")
	}
}
func TestResponsiveSafeView(t *testing.T) {
	m := model{ctx: context.Background(), app: domain.NewApp(), width: 80, height: 24, buckets: [][]byte{[]byte("\x1b[2J")}, page: domain.Page{Records: []domain.Record{{Key: []byte("宽key"), Value: []byte("\x1b[31m")}}}}
	view := ansi.Strip(m.View())
	if strings.Contains(view, "\x1b") {
		t.Fatal("terminal controls escaped into view")
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("view overflow: %d %q", lipgloss.Width(line), line)
		}
	}
	m.width = 20
	if !strings.Contains(m.View(), "Resize") {
		t.Fatal("small terminal fallback missing")
	}
}
func TestWorkerShutdown(t *testing.T) {
	w := &workers{}
	w.stop()
	ran := false
	msg := w.command(func() tea.Msg { ran = true; return finished{} })()
	if ran || msg != nil {
		t.Fatal("command started after shutdown")
	}
}

func TestStaleTickDoesNotMultiplyTimers(t *testing.T) {
	m := model{busy: true, generation: 2}
	next, cmd := m.Update(tick{generation: 1})
	if cmd != nil || next.(model).frame != 0 {
		t.Fatal("stale tick scheduled another timer")
	}
	next, cmd = m.Update(tick{generation: 2})
	if cmd == nil || next.(model).frame != 1 {
		t.Fatal("active tick did not advance")
	}
}

func TestUnicodeEditing(t *testing.T) {
	m := model{}
	m.form("put", "Edit", field{"Value", "text:ab"})
	press := func(key tea.KeyMsg) { t.Helper(); next, _ := m.Update(key); m = next.(model) }
	press(tea.KeyMsg{Type: tea.KeyLeft})
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("宽")})
	if m.modal.fields[0].value != "text:a宽b" {
		t.Fatal(m.modal.fields[0].value)
	}
	press(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.modal.fields[0].value != "text:ab" {
		t.Fatal("backspace split Unicode")
	}
	for _, b := range [][]byte{[]byte("printable"), {0, 255}, []byte{}} {
		got, e := utils.Decode(editable(b))
		if e != nil || !bytes.Equal(got, b) {
			t.Fatalf("editable roundtrip: %x %v", got, e)
		}
	}
}

func completeLoad(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected asynchronous load")
	}
	commands, ok := cmd().(tea.BatchMsg)
	if !ok || len(commands) == 0 {
		t.Fatal("expected batched database command")
	}
	next, followup := m.Update(commands[0]())
	if followup != nil {
		t.Fatal("unexpected reload")
	}
	return next.(model)
}

func TestArrowPaginationBothPanels(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, e := adapters.Open(filepath.Join(t.TempDir(), "db"), true, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = app.Add("db", db); e != nil {
		t.Fatal(e)
	}
	path := domain.Path{[]byte("users")}
	if e = db.CreateBucket(path); e != nil {
		t.Fatal(e)
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 14, path: path}
	limit := m.pageSize()
	for i := 0; i < limit*2+1; i++ {
		key := []byte(fmt.Sprintf("%02d", i))
		if e = db.Put(path, key, []byte("v")); e != nil {
			t.Fatal(e)
		}
		if e = db.CreateBucket(domain.Path{path[0], key}); e == nil {
			t.Fatal("record and bucket unexpectedly shared key")
		}
		if e = db.CreateBucket(domain.Path{path[0], []byte("bucket:" + string(key))}); e != nil {
			t.Fatal(e)
		}
	}
	for _, focus := range []int{0, 1} {
		t.Run(fmt.Sprint(focus), func(t *testing.T) {
			m.focus = focus
			pageLimit := m.pageSize()
			if focus == 0 {
				pageLimit = m.bucketPageSize()
			}
			m.after = nil
			m.bucketAfter = nil
			m.history = nil
			m.bucketHistory = nil
			m.selected = 0
			m.bucketSelected = 0
			cmd := m.reload()
			m = completeLoad(t, m, cmd)
			for i := 0; i < pageLimit; i++ {
				next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
				m = next.(model)
				if i < pageLimit-1 {
					if cmd != nil {
						t.Fatal("loaded a page before reaching its boundary")
					}
				} else {
					m = completeLoad(t, m, cmd)
				}
			}
			if focus == 0 {
				if string(m.buckets[0]) != fmt.Sprintf("bucket:%02d", pageLimit) || m.bucketSelected != 0 {
					t.Fatalf("wrong next bucket page: %q %d", m.buckets, m.bucketSelected)
				}
			} else {
				if string(m.page.Records[0].Key) != fmt.Sprintf("%02d", pageLimit) || m.selected != 0 {
					t.Fatalf("wrong next record page: %+v", m.page)
				}
			}
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyUp})
			m = completeLoad(t, next.(model), cmd)
			if focus == 0 {
				if string(m.buckets[0]) != "bucket:00" || m.bucketSelected != pageLimit-1 {
					t.Fatal("up did not select last bucket on previous page")
				}
			} else {
				if string(m.page.Records[0].Key) != "00" || m.selected != pageLimit-1 {
					t.Fatal("up did not select last record on previous page")
				}
			}
		})
	}
}

func TestBucketFinalPageHasNoEmptyContinuation(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, e := adapters.Open(filepath.Join(t.TempDir(), "db"), true, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = app.Add("db", db); e != nil {
		t.Fatal(e)
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 14}
	for i := 0; i < m.bucketPageSize(); i++ {
		if e = db.CreateBucket(domain.Path{[]byte(fmt.Sprint(i))}); e != nil {
			t.Fatal(e)
		}
	}
	cmd := m.reload()
	m = completeLoad(t, m, cmd)
	m.bucketSelected = len(m.buckets) - 1
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil || next.(model).bucketSelected != m.bucketSelected {
		t.Fatal("last page navigated to an empty page")
	}
}

func TestWrappedValuesAndDetailScrolling(t *testing.T) {
	m := model{app: domain.NewApp(), width: 60, height: 16, focus: 1, page: domain.Page{Records: []domain.Record{{Key: []byte("key"), Value: []byte(strings.Repeat("abcdefghij", 100))}}}}
	lines := recordLines(m.page.Records[0], 30)
	if len(lines) != 3 {
		t.Fatalf("long table value did not wrap: %q", lines)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 30 {
			t.Fatalf("wrapped row overflow: %q", line)
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if len(m.detailLines) <= m.overlayHeight() {
		t.Fatal("detail value did not wrap")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(model)
	if m.detailScroll == 0 {
		t.Fatal("detail page down did not scroll")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(model)
	if m.detailScroll != len(m.detailLines)-m.overlayHeight() {
		t.Fatal("detail end did not reach last lines")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if next.(model).detailScroll != 0 {
		t.Fatal("detail home did not return to start")
	}
}

func TestHelpAndContextHints(t *testing.T) {
	m := model{app: domain.NewApp(), width: 80, height: 24, info: domain.Info{Path: "db"}, path: domain.Path{[]byte("users")}, page: domain.Page{Records: []domain.Record{{Key: []byte("k"), Value: []byte("v")}}}}
	a, b := m.contextHints()
	if strings.Contains(a+b, "e edit") || !strings.Contains(a+b, "b create") {
		t.Fatal("bucket hints include record actions")
	}
	m.focus = 1
	a, b = m.contextHints()
	if !strings.Contains(a+b, "e edit") || strings.Contains(a+b, "b create") {
		t.Fatal("record hints include bucket actions")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "[ACTIVE] RECORDS") {
		t.Fatal("record focus marker missing")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = next.(model)
	if !m.helpOpen || !strings.Contains(ansi.Strip(m.View()), "HELP") {
		t.Fatal("h did not open help")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(model)
	if m.helpScroll == 0 {
		t.Fatal("help did not scroll")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.helpOpen || m.focus != 1 {
		t.Fatal("help did not preserve panel context")
	}
	m.form("put", "Edit", field{"Key", ""})
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = next.(model)
	if m.helpOpen || m.modal.fields[0].value != "h" {
		t.Fatal("help intercepted text input")
	}
}

func TestWrappedFieldKeepsCursorVisible(t *testing.T) {
	value := "text:" + strings.Repeat("宽abc", 1000)
	for _, pos := range []int{0, 5, len(value)} {
		lines := fieldLines(value, pos, 30, 4)
		if len(lines) > 4 || !strings.Contains(strings.Join(lines, ""), "▏") {
			t.Fatalf("cursor missing from field viewport: %q", lines)
		}
		for _, line := range lines {
			if lipgloss.Width(line) > 30 {
				t.Fatalf("field overflow: %q", line)
			}
		}
	}
}

func TestAllScreenDimensions(t *testing.T) {
	for _, size := range [][2]int{{45, 14}, {60, 16}, {80, 24}, {120, 40}} {
		m := model{app: domain.NewApp(), width: size[0], height: size[1], status: "Ready", info: domain.Info{Path: "db"}, page: domain.Page{Records: []domain.Record{{Key: []byte("key"), Value: []byte(strings.Repeat("long value ", 50))}}}}
		check := func() {
			t.Helper()
			view := ansi.Strip(m.View())
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > m.width {
					t.Fatalf("%v: width overflow %q", size, line)
				}
			}
			if lines := len(strings.Split(view, "\n")); lines > m.height {
				t.Fatalf("%v: height overflow %d", size, lines)
			}
		}
		check()
		m.focus = 1
		m.keepRecordVisible()
		check()
		m.focus = 0
		m.path = domain.Path{[]byte("root_bucket_with_a_long_name")}
		check()
		m.helpOpen = true
		check()
		m.helpOpen = false
		m.showDetail(strings.Repeat("long ", 100))
		check()
		m.detail = ""
		m.form("put", "Edit record", field{"Key", "key"}, field{"Value", strings.Repeat("long ", 100)})
		check()
		m.modal = nil
		m.searchOpen = true
		m.searchKind = "record keys"
		m.searchPage = domain.SearchPage{Hits: []domain.SearchHit{{Path: domain.Path{[]byte("root"), []byte("nested")}, Key: []byte(strings.Repeat("long-key-", 100))}}}
		check()
		m.form("global-search", "Find in entire database", field{"Type (←→ choose records, values, buckets)", "record keys"}, field{"Contains (empty lists all)", "SUSE"})
		check()
	}
}

func TestFocusExpandsBucketsAndShowsRoot(t *testing.T) {
	m := model{app: domain.NewApp(), width: 100, height: 24, path: domain.Path{[]byte("root_bucket_with_a_long_name"), []byte("child")}, buckets: [][]byte{[]byte("long_child_bucket_name_that_should_fit")}}
	left, right := m.columns()
	if left <= right {
		t.Fatal("bucket focus did not expand left pane")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Root:") || !strings.Contains(view, "root_bucket_with_a_long_name") || !strings.Contains(view, "long_child_bucket_name_that_should_fit") {
		t.Fatalf("bucket labels missing:\n%s", view)
	}
	m.focus = 1
	otherLeft, otherRight := m.columns()
	if otherLeft >= left || otherRight <= right {
		t.Fatal("record focus did not restore record width")
	}
	if !strings.Contains(ansi.Strip(m.View()), "Root:") {
		t.Fatal("root label disappeared with record focus")
	}
}

func TestEscapeRecordToBucketsToParent(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, e := adapters.Open(filepath.Join(t.TempDir(), "db"), true, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = app.Add("db", db); e != nil {
		t.Fatal(e)
	}
	p := domain.Path{[]byte("root")}
	if e = db.CreateBucket(p); e != nil {
		t.Fatal(e)
	}
	if e = db.CreateBucket(append(p, []byte("child"))); e != nil {
		t.Fatal(e)
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24, path: p, focus: 1, page: domain.Page{Records: []domain.Record{{Key: []byte("key"), Value: []byte("value")}}}}
	m.buckets = [][]byte{[]byte("child")}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.detail != "" || m.focus != 0 || len(m.path) != 1 || cmd != nil {
		t.Fatal("first Esc did not return to buckets at same level")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if len(m.path) != 0 || m.focus != 0 || cmd == nil {
		t.Fatal("second Esc did not navigate up")
	}
	m = completeLoad(t, m, cmd)
	if len(m.buckets) != 1 || string(m.buckets[0]) != "root" {
		t.Fatal("root bucket missing after going up")
	}
	m.focus = 1
	m.path = p
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.focus != 0 || len(m.path) != 1 || cmd != nil {
		t.Fatal("record browser Esc changed bucket level")
	}
}

func TestEscapeEmptyBucketsGoesUpOneLevel(t *testing.T) {
	for _, preview := range []bool{false, true} {
		t.Run(fmt.Sprintf("preview=%t", preview), func(t *testing.T) {
			app := domain.NewApp()
			defer app.CloseAll()
			db, err := adapters.Open(filepath.Join(t.TempDir(), "db"), true, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = app.Add("db", db); err != nil {
				t.Fatal(err)
			}
			path := domain.Path{[]byte("root"), []byte("leaf")}
			if err = db.CreateBucket(path[:1]); err != nil {
				t.Fatal(err)
			}
			if err = db.CreateBucket(path); err != nil {
				t.Fatal(err)
			}
			m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24, path: path, focus: 1, prefix: []byte("filter")}
			if preview {
				m.showDetail("Key: key\nValue (1 bytes):\nv")
				m.detailIsRecord = true
			}
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			m = next.(model)
			if len(m.path) != 1 || string(m.path[0]) != "root" || m.detail != "" || m.detailIsRecord || len(m.prefix) != 0 || cmd == nil {
				t.Fatal("Esc failed to navigate one level up from empty bucket pane")
			}
			m = completeLoad(t, m, cmd)
			if m.focus != 0 || len(m.buckets) != 1 || string(m.buckets[0]) != "leaf" {
				t.Fatal("parent buckets were not loaded and focused")
			}
		})
	}
}

func TestContextFiltersSearchBeyondVisiblePage(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, e := adapters.Open(filepath.Join(t.TempDir(), "db"), true, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = app.Add("db", db); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 10; i++ {
		if e = db.CreateBucket(domain.Path{[]byte(fmt.Sprintf("bucket:%02d", i))}); e != nil {
			t.Fatal(e)
		}
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 14, info: domain.Info{Path: "db"}}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = next.(model)
	if m.modal == nil || m.modal.kind != "bucket-filter" {
		t.Fatal("bucket filter dialog missing")
	}
	m.modal.fields[0].value = "09"
	next, cmd := m.submit()
	m = completeLoad(t, next.(model), cmd)
	if len(m.buckets) != 1 || string(m.buckets[0]) != "bucket:09" {
		t.Fatal("filter only searched visible buckets")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = completeLoad(t, next.(model), cmd)
	if len(m.bucketContains) != 0 {
		t.Fatal("parent filter leaked into child bucket")
	}
	for _, key := range []string{"first", "target", "third"} {
		value := []byte("other")
		if key == "target" {
			value = []byte("needle")
		}
		if e = db.Put(m.path, []byte(key), value); e != nil {
			t.Fatal(e)
		}
	}
	m.focus = 1
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = next.(model)
	if m.modal.kind != "record-filter" {
		t.Fatal("record filter dialog missing")
	}
	m.modal.fields[0].value = "target"
	m.modal.fields[1].value = "needle"
	next, cmd = m.submit()
	m = completeLoad(t, next.(model), cmd)
	if len(m.page.Records) != 1 || string(m.page.Records[0].Key) != "target" {
		t.Fatal("record key/value filters not applied")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(model)
	m.modal.fields[0].value = "missing"
	next, cmd = m.submit()
	m = completeLoad(t, next.(model), cmd)
	if len(m.page.Records) != 0 {
		t.Fatal("value search reused unfiltered results")
	}
}

func BenchmarkFieldWrapping(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			value := strings.Repeat("x", size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = fieldLines(value, len(value), 60, 4)
			}
		})
	}
}

func TestFullRecordNamesAndLeafFocus(t *testing.T) {
	name := "SUSE-SU-2019:2784-1"
	lines := recordLines(domain.Record{Key: []byte(name), Value: []byte(`{"FixedVersion":"6.8.8.1-71.131.1"}`)}, 24)
	if !strings.Contains(strings.Join(lines, ""), name) {
		t.Fatalf("record name truncated: %q", lines)
	}
	long := strings.Repeat("long-record-name-", 100)
	m := model{app: domain.NewApp(), width: 80, height: 24, path: domain.Path{[]byte("leaf")}}
	next, _ := m.Update(loaded{page: domain.Page{Records: []domain.Record{{Key: []byte(long), Value: []byte("v")}}}})
	m = next.(model)
	if m.focus != 1 {
		t.Fatal("empty bucket panel did not focus records")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if !strings.Contains(m.detail, long) {
		t.Fatal("record detail does not show complete readable key")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(next.(model).path) != 0 || next.(model).detail != "" {
		t.Fatal("Esc did not leave leaf bucket and close preview")
	}
}

func TestDatabaseSearchUIChoiceAndOpen(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	db, e := adapters.Open(filepath.Join(t.TempDir(), "db"), true, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = app.Add("db", db); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"first", "second"} {
		p := domain.Path{[]byte(name)}
		if e = db.CreateBucket(p); e != nil {
			t.Fatal(e)
		}
		if e = db.Put(p, []byte("SUSE-SU-2019:2784-1"), []byte("fixed")); e != nil {
			t.Fatal(e)
		}
	}
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 80, height: 24, info: domain.Info{Path: "db"}}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = next.(model)
	if m.modal == nil || m.modal.kind != "global-search" || m.modal.fields[0].value != "record keys" {
		t.Fatal("global search chooser missing")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(model)
	if m.modal.fields[0].value != "buckets" {
		t.Fatal("cannot choose bucket search")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(model)
	m.modal.fields[1].value = "SUSE"
	next, cmd := m.submit()
	m = completeLoad(t, next.(model), cmd)
	if len(m.searchPage.Hits) != 2 {
		t.Fatal("global search did not include both root buckets")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "first") || !strings.Contains(view, "second") {
		t.Fatal("result locations missing")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	commands := cmd().(tea.BatchMsg)
	next, reload := m.Update(commands[0]())
	m = completeLoad(t, next.(model), reload)
	if m.searchOpen || !m.detailIsRecord || string(m.path[0]) != "first" || !strings.Contains(m.detail, "SUSE-SU-2019:2784-1") {
		t.Fatal("result did not open its record at correct location")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = completeLoad(t, next.(model), cmd)
	if m.focus != 0 || len(m.path) != 0 || m.detail != "" || len(m.buckets) != 2 {
		t.Fatal("result preview Esc did not return to parent buckets")
	}
}
