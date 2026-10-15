package domain

import "time"

// DiffProgress is a bounded snapshot. Byte totals estimate live file contents;
// only indexing has an exact total, since no counting scan precedes comparison.
type DiffProgress struct {
	Phase                          string
	ProcessedBytes, EstimatedBytes uint64
	Compared, Differences          uint64
	Indexed, IndexTotal            int64
	Elapsed                        time.Duration
	PhaseElapsed                   time.Duration
}

// DiffEntry contains bounded previews; presence is separate from empty values.
type DiffEntry struct {
	Path                        Path   `json:"path"`
	Key                         []byte `json:"key,omitempty"`
	Kind                        string `json:"kind"`
	BasePresent, OtherPresent   bool
	BaseBucket, OtherBucket     bool
	Base, Other                 []byte
	BaseBytes, OtherBytes       int
	ValueOffset                 int
	BaseSequence, OtherSequence uint64
}

// DiffSummary describes logical contents, rather than file layout or free pages.
type DiffSummary struct {
	Differences, EqualRecords, EqualBuckets, Records uint64
}

func (s DiffSummary) Equal() bool { return s.Differences == 0 }

// CompletelyDifferent means no identical record exists at the same path/key.
// For databases without records, it means no common bucket exists.
func (s DiffSummary) CompletelyDifferent() bool {
	return s.Differences > 0 && s.EqualRecords == 0 && (s.Records > 0 || s.EqualBuckets == 0)
}
