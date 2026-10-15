package repository

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
)

// DiffStore keeps differences and their offsets on disk, not in a growing slice.
// It owns no persistent file handles. The caller owns its temporary directory.
type DiffStore struct {
	Path, Index string
	Count       int64
	Summary     domain.DiffSummary
}

type countedWriter struct {
	w io.Writer
	n uint64
}

func (w *countedWriter) Write(p []byte) (int, error) {
	n, e := w.w.Write(p)
	w.n += uint64(n)
	return n, e
}

// BuildDiff writes a disk-backed comparison, discarding rows for summary-only results.
func BuildDiff(ctx context.Context, dir string, a, b *adapters.BoltDBAdapter) (store *DiffStore, err error) {
	return BuildDiffWithProgress(ctx, dir, a, b, nil)
}

// BuildDiffWithProgress compares first, then indexes bounded batches after all
// source read transactions have closed. Progress callbacks must return promptly.
func BuildDiffWithProgress(ctx context.Context, dir string, a, b *adapters.BoltDBAdapter, progress func(domain.DiffProgress)) (store *DiffStore, err error) {
	started := time.Now()
	data, err := os.CreateTemp(dir, "diff-*.jsonl")
	if err != nil {
		return nil, err
	}
	index, err := os.CreateTemp(dir, "diff-*.index")
	if err != nil {
		return nil, errors.Join(err, data.Close(), os.Remove(data.Name()))
	}
	store = &DiffStore{Path: data.Name(), Index: index.Name()}
	defer func() {
		err = errors.Join(err, data.Close(), index.Close())
		if err != nil {
			err = errors.Join(err, store.Remove())
			store = nil
		} else if store.Summary.Equal() || store.Summary.CompletelyDifferent() {
			err = store.Remove()
		}
	}()
	dw, iw := bufio.NewWriterSize(data, 64<<10), bufio.NewWriterSize(index, 64<<10)
	cw := &countedWriter{w: dw}
	encoder := json.NewEncoder(cw)
	var comparison domain.DiffProgress
	observe := progress
	if progress != nil {
		observe = func(p domain.DiffProgress) { comparison = p; progress(p) }
	}
	store.Summary, err = adapters.DiffWithProgress(ctx, a, b, func(entry domain.DiffEntry) error {
		var offset [8]byte
		binary.LittleEndian.PutUint64(offset[:], cw.n)
		if _, e := iw.Write(offset[:]); e != nil {
			return e
		}
		if e := encoder.Encode(entry); e != nil {
			return e
		}
		store.Count++
		return nil
	}, observe)
	if err != nil {
		return store, err
	}
	err = errors.Join(dw.Flush(), iw.Flush())
	if err != nil {
		return store, err
	}
	if !store.Summary.Equal() && !store.Summary.CompletelyDifferent() {
		err = store.buildBrowse(ctx, started, comparison.Compared, progress)
	}
	if err == nil && progress != nil {
		progress(domain.DiffProgress{Phase: "done", Compared: comparison.Compared, Differences: store.Summary.Differences, Indexed: store.Count, IndexTotal: store.Count, Elapsed: time.Since(started)})
	}
	return store, err
}

// Page loads at most limit differences without scanning preceding pages.
func (s *DiffStore) Page(ctx context.Context, start int64, limit int) (entries []domain.DiffEntry, err error) {
	if start < 0 || limit < 1 || limit > 100 {
		return nil, errors.New("invalid diff page")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if s.Summary.Equal() || s.Summary.CompletelyDifferent() {
		return nil, nil
	}
	if start >= s.Count {
		return nil, nil
	}
	index, err := os.Open(s.Index)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, index.Close()) }()
	data, err := os.Open(s.Path)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, data.Close()) }()
	if _, err = index.Seek(start*8, io.SeekStart); err != nil {
		return nil, err
	}
	var offset [8]byte
	if _, err = io.ReadFull(index, offset[:]); err != nil {
		return nil, err
	}
	if _, err = data.Seek(int64(binary.LittleEndian.Uint64(offset[:])), io.SeekStart); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bufio.NewReaderSize(data, 64<<10))
	for i := 0; i < limit && start+int64(i) < s.Count; i++ {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		var entry domain.DiffEntry
		if err = decoder.Decode(&entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *DiffStore) Remove() error {
	remove := func(path string) error {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return errors.Join(remove(s.Path), remove(s.Index), remove(s.Index+".browse"))
}
