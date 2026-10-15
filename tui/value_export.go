package tui

import (
	"context"
	"errors"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository"
)

// valueJSONFilename bounds the suggested name and excludes filesystem controls.
func valueJSONFilename(path domain.Path, key []byte) string {
	var name strings.Builder
	add := func(part []byte) {
		if name.Len() > 0 && name.Len() < 180 {
			name.WriteByte('_')
		}
		for len(part) > 0 && name.Len() < 180 {
			r, size := utf8.DecodeRune(part)
			part = part[size:]
			if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.') {
				r = '_'
			}
			if name.Len()+utf8.RuneLen(r) > 180 {
				break
			}
			name.WriteRune(r)
		}
	}
	for _, part := range path {
		add(part)
	}
	add(key)
	base := strings.Trim(name.String(), ". ")
	if base == "" {
		base = "value"
	}
	// Windows device names remain reserved even with an extension.
	stem := strings.ToUpper(strings.SplitN(base, ".", 2)[0])
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" ||
		(len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
		base = "_" + base
	}
	return base + ".json"
}

type valueExported struct {
	file string
	err  error
}

func (m *model) exportValueJSON(file string) tea.Cmd {
	_, db, err := m.app.Active()
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	path, key := m.path, m.detailKey
	return m.operation(func(ctx context.Context) tea.Msg {
		if err := ctx.Err(); err != nil {
			return valueExported{err: err}
		}
		f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return valueExported{err: err}
		}
		err = repository.ExportValueJSON(ctx, db, path, key, f)
		err = errors.Join(err, f.Close())
		if err != nil {
			// Only the exclusively created output belongs to this operation.
			err = errors.Join(err, os.Remove(file))
		}
		return valueExported{file: file, err: err}
	})
}
