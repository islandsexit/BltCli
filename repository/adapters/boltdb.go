// Package adapters implements storage using bbolt.
package adapters

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/islandsexit/BltCli/domain"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// BoltDBAdapter protects database lifetime against concurrent operations.
type BoltDBAdapter struct {
	mu sync.RWMutex
	db *bolt.DB
}

var _ domain.BoltDbAdapter = (*BoltDBAdapter)(nil)

// Open opens an existing database, or exclusively creates a new one.
func Open(path string, create, readOnly bool) (*BoltDBAdapter, error) {
	if path == "" {
		return nil, errors.New("database path required")
	}
	if create && readOnly {
		return nil, errors.New("cannot create read-only database")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if create {
		f, e := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, e
		}
		if e = f.Close(); e != nil {
			return nil, e
		}
	} else {
		st, e := os.Stat(abs)
		if e != nil {
			return nil, e
		}
		if !st.Mode().IsRegular() {
			return nil, errors.New("database must be a regular file")
		}
	}
	db, err := bolt.Open(abs, 0600, &bolt.Options{Timeout: time.Second, ReadOnly: readOnly})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &BoltDBAdapter{db: db}, nil
}

// Close waits for operations before releasing the database.
func (a *BoltDBAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.db == nil {
		return nil
	}
	err := a.db.Close()
	if err == nil {
		a.db = nil
	}
	return err
}
func (a *BoltDBAdapter) tx(write bool, fn func(*bolt.Tx) error) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.db == nil {
		return domain.ErrClosed
	}
	if write {
		return a.db.Update(fn)
	}
	return a.db.View(fn)
}
func bucket(tx *bolt.Tx, path domain.Path) (*bolt.Bucket, error) {
	if len(path) == 0 {
		return nil, errors.New("bucket path required")
	}
	b := tx.Bucket(path[0])
	for _, n := range path[1:] {
		if b == nil {
			break
		}
		b = b.Bucket(n)
	}
	if b == nil {
		return nil, domain.ErrBucket
	}
	return b, nil
}

// Info returns file and engine statistics without scanning records.
func (a *BoltDBAdapter) Info() (domain.Info, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.db == nil {
		return domain.Info{}, domain.ErrClosed
	}
	st, err := os.Stat(a.db.Path())
	if err != nil {
		return domain.Info{}, err
	}
	s := a.db.Stats()
	return domain.Info{Path: a.db.Path(), Size: st.Size(), ReadOnly: a.db.IsReadOnly(), FreePages: s.FreePageN, PendingPages: s.PendingPageN, OpenTransactions: s.OpenTxN, PageSize: a.db.Info().PageSize, FreeBytes: s.FreeAlloc}, nil
}

// Analyze scans engine pages without collecting records. Cancellation is checked
// between root buckets; bbolt's individual Stats traversals are not interruptible.
func (a *BoltDBAdapter) Analyze(ctx context.Context) (out domain.Analysis, err error) {
	err = a.tx(false, func(tx *bolt.Tx) error {
		c := tx.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			s := tx.Bucket(k).Stats()
			out.RootBuckets++
			out.Buckets += s.BucketN
			out.Records += s.KeyN - s.BucketN + 1
			out.BranchPages += s.BranchPageN
			out.LeafPages += s.LeafPageN
			out.OverflowPages += s.BranchOverflowN + s.LeafOverflowN
			out.AllocatedBytes += s.BranchAlloc + s.LeafAlloc
			out.UsedBytes += usedBytes(s)
		}
		return ctx.Err()
	})
	return
}

// Inline descendants are already encoded in their parent's leaf pages.
func usedBytes(s bolt.BucketStats) int {
	if s.BranchAlloc+s.LeafAlloc == 0 {
		return s.InlineBucketInuse
	}
	return s.BranchInuse + s.LeafInuse
}

// Buckets lists at most limit child names after a binary key.
func (a *BoltDBAdapter) Buckets(ctx context.Context, p domain.Path, after []byte, limit int) (out [][]byte, err error) {
	return a.FindBuckets(ctx, p, domain.Query{After: after, Limit: limit})
}

