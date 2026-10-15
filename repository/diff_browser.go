package repository

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/islandsexit/BltCli/domain"
	bolt "go.etcd.io/bbolt"
)

// DiffBucket describes a child with changes anywhere in its subtree.
type DiffBucket struct {
	Name  []byte
	Count uint64
}

// DiffBrowsePage is a bounded page in the changed bucket hierarchy.
type DiffBrowsePage struct {
	Buckets []DiffBucket
	Entries []domain.DiffEntry
	Next    []byte
	Count   int64
}

func diffNode(tx *bolt.Tx, path domain.Path, create bool) (*bolt.Bucket, error) {
	node := tx.Bucket([]byte("root"))
	var err error
	if node == nil && create {
		node, err = tx.CreateBucket([]byte("root"))
	}
	for _, part := range path {
		if err != nil || node == nil {
			return nil, err
		}
		children := node.Bucket([]byte("children"))
		if children == nil && create {
			children, err = node.CreateBucket([]byte("children"))
		}
		if err != nil || children == nil {
			return nil, err
		}
		if create {
			node, err = children.CreateBucketIfNotExists(part)
		} else {
			node = children.Bucket(part)
		}
	}
	return node, err
}

// indexBrowse stores only an offset; previews live once in the JSONL spool.
func indexBrowse(tx *bolt.Tx, entry domain.DiffEntry, offset uint64) error {
	if key := diffEntryKey(entry); len(key) > 0 {
		keys, err := tx.CreateBucketIfNotExists([]byte("keys"))
		if err != nil {
			return err
		}
		hash := sha256.Sum256(key)
		var indexKey [40]byte
		copy(indexKey[:32], hash[:])
		binary.BigEndian.PutUint64(indexKey[32:], offset)
		var value [8]byte
		binary.LittleEndian.PutUint64(value[:], offset)
		if err = keys.Put(indexKey[:], value[:]); err != nil {
			return err
		}
	}
	node, err := diffNode(tx, nil, true)
	if err != nil {
		return err
	}
	for depth := 0; depth <= len(entry.Path); depth++ {
		if err = node.SetSequence(node.Sequence() + 1); err != nil {
			return err
		}
		if depth < len(entry.Path) {
			children, e := node.CreateBucketIfNotExists([]byte("children"))
			if e != nil {
				return e
			}
			node, err = children.CreateBucketIfNotExists(entry.Path[depth])
			if err != nil {
				return err
			}
		}
	}
	rows, err := node.CreateBucketIfNotExists([]byte("rows"))
	if err != nil {
		return err
	}
	n, err := rows.NextSequence()
	if err != nil {
		return err
	}
	var key [8]byte
	binary.BigEndian.PutUint64(key[:], n-1)
	var value [8]byte
	binary.LittleEndian.PutUint64(value[:], offset)
	return rows.Put(key[:], value[:])
}

// buildBrowse limits each temporary write transaction to 512 rows or 4 MiB of
// input. It never holds a source transaction while writing the index.
func (s *DiffStore) buildBrowse(ctx context.Context, started time.Time, compared uint64, progress func(domain.DiffProgress)) (err error) {
	db, err := bolt.Open(s.Index+".browse", 0600, nil)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	data, err := os.Open(s.Path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, data.Close()) }()
	decoder := json.NewDecoder(bufio.NewReaderSize(data, 64<<10))
	phaseStarted := time.Now()
	var indexed int64
	report := func() {
		if progress != nil {
			progress(domain.DiffProgress{Phase: "index", Indexed: indexed, IndexTotal: s.Count, Differences: s.Summary.Differences, Compared: compared, Elapsed: time.Since(started), PhaseElapsed: time.Since(phaseStarted)})
		}
	}
	report()
	for indexed < s.Count {
		if err = ctx.Err(); err != nil {
			return err
		}
		err = db.Update(func(tx *bolt.Tx) error {
			begin := decoder.InputOffset()
			for rows := 0; rows < 512 && indexed < s.Count && decoder.InputOffset()-begin < 4<<20; rows++ {
				if err := ctx.Err(); err != nil {
					return err
				}
				offset := decoder.InputOffset()
				var entry domain.DiffEntry
				if err := decoder.Decode(&entry); err != nil {
					return err
				}
				if err := indexBrowse(tx, entry, uint64(offset)); err != nil {
					return err
				}
				indexed++
			}
			return nil
		})
		if err != nil {
			return err
		}
		report()
	}
	return ctx.Err()
}

// Browse seeks directly to child and local difference pages in the disk index.
func (s *DiffStore) Browse(ctx context.Context, path domain.Path, after, contains []byte, start int64, limit int) (page DiffBrowsePage, err error) {
	if start < 0 || limit < 1 || limit > 100 {
		return page, errors.New("invalid diff page")
	}
	if err = ctx.Err(); err != nil {
		return page, err
	}
	if s.Summary.Equal() {
		return page, nil
	}
	db, err := bolt.Open(s.Index+".browse", 0600, &bolt.Options{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	data, err := os.Open(s.Path)
	if err != nil {
		return page, err
	}
	defer func() { err = errors.Join(err, data.Close()) }()
	err = db.View(func(tx *bolt.Tx) error {
		node, err := diffNode(tx, path, false)
		if err != nil || node == nil {
			return err
		}
		if children := node.Bucket([]byte("children")); children != nil {
			c := children.Cursor()
			k, _ := c.First()
			if after != nil {
				k, _ = c.Seek(after)
				if bytes.Equal(k, after) {
					k, _ = c.Next()
				}
			}
			for ; k != nil; k, _ = c.Next() {
				if err := ctx.Err(); err != nil {
					return err
				}
				if !bytes.Contains(k, contains) {
					continue
				}
				if len(page.Buckets) == limit {
					page.Next = bytes.Clone(page.Buckets[len(page.Buckets)-1].Name)
					break
				}
				page.Buckets = append(page.Buckets, DiffBucket{bytes.Clone(k), children.Bucket(k).Sequence()})
			}
		}
		if rows := node.Bucket([]byte("rows")); rows != nil {
			page.Count = int64(rows.Sequence())
			var key [8]byte
			binary.BigEndian.PutUint64(key[:], uint64(start))
			c := rows.Cursor()
			for k, v := c.Seek(key[:]); k != nil && len(page.Entries) < limit; k, v = c.Next() {
				if err := ctx.Err(); err != nil {
					return err
				}
				if len(v) != 8 {
					return errors.New("invalid diff offset")
				}
				if _, err := data.Seek(int64(binary.LittleEndian.Uint64(v)), io.SeekStart); err != nil {
					return err
				}
				var entry domain.DiffEntry
				if err := json.NewDecoder(data).Decode(&entry); err != nil {
					return err
				}
				page.Entries = append(page.Entries, entry)
			}
		}
		return nil
	})
	return page, err
}
