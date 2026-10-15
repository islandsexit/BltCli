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

	"github.com/islandsexit/BltCli/domain"
	bolt "go.etcd.io/bbolt"
)

func diffEntryKey(e domain.DiffEntry) []byte {
	if e.Key != nil {
		return e.Key
	}
	if e.Kind == "type" && len(e.Path) > 0 {
		return e.Path[len(e.Path)-1]
	}
	return nil
}

// DiffSearchPage owns a bounded result page and an exclusive continuation token.
type DiffSearchPage struct {
	Entries []domain.DiffEntry
	Next    []byte
}

// Search finds changed keys across all bucket paths in a completed comparison.
// Exact queries seek a hash index and verify the original binary key. Contains
// queries scan only the disk spool, with bounded memory and cancellation checks.
func (s *DiffStore) Search(ctx context.Context, key []byte, mode string, after []byte, limit int) (page DiffSearchPage, err error) {
	if limit < 1 || limit > 100 || (mode != "exact" && mode != "contains") {
		return page, errors.New("invalid diff search")
	}
	if err = ctx.Err(); err != nil {
		return page, err
	}
	if s.Summary.Equal() {
		return page, nil
	}
	data, err := os.Open(s.Path)
	if err != nil {
		return page, err
	}
	defer func() { err = errors.Join(err, data.Close()) }()
	if mode == "exact" {
		if after != nil && len(after) != 40 {
			return page, errors.New("invalid diff search token")
		}
		var db *bolt.DB
		db, err = bolt.Open(s.Index+".browse", 0600, &bolt.Options{ReadOnly: true})
		if err != nil {
			return page, err
		}
		defer func() { err = errors.Join(err, db.Close()) }()
		hash := sha256.Sum256(key)
		var last []byte
		err = db.View(func(tx *bolt.Tx) error {
			keys := tx.Bucket([]byte("keys"))
			if keys == nil {
				return nil
			}
			c := keys.Cursor()
			k, v := c.Seek(hash[:])
			if after != nil {
				if !bytes.HasPrefix(after, hash[:]) {
					return errors.New("diff search token does not match key")
				}
				k, v = c.Seek(after)
				if bytes.Equal(k, after) {
					k, v = c.Next()
				}
			}
			for ; k != nil && bytes.HasPrefix(k, hash[:]); k, v = c.Next() {
				if err := ctx.Err(); err != nil {
					return err
				}
				if len(v) != 8 {
					return errors.New("invalid diff offset")
				}
				if _, err := data.Seek(int64(binary.LittleEndian.Uint64(v)), io.SeekStart); err != nil {
					return err
				}
				var e domain.DiffEntry
				if err := json.NewDecoder(data).Decode(&e); err != nil {
					return err
				}
				if !bytes.Equal(diffEntryKey(e), key) {
					continue
				}
				if len(page.Entries) == limit {
					page.Next = last
					break
				}
				page.Entries = append(page.Entries, e)
				last = bytes.Clone(k)
			}
			return nil
		})
		return page, err
	}
	var start int64
	if after != nil {
		if len(after) != 8 {
			return page, errors.New("invalid diff search token")
		}
		position := binary.BigEndian.Uint64(after)
		if position >= uint64(s.Count) {
			return page, errors.New("invalid diff search token")
		}
		start = int64(position) + 1
		if start < 0 {
			return page, errors.New("invalid diff search token")
		}
	}
	if start >= s.Count {
		return page, nil
	}
	index, err := os.Open(s.Index)
	if err != nil {
		return page, err
	}
	defer func() { err = errors.Join(err, index.Close()) }()
	var offset [8]byte
	if _, err = index.ReadAt(offset[:], start*8); err != nil {
		return page, err
	}
	if _, err = data.Seek(int64(binary.LittleEndian.Uint64(offset[:])), io.SeekStart); err != nil {
		return page, err
	}
	decoder := json.NewDecoder(bufio.NewReaderSize(data, 64<<10))
	var last int64
	for pos := start; pos < s.Count; pos++ {
		if err = ctx.Err(); err != nil {
			return page, err
		}
		var e domain.DiffEntry
		if err = decoder.Decode(&e); err != nil {
			return page, err
		}
		candidate := diffEntryKey(e)
		if candidate == nil || !bytes.Contains(candidate, key) {
			continue
		}
		if len(page.Entries) == limit {
			var token [8]byte
			binary.BigEndian.PutUint64(token[:], uint64(last))
			page.Next = bytes.Clone(token[:])
			break
		}
		page.Entries = append(page.Entries, e)
		last = pos
	}
	return page, nil
}
