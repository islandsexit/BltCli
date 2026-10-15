package tui

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/islandsexit/BltCli/utils"
)

const editLimit = 1 << 20

type valueEditor struct {
	mode    string
	dirty   bool
	message string
}

func editableText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func (d *dialog) initEditor() {
	data, err := utils.Decode(d.fields[1].value)
	if err != nil {
		return
	}
	d.editor = &valueEditor{mode: "text"}
	if !editableText(data) {
		d.editor.mode = "hex"
		d.fields[1].value = hex.EncodeToString(data)
	} else {
		d.fields[1].value = string(data)
		if json.Valid(data) {
			d.editor.mode = "JSON"
			if pretty, err := formatEditorJSON(string(data)); err == nil {
				d.fields[1].value = pretty
			}
		}
	}
	d.positions[1] = len(d.fields[1].value)
}

// formatEditorJSON preserves numeric tokens and duplicate keys. Output is bounded
// as well as input, including deeply nested documents whose indentation expands.
func formatEditorJSON(s string) (string, error) {
	if len(s) > editLimit {
		return "", errors.New("JSON exceeds editor limit (1 MiB)")
	}
	if !json.Valid([]byte(s)) {
		return "", jsonEditorError(s)
	}
	var out strings.Builder
	depth := 0
	inString, escape := false, false
	previous := byte(0)
	appendText := func(v string) error {
		if out.Len()+len(v) > editLimit {
			return errors.New("formatted JSON exceeds editor limit (1 MiB)")
		}
		out.WriteString(v)
		return nil
	}
	newline := func() error { return appendText("\n" + strings.Repeat("  ", depth)) }
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if err := appendText(s[i : i+1]); err != nil {
				return "", err
			}
			if escape {
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == ' ' || c == '\r' || c == '\n' || c == '\t' {
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			if err := appendText(string(c)); err != nil {
				return "", err
			}
			depth++
			j := i + 1
			for j < len(s) && strings.ContainsRune(" \r\n\t", rune(s[j])) {
				j++
			}
			if j < len(s) && s[j] != '}' && s[j] != ']' {
				if err := newline(); err != nil {
					return "", err
				}
			}
			previous = c
			continue
		case '}', ']':
			depth--
			if previous != '{' && previous != '[' {
				if err := newline(); err != nil {
					return "", err
				}
			}
		case ',':
			if err := appendText(","); err != nil {
				return "", err
			}
			if err := newline(); err != nil {
				return "", err
			}
			previous = c
			continue
		case ':':
			if err := appendText(": "); err != nil {
				return "", err
			}
			previous = c
			continue
		}
		if err := appendText(s[i : i+1]); err != nil {
			return "", err
		}
		previous = c
	}
	return out.String(), nil
}

func jsonEditorError(s string) error {
	var raw json.RawMessage
	err := json.Unmarshal([]byte(s), &raw)
	if err == nil {
		return errors.New("invalid JSON")
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		offset := min(len(s), max(0, int(syntax.Offset)-1))
		line := strings.Count(s[:offset], "\n") + 1
		column := utf8.RuneCountInString(s[strings.LastIndexByte(s[:offset], '\n')+1:offset]) + 1
		return fmt.Errorf("invalid JSON at line %d, column %d: %s", line, column, syntax.Error())
	}
	return fmt.Errorf("invalid JSON: %w", err)
}

func (d *dialog) editorBytes() ([]byte, error) {
	value := d.fields[1].value
	switch d.editor.mode {
	case "hex":
		data, err := hex.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("invalid hex: %w", err)
		}
		return data, nil
	case "JSON":
		if !json.Valid([]byte(value)) {
			return nil, jsonEditorError(value)
		}
		fallthrough
	default:
		if !json.Valid([]byte(value)) {
			return []byte(value), nil
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(value)); err != nil {
			return nil, err
		}
		return compact.Bytes(), nil
	}
}

type editorSaved struct{ err error }

