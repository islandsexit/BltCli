package adapters

import (
	"bytes"
	"context"
	"errors"

	"github.com/islandsexit/BltCli/domain"
	bolt "go.etcd.io/bbolt"
)

var errSearchPause = errors.New("search scan block complete")
var errSearchPage = errors.New("search page complete")

func copyPath(path domain.Path) domain.Path {
	out := make(domain.Path, len(path))
	for i, p := range path {
		out[i] = bytes.Clone(p)
	}
	return out
}
func position(path domain.Path, key []byte) *domain.SearchPosition {
	return &domain.SearchPosition{Path: copyPath(path), Key: bytes.Clone(key)}
}
func comparePosition(path domain.Path, key []byte, after *domain.SearchPosition) int {
	if after == nil {
		return 1
	}
	a, b := len(path), len(after.Path)
	if key != nil {
		a++
	}
	if after.Key != nil {
		b++
	}
	for i := 0; i < min(a, b); i++ {
		left, right := key, after.Key
		if i < len(path) {
			left = path[i]
		}
		if i < len(after.Path) {
			right = after.Path[i]
		}
		if c := bytes.Compare(left, right); c != 0 {
			return c
		}
	}
	return a - b
}
func pathPrefix(path, prefix domain.Path) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i := range prefix {
		if !bytes.Equal(path[i], prefix[i]) {
			return false
		}
	}
	return true
}

// Search walks depth first in cursor order, retaining only a result page. Read
// transactions end after at most 1,024 inspected entries, bounding engine bucket
// caches and allowing page reclamation. Concurrent writes can change later blocks.
func (a *BoltDBAdapter) Search(ctx context.Context, q domain.SearchQuery) (out domain.SearchPage, err error) {
	if q.Kind != "records" && q.Kind != "buckets" {
		return out, errors.New("search kind must be records or buckets")
	}
	if q.Limit < 1 || q.Limit > 10000 {
		return out, errors.New("limit must be 1..10000")
	}
	after := q.After
	for {
		if e := ctx.Err(); e != nil {
			return out, e
		}
		var checkpoint *domain.SearchPosition
		scanned := 0
		err = a.tx(false, func(tx *bolt.Tx) error {
			event := func(path domain.Path, key, value []byte, isBucket bool) error {
				if e := ctx.Err(); e != nil {
					return e
				}
				if comparePosition(path, key, after) <= 0 {
					return nil
				}
				scanned++
				match := q.Kind == "buckets" && isBucket && bytes.Contains(path[len(path)-1], q.Contains) || q.Kind == "records" && !isBucket && bytes.Contains(key, q.Contains) && bytes.Contains(value, q.ValueContains)
				if match {
					if len(out.Hits) == q.Limit {
						last := out.Hits[len(out.Hits)-1]
						out.Next = position(last.Path, last.Key)
						return errSearchPage
					}
					out.Hits = append(out.Hits, domain.SearchHit{Path: copyPath(path), Key: bytes.Clone(key), Value: bytes.Clone(value[:min(256, len(value))]), ValueBytes: len(value)})
				}
				if scanned >= 1024 {
					checkpoint = position(path, key)
					return errSearchPause
				}
				return nil
			}
			var visit func(*bolt.Bucket, domain.Path) error
			visit = func(b *bolt.Bucket, path domain.Path) error {
				if e := ctx.Err(); e != nil {
					return e
				}
				if after != nil && !pathPrefix(after.Path, path) && comparePosition(path, nil, after) <= 0 {
					return nil
				}
				if e := event(path, nil, nil, true); e != nil {
					return e
				}
				c := b.Cursor()
				k, v := c.First()
				if after != nil && pathPrefix(after.Path, path) {
					start := after.Key
					if len(after.Path) > len(path) {
						start = after.Path[len(path)]
					}
					if start != nil {
						k, v = c.Seek(start)
					}
				}
				for ; k != nil; k, v = c.Next() {
					if e := ctx.Err(); e != nil {
						return e
					}
					if v == nil {
						if e := visit(b.Bucket(k), append(path, k)); e != nil {
							return e
						}
					} else if e := event(path, k, v, false); e != nil {
						return e
					}
				}
				return nil
			}
			c := tx.Cursor()
			k, _ := c.First()
			if after != nil && len(after.Path) > 0 {
				k, _ = c.Seek(after.Path[0])
			}
			for ; k != nil; k, _ = c.Next() {
				if e := visit(tx.Bucket(k), domain.Path{k}); e != nil {
					return e
				}
			}
			return nil
		})
		if errors.Is(err, errSearchPage) {
			return out, nil
		}
		if errors.Is(err, errSearchPause) {
			after = checkpoint
			continue
		}
		return out, err
	}
}
