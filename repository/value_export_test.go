package repository_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository"
)

func TestExportValueJSON(t *testing.T) {
	db, p := dbFor(t)
	key := []byte{0, 255}
	for _, value := range [][]byte{[]byte(" {\"n\":900719925474099312345,\"n\":1.00e99} \n"), []byte(`"` + strings.Repeat("x", 200<<10) + `"`), []byte("null"), {}, {0xff}, []byte(`{"bad":`)} {
		if err := db.Put(p, key, value); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		err := repository.ExportValueJSON(context.Background(), db, p, key, &out)
		if len(value) == 0 || value[0] == 255 || string(value) == `{"bad":` {
			if err == nil || out.Len() != 0 {
				t.Fatalf("invalid JSON exported: %v", err)
			}
		} else if err != nil || !bytes.Equal(out.Bytes(), value) {
			t.Fatalf("value changed or truncated: %v", err)
		}
	}
	if err := db.Put(p, []byte("missing-suffix"), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := repository.ExportValueJSON(context.Background(), db, p, []byte("missing"), io.Discard); !errors.Is(err, domain.ErrKey) {
		t.Fatalf("missing key: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := repository.ExportValueJSON(ctx, db, p, key, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if err := db.Put(p, key, []byte(`"`+strings.Repeat("x", 200<<10)+`"`)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	w := &cancelJSONWriter{cancel: cancel}
	if err := repository.ExportValueJSON(ctx, db, p, key, w); !errors.Is(err, context.Canceled) || w.bytes > 64<<10 {
		t.Fatalf("mid-write cancellation: %v, %d", err, w.bytes)
	}
	if err := repository.ExportValueJSON(context.Background(), db, p, key, shortJSONWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write: %v", err)
	}
}

type cancelJSONWriter struct {
	cancel context.CancelFunc
	bytes  int
}

func (w *cancelJSONWriter) Write(p []byte) (int, error) {
	w.bytes += len(p)
	w.cancel()
	return len(p), nil
}

type shortJSONWriter struct{}

func (shortJSONWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func BenchmarkExportValueJSON(b *testing.B) {
	db, p := dbFor(b)
	key := []byte("value")
	value := []byte(`"` + strings.Repeat("x", 1<<20) + `"`)
	if err := db.Put(p, key, value); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(value)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := repository.ExportValueJSON(context.Background(), db, p, key, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
