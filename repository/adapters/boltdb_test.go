package adapters_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func database(t testing.TB) (*adapters.BoltDBAdapter, domain.Path) {
	t.Helper()
	db, e := adapters.Open(filepath.Join(t.TempDir(), "test.db"), true, false)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	p := domain.Path{[]byte("users")}
	if e = db.CreateBucket(p); e != nil {
		t.Fatal(e)
	}
	return db, p
}
func TestCRUDAndBinaryOwnership(t *testing.T) {
	db, p := database(t)
	key := []byte{0, 255, 1}
	value := []byte{255, 0, 128}
	if e := db.Update(p, key, value); !errors.Is(e, domain.ErrKey) {
		t.Fatalf("update created missing key: %v", e)
	}
	if e := db.Put(p, key, value); e != nil {
		t.Fatal(e)
	}
	if e := db.Update(p, key, value); e != nil {
		t.Fatal(e)
	}
	got, e := db.Get(p, key)
	if e != nil || !bytes.Equal(got, value) {
		t.Fatalf("get: %x %v", got, e)
	}
	got[0] = 7
	got, e = db.Get(p, key)
	if e != nil || !bytes.Equal(got, value) {
		t.Fatal("returned value aliases storage")
	}
	if e = db.Put(p, key, []byte{}); e != nil {
		t.Fatal(e)
	}
	got, e = db.Get(p, key)
	if e != nil || len(got) != 0 {
		t.Fatalf("empty value missing: %v", e)
	}
	if e = db.Delete(p, key); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Get(p, key); !errors.Is(e, domain.ErrKey) {
		t.Fatal(e)
	}
	if e = db.Put(p, nil, []byte("x")); e == nil {
		t.Fatal("accepted empty key")
	}
	if _, e = db.Get(domain.Path{[]byte("absent")}, key); !errors.Is(e, domain.ErrBucket) {
		t.Fatal(e)
	}
}
func TestNestedBucketsAndPagination(t *testing.T) {
	db, p := database(t)
	for _, n := range []string{"a", "b", "c"} {
		if e := db.CreateBucket(append(append(domain.Path{}, p...), []byte(n))); e != nil {
			t.Fatal(e)
		}
	}
	names, e := db.Buckets(context.Background(), p, nil, 2)
	if e != nil || len(names) != 2 {
		t.Fatalf("%q %v", names, e)
	}
	names, e = db.Buckets(context.Background(), p, names[1], 2)
	if e != nil || len(names) != 1 || string(names[0]) != "c" {
		t.Fatalf("%q %v", names, e)
	}
	child := domain.Path{p[0], []byte("a")}
	if e = db.Put(child, []byte("nested"), []byte("value")); e != nil {
		t.Fatal(e)
	}
	stats, e := db.Analyze(context.Background())
	if e != nil || stats.RootBuckets != 1 || stats.Buckets != 4 || stats.Records != 1 {
		t.Fatalf("incorrect database stats: %+v %v", stats, e)
	}
	bs, e := db.BucketStats(p)
	if e != nil || bs.Records != 1 || bs.Buckets != 4 {
		t.Fatalf("incorrect bucket stats: %+v %v", bs, e)
	}
	if e = db.DeleteBucket(child); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Get(child, []byte("nested")); !errors.Is(e, domain.ErrBucket) {
		t.Fatal(e)
	}
}
func TestCursorQueries(t *testing.T) {
	db, p := database(t)
	for _, k := range []string{"a", "u:01", "u:02", "u:03", "u:04", "z"} {
		if e := db.Put(p, []byte(k), []byte(k)); e != nil {
			t.Fatal(e)
		}
	}
	if e := db.CreateBucket(domain.Path{p[0], []byte("u:025")}); e != nil {
		t.Fatal(e)
	}
	q := domain.Query{Prefix: []byte("u:"), Limit: 2}
	page, e := db.List(context.Background(), p, q)
	if e != nil || len(page.Records) != 2 || string(page.Next) != "u:02" {
		t.Fatalf("%+v %v", page, e)
	}
	q.After = page.Next
	page, e = db.List(context.Background(), p, q)
	if e != nil || len(page.Records) != 2 || page.Next != nil || string(page.Records[0].Key) != "u:03" {
		t.Fatalf("%+v %v", page, e)
	}
	q = domain.Query{Start: []byte("u:02"), End: []byte("u:04"), Limit: 10}
	page, e = db.List(context.Background(), p, q)
	if e != nil || len(page.Records) != 2 {
		t.Fatalf("%+v %v", page, e)
	}
	q.After = []byte("u:021")
	page, e = db.List(context.Background(), p, q)
	if e != nil || len(page.Records) != 1 || string(page.Records[0].Key) != "u:03" {
		t.Fatalf("%+v %v", page, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = db.List(ctx, p, domain.Query{Limit: 2}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e = db.List(context.Background(), p, domain.Query{Limit: 0}); e == nil {
		t.Fatal("unbounded list accepted")
	}
}
func TestBatchRollbackAndDuplicates(t *testing.T) {
	db, p := database(t)
	ctx := context.Background()
	if e := db.Put(p, []byte("old"), []byte("original")); e != nil {
		t.Fatal(e)
	}
	batch := []domain.Record{{Key: []byte("new"), Value: []byte("new")}, {Key: []byte("old"), Value: []byte("replacement")}}
	if e := db.WriteBatch(ctx, p, batch, "error"); e == nil {
		t.Fatal("duplicate accepted")
	}
	if _, e := db.Get(p, []byte("new")); !errors.Is(e, domain.ErrKey) {
		t.Fatal("batch partially committed")
	}
	if e := db.WriteBatch(ctx, p, batch, "skip"); e != nil {
		t.Fatal(e)
	}
	v, e := db.Get(p, []byte("old"))
	if e != nil || string(v) != "original" {
		t.Fatalf("%s %v", v, e)
	}
	if e = db.WriteBatch(ctx, p, batch, "overwrite"); e != nil {
		t.Fatal(e)
	}
	v, e = db.Get(p, []byte("old"))
	if e != nil || string(v) != "replacement" {
		t.Fatalf("%s %v", v, e)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if e = db.WriteBatch(ctx, p, []domain.Record{{Key: []byte("cancel"), Value: []byte("x")}}, "overwrite"); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func TestLifecycleReadOnlyAndInvalidFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	if _, e := adapters.Open(path, false, false); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	db, e := adapters.Open(path, true, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = adapters.Open(path, true, false); !errors.Is(e, os.ErrExist) {
		t.Fatal(e)
	}
	p := domain.Path{[]byte("b")}
	if e = db.CreateBucket(p); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Info(); !errors.Is(e, domain.ErrClosed) {
		t.Fatal(e)
	}
	db, e = adapters.Open(path, false, true)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = db.Put(p, []byte("k"), []byte("v")); e == nil {
		t.Fatal("read-only write accepted")
	}
	info, e := db.Info()
	if e != nil || !info.ReadOnly || info.Size == 0 {
		t.Fatalf("%+v %v", info, e)
	}
	bad := filepath.Join(t.TempDir(), "bad")
	if e = os.WriteFile(bad, []byte("not a database"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = adapters.Open(bad, false, true); e == nil {
		t.Fatal("corrupt file accepted")
	}
}
func TestCloseWaitsForReaders(t *testing.T) {
	db, p := database(t)
	if e := db.Put(p, []byte("k"), []byte("v")); e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		e := db.Walk(context.Background(), p, domain.Query{}, func(k, v []byte) error { close(entered); <-release; return nil })
		if e != nil {
			t.Error(e)
		}
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- db.Close() }()
	select {
	case e := <-closed:
		t.Fatalf("closed during read: %v", e)
	default:
	}
	close(release)
	wg.Wait()
	if e := <-closed; e != nil {
		t.Fatal(e)
	}
}
func TestConcurrentReadersAndLargeValue(t *testing.T) {
	db, p := database(t)
	value := bytes.Repeat([]byte{255}, 1<<20)
	if e := db.Put(p, []byte("large"), value); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := db.Get(p, []byte("large"))
			if e != nil || !bytes.Equal(v, value) {
				t.Error("concurrent read mismatch", e)
			}
		}()
	}
	wg.Wait()
}

func TestFilteredBucketPages(t *testing.T) {
	db, p := database(t)
	ctx := context.Background()
	for _, name := range []string{"a", "team:alpha", "team:beta", "team:gamma", "z"} {
		if e := db.CreateBucket(domain.Path{[]byte(name)}); e != nil {
			t.Fatal(e)
		}
		if e := db.CreateBucket(domain.Path{p[0], []byte(name)}); e != nil {
			t.Fatal(e)
		}
	}
	for _, parent := range []domain.Path{nil, p} {
		q := domain.Query{Contains: []byte("team:"), Limit: 2}
		names, e := db.FindBuckets(ctx, parent, q)
		if e != nil || len(names) != 2 || string(names[0]) != "team:alpha" {
			t.Fatalf("%q %v", names, e)
		}
		q.After = names[1]
		names, e = db.FindBuckets(ctx, parent, q)
		if e != nil || len(names) != 1 || string(names[0]) != "team:gamma" {
			t.Fatalf("filtered bucket continuation: %q %v", names, e)
		}
		q = domain.Query{Prefix: []byte("team:"), Contains: []byte("beta"), Limit: 10}
		names, e = db.FindBuckets(ctx, parent, q)
		if e != nil || len(names) != 1 || string(names[0]) != "team:beta" {
			t.Fatalf("combined bucket filters: %q %v", names, e)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e := db.FindBuckets(cancelled, p, domain.Query{Limit: 2}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestRecordKeyAndValueFilters(t *testing.T) {
	db, p := database(t)
	ctx := context.Background()
	for _, r := range []domain.Record{{Key: []byte("a:skip"), Value: []byte("red")}, {Key: []byte("user:01"), Value: []byte("blue")}, {Key: []byte("user:02"), Value: []byte("red")}, {Key: []byte("user:03"), Value: []byte("red")}, {Key: []byte{255, 0}, Value: []byte{0, 255, 128}}} {
		if e := db.Put(p, r.Key, r.Value); e != nil {
			t.Fatal(e)
		}
	}
	q := domain.Query{Contains: []byte("user:"), ValueContains: []byte("red"), Limit: 1}
	page, e := db.List(ctx, p, q)
	if e != nil || len(page.Records) != 1 || string(page.Records[0].Key) != "user:02" || string(page.Next) != "user:02" {
		t.Fatalf("%+v %v", page, e)
	}
	q.After = page.Next
	page, e = db.List(ctx, p, q)
	if e != nil || len(page.Records) != 1 || string(page.Records[0].Key) != "user:03" || page.Next != nil {
		t.Fatalf("%+v %v", page, e)
	}
	page, e = db.List(ctx, p, domain.Query{ValueContains: []byte{255, 128}, Limit: 10})
	if e != nil || len(page.Records) != 1 || !bytes.Equal(page.Records[0].Key, []byte{255, 0}) {
		t.Fatalf("binary value search: %+v %v", page, e)
	}
	page, e = db.List(ctx, p, domain.Query{ValueContains: []byte("missing"), Limit: 10})
	if e != nil || len(page.Records) != 0 {
		t.Fatalf("missing value matched: %+v %v", page, e)
	}
}

type probeContext struct {
	context.Context
	checks int
}

func (c *probeContext) Err() error { c.checks++; return nil }
func TestFilteredWalkStopsAtLimit(t *testing.T) {
	db, p := database(t)
	if e := db.Put(p, []byte("0000"), []byte("match")); e != nil {
		t.Fatal(e)
	}
	for i := 1; i < 100; i++ {
		if e := db.Put(p, []byte(fmt.Sprintf("%04d", i)), []byte("other")); e != nil {
			t.Fatal(e)
		}
	}
	ctx := &probeContext{Context: context.Background()}
	count := 0
	if e := db.Walk(ctx, p, domain.Query{ValueContains: []byte("match"), Limit: 1}, func(k, v []byte) error { count++; return nil }); e != nil {
		t.Fatal(e)
	}
	if count != 1 || ctx.checks > 5 {
		t.Fatalf("scanned beyond completed query: %d matches, %d checks", count, ctx.checks)
	}
}
func benchmarkDB(b *testing.B, n int) (*adapters.BoltDBAdapter, domain.Path) {
	db, p := database(b)
	records := make([]domain.Record, 500)
	for offset := 0; offset < n; offset += len(records) {
		for i := range records {
			records[i] = domain.Record{Key: []byte(fmt.Sprintf("user:%08d", offset+i)), Value: bytes.Repeat([]byte("v"), 64)}
		}
		if e := db.WriteBatch(context.Background(), p, records, "overwrite"); e != nil {
			b.Fatal(e)
		}
	}
	return db, p
}
func BenchmarkCursorWalk(b *testing.B) {
	for _, n := range []int{1000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			db, p := benchmarkDB(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if e := db.Walk(context.Background(), p, domain.Query{}, func(k, v []byte) error { return nil }); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
func BenchmarkQueries(b *testing.B) {
	db, p := benchmarkDB(b, 10000)
	for name, q := range map[string]domain.Query{"page": {Limit: 100}, "prefix": {Limit: 100, Prefix: []byte("user:000000")}, "range": {Limit: 100, Start: []byte("user:00002000"), End: []byte("user:00003000")}} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, e := db.List(context.Background(), p, q); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}

func BenchmarkSearchFilters(b *testing.B) {
	db, p := benchmarkDB(b, 10000)
	for name, q := range map[string]domain.Query{"key_contains": {Contains: []byte("000000"), Limit: 100}, "value_matches": {ValueContains: []byte("vv"), Limit: 100}, "value_full_scan": {ValueContains: []byte("missing"), Limit: 100}} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, e := db.List(context.Background(), p, q); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
