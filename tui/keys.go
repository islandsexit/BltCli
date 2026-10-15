package tui

import tea "github.com/charmbracelet/bubbletea"

// commandKey maps command shortcuts by their QWERTY/ЙЦУКЕН positions.
// KeyMsg exposes characters rather than physical scan codes in terminal input.
// Keep the original message for text fields, paste, Alt and multi-rune input.
func commandKey(key tea.KeyMsg) string {
	if key.Type != tea.KeyRunes || key.Alt || key.Paste || len(key.Runes) != 1 {
		return key.String()
	}
	// Include Ukrainian and Belarusian variants on the same letter positions.
	const cyrillic = "ёйцукенгшщзхъфывапролджэячсмитьбю.ЁЙЦУКЕНГШЩЗХЪФЫВАПРОЛДЖЭЯЧСМИТЬБЮ,іїєґўІЇЄҐЎ"
	const qwerty = "`qwertyuiop[]asdfghjkl;'zxcvbnm,./~QWERTYUIOP{}ASDFGHJKL:\"ZXCVBNM<>?s]'\\oS}\"|O"
	index := 0
	for _, r := range cyrillic {
		if r == key.Runes[0] {
			return qwerty[index : index+1]
		}
		index++
	}
	return key.String()
}
