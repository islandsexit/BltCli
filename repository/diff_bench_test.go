package repository

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
)

// BenchmarkBuildDiff includes temporary files, hierarchy indexing and cleanup.
func BenchmarkBuildDiff(b *testing.B) {
	for _, size := range []int{128, 4096} {
		b.Run(fmt.Sprintf("value=%d", size), func(b *testing.B) {
			dir := b.TempDir()
			a, err := adapters.Open(filepath.Join(dir, "a.db"), true, false)
			if err != nil {
				b.Fatal(err)
			}
			defer a.Close()
			other, err := adapters.Open(filepath.Join(dir, "b.db"), true, false)
			if err != nil {
				b.Fatal(err)
			}
			defer other.Close()
			p := domain.Path{[]byte("root"), []byte("child")}
			for side, db := range []*adapters.BoltDBAdapter{a, other} {
				for depth := 1; depth <= len(p); depth++ {
					if err := db.CreateBucket(p[:depth]); err != nil {
						b.Fatal(err)
					}
				}
				records := make([]domain.Record, 2048)
				value := bytes.Repeat([]byte{byte('a' + side)}, size)
				for i := range records {
					records[i] = domain.Record{Key: []byte(fmt.Sprintf("%08d", i)), Value: value}
				}
				records[0].Value = []byte("equal")
				if err := db.WriteBatch(context.Background(), p, records, "overwrite"); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.SetBytes(int64(2 * 2047 * size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s, err := BuildDiff(context.Background(), dir, a, other)
				if err != nil {
					b.Fatal(err)
				}
				if err = s.Remove(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
