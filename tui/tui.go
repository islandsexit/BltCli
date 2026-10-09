// Package tui implements an asynchronous, paginated terminal database manager.
package tui

import (
	"bltCli/domain"
	"bltCli/repository"
	"bltCli/repository/adapters"
	"bltCli/utils"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type loaded struct {
	limit      int
	buckets    [][]byte
	bucketNext []byte
	page       domain.Page
	info       domain.Info
	err        error
}
type finished struct {
	text string
	err  error
}
type tick struct{ generation int }
type field struct{ label, value string }
type dialog struct {
	kind, title string
	fields      []field
	positions   []int
	selected    int
}
type model struct {
	ctx                                        context.Context
	app                                        *domain.App
	workers                                    *workers
	paths                                      []string
	readOnly                                   bool
	width, height                              int
	path                                       domain.Path
	buckets                                    [][]byte
	bucketNext                                 []byte
	page                                       domain.Page
	info                                       domain.Info
	focus, selected, bucketSelected            int
	after, bucketAfter                         []byte
	prefix, start, end                         []byte
	bucketContains, keyContains, valueContains []byte
	history, bucketHistory                     [][]byte
	busy                                       bool
	cancel                                     context.CancelFunc
	status                                     string
	statusError                                bool
	notice                                     string
	noticeError                                bool
	modal                                      *dialog
	detail                                     string
	detailIsRecord                             bool
	detailLines                                []string
	detailScroll                               int
	helpOpen                                   bool
	helpScroll                                 int
	recordTop                                  int
	selectLast                                 bool
	frame                                      int
	generation                                 int
	searchOpen                                 bool
	searchKind                                 string
	searchTerm                                 []byte
	searchPage                                 domain.SearchPage
	searchAfter                                *domain.SearchPosition
	searchHistory                              []*domain.SearchPosition
	searchSelected, searchTop                  int
	searchSelectLast                           bool
}

func (m *model) setStatus(text string, isError bool) {
	m.status = text
	m.statusError = isError
}

// Run owns all database resources for one TUI session.
func Run(ctx context.Context, paths []string, readOnly bool) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	app := domain.NewApp()
	jobs := &workers{}
	defer func() { cancel(); jobs.stop(); err = errors.Join(err, app.CloseAll()) }()
	m := model{ctx: ctx, app: app, workers: jobs, paths: paths, readOnly: readOnly, width: 100, height: 30, busy: len(paths) > 0, status: "Open a database with o, or create one with c"}
	_, err = tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return
}

// workers gates late commands and waits for active commands before handles close.
type workers struct {
	mu      sync.Mutex
	wg      sync.WaitGroup
	stopped bool
}

