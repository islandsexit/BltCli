package tui

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/utils"
)

// detailJSONRange identifies trusted value previews, excluding their headers.
type detailJSONRange struct{ start, end int }

func previewJSONRange(value []byte, preview string, start, limit int) []detailJSONRange {
	// Never scan an entire large value just to choose its preview's colors.
	if len(value) > limit || !utf8.Valid(value) || !json.Valid(value) {
		return nil
	}
	end := start + len(strings.TrimSuffix(preview, "\n… (preview truncated)"))
	return []detailJSONRange{{start, end}}
}

func recordDetailText(header string, value []byte) (string, []detailJSONRange) {
	preview := utils.ValuePreview(value, 64<<10)
	return header + preview, previewJSONRange(value, preview, len(header), 64<<10)
}

func (m *model) showRecordDetail(header string, value []byte) {
	text, ranges := recordDetailText(header, value)
	m.showHighlightedDetail(text, ranges)
	m.detailIsRecord = true
}

func (m *model) showHighlightedDetail(text string, ranges []detailJSONRange) {
	m.detail = text
	m.detailJSON = ranges
	m.detailIsRecord = false
	m.detailKey = nil
	m.detailScroll = 0
	m.wrapDetail()
}

func safeDetailText(s string) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// wrapHighlightedDetail caches styled lines at preview/resize time. Scrolling
// renders only visible cached lines; ANSI from stored values is never trusted.
func wrapHighlightedDetail(text string, ranges []detailJSONRange, width int) []string {
	if len(ranges) == 0 {
		return nil
	}
	var out strings.Builder
	start := 0
	for _, part := range ranges {
		if part.start < start || part.end < part.start || part.end > len(text) {
			return nil
		}
		out.WriteString(safeDetailText(text[start:part.start]))
		out.WriteString(highlightJSON(safeDetailText(text[part.start:part.end])))
		start = part.end
	}
	out.WriteString(safeDetailText(text[start:]))
	return strings.Split(ansi.Hardwrap(out.String(), max(1, width), true), "\n")
}

func (m *model) showDiffDetail(e domain.DiffEntry) {
	header := e.Kind + "\nBucket: " + pathLabel(e.Path) + "\nKey: " + utils.Display(e.Key, len(e.Key)) + "\n\nBASE (red):\nFile: " + utils.SafeText(m.diffBaseFile) + "\n"
	base, other := diffValue(e, true), diffValue(e, false)
	middle := "\n\nOTHER (green):\nFile: " + utils.SafeText(m.diffOtherFile) + "\n"
	var ranges []detailJSONRange
	if e.Kind == "record" || e.Kind == "type" {
		for _, side := range []struct {
			value           []byte
			preview         string
			offset, size    int
			present, bucket bool
		}{
			{e.Base, base, len(header), e.BaseBytes, e.BasePresent, e.BaseBucket},
			{e.Other, other, len(header) + len(base) + len(middle), e.OtherBytes, e.OtherPresent, e.OtherBucket},
		} {
			if !side.present || side.bucket || e.ValueOffset != 0 || side.size != len(side.value) {
				continue
			}
			if i := strings.IndexByte(side.preview, '\n'); i >= 0 {
				ranges = append(ranges, previewJSONRange(side.value, side.preview[i+1:], side.offset+i+1, 4096)...)
			}
		}
	}
	m.showHighlightedDetail(header+base+middle+other, ranges)
	m.diffDetail = true
}
