package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
)

func TestDiffSearchExactContainsAndPaginationWithoutDatabases(t *testing.T) {
	dir := t.TempDir()
	var dbs []*adapters.BoltDBAdapter
	key := []byte{0, 255, 'k'}
	longKey := bytes.Repeat([]byte{255}, 32768)
	for side := 0; side < 2; side++ {
		db, err := adapters.Open(filepath.Join(dir, fmt.Sprintf("%d.db", side)), true, false)
		if err != nil {
			t.Fatal(err)
		}
		dbs = append(dbs, db)
		for bucket := 0; bucket < 5; bucket++ {
			p := domain.Path{[]byte(fmt.Sprintf("bucket-%d", bucket))}
			if err = db.CreateBucket(p); err != nil {
				t.Fatal(err)
			}
			if err = db.Put(p, key, []byte(fmt.Sprintf("value-%d", side))); err != nil {
				t.Fatal(err)
			}
			if err = db.Put(p, []byte("same"), []byte("equal")); err != nil {
				t.Fatal(err)
			}
			if bucket == 0 {
				if err = db.Put(p, longKey, []byte(fmt.Sprintf("long-side-%d", side))); err != nil {
					t.Fatal(err)
				}
			}
			if err = db.Put(p, []byte(fmt.Sprintf("prefix:%d", bucket)), []byte(fmt.Sprintf("side-%d", side))); err != nil {
				t.Fatal(err)
			}
		}
	}
	store, err := BuildDiff(context.Background(), dir, dbs[0], dbs[1])
	if err != nil {
		t.Fatal(err)
	}
	defer store.Remove()
	for _, db := range dbs {
		if err = db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		key   []byte
		mode  string
		count int
	}{{key, "exact", 5}, {longKey, "exact", 1}, {[]byte("prefix:"), "contains", 5}, {nil, "contains", 11}, {[]byte("same"), "exact", 0}, {[]byte("missing"), "exact", 0}} {
		var after []byte
		count := 0
		seen := map[string]bool{}
		for {
			page, err := store.Search(context.Background(), tc.key, tc.mode, after, 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Entries) > 2 {
				t.Fatal("unbounded search page")
			}
			for _, e := range page.Entries {
				id := fmt.Sprintf("%x/%x", e.Path, e.Key)
				if seen[id] {
					t.Fatal("duplicate pagination result")
				}
				seen[id] = true
				count++
				if tc.mode == "exact" && !bytes.Equal(e.Key, tc.key) {
					t.Fatal("wrong exact key")
				}
			}
			if page.Next == nil {
				break
			}
			after = page.Next
		}
		if count != tc.count {
			t.Fatalf("%s %x: %d != %d", tc.mode, tc.key, count, tc.count)
		}
	}
	for _, mode := range []string{"exact", "contains"} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err = store.Search(ctx, key, mode, nil, 2); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err = store.Search(context.Background(), key, mode, []byte{1}, 2); err == nil {
			t.Fatal("invalid search token accepted")
		}
	}
}

func TestAllDifferentDiffSearch(t *testing.T) {
	store, e, _ := fullDiffFixture(t, []byte("a"), []byte("b"))
	page, err := store.Search(context.Background(), e.Key, "exact", nil, 1)
	if err != nil || len(page.Entries) != 1 {
		t.Fatal("all-different database lost search index", err)
	}
}