func (w *workers) command(fn tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		w.mu.Lock()
		if w.stopped {
			w.mu.Unlock()
			return nil
		}
		w.wg.Add(1)
		w.mu.Unlock()
		defer w.wg.Done()
		return fn()
	}
}
func (w *workers) stop() { w.mu.Lock(); w.stopped = true; w.mu.Unlock(); w.wg.Wait() }
func pulse(generation int) tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tick{generation: generation} })
}
func (m model) Init() tea.Cmd {
	if len(m.paths) == 0 {
		return nil
	}
	return tea.Batch(pulse(m.generation), m.workers.command(func() tea.Msg {
		for _, p := range m.paths {
			if e := m.ctx.Err(); e != nil {
				return finished{err: e}
			}
			db, e := adapters.Open(p, false, m.readOnly)
			if e != nil {
				return finished{err: e}
			}
			info, e := db.Info()
			if e != nil {
				return finished{err: errors.Join(e, db.Close())}
			}
			if e = m.app.Add(info.Path, db); e != nil {
				return finished{err: errors.Join(e, db.Close())}
			}
		}
		return opened{}
	}))
}
func (m *model) operation(fn func(context.Context) tea.Msg) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.busy = true
	m.generation++
	m.setStatus("Working… (Esc cancels)", false)
	return tea.Batch(m.workers.command(func() tea.Msg { defer cancel(); return fn(ctx) }), pulse(m.generation))
}
func (m *model) reload() tea.Cmd {
	_, db, e := m.app.Active()
	if e != nil {
		m.buckets = nil
		m.page = domain.Page{}
		m.info = domain.Info{}
		m.setStatus("No database open — o opens, c creates", false)
		if m.notice != "" {
			m.setStatus(m.notice, m.noticeError)
			m.notice = ""
			m.noticeError = false
		}
		return nil
	}
	p := m.path
	q := domain.Query{Limit: m.pageSize(), After: m.after, Prefix: m.prefix, Start: m.start, End: m.end, Contains: m.keyContains, ValueContains: m.valueContains}
	after := m.bucketAfter
	bucketContains := m.bucketContains
	limit := m.pageSize()
	bucketLimit := m.bucketPageSize()
	m.buckets = nil
	m.bucketNext = nil
	m.page = domain.Page{}
	return m.operation(func(ctx context.Context) tea.Msg {
		info, e := db.Info()
		if e != nil {
			return loaded{err: e}
		}
		names, e := db.FindBuckets(ctx, p, domain.Query{After: after, Contains: bucketContains, Limit: bucketLimit + 1})
		if e != nil {
			return loaded{err: e}
		}
		var page domain.Page
		var next []byte
		if len(names) > bucketLimit {
			next = names[bucketLimit-1]
			names = names[:bucketLimit]
		}
		if len(p) > 0 {
			page, e = db.List(ctx, p, q)
		}
		return loaded{limit: limit, buckets: names, bucketNext: next, page: page, info: info, err: e}
	})
}
func (m model) pageSize() int       { return max(1, min(100, m.height-11)) }
func (m model) bucketPageSize() int { return max(1, m.pageSize()-2) }
func (m *model) reset() {
	m.path = nil
	m.searchOpen = false
	m.searchPage = domain.SearchPage{}
	m.searchAfter = nil
	m.searchHistory = nil
	m.searchTerm = nil
	m.searchKind = ""
	m.resetQueries()
}
func (m *model) resetQueries() {
	m.after = nil
	m.bucketAfter = nil
	m.history = nil
	m.bucketHistory = nil
	m.prefix = nil
	m.start = nil
	m.end = nil
	m.bucketContains = nil
	m.keyContains = nil
	m.valueContains = nil
	m.selected = 0
	m.bucketSelected = 0
	m.recordTop = 0
	m.selectLast = false
}
func (m *model) form(kind, title string, fields ...field) {
	positions := make([]int, len(fields))
	for i, f := range fields {
		positions[i] = len(f.value)
	}
	m.modal = &dialog{kind: kind, title: title, fields: fields, positions: positions}
}
func binary(b []byte) string { return "hex:" + hex.EncodeToString(b) }
func editable(b []byte) string {
	if utf8.Valid(b) {
		printable := true
		for _, r := range string(b) {
			if !unicode.IsPrint(r) {
				printable = false
				break
			}
		}
		if printable {
			return "text:" + string(b)
		}
	}
	return binary(b)
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.wrapDetail()
		if !m.busy {
			if m.searchOpen && m.modal == nil && m.detail == "" {
				return m, m.loadSearch()
			}
			return m, m.reload()
		}
	case tick:
		if m.busy && v.generation == m.generation {
			m.frame++
			return m, pulse(m.generation)
		}
	case loaded:
		m.busy = false
		m.cancel = nil
		if v.err != nil {
			m.setStatus(v.err.Error(), true)
			return m, nil
		}
		if v.limit > 0 && v.limit != m.pageSize() {
			return m, m.reload()
		}
		m.buckets = v.buckets
		m.bucketNext = v.bucketNext
		m.page = v.page
		m.info = v.info
		if len(m.path) > 0 && len(m.buckets) == 0 {
			m.focus = 1
		}
		m.selected = min(m.selected, max(0, len(m.page.Records)-1))
		m.bucketSelected = min(m.bucketSelected, max(0, len(m.buckets)-1))
		if m.selectLast {
			if m.focus == 0 {
				m.bucketSelected = max(0, len(m.buckets)-1)
			} else {
				m.selected = max(0, len(m.page.Records)-1)
			}
			m.selectLast = false
		}
		m.keepRecordVisible()
		m.setStatus(fmt.Sprintf("%d visible records · %d child buckets", len(m.page.Records), len(m.buckets)), false)
		if m.notice != "" {
			m.status = m.notice + " · " + m.status
			m.statusError = m.noticeError
			m.notice = ""
			m.noticeError = false
		}
		return m, nil
	case searchLoaded:
		m.busy = false
		m.cancel = nil
		if v.err != nil {
			m.setStatus(v.err.Error(), true)
			return m, nil
		}
		m.searchPage = v.page
		m.searchSelected = min(m.searchSelected, max(0, len(v.page.Hits)-1))
		if m.searchSelectLast {
			m.searchSelected = max(0, len(v.page.Hits)-1)
			m.searchSelectLast = false
		}
		m.keepSearchVisible()
		m.setStatus(fmt.Sprintf("%d search results · %s", len(v.page.Hits), m.searchKind), false)
		return m, nil
	case searchDetail:
		m.busy = false
		m.cancel = nil
		if v.err != nil {
			m.setStatus(v.err.Error(), true)
			return m, nil
		}
		m.path = v.path
		m.resetQueries()
		m.searchOpen = false
		m.searchPage = domain.SearchPage{}
		m.focus = 1
		m.showDetail(v.text)
		m.detailIsRecord = true
		return m, m.reload()
	case finished:
		m.busy = false
		m.cancel = nil
		if v.err != nil {
			m.notice = v.text + " " + v.err.Error()
			m.noticeError = true
			return m, m.reload()
		}
		m.notice = v.text
		m.noticeError = false
		return m, m.reload()
	case opened:
		m.busy = false
		m.cancel = nil
		if v.err != nil {
			m.setStatus(v.err.Error(), true)
			return m, nil
		}
		m.reset()
		return m, m.reload()
	case deletedBucket:
		m.busy = false
		m.cancel = nil
		if v.err != nil {
			m.setStatus(v.err.Error(), true)
			return m, nil
		}
		if len(m.path) > 0 {
			m.path = m.path[:len(m.path)-1]
		}
		m.after = nil
		m.bucketAfter = nil
		m.history = nil
		m.bucketHistory = nil
		return m, m.reload()
	case tea.KeyMsg:
		key := v.String()
		if key == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		if key == "f1" || key == "h" && m.modal == nil {
			m.helpOpen = !m.helpOpen
			m.helpScroll = 0
			return m, nil
		}
		if m.helpOpen {
			if key == "esc" || key == "q" {
				m.helpOpen = false
			} else {
				scrollText(key, len(m.helpLines()), m.overlayHeight(), &m.helpScroll)
			}
			return m, nil
		}
		if m.busy {
			if key == "esc" && m.cancel != nil {
				m.cancel()
			}
			return m, nil
		}
		if m.detail != "" {
			if key == "esc" || key == "enter" || key == "q" {
				if key == "esc" && m.detailIsRecord {
					m.focus = 0
					m.keepRecordVisible()
				}
				m.detail = ""
				m.detailIsRecord = false
				m.detailLines = nil
				m.detailScroll = 0
			} else {
				scrollText(key, len(m.detailLines), m.overlayHeight(), &m.detailScroll)
			}
			return m, nil
		}
		if m.modal != nil {
			return m.updateDialog(v)
		}
		if m.searchOpen {
			return m.updateSearch(key)
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "g", "ctrl+f":
			if m.info.Path != "" {
				kind := m.searchKind
				if kind == "" {
					kind = "record keys"
				}
				m.form("global-search", "Find in entire database", field{"Type (←→ choose records, values, buckets)", kind}, field{"Contains (empty lists all)", filterInput(m.searchTerm)})
			}
		case "tab":
			m.focus = 1 - m.focus
			m.keepRecordVisible()
		case "esc":
			if m.focus == 1 {
				m.focus = 0
				m.keepRecordVisible()
				return m, nil
			}
			return m, m.goParent()
		case "up", "k":
			if m.focus == 0 {
				if m.bucketSelected > 0 {
					m.bucketSelected--
				} else {
					return m, m.changePage(-1, true)
				}
			} else {
				if m.selected > 0 {
					m.selected--
					m.keepRecordVisible()
				} else {
					return m, m.changePage(-1, true)
				}
			}
		case "down", "j":
			if m.focus == 0 {
				if m.bucketSelected+1 < len(m.buckets) {
					m.bucketSelected++
				} else {
					return m, m.changePage(1, false)
				}
			} else {
				if m.selected+1 < len(m.page.Records) {
					m.selected++
					m.keepRecordVisible()
				} else {
					return m, m.changePage(1, false)
				}
			}
		case "enter":
			if m.focus == 0 && len(m.buckets) > 0 {
				m.path = append(append(domain.Path{}, m.path...), m.buckets[m.bucketSelected])
				m.resetQueries()
				return m, m.reload()
			}
			if m.focus == 1 && len(m.page.Records) > 0 {
				r := m.page.Records[m.selected]
				m.showDetail(fmt.Sprintf("Key: %s\nValue (%d bytes):\n%s", utils.Display(r.Key, len(r.Key)), len(r.Value), utils.Display(r.Value, 64<<10)))
				m.detailIsRecord = true
			}
		case "backspace":
			return m, m.goParent()
		case "n", "pgdown":
			return m, m.changePage(1, false)
		case "p", "pgup":
			return m, m.changePage(-1, false)
		case "o":
			m.form("open", "Open database", field{"File path", ""})
		case "c":
			m.form("create-db", "Create database", field{"New file path", ""})
		case "x":
			name, _, e := m.app.Active()
			if e == nil {
				m.form("close", "Close active database", field{"Type yes to close " + name, ""})
			}
		case "[", "]":
			names := m.app.Names()
			if len(names) > 0 {
				name, _, _ := m.app.Active()
				idx := 0
				for i, n := range names {
					if n == name {
						idx = i
						break
					}
				}
				if key == "]" {
					idx = (idx + 1) % len(names)
				} else {
					idx = (idx + len(names) - 1) % len(names)
				}
				if e := m.app.Select(names[idx]); e != nil {
					m.setStatus(e.Error(), true)
				} else {
					m.reset()
					return m, m.reload()
				}
			}
		case "b":
			if m.info.Path != "" {
				m.form("bucket", "Create child bucket", field{"Name (text:, hex:, base64:)", ""})
			}
		case "D":
			if len(m.path) > 0 {
				m.form("delete-bucket", "Delete current bucket and descendants", field{"Type yes to delete", ""})
			}
		case "r":
			if len(m.path) > 0 {
				m.form("put", "Create or replace record", field{"Key", ""}, field{"Value", ""})
			}
		case "e":
			if m.focus == 1 && len(m.page.Records) > 0 {
				r := m.page.Records[m.selected]
				if len(r.Value) > 1<<20 {
					m.setStatus("Value exceeds interactive edit limit (1 MiB); use CLI", true)
				} else {
					m.form("put", "Edit record", field{"Key", editable(r.Key)}, field{"Value", editable(r.Value)})
				}
			}
		case "d":
			if m.focus == 1 && len(m.page.Records) > 0 {
				m.form("delete", "Delete selected record", field{"Type yes to delete", ""})
			}
		case "/":
			if m.focus == 0 && m.info.Path != "" {
				m.form("bucket-filter", "Filter bucket names", field{"Name contains (empty clears)", filterInput(m.bucketContains)})
			} else if len(m.path) > 0 {
				m.form("record-filter", "Filter records (all conditions apply)", field{"Key contains (empty clears)", filterInput(m.keyContains)}, field{"Value contains (empty clears)", filterInput(m.valueContains)})
			}
		case "v":
			if m.focus == 1 && len(m.path) > 0 {
				m.form("value-filter", "Search record values", field{"Value contains (empty clears)", filterInput(m.valueContains)})
			}
		case "P":
			if m.focus == 1 && len(m.path) > 0 {
				m.form("filter", "Key prefix", field{"Prefix (empty clears)", filterInput(m.prefix)})
			}
		case "f":
			if len(m.path) > 0 {
				m.form("range", "Key range", field{"Start inclusive (empty clears)", filterInput(m.start)}, field{"End exclusive (empty clears)", filterInput(m.end)})
			}
		case "s":
			_, db, e := m.app.Active()
			if e != nil {
				break
			}
			p := m.path
			return m, m.operation(func(ctx context.Context) tea.Msg {
				info, e := db.Info()
				if e != nil {
					return statsResult{err: e}
				}
				text := fmt.Sprintf("File: %s\nSize: %d bytes\nRead only: %t\nFree pages: %d\nPending pages: %d", info.Path, info.Size, info.ReadOnly, info.FreePages, info.PendingPages)
				if len(p) > 0 {
					if e = ctx.Err(); e != nil {
						return statsResult{err: e}
					}
					s, e := db.BucketStats(p)
					if e != nil {
						return statsResult{err: e}
					}
					text += fmt.Sprintf("\nRecords (includes descendants): %d\nBuckets (includes current): %d\nDepth: %d\nBranch pages: %d\nLeaf pages: %d\nUsed bytes: %d", s.Records, s.Buckets, s.Depth, s.BranchPages, s.LeafPages, s.UsedBytes)
				} else {
					stats, e := db.Analyze(ctx)
					if e != nil {
						return statsResult{err: e}
					}
					text += fmt.Sprintf("\nRoot buckets: %d\nTotal buckets: %d\nTotal records: %d\nUsed bucket bytes: %d", stats.RootBuckets, stats.Buckets, stats.Records, stats.UsedBytes)
				}
				return statsResult{text: text}
			})
		case "i":
			if len(m.path) > 0 {
				m.form("import", "Import base64 JSONL (committed batches survive failure)", field{"Input file", ""}, field{"Duplicates: overwrite / skip / error", "error"})
			}
		case "E":
			if len(m.path) > 0 {
				m.form("export", "Export current search to new JSONL file", field{"New output file", ""})
			}
		case "ctrl+r":
			return m, m.reload()
		}
	case statsResult:
		m.busy = false
		m.cancel = nil
		if v.err != nil {
			m.setStatus(v.err.Error(), true)
		} else {
			m.showDetail(v.text)
		}
	}
	return m, nil
}

