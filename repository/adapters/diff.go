package adapters

import (
	"bytes"
	"context"
	"errors"
	"reflect"

	"github.com/islandsexit/BltCli/domain"
	bolt "go.etcd.io/bbolt"
)

var errDiffBlock = errors.New("diff block complete")

// Lifetime locks have a consistent order, including a handle compared to itself.
func diffTransactions(a, b *BoltDBAdapter, fn func(*bolt.Tx, *bolt.Tx) error) (err error) {
	first, second := a, b
	if reflect.ValueOf(first).Pointer() > reflect.ValueOf(second).Pointer() {
		first, second = second, first
	}
	first.mu.RLock()
	defer first.mu.RUnlock()
	if first != second {
		second.mu.RLock()
		defer second.mu.RUnlock()
	}
	if first.db == nil || second.db == nil {
		return domain.ErrClosed
	}
	left, err := first.db.Begin(false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, left.Rollback()) }()
	right := left
	if first != second {
		right, err = second.db.Begin(false)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, right.Rollback()) }()
	}
	if first == a {
		return fn(left, right)
	}
	return fn(right, left)
}

// Diff merge-walks binary-sorted cursors. Each paired read transaction inspects
// at most 1024 entries. Values are compared in mapped memory and only bounded
// previews reach emit. Concurrent writers can change contents between blocks.
// Entries reference transaction-owned bytes: emit must serialize or copy them
// before returning. Preview offsets identify context near the first difference.
func Diff(ctx context.Context, base, other *BoltDBAdapter, emit func(domain.DiffEntry) error) (summary domain.DiffSummary, err error) {
	if base == other {
		return summary, base.tx(false, func(*bolt.Tx) error { return ctx.Err() })
	}
	var after *domain.SearchPosition
	for {
		if err = ctx.Err(); err != nil {
			return summary, err
		}
		var checkpoint *domain.SearchPosition
		scanned := 0
		err = diffTransactions(base, other, func(left, right *bolt.Tx) error {
			event := func(path domain.Path, key, a, b []byte, ap, bp bool, kind string, as, bs uint64, ab, bb bool) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if comparePosition(path, key, after) <= 0 {
					return nil
				}
				scanned++
				valueEqual, mismatch := true, 0
				if kind == "record" && ap && bp {
					var err error
					valueEqual, mismatch, err = diffValues(ctx, a, b)
					if err != nil {
						return err
					}
				} else {
					valueEqual = bytes.Equal(a, b)
				}
				equal := ap && bp && valueEqual && as == bs && kind != "type"
				if kind == "record" || kind == "type" {
					summary.Records++
					if equal {
						summary.EqualRecords++
					}
				} else if equal {
					summary.EqualBuckets++
				}
				if !equal {
					summary.Differences++
					offset := 0
					if kind == "record" && ap && bp {
						offset = max(0, mismatch-128)
					}
					leftStart, rightStart := min(offset, len(a)), min(offset, len(b))
					entry := domain.DiffEntry{Path: path, Key: key, Kind: kind, BasePresent: ap, OtherPresent: bp, Base: a[leftStart:min(len(a), leftStart+4096)], Other: b[rightStart:min(len(b), rightStart+4096)], BaseBytes: len(a), OtherBytes: len(b), ValueOffset: offset, BaseSequence: as, OtherSequence: bs, BaseBucket: ab, OtherBucket: bb}
					if err := emit(entry); err != nil {
						return err
					}
				}
				if scanned >= 1024 {
					checkpoint = position(path, key)
					return errDiffBlock
				}
				return nil
			}
			var visit func(*bolt.Bucket, *bolt.Bucket, domain.Path, []byte, []byte, bool, bool) error
			visit = func(a, b *bolt.Bucket, path domain.Path, av, bv []byte, ap, bp bool) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if after != nil && !pathPrefix(after.Path, path) && comparePosition(path, nil, after) <= 0 {
					return nil
				}
				kind := "bucket"
				var as, bs uint64
				if a != nil {
					as = a.Sequence()
				}
				if b != nil {
					bs = b.Sequence()
				}
				if ap && a == nil || bp && b == nil {
					kind = "type"
				}
				if err := event(path, nil, av, bv, ap, bp, kind, as, bs, a != nil, b != nil); err != nil {
					return err
				}
				var ac, bc *bolt.Cursor
				if a != nil {
					ac = a.Cursor()
				}
				if b != nil {
					bc = b.Cursor()
				}
				return mergeDiffCursors(ctx, ac, bc, path, after, func(key, av, bv []byte, ap, bp bool) error {
					var ab, bb *bolt.Bucket
					if ap && av == nil {
						ab = a.Bucket(key)
					}
					if bp && bv == nil {
						bb = b.Bucket(key)
					}
					if ab != nil || bb != nil {
						return visit(ab, bb, append(path, key), av, bv, ap, bp)
					}
					return event(path, key, av, bv, ap, bp, "record", 0, 0, false, false)
				})
			}
			return mergeDiffCursors(ctx, left.Cursor(), right.Cursor(), nil, after, func(key, av, bv []byte, ap, bp bool) error {
				return visit(left.Bucket(key), right.Bucket(key), domain.Path{key}, nil, nil, ap, bp)
			})
		})
		if errors.Is(err, errDiffBlock) {
			after = checkpoint
			continue
		}
		return summary, errors.Join(err, ctx.Err())
	}
}

func diffValues(ctx context.Context, a, b []byte) (bool, int, error) {
	length := min(len(a), len(b))
	for start := 0; start < length; start += 64 << 10 {
		if err := ctx.Err(); err != nil {
			return false, 0, err
		}
		end := min(length, start+(64<<10))
		if !bytes.Equal(a[start:end], b[start:end]) {
			for i := start; i < end; i++ {
				if a[i] != b[i] {
					return false, i, nil
				}
			}
		}
	}
	return len(a) == len(b), length, nil
}

func mergeDiffCursors(ctx context.Context, a, b *bolt.Cursor, path domain.Path, after *domain.SearchPosition, event func([]byte, []byte, []byte, bool, bool) error) error {
	var start []byte
	if after != nil && pathPrefix(after.Path, path) {
		start = after.Key
		if len(after.Path) > len(path) {
			start = after.Path[len(path)]
		}
	}
	first := func(c *bolt.Cursor) ([]byte, []byte) {
		if c == nil {
			return nil, nil
		}
		if start != nil {
			return c.Seek(start)
		}
		return c.First()
	}
	ak, av := first(a)
	bk, bv := first(b)
	for ak != nil || bk != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		cmp := bytes.Compare(ak, bk)
		ap, bp := ak != nil && (bk == nil || cmp <= 0), bk != nil && (ak == nil || cmp >= 0)
		key := ak
		if !ap {
			key = bk
		}
		var left, right []byte
		if ap {
			left = av
		}
		if bp {
			right = bv
		}
		if err := event(key, left, right, ap, bp); err != nil {
			return err
		}
		if ap {
			ak, av = a.Next()
		}
		if bp {
			bk, bv = b.Next()
		}
	}
	return nil
}
