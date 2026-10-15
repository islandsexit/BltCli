package tui

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/islandsexit/BltCli/domain"
)

// diffProgressState bridges the worker and event loop with one bounded snapshot.
type diffProgressState struct {
	mu    sync.Mutex
	value domain.DiffProgress
}

func (s *diffProgressState) set(p domain.DiffProgress) { s.mu.Lock(); s.value = p; s.mu.Unlock() }
func (s *diffProgressState) snapshot() domain.DiffProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value
}

func progressFraction(p domain.DiffProgress) (fraction float64, known bool) {
	switch p.Phase {
	case "done":
		return 1, true
	case "index":
		if p.IndexTotal > 0 {
			return min(.999, float64(p.Indexed)/float64(p.IndexTotal)), true
		}
	case "compare":
		if p.EstimatedBytes > 0 {
			return min(.99, float64(p.ProcessedBytes)/float64(p.EstimatedBytes)), true
		}
	}
	return 0, false
}

func progressETA(p domain.DiffProgress) string {
	fraction, known := progressFraction(p)
	if !known || fraction <= 0 || p.PhaseElapsed < time.Second {
		return "calculating…"
	}
	seconds := p.PhaseElapsed.Seconds() * (1 - fraction) / fraction
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > 365*24*3600 {
		return "calculating…"
	}
	return "~" + (time.Duration(max(1, seconds)) * time.Second).String()
}

func (m model) diffProgressView(title lipgloss.Style) string {
	width := max(1, m.width-4)
	p := m.diffProgress.snapshot()
	fraction, known := progressFraction(p)
	phase := "Opening databases"
	if p.Phase == "compare" {
		phase = "1/2 · Comparing contents (estimated)"
	}
	if p.Phase == "index" {
		phase = "2/2 · Building bucket navigation"
	}
	label := "…"
	if known {
		label = fmt.Sprintf("~%.1f%%", fraction*100)
		if p.Phase == "index" {
			label = fmt.Sprintf("%.1f%%", fraction*100)
		}
	}
	barWidth := max(1, min(50, width-12))
	filled := int(fraction * float64(barWidth))
	bar := "[" + strings.Repeat("━", filled) + strings.Repeat("─", barWidth-filled) + "] " + label
	lines := []string{
		title.Render("DATABASE COMPARISON"),
		"BASE: " + m.diffBase,
		"OTHER: " + m.diffOther,
		phase,
		title.Render(bar),
		fmt.Sprintf("Checked: %d · Differences: %d", p.Compared, p.Differences),
	}
	if p.Phase == "index" {
		lines = append(lines, fmt.Sprintf("Indexed: %d / %d", p.Indexed, p.IndexTotal))
	} else {
		lines = append(lines, fmt.Sprintf("Processed: %.1f MiB", float64(p.ProcessedBytes)/(1<<20)))
	}
	lines = append(lines, "Elapsed: "+p.Elapsed.Round(time.Second).String()+" · Phase remaining: "+progressETA(p))
	lines = append(lines, "Compare ETA is approximate; indexing follows.")
	height := max(1, m.height-8-m.statusExtraRows())
	for i := range lines {
		lines[i] = lipgloss.NewStyle().MaxWidth(width).Render(lines[i])
	}
	return strings.Join(lines[:min(height, len(lines))], "\n")
}
