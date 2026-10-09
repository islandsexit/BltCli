// Package domain defines database operations independently of bbolt.
package domain

import (
	"context"
	"errors"
)

var (
	ErrBucket = errors.New("bucket not found")
	ErrKey    = errors.New("key not found")
	ErrClosed = errors.New("database closed")
)

// Path is a sequence of binary bucket names.
type Path [][]byte

// Record owns its binary key and value. JSON encodes both as base64.
type Record struct {
	Key   []byte `json:"key"`
	Value []byte `json:"value"`
}

// Query has inclusive Start and exclusive End and After boundaries. Contains
// and ValueContains are case-sensitive byte substrings combined with other bounds.
type Query struct {
	Prefix, Start, End, After []byte
	Contains, ValueContains   []byte
	Limit                     int
}

// Page is a bounded result with an exclusive continuation key.
type Page struct {
	Records []Record
	Next    []byte
}

// SearchPosition is an exclusive DFS checkpoint. A nil Key denotes a bucket.
type SearchPosition struct {
	Path Path
	Key  []byte
}

// SearchQuery searches every root and nested bucket using binary substrings.
type SearchQuery struct {
	Kind                    string
	Contains, ValueContains []byte
	Limit                   int
	After                   *SearchPosition
}

// SearchHit owns its path, key and bounded value preview.
type SearchHit struct {
	Path       Path
	Key, Value []byte
	ValueBytes int
}

// SearchPage contains bounded results and a continuation checkpoint.
type SearchPage struct {
	Hits []SearchHit
	Next *SearchPosition
}

// Info contains inexpensive engine statistics.
type Info struct {
	Path                                      string
	Size                                      int64
	ReadOnly                                  bool
	FreePages, PendingPages, OpenTransactions int
	PageSize                                  int
	FreeBytes                                 int
}

// BucketInfo contains engine bucket statistics.
type BucketInfo struct{ Keys, Records, Buckets, Depth, BranchPages, LeafPages, OverflowPages, AllocatedBytes, UsedBytes int }

// Analysis aggregates page statistics across all root buckets and descendants.
type Analysis struct{ RootBuckets, Buckets, Records, BranchPages, LeafPages, OverflowPages, AllocatedBytes, UsedBytes int }

// BoltDbAdapter is the storage boundary shared by CLI and TUI.
type BoltDbAdapter interface {
	Close() error
	Info() (Info, error)
	Analyze(context.Context) (Analysis, error)
	Buckets(context.Context, Path, []byte, int) ([][]byte, error)
	FindBuckets(context.Context, Path, Query) ([][]byte, error)
	Search(context.Context, SearchQuery) (SearchPage, error)
	CreateBucket(Path) error
	DeleteBucket(Path) error
	BucketStats(Path) (BucketInfo, error)
	Get(Path, []byte) ([]byte, error)
	Put(Path, []byte, []byte) error
	Update(Path, []byte, []byte) error
	Delete(Path, []byte) error
	List(context.Context, Path, Query) (Page, error)
	Walk(context.Context, Path, Query, func([]byte, []byte) error) error
	WriteBatch(context.Context, Path, []Record, string) error
}