// FindBuckets filters names while streaming a bounded cursor page. Contains
// searches may scan unrelated names; prefix searches start at Cursor.Seek.
func (a *BoltDBAdapter) FindBuckets(ctx context.Context, p domain.Path, q domain.Query) (out [][]byte, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if q.Limit < 1 || q.Limit > 10000 {
		return nil, errors.New("limit must be 1..10000")
	}
	err = a.tx(false, func(tx *bolt.Tx) error {
		var c *bolt.Cursor
		if len(p) == 0 {
			c = tx.Cursor()
		} else {
			b, e := bucket(tx, p)
			if e != nil {
				return e
			}
			c = b.Cursor()
		}
		k, v := c.First()
		start := q.Prefix
		if bytes.Compare(q.Start, start) > 0 {
			start = q.Start
		}
		if bytes.Compare(q.After, start) > 0 {
			start = q.After
		}
		if start != nil {
			k, v = c.Seek(start)
			if q.After != nil && bytes.Equal(k, q.After) {
				k, v = c.Next()
			}
		}
		for ; k != nil && len(out) < q.Limit; k, v = c.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			if q.End != nil && bytes.Compare(k, q.End) >= 0 {
				break
			}
			if len(q.Prefix) > 0 && !bytes.HasPrefix(k, q.Prefix) {
				break
			}
			if v == nil && bytes.Contains(k, q.Contains) {
				out = append(out, bytes.Clone(k))
			}
		}
		return nil
	})
	return
}

// CreateBucket creates a bucket under an existing parent.
func (a *BoltDBAdapter) CreateBucket(p domain.Path) error {
	if len(p) == 0 {
		return errors.New("bucket path required")
	}
	return a.tx(true, func(tx *bolt.Tx) error {
		if len(p) == 1 {
			_, e := tx.CreateBucket(p[0])
			return e
		}
		b, e := bucket(tx, p[:len(p)-1])
		if e != nil {
			return e
		}
		_, e = b.CreateBucket(p[len(p)-1])
		return e
	})
}

// DeleteBucket removes a bucket and its descendants atomically.
func (a *BoltDBAdapter) DeleteBucket(p domain.Path) error {
	if len(p) == 0 {
		return errors.New("bucket path required")
	}
	return a.tx(true, func(tx *bolt.Tx) error {
		if len(p) == 1 {
			return tx.DeleteBucket(p[0])
		}
		b, e := bucket(tx, p[:len(p)-1])
		if e != nil {
			return e
		}
		return b.DeleteBucket(p[len(p)-1])
	})
}

// BucketStats collects bbolt statistics and may traverse bucket pages.
func (a *BoltDBAdapter) BucketStats(p domain.Path) (out domain.BucketInfo, err error) {
	err = a.tx(false, func(tx *bolt.Tx) error {
		b, e := bucket(tx, p)
		if e != nil {
			return e
		}
		s := b.Stats()
		out = domain.BucketInfo{Keys: s.KeyN, Records: s.KeyN - s.BucketN + 1, Buckets: s.BucketN, Depth: s.Depth, BranchPages: s.BranchPageN, LeafPages: s.LeafPageN, OverflowPages: s.BranchOverflowN + s.LeafOverflowN, AllocatedBytes: s.BranchAlloc + s.LeafAlloc, UsedBytes: usedBytes(s)}
		return nil
	})
	return
}

// Get copies a value before the read transaction closes.
func (a *BoltDBAdapter) Get(p domain.Path, k []byte) (out []byte, err error) {
	err = a.tx(false, func(tx *bolt.Tx) error {
		b, e := bucket(tx, p)
		if e != nil {
			return e
		}
		v := b.Get(k)
		if v == nil {
			return domain.ErrKey
		}
		out = bytes.Clone(v)
		return nil
	})
	return
}

