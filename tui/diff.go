package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository"
	"github.com/islandsexit/BltCli/repository/adapters"
	"github.com/islandsexit/BltCli/utils"
)

type diffBuilt struct {
	baseFile, otherFile string
	store               *repository.DiffStore
	entries             []domain.DiffEntry
	err                 error
}
type diffPage struct {
	entries []domain.DiffEntry
	start   int64
	last    bool
	err     error
}
type diffRequested struct{}

func (m model) diffPageSize() int { return max(1, min(20, (m.height-13-m.statusExtraRows())/4)) }

func (m model) diffHandle(path string) (*adapters.BoltDBAdapter, bool, error) {
	requested, err := os.Stat(path)
	if err != nil {
		if handle, ok := m.app.Lookup(path); ok {
			if db, ok := handle.(*adapters.BoltDBAdapter); ok {
				return db, false, nil
			}
		}
		return nil, false, err
	}
	for _, name := range m.app.Names() {
		handle, ok := m.app.Lookup(name)
		if !ok {
			continue
		}
		info, err := handle.Info()
		if err != nil {
			return nil, false, err
		}
		st, err := os.Stat(info.Path)
		if err == nil && os.SameFile(requested, st) {
			if db, ok := handle.(*adapters.BoltDBAdapter); ok {
				return db, false, nil
			}
			return nil, false, errors.New("database adapter does not support diff")
		}
	}
	db, err := adapters.Open(path, false, true)
	return db, true, err
}

func (m model) diffCommand(ctx context.Context, base, other string) tea.Cmd {
	return func() (msg tea.Msg) {
		result := diffBuilt{}
		if err := ctx.Err(); err != nil {
			return diffBuilt{err: err}
		}
		a, owned, err := m.diffHandle(base)
		if err != nil {
			return diffBuilt{err: fmt.Errorf("base database: %w", err)}
		}
		defer func() {
			if owned {
				result.err = errors.Join(result.err, a.Close())
			}
			if result.err != nil && result.store != nil {
				result.err = errors.Join(result.err, result.store.Remove())
				result.store = nil
			}
			msg = result
		}()
		var b *adapters.BoltDBAdapter
		bOwned := false
		info, infoErr := a.Info()
		st, statErr := os.Stat(other)
		baseStat, baseStatErr := os.Stat(info.Path)
		if infoErr == nil && statErr == nil && baseStatErr == nil && os.SameFile(st, baseStat) {
			b = a
		} else {
			b, bOwned, err = m.diffHandle(other)
		}
		if err != nil {
			result.err = fmt.Errorf("other database: %w", err)
			return result
		}
		defer func() {
			if bOwned {
				result.err = errors.Join(result.err, b.Close())
			}
		}()
		baseInfo, baseErr := a.Info()
		otherInfo, otherErr := b.Info()
		if baseErr != nil || otherErr != nil {
			result.err = errors.Join(baseErr, otherErr)
			return result
		}
		result.baseFile, result.otherFile = baseInfo.Path, otherInfo.Path
		result.store, result.err = repository.BuildDiff(ctx, m.diffDir, a, b)
		if result.err == nil && !result.store.Summary.Equal() && !result.store.Summary.CompletelyDifferent() {
			result.entries, result.err = result.store.Page(ctx, 0, m.diffPageSize())
		}
		return result
	}
}

func (m *model) startDiff() tea.Cmd {
	return m.operation(func(ctx context.Context) tea.Msg {
		return m.diffCommand(ctx, m.diffBase, m.diffOther)()
	})
}

func (m *model) diffLoad(start int64, last bool) tea.Cmd {
	store, limit := m.diffStore, m.diffPageSize()
	return m.operation(func(ctx context.Context) tea.Msg {
		entries, err := store.Page(ctx, start, limit)
		return diffPage{entries: entries, start: start, last: last, err: err}
	})
}

