package adapters_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/islandsexit/BltCli/domain"
)

func TestDatabaseSearchNestedPaginationAndOwnership(t *testing.T) {
	db, _ := database(t)
	ctx := context.Background()
	paths := []domain.Path{{[]byte("a")}, {[]byte("a"), []byte("nested")}, {[]byte("b")}}
	for _, p := range paths {
		if e := db.CreateBucket(p); e != nil {
			t.Fatal(e)
		}
		if e := db.Put(p, []byte("needle:shared"), []byte("red")); e != nil {
			t.Fatal(e)
		}
	}
	binaryPath := domain.Path{[]byte("a"), []byte("nested"), {0, 255}}
	if e := db.CreateBucket(binaryPath); e != nil {
		t.Fatal(e)
	}
	if e := db.Put(binaryPath, []byte{255, 0}, []byte{0, 255, 128}); e != nil {
		t.Fatal(e)
	}
	q := domain.SearchQuery{Kind: "records", Contains: []byte("needle"), Limit: 1}
	for i, want := range paths {
		page, e := db.Search(ctx, q)
		if e != nil || len(page.Hits) != 1 {
			t.Fatalf("page %d: %+v %v", i, page, e)
		}
		hit := page.Hits[0]
		if len(hit.Path) != len(want) {
			t.Fatalf("wrong path: %q", hit.Path)
		}
		for j := range want {
			if !bytes.Equal(hit.Path[j], want[j]) {
				t.Fatalf("wrong path: %q", hit.Path)
			}
		}
		if i < 2 && page.Next == nil || i == 2 && page.Next != nil {
			t.Fatal("incorrect continuation")
		}
		q.After = page.Next
	}
	page, e := db.Search(ctx, domain.SearchQuery{Kind: "buckets", Contains: []byte("nested"), Limit: 10})
	if e != nil || len(page.Hits) != 1 || page.Hits[0].Key != nil {
		t.Fatalf("bucket search: %+v %v", page, e)
	}
	page.Hits[0].Path[0][0] = 'z'
	page, e = db.Search(ctx, domain.SearchQuery{Kind: "records", ValueContains: []byte{255, 128}, Limit: 10})
	if e != nil || len(page.Hits) != 1 || !bytes.Equal(page.Hits[0].Path[2], []byte{0, 255}) || !bytes.Equal(page.Hits[0].Key, []byte{255, 0}) {
		t.Fatalf("binary global search: %+v %v", page, e)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = db.Search(cancelled, domain.SearchQuery{Kind: "records", Limit: 10}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestDatabaseSearchDeletedCheckpoint(t *testing.T) {
	db, _ := database(t)
	for _, name := range []string{"a", "b"} {
		p := domain.Path{[]byte(name)}
		if e := db.CreateBucket(p); e != nil {
			t.Fatal(e)
		}
		if e := db.Put(p, []byte("target"), []byte("v")); e != nil {
			t.Fatal(e)
		}
	}
	q := domain.SearchQuery{Kind: "records", Contains: []byte("target"), Limit: 1}
	page, e := db.Search(context.Background(), q)
	if e != nil || page.Next == nil {
		t.Fatal(e)
	}
	q.After = page.Next
	if e = db.DeleteBucket(domain.Path{[]byte("a")}); e != nil {
		t.Fatal(e)
	}
	page, e = db.Search(context.Background(), q)
	if e != nil || len(page.Hits) != 1 || string(page.Hits[0].Path[0]) != "b" {
		t.Fatalf("deleted checkpoint: %+v %v", page, e)
	}
}

func BenchmarkDatabaseSearch(b *testing.B) {
	db, _ := benchmarkDB(b, 10000)
	for name, q := range map[string]domain.SearchQuery{"keys": {Kind: "records", Contains: []byte("user:"), Limit: 100}, "no_match": {Kind: "records", Contains: []byte("absent"), Limit: 100}} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, e := db.Search(context.Background(), q); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
