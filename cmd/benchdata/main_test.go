package main

import (
	"bltCli/domain"
	"bltCli/repository/adapters"
	"context"
	"path/filepath"
	"testing"
)

func TestDataset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	if e := generate(context.Background(), path, 23, 16, 3, 5); e != nil {
		t.Fatal(e)
	}
	db, e := adapters.Open(path, false, true)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	names, e := db.Buckets(context.Background(), nil, nil, 10)
	if e != nil || len(names) != 3 {
		t.Fatalf("%q %v", names, e)
	}
	count := 0
	for _, name := range names {
		if e = db.Walk(context.Background(), domain.Path{name}, domain.Query{}, func(k, v []byte) error {
			if len(v) != 16 {
				t.Fatal("value size mismatch")
			}
			count++
			return nil
		}); e != nil {
			t.Fatal(e)
		}
	}
	if count != 23 {
		t.Fatalf("got %d records", count)
	}
	if e = generate(context.Background(), path, 1, 1, 1, 1); e == nil {
		t.Fatal("overwrote existing dataset")
	}
}
