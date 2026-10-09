package adapters

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/islandsexit/BltCli/domain"
	bolt "go.etcd.io/bbolt"
)

func diffDB(t testing.TB, name string) *BoltDBAdapter {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), name), true, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

func TestDiffLogicalContents(t *testing.T) {
	a, b := diffDB(t, "a"), diffDB(t, "b")
	root := domain.Path{[]byte{0, 255}}
	for _, db := range []*BoltDBAdapter{a, b} {
		if err := db.CreateBucket(root); err != nil {
			t.Fatal(err)
		}
		if err := db.Put(root, []byte("equal"), nil); err != nil {
			t.Fatal(err)
		}
	}
	large := bytes.Repeat([]byte("x"), 1<<20)
	changed := bytes.Clone(large)
	changed[len(changed)-1] = 'y'
	if err := a.Put(root, []byte("changed"), large); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(root, []byte("changed"), changed); err != nil {
		t.Fatal(err)
	}
	if err := a.Put(root, []byte("deleted"), nil); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(root, []byte("added"), []byte{0, 255}); err != nil {
		t.Fatal(err)
	}
	if err := a.CreateBucket(append(root, []byte("nested"))); err != nil {
		t.Fatal(err)
	}
	if err := a.Put(append(root, []byte("nested")), []byte("key"), []byte("value")); err != nil {
		t.Fatal(err)
	}
	var entries []domain.DiffEntry
	summary, err := Diff(context.Background(), a, b, func(e domain.DiffEntry) error {
		e.Path = copyPath(e.Path)
		e.Key = bytes.Clone(e.Key)
		e.Base = bytes.Clone(e.Base)
		e.Other = bytes.Clone(e.Other)
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Differences != 5 || summary.EqualRecords != 1 || summary.Equal() || summary.CompletelyDifferent() {
		t.Fatalf("summary: %+v", summary)
	}
	for _, e := range entries {
		if string(e.Key) == "changed" && (len(e.Base) > 4096 || e.BaseBytes != len(large) || e.OtherBytes != len(changed) || e.ValueOffset == 0 || bytes.Equal(e.Base, e.Other)) {
			t.Fatal("full value was not compared with bounded previews")
		}
		if string(e.Key) == "deleted" && (!e.BasePresent || e.OtherPresent) {
			t.Fatal("empty value confused with missing")
		}
	}
	self, err := Diff(context.Background(), a, a, func(domain.DiffEntry) error { return errors.New("unexpected diff") })
	if err != nil || !self.Equal() {
		t.Fatalf("self diff: %+v %v", self, err)
	}
}

func TestDiffResumesAcrossBlocksAndTypeChanges(t *testing.T) {
	a, b := diffDB(t, "a"), diffDB(t, "b")
	for _, db := range []*BoltDBAdapter{a, b} {
		if err := db.CreateBucket(domain.Path{[]byte("root")}); err != nil {
			t.Fatal(err)
		}
		if err := db.tx(true, func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("root"))
			for i := 0; i < 2500; i++ {
				if err := bucket.Put([]byte(fmt.Sprintf("%05d", i)), []byte("same")); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	path := domain.Path{[]byte("root")}
	if err := a.Put(path, []byte("type"), nil); err != nil {
		t.Fatal(err)
	}
	if err := b.CreateBucket(append(path, []byte("type"))); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(append(path, []byte("type")), []byte("child"), []byte("only other")); err != nil {
		t.Fatal(err)
	}
	if err := b.Put(path, []byte("zz"), []byte("added")); err != nil {
		t.Fatal(err)
	}
	count := 0
	summary, err := Diff(context.Background(), a, b, func(e domain.DiffEntry) error {
		count++
		if e.Kind == "type" && (e.BaseBucket || !e.OtherBucket || !e.BasePresent) {
			t.Fatal("record/bucket type lost")
		}
		return nil
	})
	if err != nil || count != 3 || summary.EqualRecords != 2500 {
		t.Fatalf("block resume: %+v count=%d err=%v", summary, count, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Diff(ctx, a, b, func(domain.DiffEntry) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	sentinel := errors.New("output failed")
	if _, err := Diff(context.Background(), a, b, func(domain.DiffEntry) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Diff(context.Background(), a, b, func(domain.DiffEntry) error { return nil }); !errors.Is(err, domain.ErrClosed) {
		t.Fatal(err)
	}
}

func TestDiffEmptyBucketsAndSequences(t *testing.T) {
	a, b := diffDB(t, "a"), diffDB(t, "b")
	for _, db := range []*BoltDBAdapter{a, b} {
		if err := db.CreateBucket(domain.Path{[]byte("empty")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.tx(true, func(tx *bolt.Tx) error { return tx.Bucket([]byte("empty")).SetSequence(42) }); err != nil {
		t.Fatal(err)
	}
	summary, err := Diff(context.Background(), a, b, func(e domain.DiffEntry) error {
		if e.Kind != "bucket" || e.BaseSequence != 0 || e.OtherSequence != 42 {
			t.Fatalf("sequence: %+v", e)
		}
		return nil
	})
	if err != nil || summary.Differences != 1 {
		t.Fatalf("%+v %v", summary, err)
	}
}

func BenchmarkDiffMostlyEqual(b *testing.B) {
	a, c := diffDB(b, "a"), diffDB(b, "b")
	for _, db := range []*BoltDBAdapter{a, c} {
		if err := db.CreateBucket(domain.Path{[]byte("root")}); err != nil {
			b.Fatal(err)
		}
		if err := db.tx(true, func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("root"))
			for i := 0; i < 10000; i++ {
				if err := bucket.Put([]byte(fmt.Sprintf("%05d", i)), []byte("value")); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			b.Fatal(err)
		}
	}
	if err := c.Put(domain.Path{[]byte("root")}, []byte("09999"), []byte("changed")); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Diff(context.Background(), a, c, func(domain.DiffEntry) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}