type statsResult struct {
	text string
	err  error
}

func (m model) updateDialog(v tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.modal
	if d.kind == "global-search" && d.selected == 0 {
		choices := []string{"record keys", "record values", "buckets"}
		index := 0
		for i, c := range choices {
			if c == d.fields[0].value {
				index = i
			}
		}
		switch v.String() {
		case "left":
			index = (index + len(choices) - 1) % len(choices)
		case "right", " ":
			index = (index + 1) % len(choices)
		case "r":
			index = 0
		case "v":
			index = 1
		case "b":
			index = 2
		case "backspace", "delete", "home", "end", "ctrl+a", "ctrl+e", "ctrl+u", "ctrl+k":
			return m, nil
		default:
			if v.Type == tea.KeyRunes {
				return m, nil
			}
			return m.updateDialogField(v)
		}
		d.fields[0].value = choices[index]
		d.positions[0] = len(choices[index])
		return m, nil
	}
	return m.updateDialogField(v)
}
func (m model) updateDialogField(v tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.modal
	f := &d.fields[d.selected]
	pos := &d.positions[d.selected]
	switch v.String() {
	case "esc":
		m.modal = nil
	case "tab", "down":
		d.selected = (d.selected + 1) % len(d.fields)
	case "shift+tab", "up":
		d.selected = (d.selected + len(d.fields) - 1) % len(d.fields)
	case "backspace":
		s := f.value
		_, n := utf8.DecodeLastRuneInString(s[:*pos])
		if n > 0 {
			f.value = s[:*pos-n] + s[*pos:]
			*pos -= n
		}
	case "left":
		_, n := utf8.DecodeLastRuneInString(f.value[:*pos])
		*pos -= n
	case "right":
		_, n := utf8.DecodeRuneInString(f.value[*pos:])
		*pos += n
	case "home", "ctrl+a":
		*pos = 0
	case "end", "ctrl+e":
		*pos = len(f.value)
	case "delete":
		_, n := utf8.DecodeRuneInString(f.value[*pos:])
		f.value = f.value[:*pos] + f.value[*pos+n:]
	case "ctrl+k":
		f.value = f.value[:*pos]
	case "ctrl+u":
		f.value = ""
		*pos = 0
	case "enter":
		return m.submit()
	default:
		if v.Type == tea.KeyRunes {
			input := string(v.Runes)
			if len(f.value)+len(input) <= 3<<20 {
				f.value = f.value[:*pos] + input + f.value[*pos:]
				*pos += len(input)
			}
		}
	}
	return m, nil
}
func (m model) submit() (tea.Model, tea.Cmd) {
	d := m.modal
	value := d.fields[0].value
	_, db, activeErr := m.app.Active()
	p := append(domain.Path{}, m.path...)
	fail := func(e error) (tea.Model, tea.Cmd) { m.setStatus(e.Error(), true); return m, nil }
	switch d.kind {
	case "global-search":
		term, e := utils.Decode(d.fields[1].value)
		if e != nil {
			return fail(e)
		}
		m.searchKind = value
		m.searchTerm = term
		m.searchAfter = nil
		m.searchHistory = nil
		m.searchSelected = 0
		m.searchTop = 0
		m.searchOpen = true
		m.modal = nil
		return m, m.loadSearch()
	case "open", "create-db":
		m.modal = nil
		create := d.kind == "create-db"
		return m, m.operation(func(ctx context.Context) tea.Msg {
			if e := ctx.Err(); e != nil {
				return finished{err: e}
			}
			handle, e := adapters.Open(value, create, m.readOnly)
			if e != nil {
				return finished{err: e}
			}
			info, e := handle.Info()
			if e != nil {
				return finished{err: errors.Join(e, handle.Close())}
			}
			if e = m.app.Add(info.Path, handle); e != nil {
				return finished{err: errors.Join(e, handle.Close())}
			}
			return opened{}
		})
	case "bucket-filter", "record-filter", "value-filter":
		a, e := utils.Decode(value)
		if e != nil {
			return fail(e)
		}
		switch d.kind {
		case "bucket-filter":
			m.bucketContains = a
			m.bucketAfter = nil
			m.bucketHistory = nil
			m.bucketSelected = 0
		case "record-filter":
			b, e := utils.Decode(d.fields[1].value)
			if e != nil {
				return fail(e)
			}
			m.keyContains = a
			m.valueContains = b
			m.resetRecordPage()
		case "value-filter":
			m.valueContains = a
			m.resetRecordPage()
		}
		m.modal = nil
		return m, m.reload()
	case "filter", "range":
		a, e := utils.Decode(value)
		if e != nil {
			return fail(e)
		}
		if value == "" {
			a = nil
		}
		if d.kind == "filter" {
			m.prefix = a
		} else {
			b, e := utils.Decode(d.fields[1].value)
			if e != nil {
				return fail(e)
			}
			if d.fields[1].value == "" {
				b = nil
			}
			m.start = a
			m.end = b
		}
		m.resetRecordPage()
		m.modal = nil
		return m, m.reload()
	}
	if activeErr != nil {
		return fail(activeErr)
	}
	if d.kind == "close" || d.kind == "delete" || d.kind == "delete-bucket" {
		if value != "yes" {
			return fail(errors.New("enter yes to confirm"))
		}
	}
	switch d.kind {
	case "close":
		name, _, _ := m.app.Active()
		m.modal = nil
		return m, m.operation(func(context.Context) tea.Msg { return opened{err: m.app.Close(name)} })
	case "bucket":
		n, e := utils.Decode(value)
		if e != nil {
			return fail(e)
		}
		m.modal = nil
		return m, m.operation(func(ctx context.Context) tea.Msg {
			if e := ctx.Err(); e != nil {
				return finished{err: e}
			}
			return finished{err: db.CreateBucket(append(p, n))}
		})
	case "delete-bucket":
		m.modal = nil
		return m, m.operation(func(ctx context.Context) tea.Msg {
			if e := ctx.Err(); e != nil {
				return finished{err: e}
			}
			return deletedBucket{err: db.DeleteBucket(p)}
		})
	case "put":
		k, e := utils.Decode(value)
		if e != nil {
			return fail(e)
		}
		val, e := utils.Decode(d.fields[1].value)
		if e != nil {
			return fail(e)
		}
		m.modal = nil
		return m, m.operation(func(ctx context.Context) tea.Msg {
			return finished{err: db.WriteBatch(ctx, p, []domain.Record{{Key: k, Value: val}}, "overwrite")}
		})
	case "delete":
		k := m.page.Records[m.selected].Key
		m.modal = nil
		return m, m.operation(func(ctx context.Context) tea.Msg {
			if e := ctx.Err(); e != nil {
				return finished{err: e}
			}
			return finished{err: db.Delete(p, k)}
		})
	case "import":
		mode := d.fields[1].value
		m.modal = nil
		return m, m.operation(func(ctx context.Context) tea.Msg {
			f, e := os.Open(value)
			if e != nil {
				return finished{err: e}
			}
			n, e := repository.Import(ctx, db, p, f, repository.ImportOptions{BatchSize: 500, BatchBytes: 8 << 20, MaxRecordBytes: 16 << 20, Duplicates: mode})
			return finished{text: fmt.Sprintf("Committed input records: %d", n), err: errors.Join(e, f.Close())}
		})
	case "export":
		q := domain.Query{Prefix: m.prefix, Start: m.start, End: m.end, Contains: m.keyContains, ValueContains: m.valueContains}
		m.modal = nil
		return m, m.operation(func(ctx context.Context) tea.Msg {
			f, e := os.OpenFile(value, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return finished{err: e}
			}
			e = repository.Export(ctx, db, p, q, f)
			return finished{text: "Export completed", err: errors.Join(e, f.Close())}
		})
	}
	return m, nil
}

