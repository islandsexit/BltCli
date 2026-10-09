// Package cli implements script-friendly commands.
package cli

import (
	"bltCli/config"
	"bltCli/domain"
	"bltCli/repository"
	"bltCli/repository/adapters"
	"bltCli/tui"
	"bltCli/utils"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// Help describes the public command interface.
const Help = `BoltCLI — binary-safe BoltDB manager

Usage: boltcli <group> <operation> [flags] [arguments]
       boltcli tui [flags] [database files...]

  db create <path>             exclusively create a database
  db info --db <path>          inspect file and engine statistics
  db stats --db <path>         count records/buckets and inspect page usage
  db delete --yes <path>       delete a closed database
  bucket list --db <path> [--bucket parent] [--limit N] [--after key]
  bucket create --db <path> --bucket <path>
  bucket delete --db <path> --bucket <path> --yes
  bucket stats --db <path> --bucket <path>
  record get|exists|delete|export --db <path> --bucket <path> <key>
  record put|update --db <path> --bucket <path> <key> <value>
  record import --db <path> --bucket <path> [--input file]
  record list|search --db <path> --bucket <path> [query flags]
  export --db <path> --bucket <path> [--output file] [query flags]
  import --db <path> --bucket <path> [--input file] [--batch-size N]
  tui [--read-only] [database files...]

Flags precede arguments. Keys/values accept text:, hex:, base64: or plain text.
Bucket paths use / separators; encode slashes inside names as hex.
Query flags: --limit, --prefix, --contains, --value-contains, --start, --end, --after.
Export streams all matches unless --limit is explicitly supplied.
JSONL records contain base64 key/value strings. Diagnostics go to stderr.
Use <command> --help for all flag descriptions.
TUI sessions support opening, switching and closing multiple databases.

Signature: Written by Dem4ik
`

// Run executes a command with injectable streams and cancellation.
func Run(ctx context.Context, args []string, in io.Reader, out, diag io.Writer) (err error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, err = io.WriteString(out, Help)
		return
	}
	group := args[0]
	op := ""
	rest := args[1:]
	if group == "db" || group == "bucket" || group == "record" {
		if len(rest) == 0 {
			return errors.New("operation required; use --help")
		}
		op = rest[0]
		rest = rest[1:]
	}
	c, e := config.Parse(rest, diag)
	if errors.Is(e, flag.ErrHelp) {
		return nil
	}
	if e != nil {
		return e
	}
	if group == "record" && op == "import" {
		group = "import"
		op = ""
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if group == "tui" {
		paths := c.Args
		if c.DB != "" {
			paths = append([]string{c.DB}, paths...)
		}
		return tui.Run(ctx, paths, c.ReadOnly)
	}
	count := func(n int) error {
		if len(c.Args) != n {
			return fmt.Errorf("expected %d positional arguments, got %d", n, len(c.Args))
		}
		return nil
	}
	if group == "db" && (op == "create" || op == "delete") {
		if e = count(1); e != nil {
			return e
		}
		if op == "create" {
			db, e := adapters.Open(c.Args[0], true, c.ReadOnly)
			if e != nil {
				return e
			}
			return db.Close()
		}
		if !c.Yes {
			return errors.New("database deletion requires --yes")
		}
		if c.ReadOnly {
			return errors.New("database deletion is unavailable in read-only mode")
		}
		db, e := adapters.Open(c.Args[0], false, false)
		if e != nil {
			return e
		}
		if e = db.Close(); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		return os.Remove(c.Args[0])
	}
	if group != "db" && group != "bucket" && group != "record" && group != "import" && group != "export" {
		return fmt.Errorf("unknown command %q", group)
	}
	if c.DB == "" {
		return errors.New("--db is required")
	}
	read := group == "export" || group == "db" && (op == "info" || op == "stats") || group == "bucket" && (op == "list" || op == "stats") || group == "record" && (op == "get" || op == "export" || op == "exists" || op == "list" || op == "search")
	db, e := adapters.Open(c.DB, false, c.ReadOnly || read)
	if e != nil {
		return e
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if e = ctx.Err(); e != nil {
		return e
	}
	p, e := c.Path()
	if e != nil {
		return e
	}
	q, e := c.Query()
	if e != nil {
		return e
	}
	enc := json.NewEncoder(out)
	switch group {
	case "db":
		if op == "stats" {
			if e = count(0); e != nil {
				return e
			}
			stats, e := db.Analyze(ctx)
			if e != nil {
				return e
			}
			return enc.Encode(stats)
		}
		if op != "info" {
			return fmt.Errorf("unknown db operation %q", op)
		}
		if e = count(0); e != nil {
			return e
		}
		info, e := db.Info()
		if e != nil {
			return e
		}
		return enc.Encode(info)
	case "bucket":
		if e = count(0); e != nil {
			return e
		}
		switch op {
		case "list":
			names, e := db.FindBuckets(ctx, p, q)
			if e != nil {
				return e
			}
			for _, n := range names {
				if e = enc.Encode(struct {
					Name []byte `json:"name"`
				}{n}); e != nil {
					return e
				}
			}
			if len(names) == c.Limit {
				_, e = fmt.Fprintf(diag, "continue with --after base64:%s\n", base64.StdEncoding.EncodeToString(names[len(names)-1]))
			}
			return e
		case "create":
			return db.CreateBucket(p)
		case "delete":
			if !c.Yes {
				return errors.New("bucket deletion requires --yes")
			}
			return db.DeleteBucket(p)
		case "stats":
			s, e := db.BucketStats(p)
			if e != nil {
				return e
			}
			return enc.Encode(s)
		}
	case "record":
		switch op {
		case "get", "export", "exists", "delete", "put", "update":
			n := 1
			if op == "put" || op == "update" {
				n = 2
			}
			if e = count(n); e != nil {
				return e
			}
			k, e := utils.Decode(c.Args[0])
			if e != nil {
				return e
			}
			if op == "put" || op == "update" {
				v, e := utils.Decode(c.Args[1])
				if e != nil {
					return e
				}
				if op == "update" {
					return db.Update(p, k, v)
				}
				return db.Put(p, k, v)
			}
			if op == "delete" {
				if !c.Yes {
					return errors.New("record deletion requires --yes")
				}
				return db.Delete(p, k)
			}
			v, e := db.Get(p, k)
			if op == "exists" {
				if e != nil && !errors.Is(e, domain.ErrKey) {
					return e
				}
				return enc.Encode(struct {
					Exists bool `json:"exists"`
				}{e == nil})
			}
			if e != nil {
				return e
			}
			if op == "export" && c.Output != "-" {
				f, e := os.OpenFile(c.Output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if e != nil {
					return e
				}
				return errors.Join(json.NewEncoder(f).Encode(domain.Record{Key: k, Value: v}), f.Close())
			}
			return enc.Encode(domain.Record{Key: k, Value: v})
		case "list", "search":
			if e = count(0); e != nil {
				return e
			}
			page, e := db.List(ctx, p, q)
			if e != nil {
				return e
			}
			for _, r := range page.Records {
				if e = enc.Encode(r); e != nil {
					return e
				}
			}
			if page.Next != nil {
				_, e = fmt.Fprintf(diag, "continue with --after base64:%s\n", base64.StdEncoding.EncodeToString(page.Next))
			}
			return e
		}
	case "export":
		if e = count(0); e != nil {
			return e
		}
		if !c.LimitSet {
			q.Limit = 0
		}
		w := out
		if c.Output != "-" {
			f, e := os.OpenFile(c.Output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return e
			}
			defer func() { err = errors.Join(err, f.Close()) }()
			w = f
		}
		return repository.Export(ctx, db, p, q, w)
	case "import":
		if e = count(0); e != nil {
			return e
		}
		r := in
		if c.Input != "-" {
			f, e := os.Open(c.Input)
			if e != nil {
				return e
			}
			defer func() { err = errors.Join(err, f.Close()) }()
			r = f
		}
		n, e := repository.Import(ctx, db, p, r, repository.ImportOptions{BatchSize: c.BatchSize, BatchBytes: c.BatchBytes, MaxRecordBytes: c.MaxRecord, Duplicates: c.Duplicates})
		_, de := fmt.Fprintf(diag, "committed input records: %d\n", n)
		return errors.Join(e, de)
	}
	return fmt.Errorf("unknown %s operation %q", group, op)
}