// Put inserts or replaces a binary record.
func (a *BoltDBAdapter) Put(p domain.Path, k, v []byte) error {
	return a.WriteBatch(context.Background(), p, []domain.Record{{Key: k, Value: v}}, "overwrite")
}

// Update replaces an existing record atomically without creating a missing key.
func (a *BoltDBAdapter) Update(p domain.Path, k, v []byte) error {
	return a.tx(true, func(tx *bolt.Tx) error {
		b, e := bucket(tx, p)
		if e != nil {
			return e
		}
		if b.Get(k) == nil {
			return domain.ErrKey
		}
		return b.Put(k, v)
	})
}

// Delete removes an existing record.
func (a *BoltDBAdapter) Delete(p domain.Path, k []byte) error {
	return a.tx(true, func(tx *bolt.Tx) error {
		b, e := bucket(tx, p)
		if e != nil {
			return e
		}
		if b.Get(k) == nil {
			return domain.ErrKey
		}
		return b.Delete(k)
	})
}

// WriteBatch applies a caller-bounded batch atomically.
func (a *BoltDBAdapter) WriteBatch(ctx context.Context, p domain.Path, records []domain.Record, mode string) error {
	if mode != "overwrite" && mode != "skip" && mode != "error" {
		return errors.New("duplicate mode must be overwrite, skip or error")
	}
	return a.tx(true, func(tx *bolt.Tx) error {
		b, e := bucket(tx, p)
		if e != nil {
			return e
		}
		for _, r := range records {
			if e = ctx.Err(); e != nil {
				return e
			}
			if b.Get(r.Key) != nil {
				if mode == "skip" {
					continue
				}
				if mode == "error" {
					return errors.New("duplicate key")
				}
			}
			if e = b.Put(r.Key, r.Value); e != nil {
				return e
			}
		}
		return ctx.Err()
	})
}

// Walk streams borrowed bytes in a read transaction. Callbacks must not retain
// bytes or start a write transaction on this database.
func (a *BoltDBAdapter) Walk(ctx context.Context, p domain.Path, q domain.Query, fn func([]byte, []byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if fn == nil {
		return errors.New("record callback required")
	}
	return a.tx(false, func(tx *bolt.Tx) error {
		b, e := bucket(tx, p)
		if e != nil {
			return e
		}
		c := b.Cursor()
		start := q.Start
		if bytes.Compare(q.Prefix, start) > 0 {
			start = q.Prefix
		}
		if bytes.Compare(q.After, start) > 0 {
			start = q.After
		}
		k, v := c.First()
		if start != nil {
			k, v = c.Seek(start)
		}
		n := 0
		for ; k != nil; k, v = c.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			if q.Limit > 0 && n >= q.Limit {
				break
			}
			if q.After != nil && bytes.Compare(k, q.After) <= 0 {
				continue
			}
			if q.End != nil && bytes.Compare(k, q.End) >= 0 {
				break
			}
			if len(q.Prefix) > 0 && !bytes.HasPrefix(k, q.Prefix) {
				break
			}
			if v == nil {
				continue
			}
			if !bytes.Contains(k, q.Contains) || !bytes.Contains(v, q.ValueContains) {
				continue
			}
			if e := fn(k, v); e != nil {
				return e
			}
			n++
		}
		return ctx.Err()
	})
}

// List copies a bounded page, detecting continuation with one lookahead key.
func (a *BoltDBAdapter) List(ctx context.Context, p domain.Path, q domain.Query) (out domain.Page, err error) {
	if q.Limit < 1 || q.Limit > 10000 {
		return out, errors.New("limit must be 1..10000")
	}
	limit := q.Limit
	q.Limit++
	err = a.Walk(ctx, p, q, func(k, v []byte) error {
		if len(out.Records) == limit {
			out.Next = bytes.Clone(out.Records[limit-1].Key)
			return nil
		}
		out.Records = append(out.Records, domain.Record{Key: bytes.Clone(k), Value: bytes.Clone(v)})
		return nil
	})
	return
}