type opened struct{ err error }
type deletedBucket struct{ err error }

func (m model) View() string {
	accent := lipgloss.AdaptiveColor{Light: "#5B21B6", Dark: "#C4B5FD"}
	muted := lipgloss.AdaptiveColor{Light: "#475569", Dark: "#94A3B8"}
	title := lipgloss.NewStyle().Bold(true).Foreground(accent)
	hint := lipgloss.NewStyle().Foreground(muted)
	if m.width < 45 || m.height < 14 {
		return "BoltCLI\nResize terminal to at least 45 × 14.\nh help · Ctrl+C quit"
	}
	width := m.width - 4
	var body strings.Builder
	name, _, _ := m.app.Active()
	if name == "" {
		name = "No database"
	}
	body.WriteString(title.Render("◈ BOLTCLI") + "  " + hint.Render(clip(name, width-14)) + "\n")
	parts := []string{"Database root"}
	if len(m.path) > 0 {
		parts = nil
	}
	for _, n := range m.path {
		parts = append(parts, utils.Display(n, 1024))
	}
	focus := "BUCKETS"
	if m.focus == 1 {
		focus = "RECORDS"
	}
	body.WriteString(hint.Render(clip(strings.Join(parts, " / ")+" · Focus: "+focus, width)) + "\n")
	if m.helpOpen {
		body.WriteString(m.overlayView(m.helpLines(), m.helpScroll, "HELP", title))
	} else if m.modal != nil {
		d := m.modal
		body.WriteString(title.Render(clip(d.title, width)) + "\n")
		fieldHeight := max(1, (m.height-10-len(d.fields)*2)/len(d.fields))
		for i, f := range d.fields {
			label := f.label
			if i == d.selected {
				label = "› " + label
			}
			body.WriteString(hint.Render(clip(label, width)) + "\n")
			lines := fieldLines(f.value, d.positions[i], width, fieldHeight)
			for row := 0; row < fieldHeight; row++ {
				if row < len(lines) {
					line := lines[row]
					if i == d.selected {
						line = title.Render(line)
					}
					body.WriteString(line)
				}
				body.WriteByte('\n')
			}
			if i+1 < len(d.fields) {
				body.WriteByte('\n')
			}
		}
	} else if m.detail != "" {
		body.WriteString(m.overlayView(m.detailLines, m.detailScroll, "DETAILS", title))
	} else if m.searchOpen {
		body.WriteString(m.searchView(title))
	} else {
		body.WriteString(m.browserView(accent, muted))
	}
	primary, secondary := m.contextHints()
	body.WriteString("\n" + hint.Render(clip(primary, width)) + "\n" + hint.Render(clip(secondary, width)))
	status := m.status
	if m.busy {
		status = []string{"◐", "◓", "◑", "◒"}[m.frame%4] + " " + status
	}
	if filters := m.filterStatus(); filters != "" && !m.busy {
		status = strings.TrimPrefix(filters, " · ") + " · " + status
	}
	statusStyle := hint
	if m.statusError {
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B45360", Dark: "#F2A6AD"})
		status = "Error: " + status
	}
	body.WriteString("\n" + statusStyle.Render(clip(status, width)))
	return lipgloss.NewStyle().Padding(1, 2).Render(body.String())
}
func clip(s string, width int) string {
	if width < 1 {
		return ""
	}
	return ansi.Truncate(utils.SafeText(s), width, "…")
}
func pad(s string, width int) string { return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s))) }