func (m model) updateEditor(v tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.modal
	key := v.String()
	if key == "ctrl+s" {
		return m.submit()
	}
	if key == "esc" {
		m.modal = nil
		return m, nil
	}
	if key == "f4" || key == "ctrl+f" {
		if d.editor.mode == "hex" {
			d.editor.message = "Switch to text or JSON with F6 before formatting"
			return m, nil
		}
		pretty, err := formatEditorJSON(d.fields[1].value)
		if err != nil {
			d.editor.message = err.Error()
			return m, nil
		}
		d.editor.mode = "JSON"
		d.fields[1].value = pretty
		d.positions[1] = min(d.positions[1], len(pretty))
		for d.positions[1] > 0 && d.positions[1] < len(pretty) && !utf8.RuneStart(pretty[d.positions[1]]) {
			d.positions[1]--
		}
		d.editor.dirty = true
		d.editor.message = "JSON formatted · saves compact JSON"
		return m, nil
	}
	if key == "f6" {
		value := d.fields[1].value
		switch d.editor.mode {
		case "JSON":
			d.editor.mode = "text"
		case "text":
			d.editor.mode = "hex"
			d.fields[1].value = hex.EncodeToString([]byte(value))
		case "hex":
			data, err := hex.DecodeString(value)
			if err != nil || !editableText(data) {
				d.editor.message = "Cannot decode as UTF-8 text"
				return m, nil
			}
			d.fields[1].value = string(data)
			d.editor.mode = "text"
			if json.Valid(data) {
				d.editor.mode = "JSON"
			}
		}
		d.positions[1] = min(d.positions[1], len(d.fields[1].value))
		for d.positions[1] > 0 && d.positions[1] < len(d.fields[1].value) && !utf8.RuneStart(d.fields[1].value[d.positions[1]]) {
			d.positions[1]--
		}
		d.editor.message = "Mode: " + d.editor.mode
		return m, nil
	}
	if key == "shift+tab" {
		d.selected = 0
		return m, nil
	}
	if d.selected == 0 {
		if key == "tab" || key == "enter" || key == "down" {
			d.selected = 1
			return m, nil
		}
		return m.updateDialogField(v)
	}
	f := &d.fields[1]
	pos := &d.positions[1]
	s := f.value
	insert := func(text string) {
		limit := editLimit
		if d.editor.mode == "hex" {
			limit = 2 * editLimit
		}
		if len(s)+len(text) > limit {
			d.editor.message = "Editor size limit reached"
			return
		}
		f.value = s[:*pos] + text + s[*pos:]
		*pos += len(text)
		d.editor.dirty = true
		d.editor.message = ""
	}
	lineStart := strings.LastIndexByte(s[:*pos], '\n') + 1
	lineEnd := len(s)
	if n := strings.IndexByte(s[*pos:], '\n'); n >= 0 {
		lineEnd = *pos + n
	}
	switch key {
	case "enter":
		indentEnd := lineStart
		for indentEnd < *pos && (s[indentEnd] == ' ' || s[indentEnd] == '\t') {
			indentEnd++
		}
		indent := s[lineStart:indentEnd]
		if d.editor.mode == "JSON" && *pos > 0 && (s[*pos-1] == '{' || s[*pos-1] == '[') {
			indent += "  "
		}
		insert("\n" + indent)
	case "tab":
		insert("  ")
	case "home", "ctrl+a":
		*pos = lineStart
	case "end", "ctrl+e":
		*pos = lineEnd
	case "ctrl+home":
		*pos = 0
	case "ctrl+end":
		*pos = len(s)
	case "up", "down", "pgup", "pgdown":
		steps := 1
		if key == "pgup" || key == "pgdown" {
			steps = max(1, m.height-12)
		}
		column := utf8.RuneCountInString(s[lineStart:*pos])
		target := lineStart
		for i := 0; i < steps; i++ {
			if key == "up" || key == "pgup" {
				if target == 0 {
					break
				}
				target = strings.LastIndexByte(s[:target-1], '\n') + 1
			} else {
				n := strings.IndexByte(s[target:], '\n')
				if n < 0 {
					break
				}
				target += n + 1
			}
		}
		end := len(s)
		if n := strings.IndexByte(s[target:], '\n'); n >= 0 {
			end = target + n
		}
		for i := 0; i < column && target < end; i++ {
			_, n := utf8.DecodeRuneInString(s[target:])
			target += n
		}
		*pos = target
	case "ctrl+k":
		f.value = s[:*pos] + s[lineEnd:]
		d.editor.dirty = true
	case "ctrl+u":
		f.value = ""
		*pos = 0
		d.editor.dirty = true
	default:
		if v.Type == tea.KeyRunes {
			insert(string(v.Runes))
		} else {
			old := f.value
			next, cmd := m.updateDialogField(v)
			m = next.(model)
			if d.fields[1].value != old {
				d.editor.dirty = true
			}
			return m, cmd
		}
	}
	return m, nil
}

