# Performance evaluation

Measure equivalent operations on the same datasets. Record the commit or source archive, Go and dependency versions, OS, CPU, RAM, storage medium, filesystem, and cache conditions. Run each measurement repeatedly and report the distribution, not just the fastest run.

## Reproducible datasets

The generator creates a new database exclusively and writes bounded batches. Record names are deterministic; values contain repeated `v` bytes. Record count is total across the specified root buckets.

```sh
go run ./cmd/benchdata --output small.db --records 100000 --value-bytes 128 --buckets 10
go run ./cmd/benchdata --output large.db --records 1000000 --value-bytes 1024 --batch-size 500
go run ./cmd/benchdata --output large-values.db --records 10000 --value-bytes 1048576 --batch-size 32
```

Actual file sizes differ from payload sizes because of page overhead, tree layout, and free pages. Choose record counts for approximate 10 MB, 100 MB, 1 GB, and 5 GB datasets and report the measured file size. Create nested-bucket and binary-key fixtures with normal CLI commands or an additional controlled generator. Ensure enough free disk space before generating multi-gigabyte data. Interrupted generation leaves a partial, valid database containing completed batches.

## Benchmarks and profiles

```sh
go test ./repository/... -run '^$' -bench . -benchmem -count 5
go test ./repository/adapters -run '^$' -bench BenchmarkCursorWalk -benchtime 3s -memprofile heap.out -cpuprofile cpu.out
go tool pprof -alloc_space heap.out
go tool pprof cpu.out
```

Cursor-walk benchmarks compare 1,000 and 100,000 records without copying all data. Query benchmarks cover prefix, range, and 100-record pages. Transfer benchmarks cover streaming exports and repeated batch imports. Setup is excluded from timing. An overwrite import benchmark is not equivalent to importing into an empty database; specify which workload you measure.

For process-level evaluation, build a release binary and export the same bucket to a file or null device with every tool. Use `/usr/bin/time -v` on Linux to capture maximum RSS and elapsed time. On Windows, use a process monitor that records peak working set; record the sampling interval. Keep output destination and formatting comparable.

Go `B/op` reports total allocation over an operation, not peak live heap. `alloc_space` measures allocation volume; `inuse_space` approximates sampled live heap. RSS includes mapped database pages and runtime/native memory. Virtual memory size includes mapped address space and must not be presented as physical RAM usage. Streaming JSON encoding still allocates per record, so total allocated bytes can grow with record count while live memory stays bounded.

Separate cold-cache and warm-cache trials and describe how cache state was controlled. Do not infer cold-cache behavior from a second run against the same file. Avoid clearing system caches on shared machines. Include database creation/import time separately from query/export time.

## Comparison matrix

Compare BoltCLI with boltbrowser, bbolt-cli, and bolt-shell only where their versions support equivalent operations. Record unsupported operations rather than inventing substitutes. For each operation record dataset, selected bucket, record count, value size distribution, output bytes, elapsed time, records/second, peak RSS, and relevant Go allocation measurements. GUI/TUI rendering and CLI export are different workloads.

The checked-in benchmarks are a repeatable baseline. They do not demonstrate a competitive improvement or guarantee RSS limits for 5 GB databases; representative external measurements are required for those conclusions.

## Local baseline

Database comparison has a cursor benchmark:

```sh
go test ./repository/adapters -run '^$' -bench BenchmarkDiffMostlyEqual -benchmem
```

It compares 10,000 records with one changed value, including block
continuations. A short Windows/amd64 sample on the machine below
(`-benchtime=100ms`) measured about 5.25 ms/op, 11,832 B/op, and 262
allocations/op. This measures the comparison traversal, excluding disk
spooling and TUI rendering; it does not establish peak RSS or multi-GB
performance. Temporary-file usage is proportional to stored differences.

[Raw results](BENCHMARK_BASELINE.txt) were captured on 2026-10-09 using Go 1.27.2, Windows/amd64, and an AMD Ryzen 5 3500U, with `-benchtime 200ms -count 3`. These are short local samples with uncontrolled OS cache state and no competing-tool measurements.

The cursor walk allocated 480 bytes/op and 8 allocations for 1,000 records, versus 576 bytes/op and 9 allocations for 100,000 records. This demonstrates that the iterator does not allocate a record collection in these fixtures; it does not measure peak RSS. A 100-record page allocated about 24 KiB/op, including owned key/value copies. Export of 10,000 records allocated about 1 MiB in total across the operation, reflecting per-record serialization rather than a retained result set. Re-run on the evaluation hardware before drawing timing or large-database conclusions.
