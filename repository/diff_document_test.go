package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
)

func fullDiffFixture(t testing.TB, base, other []byte) (*DiffStore, domain.DiffEntry, string) {
	t.Helper()
	dir := t.TempDir()
	a, err := adapters.Open(filepath.Join(dir, "a.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := adapters.Open(filepath.Join(dir, "b.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	p := domain.Path{[]byte("root")}
	for i, db := range []*adapters.BoltDBAdapter{a, b} {
		if err = db.CreateBucket(p); err != nil {
			t.Fatal(err)
		}
		value := base
		if i == 1 {
			value = other
		}
		if err = db.Put(p, []byte("key"), value); err != nil {
			t.Fatal(err)
		}
	}
	store, err := BuildDiff(context.Background(), dir, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if err = errors.Join(a.Close(), b.Close()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Remove(); err != nil {
			t.Error(err)
		}
	})
	entries, err := store.Page(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if string(e.Key) == "key" {
			return store, e, dir
		}
	}
	t.Fatal("record difference missing")
	return nil, domain.DiffEntry{}, dir
}

func TestFullDiffDocumentContextAlignmentAndSnapshot(t *testing.T) {
	base := []byte(`{"unchanged":1,"items":["first","last"],"tail":"BASE END"}`)
	other := []byte(`{"unchanged":1,"items":["first","inserted","last"],"tail":"OTHER END"}`)
	store, e, dir := fullDiffFixture(t, base, other)
	doc, err := store.BuildDiffDocument(context.Background(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Remove()
	rows, err := doc.Page(context.Background(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var unchanged, inserted, replaced bool
	for _, r := range rows {
		if strings.Contains(r.Base, "unchanged") && r.Kind == "equal" {
			unchanged = true
		}
		if strings.Contains(r.Other, "inserted") && r.BaseLine == 0 && r.Kind == "insert" {
			inserted = true
		}
		if strings.Contains(r.Base, "BASE END") && strings.Contains(r.Other, "OTHER END") && r.Kind == "replace" {
			replaced = true
		}
	}
	if !unchanged || !inserted || !replaced {
		t.Fatalf("alignment: %+v", rows)
	}
	if doc.Changes < 1 {
		t.Fatal("missing change blocks")
	}
	change, err := doc.Change(context.Background(), -1, true)
	if err != nil || change < 0 || change >= doc.Count {
		t.Fatal(change, err)
	}
	end, err := doc.Page(context.Background(), doc.Count-1, 1)
	if err != nil || len(end) != 1 {
		t.Fatal("tail page", err)
	}
}

func TestFullDiffDocumentBeyondPreviewAndWhitespace(t *testing.T) {
	for _, tc := range []struct {
		name        string
		base, other []byte
		raw         bool
	}{
		{"long value", []byte("START\n" + strings.Repeat("unchanged\n", 10000) + "BASE END"), []byte("START\n" + strings.Repeat("unchanged\n", 10000) + "OTHER END"), false},
		{"JSON whitespace", []byte(`{"a":1}`), []byte("{\n \"a\": 1\n}"), true},
		{"line endings", []byte("first\r\nlast"), []byte("first\nlast"), false},
		{"binary", []byte{0, 255, 27, 10}, []byte{0, 254, 27, 10}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, e, dir := fullDiffFixture(t, tc.base, tc.other)
			doc, err := store.BuildDiffDocument(context.Background(), dir, e)
			if err != nil {
				t.Fatal(err)
			}
			defer doc.Remove()
			if doc.Changes == 0 {
				t.Fatal("recorded difference disappeared")
			}
			if tc.raw && doc.Formatted {
				t.Fatal("whitespace-only change was hidden")
			}
			if tc.name == "long value" {
				rows, err := doc.Page(context.Background(), 0, 1)
				if err != nil || rows[0].Base != "START" {
					t.Fatal("full initial context missing")
				}
				tail, err := doc.Page(context.Background(), doc.Count-1, 1)
				if err != nil || tail[0].Base != "BASE END" || tail[0].Other != "OTHER END" {
					t.Fatal("full ending missing")
				}
			}
			if tc.name == "binary" {
				rows, err := doc.Page(context.Background(), 0, 1)
				if err != nil || !strings.Contains(rows[0].Base, "00 ff 1b 0a") {
					t.Fatal("binary bytes lost")
				}
			}
		})
	}
}

func TestDocumentSegmentsAndEmptySides(t *testing.T) {
	for _, text := range []string{"", strings.Repeat("x", 4096) + "\nEND", strings.Repeat("界", 5000) + "\nEND", "a\n\nEND", strings.Repeat("x", 5000) + "\xff\nEND"} {
		lines, err := newDocumentLines(strings.NewReader(text), true)
		if err != nil {
			t.Fatal(err)
		}
		lines.json = false
		var rows []documentLine
		for {
			line, err := lines.next(context.Background())
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(line.text) > 20<<10 {
				t.Fatal("unbounded segment")
			}
			rows = append(rows, line)
		}
		if text != "" && rows[len(rows)-1].text != "END" {
			t.Fatal("tail missing")
		}
		if strings.HasPrefix(text, strings.Repeat("x", 4096)+"\n") && len(rows) != 2 {
			t.Fatal("extra empty segment")
		}
	}
	store, e, dir := fullDiffFixture(t, []byte(""), []byte("value"))
	doc, err := store.BuildDiffDocument(context.Background(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Remove()
	rows, err := doc.Page(context.Background(), 0, 10)
	if err != nil || rows[0].BaseLine != 1 || rows[0].Base != "" {
		t.Fatal("empty value treated as absent")
	}
	e.BasePresent = false
	missing, err := store.BuildDiffDocument(context.Background(), dir, e)
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Remove()
	rows, err = missing.Page(context.Background(), 0, 10)
	if err != nil || rows[0].BaseLine != 0 || rows[0].Kind != "insert" {
		t.Fatal("absent side treated as empty")
	}
}

type cancelDocumentContext struct {
	context.Context
	checks, limit int
}

func (c *cancelDocumentContext) Err() error {
	c.checks++
	if c.checks > c.limit {
		return context.Canceled
	}
	return nil
}

func TestDocumentCancellationCleanup(t *testing.T) {
	store, e, dir := fullDiffFixture(t, bytes.Repeat([]byte("a\n"), 10000), bytes.Repeat([]byte("b\n"), 10000))
	ctx := &cancelDocumentContext{Context: context.Background(), limit: 400}
	_, err := store.BuildDiffDocument(ctx, dir, e)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "diff-document-*"))
	if err != nil || len(matches) != 0 {
		t.Fatal("document files leaked", matches, err)
	}
}

func TestDocumentWhitespaceCancellation(t *testing.T) {
	source, err := newDocumentLines(strings.NewReader("{"+strings.Repeat(" ", 1<<20)+"}"), true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &cancelDocumentContext{Context: context.Background(), limit: 3}
	if _, err = source.next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("long whitespace run ignored cancellation", err)
	}
}

func BenchmarkFullDiffDocument(b *testing.B) {
	for _, count := range []int{1000, 100000} {
		b.Run(fmt.Sprintf("lines=%d", count), func(b *testing.B) {
			base := []byte(strings.Repeat("same line\n", count) + "base end")
			other := []byte(strings.Repeat("same line\n", count) + "other end")
			store, e, dir := fullDiffFixture(b, base, other)
			b.ReportAllocs()
			b.SetBytes(int64(len(base) + len(other)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				doc, err := store.BuildDiffDocument(context.Background(), dir, e)
				if err != nil {
					b.Fatal(err)
				}
				if err = doc.Remove(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func FuzzDocumentLines(f *testing.F) {
	for _, s := range []string{`{"key":"界","n":1.00e99,"b":true}`, `[{},[],"a\\b\"c"]`, "line\r\nnext\n", string([]byte{0, 255, 1}), strings.Repeat("x", 4096)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			return
		}
		source, err := newDocumentLines(strings.NewReader(s), true)
		if err != nil {
			t.Fatal(err)
		}
		var reconstructed strings.Builder
		var previous int64
		for {
			row, err := source.next(context.Background())
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(row.text) > 20<<10 {
				t.Fatal("unbounded row")
			}
			if strings.Contains(row.text, "\n") {
				t.Fatal("embedded row separator")
			}
			if previous != 0 && previous != row.number {
				reconstructed.WriteByte('\n')
			}
			previous = row.number
			reconstructed.WriteString(row.text)
		}
		if !source.hex && source.json && json.Valid([]byte(s)) {
			var before, after bytes.Buffer
			if err = json.Compact(&before, []byte(s)); err != nil {
				t.Fatal(err)
			}
			if err = json.Compact(&after, []byte(reconstructed.String())); err != nil {
				t.Fatalf("invalid display JSON: %v", err)
			}
			if !bytes.Equal(before.Bytes(), after.Bytes()) {
				t.Fatal("format changed JSON tokens")
			}
		}
	})
}