func (m model) updateDiff(key string) (tea.Model, tea.Cmd) {
	if m.diffStore == nil {
		return m, nil
	}
	if key == "esc" || key == "q" {
		store := m.diffStore
		m.diffOpen = false
		m.diffStore = nil
		m.diffEntries = nil
		return m, m.operation(func(context.Context) tea.Msg { err := store.Remove(); return diffClosed{err: err} })
	}
	if m.diffStore.Summary.Equal() || m.diffStore.Summary.CompletelyDifferent() {
		return m, nil
	}
	pageSize := int64(m.diffPageSize())
	switch key {
	case "down", "j":
		if m.diffSelected+1 < len(m.diffEntries) {
			m.diffSelected++
		} else if m.diffStart+int64(len(m.diffEntries)) < m.diffStore.Count {
			return m, m.diffLoad(m.diffStart+int64(len(m.diffEntries)), false)
		}
	case "up", "k":
		if m.diffSelected > 0 {
			m.diffSelected--
		} else if m.diffStart > 0 {
			return m, m.diffLoad(max(0, m.diffStart-pageSize), true)
		}
	case "n", "pgdown":
		if m.diffStart+int64(len(m.diffEntries)) < m.diffStore.Count {
			return m, m.diffLoad(m.diffStart+int64(len(m.diffEntries)), false)
		}
	case "p", "pgup":
		if m.diffStart > 0 {
			return m, m.diffLoad(max(0, m.diffStart-pageSize), false)
		}
	case "home":
		return m, m.diffLoad(0, false)
	case "end":
		return m, m.diffLoad(max(0, ((m.diffStore.Count-1)/pageSize)*pageSize), true)
	case "enter":
		if len(m.diffEntries) > 0 {
			e := m.diffEntries[m.diffSelected]
			m.showDetail(fmt.Sprintf("%s\nBucket: %s\nKey: %s\n\nBASE (green):\nFile: %s\n%s\n\nOTHER (red):\nFile: %s\n%s", e.Kind, pathLabel(e.Path), utils.Display(e.Key, len(e.Key)), utils.SafeText(m.diffBaseFile), diffValue(e, true), utils.SafeText(m.diffOtherFile), diffValue(e, false)))
			m.diffDetail = true
		}
	}
	return m, nil
}

type diffClosed struct{ err error }

func diffValue(e domain.DiffEntry, base bool) string {
	present, value, size, sequence := e.OtherPresent, e.Other, e.OtherBytes, e.OtherSequence
	if base {
		present, value, size, sequence = e.BasePresent, e.Base, e.BaseBytes, e.BaseSequence
	}
	if !present {
		return "(absent)"
	}
	if e.Kind == "bucket" {
		return fmt.Sprintf("bucket · sequence %d", sequence)
	}
	if e.Kind == "type" && (base && e.BaseBucket || !base && e.OtherBucket) {
		return "bucket"
	}
	text := utils.ValuePreview(value, 4096)
	if e.ValueOffset > 0 {
		text = "… (earlier bytes omitted)\n" + text
	}
	if size > e.ValueOffset+len(value) {
		text += "\n… (preview truncated)"
	}
	return fmt.Sprintf("%d bytes · preview offset %d\n%s", size, e.ValueOffset, text)
}

func diffColors() (lipgloss.Style, lipgloss.Style) {
	return lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#237A4B", Dark: "#A7DDB5"}), lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B45360", Dark: "#F2A6AD"})
}

func (m model) diffView(title lipgloss.Style) string {
	width := max(1, m.width-4)
	green, red := diffColors()
	var out strings.Builder
	out.WriteString(title.Render("DATABASE DIFF") + "\n")
	out.WriteString(green.Render(clip("BASE · "+m.diffBase, width)) + "\n")
	out.WriteString(red.Render(clip("OTHER · "+m.diffOther, width)) + "\n")
	if m.diffStore == nil {
		return out.String()
	}
	summary := m.diffStore.Summary
	if summary.Equal() {
		out.WriteString("\nDatabases are equal.\n")
		return out.String()
	}
	if summary.CompletelyDifferent() {
		out.WriteString("\nDatabases are completely different.\n")
		if summary.Records == 0 {
			out.WriteString("No identical buckets.\n")
		} else {
			out.WriteString("No identical records at matching paths.\n")
		}
		return out.String()
	}
	top := max(0, m.diffSelected-m.diffPageSize()+1)
	for i := top; i < min(len(m.diffEntries), top+m.diffPageSize()); i++ {
		e := m.diffEntries[i]
		marker := "  "
		if i == m.diffSelected {
			marker = "› "
		}
		label := pathLabel(e.Path)
		if e.Key != nil {
			label += " / " + utils.Display(e.Key, 256)
		}
		heading := lipgloss.NewStyle().Bold(i == m.diffSelected)
		out.WriteString(heading.Render(clip(marker+e.Kind+" · "+label, width)) + "\n")
		out.WriteString(green.Render(clip("  BASE  · "+strings.ReplaceAll(diffValue(e, true), "\n", " "), width)) + "\n")
		out.WriteString(red.Render(clip("  OTHER · "+strings.ReplaceAll(diffValue(e, false), "\n", " "), width)) + "\n")
		if m.height >= 15+m.statusExtraRows() {
			out.WriteByte('\n')
		}
	}
	out.WriteString(fmt.Sprintf("Differences %d–%d of %d", min(m.diffStart+1, m.diffStore.Count), m.diffStart+int64(len(m.diffEntries)), m.diffStore.Count))
	return out.String()
}
