// benchdata creates reproducible, bounded-memory benchmark datasets.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
	"os"
	"os/signal"
)

func generate(ctx context.Context, path string, count, valueSize, bucketCount, batchSize int) (err error) {
	if count < 1 || valueSize < 0 || valueSize > 16<<20 || bucketCount < 1 || bucketCount > 1000 || batchSize < 1 || batchSize > 10000 || int64(valueSize)*int64(batchSize) > 64<<20 {
		return errors.New("invalid limits: batch payload must be at most 64 MiB")
	}
	db, e := adapters.Open(path, true, false)
	if e != nil {
		return e
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	value := bytes.Repeat([]byte("v"), valueSize)
	records := make([]domain.Record, 0, batchSize)
	for bucket := 0; bucket < bucketCount; bucket++ {
		p := domain.Path{[]byte(fmt.Sprintf("bucket:%04d", bucket))}
		if e = db.CreateBucket(p); e != nil {
			return e
		}
		for i := bucket; i < count; {
			records = records[:0]
			for len(records) < batchSize && i < count {
				records = append(records, domain.Record{Key: []byte(fmt.Sprintf("user:%012d", i)), Value: value})
				i += bucketCount
			}
			if e = db.WriteBatch(ctx, p, records, "overwrite"); e != nil {
				return e
			}
		}
	}
	info, e := db.Info()
	if e == nil {
		_, e = fmt.Fprintf(os.Stderr, "Created %s: %d records, %d buckets, %d bytes on disk\n", info.Path, count, bucketCount, info.Size)
	}
	return e
}
func main() {
	path := flag.String("output", "benchmark.db", "new database path")
	count := flag.Int("records", 100000, "total record count")
	size := flag.Int("value-bytes", 128, "bytes per value")
	buckets := flag.Int("buckets", 1, "root buckets")
	batch := flag.Int("batch-size", 500, "records per transaction")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if e := generate(ctx, *path, *count, *size, *buckets, *batch); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
