// Package config parses command flags without global state.
package config

import (
	"flag"
	"fmt"
	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/utils"
	"io"
	"time"
)

// Config contains per-operation resource bounds and input encodings.
type Config struct {
	DB, Bucket, Prefix, Start, End, After, Input, Output, Duplicates string
	Limit, BatchSize, MaxRecord, BatchBytes                          int
	ReadOnly, Yes                                                    bool
	Timeout                                                          time.Duration
	Args                                                             []string
	LimitSet                                                         bool
	Contains, ValueContains                                          string
}

// Parse parses flags preceding positional arguments.
func Parse(args []string, w io.Writer) (Config, error) {
	c := Config{}
	f := flag.NewFlagSet("boltcli", flag.ContinueOnError)
	f.SetOutput(w)
	f.Usage = func() {
		fmt.Fprintln(f.Output(), "Usage of boltcli:")
		f.PrintDefaults()
		fmt.Fprintln(f.Output(), "\nSignature: Written by Dem4ik")
	}
	f.StringVar(&c.DB, "db", "", "database file")
	f.StringVar(&c.Bucket, "bucket", "", "slash-separated bucket path")
	f.StringVar(&c.Prefix, "prefix", "", "binary-safe key prefix")
	f.StringVar(&c.Contains, "contains", "", "key or bucket name contains these bytes")
	f.StringVar(&c.ValueContains, "value-contains", "", "record value contains these bytes (requires a scan)")
	f.StringVar(&c.Start, "start", "", "inclusive start key")
	f.StringVar(&c.End, "end", "", "exclusive end key")
	f.StringVar(&c.After, "after", "", "exclusive pagination key")
	f.StringVar(&c.Input, "input", "-", "input JSONL file (- for stdin)")
	f.StringVar(&c.Output, "output", "-", "output file (- for stdout; refuses overwrite)")
	f.StringVar(&c.Duplicates, "duplicates", "overwrite", "overwrite, skip or error")
	f.IntVar(&c.Limit, "limit", 100, "maximum results (1..10000)")
	f.IntVar(&c.BatchSize, "batch-size", 500, "records per import transaction")
	f.IntVar(&c.MaxRecord, "max-record-bytes", 16<<20, "maximum JSONL line size")
	f.IntVar(&c.BatchBytes, "batch-bytes", 8<<20, "decoded bytes per import transaction")
	f.BoolVar(&c.ReadOnly, "read-only", false, "open database read-only")
	f.BoolVar(&c.Yes, "yes", false, "confirm destructive operation")
	f.DurationVar(&c.Timeout, "timeout", 0, "operation timeout (0 disables)")
	if e := f.Parse(args); e != nil {
		return c, e
	}
	c.Args = f.Args()
	f.Visit(func(f *flag.Flag) {
		if f.Name == "limit" {
			c.LimitSet = true
		}
	})
	if c.Limit < 1 || c.Limit > 10000 || c.BatchSize < 1 || c.BatchSize > 10000 || c.MaxRecord < 1 || c.MaxRecord > 256<<20 || c.BatchBytes < 1 || c.BatchBytes > 256<<20 || c.Timeout < 0 {
		return c, fmt.Errorf("invalid resource limit")
	}
	return c, nil
}

// Path parses the selected nested bucket.
func (c Config) Path() (domain.Path, error) { return utils.ParsePath(c.Bucket) }

// Query validates binary-safe query bounds.
func (c Config) Query() (domain.Query, error) {
	q := domain.Query{Limit: c.Limit}
	for _, item := range []struct {
		s string
		p *[]byte
	}{{c.Prefix, &q.Prefix}, {c.Start, &q.Start}, {c.End, &q.End}, {c.After, &q.After}, {c.Contains, &q.Contains}, {c.ValueContains, &q.ValueContains}} {
		if item.s != "" {
			v, e := utils.Decode(item.s)
			if e != nil {
				return q, e
			}
			*item.p = v
		}
	}
	return q, nil
}
