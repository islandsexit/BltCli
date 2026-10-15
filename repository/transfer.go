// Package repository provides bounded streaming import and export.
package repository

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/islandsexit/BltCli/domain"
	"io"
	"unicode/utf8"
)

// ExportValueJSON writes one complete JSON value without copying it off mmap.
// The read snapshot remains open during validation and output; slow writers can
// delay page reclamation. Invalid JSON and non-UTF-8 values are rejected.
func ExportValueJSON(ctx context.Context, db domain.BoltDbAdapter, p domain.Path, key []byte, w io.Writer) error {
	found := false
	err := db.Walk(ctx, p, domain.Query{Start: key, Prefix: key, Limit: 1}, func(k, v []byte) error {
		if !bytes.Equal(k, key) {
			return domain.ErrKey
		}
		found = true
		if !utf8.Valid(v) || !json.Valid(v) {
			return errors.New("value is not valid UTF-8 JSON")
		}
		for len(v) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			n := min(len(v), 64<<10)
			written, err := w.Write(v[:n])
			if err != nil {
				return err
			}
			if written != n {
				return io.ErrShortWrite
			}
			v = v[n:]
		}
		return ctx.Err()
	})
	if err != nil {
		return err
	}
	if !found {
		return domain.ErrKey
	}
	return nil
}

// ImportOptions bounds both records and bytes in a transaction.
type ImportOptions struct {
	BatchSize, BatchBytes, MaxRecordBytes int
	Duplicates                            string
}

// Export streams base64 JSONL while holding one consistent read snapshot.
// Slow writers prolong that snapshot and may delay page reclamation.
func Export(ctx context.Context, db domain.BoltDbAdapter, p domain.Path, q domain.Query, w io.Writer) error {
	bw := bufio.NewWriterSize(w, 64<<10)
	enc := json.NewEncoder(bw)
	err := db.Walk(ctx, p, q, func(k, v []byte) error { return enc.Encode(domain.Record{Key: k, Value: v}) })
	return errors.Join(err, bw.Flush())
}

// Import consumes one bounded JSON record per line. Completed batches remain
// committed after failures or cancellation; the returned count is committed input records.
func Import(ctx context.Context, db domain.BoltDbAdapter, p domain.Path, r io.Reader, o ImportOptions) (committed int, err error) {
	if o.BatchSize < 1 || o.BatchSize > 10000 || o.BatchBytes < 1 || o.BatchBytes > 256<<20 || o.MaxRecordBytes < 1 || o.MaxRecordBytes > 256<<20 {
		return 0, errors.New("positive bounded import limits required")
	}
	if o.Duplicates != "overwrite" && o.Duplicates != "skip" && o.Duplicates != "error" {
		return 0, errors.New("invalid duplicate mode")
	}
	if e := ctx.Err(); e != nil {
		return 0, e
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, min(64<<10, o.MaxRecordBytes)), o.MaxRecordBytes+1)
	batch := make([]domain.Record, 0, o.BatchSize)
	size := 0
	line := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if e := db.WriteBatch(ctx, p, batch, o.Duplicates); e != nil {
			return e
		}
		committed += len(batch)
		clear(batch)
		batch = batch[:0]
		size = 0
		return nil
	}
	for scanner.Scan() {
		line++
		if e := ctx.Err(); e != nil {
			return committed, e
		}
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		if len(raw) > o.MaxRecordBytes {
			return committed, fmt.Errorf("line %d exceeds record limit", line)
		}
		var rec domain.Record
		if e := json.Unmarshal(raw, &rec); e != nil {
			return committed, fmt.Errorf("line %d: %w", line, e)
		}
		if len(rec.Key) == 0 || rec.Value == nil {
			return committed, fmt.Errorf("line %d requires a nonempty base64 key and base64 value", line)
		}
		n := len(rec.Key) + len(rec.Value)
		if n > o.BatchBytes {
			return committed, fmt.Errorf("line %d exceeds batch byte limit", line)
		}
		if size+n > o.BatchBytes {
			if e := flush(); e != nil {
				return committed, e
			}
		}
		batch = append(batch, rec)
		size += n
		if len(batch) == o.BatchSize {
			if e := flush(); e != nil {
				return committed, e
			}
		}
	}
	if e := scanner.Err(); e != nil {
		return committed, fmt.Errorf("line %d: %w", line+1, e)
	}
	if e := ctx.Err(); e != nil {
		return committed, e
	}
	return committed, flush()
}
