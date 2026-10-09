package adapters

import (
	"bltCli/domain"
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestSearchScanBlocksDoNotSkipRecords(t *testing.T) {
	db, e := Open(filepath.Join(t.TempDir(), "db"), true, false)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	p := domain.Path{[]byte("bucket")}
	if e = db.CreateBucket(p); e != nil {
		t.Fatal(e)
	}
	batch := make([]domain.Record, 2500)
	for i := range batch {
		batch[i] = domain.Record{Key: []byte(fmt.Sprintf("%04d", i)), Value: []byte("v")}
	}
	if e = db.WriteBatch(context.Background(), p, batch, "overwrite"); e != nil {
		t.Fatal(e)
	}
	before := db.db.Stats().TxN
	page, e := db.Search(context.Background(), domain.SearchQuery{Kind: "records", Contains: []byte("absent"), Limit: 10})
	if e != nil || len(page.Hits) != 0 {
		t.Fatalf("%+v %v", page, e)
	}
	if reads := db.db.Stats().TxN - before; reads != 3 {
		t.Fatalf("2501 entries should use three bounded read transactions; got %d", reads)
	}
	page, e = db.Search(context.Background(), domain.SearchQuery{Kind: "records", Contains: []byte("2499"), Limit: 1})
	if e != nil || len(page.Hits) != 1 || string(page.Hits[0].Key) != "2499" {
		t.Fatalf("record after block boundaries missing: %+v %v", page, e)
	}
}
