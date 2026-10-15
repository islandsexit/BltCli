package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
)

func TestDiffStorePaginationAndCleanup(t *testing.T) {
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
	path := domain.Path{[]byte("root")}
	for _, db := range []*adapters.BoltDBAdapter{a, b} {
		if err := db.CreateBucket(path); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 40; i++ {
		if err := a.Put(path, []byte(fmt.Sprintf("%03d", i)), []byte("different")); err != nil {
			t.Fatal(err)
		}
	}
	for _, db := range []*adapters.BoltDBAdapter{a, b} {
		if err := db.Put(path, []byte("same"), []byte("same")); err != nil {
			t.Fatal(err)
		}
	}
	store, err := BuildDiff(context.Background(), dir, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if store.Count != 40 {
		t.Fatal(store.Count)
	}
	root, err := store.Browse(context.Background(), nil, nil, nil, 0, 7)
	if err != nil || len(root.Buckets) != 1 || string(root.Buckets[0].Name) != "root" || root.Buckets[0].Count != 40 {
		t.Fatalf("hierarchy root: %+v %v", root, err)
	}
	local, err := store.Browse(context.Background(), path, nil, nil, 11, 7)
	if err != nil || len(local.Entries) != 7 || local.Count != 40 || string(local.Entries[0].Key) != "011" {
		t.Fatalf("local page: %+v %v", local, err)
	}
	for _, start := range []int64{0, 11, 39, 40} {
		entries, err := store.Page(context.Background(), start, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != min(7, int(40-start)) {
			t.Fatalf("page at %d: %d", start, len(entries))
		}
		if len(entries) > 0 && string(entries[0].Key) != fmt.Sprintf("%03d", start) {
			t.Fatal("bad index offset")
		}
	}
	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildDiff(ctx, dir, a, b); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "diff-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("cancel leaked temp files: %v %v", matches, err)
	}
}
