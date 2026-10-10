package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/utils"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const helpText = `BOLTCLI — APPLICATION HELP

Getting started
o opens an existing database. c creates a new database.
Enter a file path, then press Enter. Existing files are never overwritten.
[ and ] switch between open databases. x closes the active database.

Panels and navigation
The active panel has a highlighted border and an [ACTIVE] heading.
Tab switches between BUCKETS and RECORDS. A › marker shows the selected row.
↑ / ↓ or k / j move through rows. Moving past the first or last row automatically loads the previous or next page.
Page Up / Page Down or p / n jump between pages. When ↑ returns to a previous page, its last row is selected.
The last 128 page boundaries are retained for going back; forward browsing remains unlimited.
Enter in BUCKETS opens a child bucket. The focused bucket pane expands, and the Root label keeps the root bucket visible.
Esc in RECORDS or a record preview focuses BUCKETS if it has rows; when empty, Esc goes directly to the parent bucket. Esc in BUCKETS or Backspace also goes to the parent.

Records and long values
Enter in RECORDS opens a wrapped value preview. ↑ / ↓ scroll, Page Up / Page Down scroll a screen, and Home / End jump to the beginning or end.
Keys wrap over up to three table lines; values use up to two more. Enter shows the complete key and a value preview of up to 64 KiB; export preserves the complete value.
Record keys wrap across the full row width; Enter shows the complete key without truncation. Leaf buckets automatically focus RECORDS when opened.
r creates or replaces a record. e edits the selected record. d deletes it after confirmation. Editing and deletion apply to the RECORDS panel.

Search and statistics
g or Ctrl+F opens database-wide search. Choose record keys, record values, or buckets with ←/→, then Tab to enter the substring and Enter to search.
All root and nested buckets are searched. Results show their bucket path and support arrow/page navigation.
Enter opens a bucket result or previews a record at its location; o opens the result's bucket. Esc returns to the browser.
/ in BUCKETS filters child bucket names by substring, including root-level names when at the database root.
/ in RECORDS filters keys and values by substring. Both conditions must match.
v searches values directly. P sets a key prefix. f sets an inclusive start and exclusive end.
Submit an empty field to clear its filter. Searches are bytewise and case-sensitive; text:, hex:, and base64: inputs work.
Filters search the whole current bucket, not just its visible page. Value/substring searches scan records asynchronously and can be cancelled with Esc.
Changing the bucket resets its filters. Export applies the current record filters.
s shows statistics for the current bucket, or the database when no bucket is selected.
Ctrl+R refreshes the current view.

Import and export
i imports a JSONL file into the current bucket. E exports the current filtered bucket to a new file.
JSONL key and value fields are base64 strings. Duplicate modes: overwrite, skip, error.
Imports commit bounded batches. Already committed batches remain after an error or cancellation.

Dialog editing
Tab / Shift+Tab switch fields. ← / → and Home / End move the text cursor.
Backspace / Delete remove characters. Ctrl+U clears a field; Ctrl+K removes text after the cursor.
Long fields wrap and scroll to keep the cursor visible. Enter submits; Esc cancels.
Keys and values accept plain text, text:, hex:, and base64: prefixes. Other bytes are preserved exactly.
Destructive dialogs require typing yes. Read-only databases reject writes.

Help and cancellation
h opens or closes this help outside a text field. F1 also opens help inside a dialog.
F or Ctrl+D compares the active database with another database path or open session name.
Diff uses green for BASE and pastel red for OTHER; only differing entries are listed.
Arrow keys and Page Up/Down browse differences; Enter inspects both sides. Esc returns to the browser.
Equal databases and databases without identical records at the same path/key show only a summary.
F2 opens the complete error text, including errors inside a dialog; arrows and Page Up/Down scroll it.
Esc closes help or the current dialog, or cancels an active operation.
q quits from the browser. Ctrl+C quits from any screen.