func safeEditorText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ReplaceAll(s, "\t", "  "))
}

// highlightJSON handles incomplete JSON too, and only receives the visible window.
func highlightJSON(s string) string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#245EAE", Dark: "#86BDF5"})
	stringStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#28734A", Dark: "#A7DDB5"})
	numberStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#925300", Dark: "#EFC78E"})
	literalStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#8546A0", Dark: "#D6A6EF"})
	var out strings.Builder
	for i := 0; i < len(s); {
		start := i
		c := s[i]
		style := lipgloss.NewStyle()
		switch {
		case c == '"':
			i++
			escape := false
			for i < len(s) {
				c = s[i]
				i++
				if escape {
					escape = false
				} else if c == '\\' {
					escape = true
				} else if c == '"' {
					break
				}
			}
			style = stringStyle
			j := i
			for j < len(s) && strings.ContainsRune(" \n\r\t", rune(s[j])) {
				j++
			}
			if j < len(s) && s[j] == ':' {
				style = keyStyle
			}
		case c == '-' || c >= '0' && c <= '9':
			i++
			for i < len(s) && strings.ContainsRune("0123456789.eE+-", rune(s[i])) {
				i++
			}
			style = numberStyle
		case c >= 'a' && c <= 'z':
			i++
			for i < len(s) && s[i] >= 'a' && s[i] <= 'z' {
				i++
			}
			style = literalStyle
		default:
			_, n := utf8.DecodeRuneInString(s[i:])
			i += n
		}
		out.WriteString(style.Render(safeEditorText(s[start:i])))
	}
	return out.String()
}

func editorViewport(value string, pos, width, height int, jsonMode, focused bool) []string {
	width = max(1, width)
	height = max(1, height)
	// The window bounds wrapping and highlighting allocations even for long lines.
	window := width * (height + 2) * utf8.UTFMax
	start, end := max(0, pos-window), min(len(value), pos+window)
	for start < pos && !utf8.RuneStart(value[start]) {
		start++
	}
	for end < len(value) && end > pos && !utf8.RuneStart(value[end]) {
		end--
	}
	before, after := value[start:pos], value[pos:end]
	plainBefore := safeEditorText(before)
	cursorLine := strings.Count(ansi.Hardwrap(plainBefore, width, true), "\n")
	text := safeEditorText(before + "▏" + after)
	if !focused {
		text = safeEditorText(before + after)
	}
	if jsonMode {
		text = highlightJSON(text)
	}
	lines := strings.Split(ansi.Hardwrap(text, width, true), "\n")
	top := max(0, cursorLine-height+1)
	return lines[top:min(len(lines), top+height)]
}

func (m model) editorView(title, hint lipgloss.Style) string {
	d := m.modal
	ed := d.editor
	width := max(1, m.width-4)
	rows := max(1, m.height-12-m.statusExtraRows())
	var out strings.Builder
	dirty := ""
	if ed.dirty {
		dirty = " · modified"
	}
	out.WriteString(title.Render(clip(d.title+dirty, width)) + "\n")
	key := fieldLines(d.fields[0].value, d.positions[0], max(1, width-6), 1)
	keyMark := "Key: "
	if d.selected == 0 {
		keyMark = ">Key: "
	}
	if len(key) > 0 {
		out.WriteString(clip(keyMark+key[0], width))
	}
	out.WriteByte('\n')
	out.WriteString(hint.Render(clip("VALUE · "+ed.mode+" · F4 format · F6 mode", width)) + "\n")
	lines := editorViewport(d.fields[1].value, d.positions[1], max(1, width-2), rows, ed.mode == "JSON", d.selected == 1)
	for i := 0; i < rows; i++ {
		out.WriteString(hint.Render("│ "))
		if i < len(lines) {
			out.WriteString(lines[i])
		}
		out.WriteByte('\n')
	}
	pos := d.positions[1]
	value := d.fields[1].value
	line := strings.Count(value[:pos], "\n") + 1
	column := utf8.RuneCountInString(value[strings.LastIndexByte(value[:pos], '\n')+1:pos]) + 1
	status := fmt.Sprintf("Ln %d, Col %d · %d bytes", line, column, len(value))
	if ed.mode == "JSON" {
		status += " · saves compact JSON"
	}
	if ed.message != "" {
		status = ed.message
	}
	out.WriteString(hint.Render(clip(status, width)))
	return out.String()
}
