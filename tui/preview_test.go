package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/islandsexit/BltCli/domain"
	"github.com/muesli/termenv"
)

func previewColors(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

func checkPreviewLines(t *testing.T, m model) {
	t.Helper()
	if len(m.detailStyledLines) != len(m.detailLines) {
		t.Fatalf("styled/plain line count: %d / %d", len(m.detailStyledLines), len(m.detailLines))
	}
	colored := false
	for i, line := range m.detailStyledLines {
		if ansi.Strip(line) != m.detailLines[i] {
			t.Fatalf("highlight changed line %d: %q / %q", i, ansi.Strip(line), m.detailLines[i])
		}
		if line != ansi.Strip(line) {
			colored = true
		}
		if lipgloss.Width(line) > max(1, m.width-4) {
			t.Fatal("preview width overflow")
		}
	}
	if !colored {
		t.Fatal("JSON has no syntax colors")
	}
}

func TestRecordPreviewSyntaxHighlightResizeAndScroll(t *testing.T) {
	previewColors(t)
	m := model{app: domain.NewApp(), width: 45, height: 14}
	value := []byte(`{"ключ":"` + strings.Repeat(`界\"строка`, 20) + `","n":123.00e9,"bool":true,"empty":null}`)
	m.showRecordDetail("Key: k\nValue (JSON):\n", value)
	checkPreviewLines(t, m)
	if strings.Contains(m.detailStyledLines[0], "\x1b") {
		t.Fatal("header was JSON highlighted")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(model)
	if m.detailScroll == 0 {
		t.Fatal("long preview did not scroll")
	}
	view := m.View()
	if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
		t.Fatal("preview overflow")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(model)
	checkPreviewLines(t, m)
	m.showDetail("Statistics: 123")
	if len(m.detailStyledLines) != 0 || len(m.detailJSON) != 0 {
		t.Fatal("unrelated detail retained syntax colors")
	}
}

func TestSearchAndDiffPreviewSyntaxHighlight(t *testing.T) {
	previewColors(t)
	value := []byte(`{"s":"hello","b":false,"n":10}`)
	text, ranges := recordDetailText("Bucket: root\nKey: k\nValue (JSON):\n", value)
	m := model{app: domain.NewApp(), width: 60, height: 16}
	next, _ := m.Update(searchDetail{text: text, jsonRanges: ranges, path: domain.Path{[]byte("root")}})
	m = next.(model)
	checkPreviewLines(t, m)
	e := domain.DiffEntry{Kind: "record", Path: domain.Path{[]byte("root")}, Key: []byte("k"), BasePresent: true, OtherPresent: true, Base: value, Other: []byte(`{"s":"other","b":true,"n":20}`)}
	e.BaseBytes, e.OtherBytes = len(e.Base), len(e.Other)
	m.showDiffDetail(e)
	if len(m.detailJSON) != 2 {
		t.Fatal("both JSON sides were not recognized")
	}
	checkPreviewLines(t, m)
	m.detailScroll = max(0, len(m.detailLines)-m.overlayHeight())
	view := ansi.Strip(m.View())
	if !strings.Contains(view, `"other"`) {
		t.Fatal("OTHER value inaccessible")
	}
	e.ValueOffset = 128
	m.showDiffDetail(e)
	if len(m.detailStyledLines) != 0 {
		t.Fatal("fragment at nonzero offset treated as complete JSON")
	}
}

func TestPreviewPlainBinaryAndBoundedValues(t *testing.T) {
	for _, value := range [][]byte{[]byte("plain text"), []byte("a: 123\nflag: true"), []byte{0, 27, 255}, []byte{'"', 255, '"'}, []byte("{\"x\":\"\x1b[2J\"}"), []byte(`{"s":"` + strings.Repeat("x", 64<<10) + `"}`)} {
		m := model{app: domain.NewApp(), width: 60, height: 16}
		m.showRecordDetail("Key: k\nValue:\n", value)
		if len(m.detailStyledLines) != 0 {
			t.Fatal("non-JSON or oversized value was highlighted")
		}
		if strings.Contains(strings.Join(m.detailLines, "\n"), "\x1b") {
			t.Fatal("stored terminal control escaped")
		}
		if len(m.detail) > 66<<10 {
			t.Fatal("preview is unbounded")
		}
	}
}