Signature: Written by Dem4ik
`

func (m *model) changePage(direction int, last bool) tea.Cmd {
	history, after, next := &m.history, &m.after, m.page.Next
	selection := &m.selected
	if m.focus == 0 {
		history, after, next = &m.bucketHistory, &m.bucketAfter, m.bucketNext
		selection = &m.bucketSelected
	}
	if direction > 0 {
		if next == nil {
			return nil
		}
		if len(*history) == 128 {
			copy(*history, (*history)[1:])
			(*history)[127] = nil
			*history = (*history)[:127]
		}
		*history = append(*history, *after)
		*after = next
	} else {
		if len(*history) == 0 {
			return nil
		}
		i := len(*history) - 1
		*after = (*history)[i]
		(*history)[i] = nil
		*history = (*history)[:i]
	}
	*selection = 0
	m.recordTop = 0
	m.selectLast = direction < 0 && last
	return m.reload()
}

func (m model) columns() (int, int) {
	width := max(1, m.width-4)
	left := max(16, min(32, width/3))
	if m.focus == 0 {
		left = max(16, min(width-15, width*2/3))
	}
	return left, max(1, width-left-1)
}

func filterInput(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return editable(b)
}
func (m *model) resetRecordPage() {
	m.after = nil
	m.history = nil
	m.selected = 0
	m.recordTop = 0
	m.selectLast = false
}
func (m *model) goParent() tea.Cmd {
	if len(m.path) == 0 {
		return nil
	}
	m.path = m.path[:len(m.path)-1]
	m.focus = 0
	m.resetQueries()
	return m.reload()
}

func (m *model) escapeRecords() tea.Cmd {
	if len(m.buckets) == 0 {
		return m.goParent()
	}
	m.focus = 0
	m.keepRecordVisible()
	return nil
}
func (m model) filterStatus() string {
	var filters []string
	if len(m.bucketContains) > 0 {
		filters = append(filters, "bucket:"+utils.Display(m.bucketContains, 12))
	}
	if len(m.keyContains) > 0 {
		filters = append(filters, "key:"+utils.Display(m.keyContains, 12))
	}
	if len(m.valueContains) > 0 {
		filters = append(filters, "value:"+utils.Display(m.valueContains, 12))
	}
	if len(m.prefix) > 0 {
		filters = append(filters, "prefix:"+utils.Display(m.prefix, 12))
	}
	if len(filters) == 0 {
		return ""
	}
	return " · Filter " + strings.Join(filters, " ")
}
func recordLines(rec domain.Record, width int) []string {
	keys, values := recordParts(rec, width)
	return append(keys, values...)
}

func recordParts(rec domain.Record, width int) ([]string, []string) {
	width = max(8, width)
	keys := wrapLines(utils.Display(rec.Key, width*3*utf8.UTFMax), width)
	if len(keys) > 3 {
		keys = keys[:3]
		keys[2] = clip(keys[2], width-1) + "…"
	}
	values := wrapLines("→ "+utils.Display(rec.Value, width*2*utf8.UTFMax), width)
	if len(values) > 2 {
		values = values[:2]
		values[1] = ansi.Truncate(values[1], width-1, "") + "…"
	}
	return keys, values
}
func (m *model) keepRecordVisible() {
	if len(m.page.Records) == 0 {
		m.recordTop = 0
		return
	}
	m.recordTop = max(0, min(m.recordTop, m.selected))
	_, right := m.columns()
	width := right - 4
	rows := 0
	for i := m.recordTop; i <= m.selected && i < len(m.page.Records); i++ {
		rows += len(recordLines(m.page.Records[i], width))
	}
	for rows > m.pageSize() && m.recordTop < m.selected {
		rows -= len(recordLines(m.page.Records[m.recordTop], width))
		m.recordTop++
	}
}
func (m model) statusExtraRows() int {
	if m.statusError && !m.errorOpen {
		return 3
	}
	return 0
}

func (m model) overlayHeight() int { return max(1, m.height-9-m.statusExtraRows()) }
func (m *model) showDetail(text string) {
	m.detail = text
	m.detailIsRecord = false
	m.detailScroll = 0
	m.wrapDetail()
}
func wrapLines(text string, width int) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, strings.Split(ansi.Hardwrap(utils.SafeText(line), max(1, width), true), "\n")...)
	}
	return lines
}
func (m *model) wrapDetail() {
	if m.detail == "" {
		m.detailLines = nil
		return
	}
	m.detailLines = wrapLines(m.detail, max(1, m.width-4))
	m.detailScroll = min(m.detailScroll, max(0, len(m.detailLines)-m.overlayHeight()))
}
func (m model) helpLines() []string { return wrapLines(helpText, max(1, m.width-4)) }
func scrollText(key string, total, height int, offset *int) {
	switch key {
	case "up", "k":
		*offset--
	case "down", "j":
		*offset++
	case "pgup", "p":
		*offset -= height
	case "pgdown", "n":
		*offset += height
	case "home":
		*offset = 0
	case "end":
		*offset = total - height
	}
	*offset = max(0, min(*offset, max(0, total-height)))
}

// fieldLines wraps only a bounded window around the cursor, even for large fields.
func fieldLines(value string, pos, width, height int) []string {
	width = max(1, width)
	height = max(1, height)
	pos = max(0, min(pos, len(value)))
	window := width * (height + 2) * utf8.UTFMax
	start, end := max(0, pos-window), min(len(value), pos+window)
	for start < pos && !utf8.RuneStart(value[start]) {
		start++
	}
	for end < len(value) && end > pos && !utf8.RuneStart(value[end]) {
		end--
	}
	before := utils.SafeText(value[start:pos])
	after := utils.SafeText(value[pos:end])
	if start > 0 {
		before = "…" + before
	}
	if end < len(value) {
		after += "…"
	}
	cursorLine := strings.Count(ansi.Hardwrap(before, width, true), "\n")
	lines := strings.Split(ansi.Hardwrap(before+"▏"+after, width, true), "\n")
	top := max(0, cursorLine-height+1)
	return lines[top:min(len(lines), top+height)]
}

func (m model) contextHints() (string, string) {
	if m.errorOpen {
		return "↑↓ scroll · PgUp/PgDn page · Home/End", "F2 / Esc close error · Ctrl+C quit"
	}
	if m.helpOpen {
		return "↑↓ scroll · PgUp/PgDn page · Home/End", "h / Esc close help · Ctrl+C quit"
	}
	if m.busy {
		return "Esc cancel operation · h help", "Ctrl+C quit"
	}
	if m.modal != nil {
		if m.modal.kind == "global-search" {
			return "←→ choose type · Tab query", "Enter find · Esc cancel · F1 help"
		}
		action := "submit"
		switch m.modal.kind {
		case "put":
			action = "save record"
		case "delete", "delete-bucket", "close":
			action = "confirm"
		case "open", "create-db":
			action = "open database"
		case "diff":
			action = "compare databases"
		}
		return "←→ edit · Tab field · Ctrl+U clear", "Enter " + action + " · Esc cancel · F1 help"
	}
	if m.diffOpen && m.detail == "" {
		return "↑↓ select · Enter inspect · PgUp/PgDn · Home/End", "Esc close diff · h help · F2 full error"
	}
	if m.detail != "" {
		if m.diffDetail {
			return "↑↓ scroll · PgUp/PgDn page · Home/End", "Esc differences · h help"
		}
		return "↑↓ scroll · PgUp/PgDn page · Home/End", "Esc buckets · Enter close · h help"
	}
	if m.searchOpen {
		return "↑↓ results · Enter open · o bucket", "g change search · Esc back · h help"
	}
	if m.info.Path == "" {
		return "o open database · c create database", "h help · F2 full error · q quit"
	}
	if m.focus == 0 {
		navigation := "↑↓ browse · Enter open · Tab records"
		if len(m.buckets) == 0 {
			navigation = "Tab records"
			if len(m.path) > 0 {
				navigation += " · Backspace parent"
			}
		}
		actions := "s statistics"
		if !m.info.ReadOnly {
			actions = "b create bucket"
			if len(m.path) > 0 {
				actions += " · D delete bucket"
			}
		}
		return navigation, "F diff · g find all · / filter · " + actions + " · h help"
	}
	if len(m.path) == 0 {
		return "Tab buckets · Enter opens a bucket", "F diff · g find all · h help · q quit"
	}
	if len(m.page.Records) == 0 {
		actions := "/ search · f range"
		if !m.info.ReadOnly {
			actions = "r new record · i import · / search"
		}
		return actions, "F diff · g find all · Esc buckets · h help"
	}
	actions := "g find all · / filter · v values"
	if !m.info.ReadOnly {
		actions = "g find all · / filter · e edit"
	}
	return "↑↓ browse · Enter view · Tab buckets", "F diff · " + actions + " · h help"
}

func (m model) panelView(title string, lines []string, width int, active bool, accent, muted lipgloss.TerminalColor) string {
	color := muted
	header := "  " + title
	if active {
		color = accent
		header = "[ACTIVE] " + title
	}
	heading := lipgloss.NewStyle().Bold(active).Foreground(color).Render(clip(header, width-2))
	contents := heading + "\n" + strings.Join(lines, "\n")
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color).Width(width - 2).Render(contents)
}

func (m model) browserView(accent, muted lipgloss.TerminalColor) string {
	left, right := m.columns()
	rows := m.pageSize()
	leftLines := make([]string, rows)
	rightLines := make([]string, rows)
	activeRow := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#111827"}).Background(accent)
	inactiveRow := lipgloss.NewStyle().Foreground(muted)
	for i := 0; i < rows; i++ {
		line := ""
		bucketIndex := i - 2
		if bucketIndex >= 0 && bucketIndex < len(m.buckets) {
			mark := "  "
			if bucketIndex == m.bucketSelected {
				mark = "› "
			}
			line = mark + clip(utils.Display(m.buckets[bucketIndex], (left-4)*4), left-4)
		}
		line = pad(line, left-2)
		if bucketIndex == m.bucketSelected && bucketIndex >= 0 && bucketIndex < len(m.buckets) {
			if m.focus == 0 {
				line = activeRow.Render(line)
			} else {
				line = inactiveRow.Render(line)
			}
		}
		leftLines[i] = line
		rightLines[i] = strings.Repeat(" ", right-2)
	}
	context := []string{"Root buckets", ""}
	if len(m.path) > 0 {
		context = wrapLines("Root: "+utils.Display(m.path[0], 1024), left-2)
		if len(context) > 2 {
			context = context[:2]
			context[1] = ansi.Truncate(context[1], left-3, "") + "…"
		}
	}
	for i := 0; i < min(rows, 2, len(context)); i++ {
		leftLines[i] = lipgloss.NewStyle().Foreground(accent).Render(pad(context[i], left-2))
	}
	row := 0
	for i := m.recordTop; i < len(m.page.Records) && row < rows; i++ {
		keys, values := recordParts(m.page.Records[i], right-4)
		lines := append(keys, values...)
		for j, line := range lines {
			if row >= rows {
				break
			}
			mark := "  "
			if i == m.selected && j == 0 {
				mark = "› "
			}
			line = pad(mark+line, right-2)
			style := lipgloss.NewStyle().Foreground(muted)
			if j < len(keys) {
				style = lipgloss.NewStyle().Bold(true).Foreground(accent)
			}
			if i == m.selected {
				if m.focus == 1 {
					style = activeRow.Bold(j < len(keys))
				}
			}
			line = style.Render(line)
			rightLines[row] = line
			row++
		}
	}
	if len(m.buckets) == 0 {
		leftLines[min(2, rows-1)] = clip("  No matching buckets", left-2)
	}
	if len(m.page.Records) == 0 {
		empty := "  No matching records"
		if len(m.path) == 0 {
			empty = "  Select a bucket"
		}
		rightLines[0] = clip(empty, right-2)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, m.panelView("BUCKETS", leftLines, left, m.focus == 0, accent, muted), " ", m.panelView("RECORDS", rightLines, right, m.focus == 1, accent, muted))
}

func (m model) overlayView(lines []string, offset int, title string, heading lipgloss.Style) string {
	height := m.overlayHeight()
	offset = max(0, min(offset, max(0, len(lines)-height)))
	var content strings.Builder
	content.WriteString(heading.Render(title) + "\n")
	valueStart := len(lines)
	diffStyle := heading
	green, red := diffColors()
	if m.diffDetail {
		for _, line := range lines[:offset] {
			if line == "BASE (green):" {
				diffStyle = green
			}
			if line == "OTHER (red):" {
				diffStyle = red
			}
		}
	}
	if title == "DETAILS" && m.detailIsRecord {
		for i, line := range lines {
			if strings.HasPrefix(line, "Value (") {
				valueStart = i
				break
			}
		}
	}
	for i := 0; i < height; i++ {
		if offset+i < len(lines) {
			line := lines[offset+i]
			if title == "DETAILS" && m.detailIsRecord && offset+i < valueStart {
				line = heading.Render(line)
			}
			if title == "DETAILS" && m.diffDetail {
				if line == "BASE (green):" {
					diffStyle = green
				}
				if line == "OTHER (red):" {
					diffStyle = red
				}
				line = diffStyle.Render(line)
			}
			content.WriteString(line)
		}
		if i+1 < height {
			content.WriteByte('\n')
		}
	}
	return content.String() + "\n" + fmt.Sprintf("Lines %d–%d of %d", min(offset+1, len(lines)), min(offset+height, len(lines)), len(lines))
}
