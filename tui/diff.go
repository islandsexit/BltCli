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
	browse              repository.DiffBrowsePage
	err                 error
}
type diffPage struct {
	entries []domain.DiffEntry
	browse  repository.DiffBrowsePage
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
		var progress func(domain.DiffProgress)
		if m.diffProgress != nil {
			progress = m.diffProgress.set
		}
		result.store, result.err = repository.BuildDiffWithProgress(ctx, m.diffDir, a, b, progress)
		if result.err == nil && !result.store.Summary.Equal() && !result.store.Summary.CompletelyDifferent() {
			result.browse, result.err = result.store.Browse(ctx, nil, nil, nil, 0, m.diffPageSize())
			result.entries = result.browse.Entries
		}
		return result
	}
}

func (m *model) startDiff() tea.Cmd {
	m.diffProgress = &diffProgressState{}
	m.diffRunning = true
	return m.operation(func(ctx context.Context) tea.Msg {
		return m.diffCommand(ctx, m.diffBase, m.diffOther)()
	})
}

func (m *model) diffLoad(start int64, last bool) tea.Cmd {
	store, limit := m.diffStore, m.diffPageSize()
	return m.operation(func(ctx context.Context) tea.Msg {
		page, err := store.Browse(ctx, m.diffPath, m.diffAfter, nil, start, limit)
		return diffPage{entries: page.Entries, browse: page, start: start, last: last, err: err}
	})
}

func (m model) updateDiff(key string) (tea.Model, tea.Cmd) {
	if m.diffStore == nil {
		return m, nil
	}
	if (key == "esc" || key == "left" || key == "backspace") && len(m.diffPath) > 0 {
		m.diffPath = m.diffPath[:len(m.diffPath)-1]
		m.diffAfter = nil
		m.diffHistory = nil
		return m, m.diffLoad(0, false)
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
	total := len(m.diffEntries) + len(m.diffBrowse.Buckets)
	next := func() (tea.Model, tea.Cmd) {
		if len(m.diffHistory) == 128 {
			copy(m.diffHistory, m.diffHistory[1:])
			m.diffHistory[127] = nil
			m.diffHistory = m.diffHistory[:127]
		}
		m.diffHistory = append(m.diffHistory, m.diffAfter)
		if len(m.diffBrowse.Buckets) > 0 {
			m.diffAfter = m.diffBrowse.Buckets[len(m.diffBrowse.Buckets)-1].Name
		}
		return m, m.diffLoad(m.diffStart+pageSize, false)
	}
	previous := func(last bool) (tea.Model, tea.Cmd) {
		if len(m.diffHistory) == 0 {
			return m, nil
		}
		if len(m.diffHistory) > 0 {
			m.diffAfter = m.diffHistory[len(m.diffHistory)-1]
			m.diffHistory = m.diffHistory[:len(m.diffHistory)-1]
		}
		return m, m.diffLoad(max(0, m.diffStart-pageSize), last)
	}
	switch key {
	case "down", "j":
		if m.diffSelected+1 < total {
			m.diffSelected++
		} else if m.diffBrowse.Next != nil || m.diffStart+int64(len(m.diffEntries)) < m.diffBrowse.Count {
			return next()
		}
	case "up", "k":
		if m.diffSelected > 0 {
			m.diffSelected--
		} else if m.diffStart > 0 {
			return previous(true)
		}
	case "n", "pgdown":
		if m.diffBrowse.Next != nil || m.diffStart+int64(len(m.diffEntries)) < m.diffBrowse.Count {
			return next()
		}
	case "p", "pgup":
		if m.diffStart > 0 {
			return previous(false)
		}
	case "home":
		m.diffAfter = nil
		m.diffHistory = nil
		return m, m.diffLoad(0, false)
	case "enter", "right":
		if m.diffSelected < len(m.diffBrowse.Buckets) {
			m.diffPath = append(append(domain.Path(nil), m.diffPath...), m.diffBrowse.Buckets[m.diffSelected].Name)
			m.diffAfter = nil
			m.diffHistory = nil
			return m, m.diffLoad(0, false)
		}
		if m.diffSelected < total {
			e := m.diffEntries[m.diffSelected-len(m.diffBrowse.Buckets)]
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
	out.WriteString(title.Render(clip("DIFF · "+pathLabel(m.diffPath), width)) + "\n")
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
	for i := top; i < min(len(m.diffEntries)+len(m.diffBrowse.Buckets), top+m.diffPageSize()); i++ {
		if i < len(m.diffBrowse.Buckets) {
			b := m.diffBrowse.Buckets[i]
			marker := "  "
			if i == m.diffSelected {
				marker = "> "
			}
			out.WriteString(clip(fmt.Sprintf("%s[+] %s (%d changes)", marker, utils.Display(b.Name, 256), b.Count), width) + "\n\n\n\n")
			continue
		}
		e := m.diffEntries[i-len(m.diffBrowse.Buckets)]
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
