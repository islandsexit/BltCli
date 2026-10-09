package repository_test

import (
	"bltCli/domain"
	"bltCli/repository"
	"bltCli/repository/adapters"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func dbFor(t testing.TB) (*adapters.BoltDBAdapter, domain.Path) {
	t.Helper()
	db, e := adapters.Open(filepath.Join(t.TempDir(), "db"), true, false)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	})
	p := domain.Path{[]byte("data")}
	if e = db.CreateBucket(p); e != nil {
		t.Fatal(e)
	}
	return db, p
}

var options = repository.ImportOptions{BatchSize: 2, BatchBytes: 1024, MaxRecordBytes: 1024, Duplicates: "error"}

func TestBinaryRoundTrip(t *testing.T) {
	db, p := dbFor(t)
	records := []domain.Record{{Key: []byte{0, 255}, Value: []byte{128, 0, 255}}, {Key: []byte("empty"), Value: []byte{}}}
	var input bytes.Buffer
	for _, r := range records {
		if e := json.NewEncoder(&input).Encode(r); e != nil {
			t.Fatal(e)
		}
	}
	n, e := repository.Import(context.Background(), db, p, &input, options)
	if e != nil || n != 2 {
		t.Fatalf("%d %v", n, e)
	}
	var output bytes.Buffer
	if e = repository.Export(context.Background(), db, p, domain.Query{}, &output); e != nil {
		t.Fatal(e)
	}
	decoder := json.NewDecoder(&output)
	for _, want := range records {
		var got domain.Record
		if e = decoder.Decode(&got); e != nil || !bytes.Equal(got.Key, want.Key) || !bytes.Equal(got.Value, want.Value) {
			t.Fatalf("%+v %v", got, e)
		}
	}
}
func TestPartialImportAndBounds(t *testing.T) {
	for _, tail := range []string{"invalid", `{"key":"","value":""}`, strings.Repeat("x", 2048), `{"key":"Yw==","value":null}`} {
		t.Run(fmt.Sprint(len(tail)), func(t *testing.T) {
			db, p := dbFor(t)
			input := `{"key":"YQ==","value":"MQ=="}` + "\n" + `{"key":"Yg==","value":"Mg=="}` + "\n" + tail
			count, e := repository.Import(context.Background(), db, p, strings.NewReader(input), options)
			if e == nil || count != 2 {
				t.Fatalf("%d %v", count, e)
			}
			if _, e = db.Get(p, []byte("a")); e != nil {
				t.Fatal(e)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestExportFailureAndCancellation(t *testing.T) {
	db, p := dbFor(t)
	if e := db.Put(p, []byte("a"), []byte("v")); e != nil {
		t.Fatal(e)
	}
	if e := repository.Export(context.Background(), db, p, domain.Query{}, failingWriter{}); !errors.Is(e, io.ErrClosedPipe) {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := repository.Import(ctx, db, p, strings.NewReader(""), options); !errors.Is(e, context.Canceled) {
		t.Fatal("empty import ignored cancellation", e)
	}
	if e := repository.Export(ctx, db, p, domain.Query{}, io.Discard); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
func FuzzImport(f *testing.F) {
	f.Add([]byte(`{"key":"YQ==","value":""}`))
	f.Add([]byte("invalid"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			t.Skip()
		}
		db, p := dbFor(t)
		_, _ = repository.Import(context.Background(), db, p, bytes.NewReader(data), repository.ImportOptions{BatchSize: 4, BatchBytes: 4096, MaxRecordBytes: 4096, Duplicates: "overwrite"})
	})
}
func BenchmarkStreamingExport(b *testing.B) {
	db, p := dbFor(b)
	batch := make([]domain.Record, 500)
	for offset := 0; offset < 10000; offset += 500 {
		for i := range batch {
			batch[i] = domain.Record{Key: []byte(fmt.Sprintf("%08d", offset+i)), Value: bytes.Repeat([]byte("v"), 64)}
		}
		if e := db.WriteBatch(context.Background(), p, batch, "overwrite"); e != nil {
			b.Fatal(e)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if e := repository.Export(context.Background(), db, p, domain.Query{}, io.Discard); e != nil {
			b.Fatal(e)
		}
	}
}
func BenchmarkBatchImport(b *testing.B) {
	db, p := dbFor(b)
	var input bytes.Buffer
	for i := 0; i < 1000; i++ {
		_ = json.NewEncoder(&input).Encode(domain.Record{Key: []byte(fmt.Sprintf("%08d", i)), Value: bytes.Repeat([]byte("v"), 64)})
	}
	data := input.Bytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, e := repository.Import(context.Background(), db, p, bytes.NewReader(data), repository.ImportOptions{BatchSize: 100, BatchBytes: 1 << 20, MaxRecordBytes: 1 << 20, Duplicates: "overwrite"})
		if e != nil {
			b.Fatal(e)
		}
	}
}
