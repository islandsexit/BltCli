package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/islandsexit/BltCli/domain"
)

func TestDiffProgressViewsAndETA(t *testing.T) {
	p := domain.DiffProgress{Phase: "index", Indexed: 50, IndexTotal: 100, PhaseElapsed: 10 * time.Second, Elapsed: 20 * time.Second}
	if eta := progressETA(p); eta != "~10s" {
		t.Fatal(eta)
	}
	if fraction, known := progressFraction(domain.DiffProgress{Phase: "compare", ProcessedBytes: 200, EstimatedBytes: 100}); !known || fraction >= 1 {
		t.Fatal("estimated progress completed prematurely")
	}
	for _, size := range [][2]int{{45, 14}, {60, 16}, {80, 24}} {
		for _, phase := range []string{"", "compare", "index"} {
			state := &diffProgressState{}
			p.Phase = phase
			state.set(p)
			m := model{ctx: context.Background(), app: domain.NewApp(), width: size[0], height: size[1], busy: true, diffRunning: true, diffProgress: state, diffBase: "a", diffOther: "b"}
			view := ansi.Strip(m.View())
			if !strings.Contains(view, "DATABASE COMPARISON") || lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
				t.Fatalf("progress overflow %v: %s", size, view)
			}
		}
	}
}
