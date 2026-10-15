package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/islandsexit/BltCli/domain"
)

func TestModifierNULDoesNotChangeTextFields(t *testing.T) {
	for _, kind := range []string{"open", "global-search", "put"} {
		t.Run(kind, func(t *testing.T) {
			m := model{app: domain.NewApp(), width: 80, height: 24}
			switch kind {
			case "put":
				m.form(kind, "Edit", field{"Key", "key"}, field{"Value", `{"n":1}`})
			case "global-search":
				m.form(kind, "Search", field{"Type", "record keys"}, field{"Query", "query"})
			default:
				m.form(kind, "Open", field{"Path", "database.db"})
			}
			for selected := range m.modal.fields {
				m.modal.selected = selected
				before, pos := m.modal.fields[selected].value, m.modal.positions[selected]
				for _, alt := range []bool{false, true} {
					next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}, Alt: alt})
					m = next.(model)
					if cmd != nil || m.modal.fields[selected].value != before || m.modal.positions[selected] != pos {
						t.Fatal("modifier changed field")
					}
				}
			}
		})
	}
}

func TestExplicitlyPastedNULIsPreserved(t *testing.T) {
	m := model{app: domain.NewApp(), width: 80, height: 24}
	m.form("put", "Edit", field{"Key", "key"}, field{"Value", "text:"})
	m.modal.selected = 1
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}, Paste: true})
	m = next.(model)
	if m.modal.fields[1].value != "\x00" {
		t.Fatal("explicit pasted binary byte was removed")
	}
	value, err := m.modal.editorBytes()
	if err != nil || len(value) != 1 || value[0] != 0 {
		t.Fatalf("binary round trip: %v %v", value, err)
	}
}
