package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
)

func TestDiffBatchedIndexProgressAndCancellation(t *testing.T) {
	dir := t.TempDir()
	a, err := adapters.Open(filepath.Join(dir, "a.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := adapters.Open(filepath.Join(dir, "b.db"), true, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	path := domain.Path{[]byte{0, 255}, []byte("nested")}
	for side, db := range []*adapters.BoltDBAdapter{a, b} {
		for depth := 1; depth <= len(path); depth++ {
			if err = db.CreateBucket(path[:depth]); err != nil {
				t.Fatal(err)
			}
		}
		rows := make([]domain.Record, 1300)
		value := bytes.Repeat([]byte{byte('a' + side)}, 4096)
		for i := range rows {
			rows[i] = domain.Record{Key: []byte(fmt.Sprintf("%04d", i)), Value: value}
		}
		rows[0].Value = []byte("same")
		if err = db.WriteBatch(context.Background(), path, rows, "overwrite"); err != nil {
			t.Fatal(err)
		}
	}
	var updates []domain.DiffProgress
	store, err := BuildDiffWithProgress(context.Background(), dir, a, b, func(p domain.DiffProgress) { updates = append(updates, p) })
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) < 4 || updates[0].Phase != "compare" || updates[len(updates)-1].Phase != "done" {
		t.Fatalf("progress: %+v", updates)
	}
	var indexed int64
	for _, p := range updates {
		if p.Phase == "index" {
			if p.Indexed < indexed || p.Indexed > p.IndexTotal {
				t.Fatal("invalid progress")
			}
			indexed = p.Indexed
		}
	}
	if indexed != store.Count {
		t.Fatal("index total mismatch")
	}
	for _, start := range []int64{0, 510, 511, 512, 1023, 1298} {
		flat, err := store.Page(context.Background(), start, 7)
		if err != nil {
			t.Fatal(err)
		}
		page, err := store.Browse(context.Background(), path, nil, nil, start, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(flat) != len(page.Entries) {
			t.Fatal("local page length mismatch")
		}
		for i, e := range page.Entries {
			if !bytes.Equal(e.Key, flat[i].Key) || !bytes.Equal(e.Base, flat[i].Base) || !bytes.Equal(e.Other, flat[i].Other) {
				t.Fatal("offset index mismatch")
			}
		}
	}
	if err = store.Remove(); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"compare", "index"} {
		ctx, cancel := context.WithCancel(context.Background())
		_, err = BuildDiffWithProgress(ctx, dir, a, b, func(p domain.DiffProgress) {
			if p.Phase == phase {
				cancel()
			}
		})
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel %s: %v", phase, err)
		}
		matches, err := filepath.Glob(filepath.Join(dir, "diff-*"))
		if err != nil || len(matches) != 0 {
			t.Fatalf("leaked files: %v %v", matches, err)
		}
	}
	if _, err = os.Stat(filepath.Join(dir, "a.db")); err != nil {
		t.Fatal(err)
	}
}
