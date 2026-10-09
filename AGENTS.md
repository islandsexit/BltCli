# AGENTS.md — BoltDB CLI Manager

## 1. Project Overview

This project is a high-performance, memory-efficient command-line application for managing BoltDB databases, developed as part of a bachelor's thesis.

**Thesis topic:** Development of a CLI Application for Managing BoltDB Databases.

The application provides tools for creating, opening, inspecting, modifying, searching, filtering, importing, and exporting BoltDB data.

The primary objective is to provide comprehensive database management capabilities while minimizing memory consumption, especially when working with multi-gigabyte databases on memory-constrained systems.

The application is intended for:

* Software developers
* DevOps engineers
* System administrators
* Database maintainers

### Primary Competitive Advantage

Unlike CLI tools that materialize large collections of records in memory, this application must process database contents incrementally.

Memory efficiency is a first-class architectural requirement, not an optional optimization.

---

## 2. Technology Stack

* **Language:** Go
* **Database engine:** `go.etcd.io/bbolt`
* **Interface:** Command-line interface (CLI)
* **Supported platforms:** Linux, macOS, Windows
* **Testing:** Go standard testing framework
* **Profiling:** `runtime/pprof`, Go benchmarks
* **Preferred dependencies:** Go standard library wherever practical

Use third-party dependencies only when they provide clear benefits.

Do not introduce unnecessary frameworks, abstractions, or dependencies.

## 3. Core Functionality

### 3.1 Database Management

The application must support:

* Creating new BoltDB database files.
* Opening existing database files.
* Closing opened databases.
* Deleting database files with explicit confirmation.
* Displaying database information.
* Managing multiple databases within a single application session.
* Listing currently opened databases.
* Switching between active databases.
* Opening databases in read-only mode.

Multiple databases must maintain independent handles and lifecycle states.

### 3.2 Bucket Management

Support the following operations:

* List root-level and nested buckets.
* Create buckets.
* Delete buckets.
* Inspect bucket contents.
* Display bucket statistics.
* Navigate nested bucket structures.

Bucket operations must respect bbolt transaction semantics.

### 3.3 Record Management

Support standard CRUD operations:

* Create records.
* Read records by key.
* Update existing records.
* Delete records.
* Check whether a key exists.
* List records.
* Search records by key.
* Filter records by key prefix.
* Iterate over key ranges.
* Paginate large result sets.

Keys and values must be treated as binary data.

Do not assume that every key or value contains valid UTF-8 text.

### 3.4 Import and Export

Support:

* Exporting individual records.
* Exporting selected key ranges.
* Exporting entire buckets incrementally.
* Importing individual records.
* Importing records in batches.
* Streaming large imports and exports.
* Configurable handling of duplicate keys.

Where applicable, support JSON and binary-safe formats.

Binary data must be encoded explicitly when exported to text formats.

Avoid loading entire import or export files into memory.

### 3.5 Database Analysis

Provide tools for inspecting:

* Database file size.
* Database metadata.
* Number of buckets.
* Number of records.
* Nested bucket structure.
* Bucket statistics.
* Page allocation statistics.
* Free and pending pages.
* Database storage utilization.

Use existing bbolt metadata and statistics where possible.

Avoid unnecessary full-database scans.

If a full scan is required, process data incrementally.

---

## 4. Memory Optimization Requirements

**This is the most important part of the project.**

Every feature must be designed with predictable and bounded application-level memory consumption.

### 4.1 Incremental Processing

Never materialize an entire database or bucket into an in-memory collection.

Avoid patterns such as:

```go
records := make([]Record, 0)

cursor := bucket.Cursor()

for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
    records = append(records, Record{
        Key:   bytes.Clone(k),
        Value: bytes.Clone(v),
    })
}
```

Instead, process records incrementally:

```go
cursor := bucket.Cursor()

for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
    if err := processRecord(k, v); err != nil {
        return err
    }
}
```

Processing functions must not retain cursor-owned memory beyond the transaction lifetime.

### 4.2 Bounded Memory

Memory consumption should depend primarily on:

* Configured batch size.
* Maximum record size.
* Output buffer size.
* Number of active operations.
* Database engine overhead.

Memory consumption must not grow linearly with the total number of records for streaming operations.

Prefer constant-memory or bounded-memory algorithms wherever practical.

### 4.3 Cursor-Based Iteration

Use bbolt cursors for:

* Sequential iteration.
* Prefix searches.
* Range queries.
* Pagination.
* Incremental exports.

Use `Cursor.Seek()` for efficient navigation to a starting key.

Avoid scanning unrelated records when indexed key navigation is possible.

### 4.4 Pagination

Listing commands must support pagination or explicit result limits.

Recommended parameters:

* `--limit`
* `--after`
* `--prefix`
* `--start`
* `--end`

Prefer keyset pagination over offset-based pagination.

Do not implement pagination by loading all records and slicing the resulting collection.

### 4.5 Batch Processing

Large operations must support configurable batch sizes.

Examples:

* Batch imports.
* Batch updates.
* Batch deletes.
* Incremental exports.

Batch sizes must be configurable and have reasonable defaults.

Avoid creating extremely large write transactions.

Use bounded transactions where atomicity requirements permit.

### 4.6 Memory-Mapped Database Behavior

bbolt uses memory-mapped files.

Therefore:

* Do not equate virtual memory size with actual RAM consumption.
* Distinguish heap allocation, RSS, and mapped memory.
* Avoid unnecessary copies of keys and values.
* Copy cursor-returned data only when it must outlive the transaction or cursor operation.
* Avoid holding read transactions open longer than necessary.
* Consider how long-running transactions affect page reclamation.
* Never assume that `mmap` guarantees constant physical memory usage.

Performance measurements must account for operating-system page caching.

### 4.7 Streaming Output

CLI output must be streamed directly to an `io.Writer`.

Avoid accumulating large output in `strings.Builder`, `bytes.Buffer`, or slices.

Prefer buffered writers with fixed or configurable buffer sizes.

### 4.8 Resource Limits

Where practical, support configurable limits for:

* Batch size.
* Maximum records returned.
* Output buffering.
* Operation timeout.
* Maximum concurrent operations.

Large operations should support cancellation through `context.Context`.

---

## 5. Architecture Guidelines

Use a modular architecture with clear separation of responsibilities.

Recommended logical components:

### CLI Layer

Responsible for:

* Parsing commands and flags.
* Validating user input.
* Formatting output.
* Reporting errors.
* Managing interactive sessions.

The CLI layer must not contain database business logic.

### Database Manager

Responsible for:

* Opening databases.
* Closing databases.
* Tracking active database handles.
* Managing multiple databases.
* Coordinating database lifecycle operations.

Database handles must be closed reliably.

### Storage Layer

Responsible for:

* bbolt transactions.
* Bucket operations.
* Record CRUD.
* Cursor-based iteration.
* Database statistics.

Keep bbolt-specific implementation details inside this layer.

### Query Layer

Responsible for:

* Exact-key lookups.
* Prefix searches.
* Range queries.
* Filtering.
* Pagination.

Queries must avoid unnecessary allocations and full scans.

### Import/Export Layer

Responsible for:

* Streaming serialization.
* Streaming deserialization.
* Batch imports.
* Binary-safe encoding.
* Error handling and partial failure reporting.

### Performance Layer

Responsible for:

* Benchmarks.
* Memory profiling.
* Allocation analysis.
* Performance regression detection.

These are logical responsibilities, not mandatory packages.

Do not create additional packages unless the complexity justifies them.

---

## 6. CLI Design Principles

Commands must be consistent, predictable, and suitable for scripting.

Suggested command structure:

```text
boltcli db create <path>
boltcli db open <path>
boltcli db close <name>
boltcli db list
boltcli db info <name>
boltcli db delete <path>

boltcli bucket list
boltcli bucket create <name>
boltcli bucket delete <name>
boltcli bucket stats <name>

boltcli record get <key>
boltcli record put <key> <value>
boltcli record delete <key>
boltcli record list --limit 100
boltcli record search --prefix user:
boltcli record export <key>
boltcli record import <file>

boltcli export --bucket users --output users.jsonl
boltcli import --bucket users --input users.jsonl
```

These commands are examples, not a finalized public interface.

The actual CLI design must be internally consistent.

### CLI Requirements

* Provide helpful `--help` output.
* Use meaningful exit codes.
* Print diagnostics to stderr.
* Keep machine-readable stdout clean.
* Support non-interactive execution.
* Avoid unnecessary prompts in scripted workflows.
* Require confirmation for destructive operations in interactive mode.
* Support explicit flags for automation.
* Handle interrupted operations gracefully.

If an interactive shell is implemented, distinguish session-specific commands from standalone CLI commands.

---

## 7. Concurrency and Transactions

Follow bbolt concurrency constraints.

* Multiple read-only transactions may execute concurrently.
* Only one write transaction may execute at a time per database.
* Never share a transaction across goroutines.
* Never share a cursor across goroutines.
* Keep transactions short whenever possible.
* Avoid blocking operations inside write transactions.
* Ensure database handles are not closed while operations are using them.

Use synchronization only where shared state requires it.

Avoid unnecessary goroutines.

Parallelism must not be introduced unless it provides measurable benefits.

### Cancellation

Long-running operations should accept `context.Context`.

Check cancellation periodically during iteration.

Ensure that cancellation does not leave partially initialized resources or leaked goroutines.

---

## 8. Error Handling

Follow idiomatic Go error handling.

* Return errors explicitly.
* Wrap errors with `%w` when useful.
* Use `errors.Is` and `errors.As`.
* Never silently ignore errors.
* Do not use `panic` for expected failures.
* Validate paths, bucket names, and command arguments.
* Handle missing databases, buckets, and keys gracefully.
* Distinguish empty values from missing keys.
* Ensure resources are released after failures.

Errors must provide enough context for troubleshooting without exposing sensitive data unnecessarily.

---

## 9. Code Quality Standards

All code must follow idiomatic Go practices.

### General Rules

* Keep functions small and focused.
* Prefer simple implementations.
* Avoid premature abstraction.
* Avoid unnecessary allocations.
* Use descriptive names.
* Prefer standard library solutions.
* Document exported APIs.
* Use `gofmt` and `go vet`.
* Avoid global mutable state when possible.
* Do not introduce dependencies without justification.

### Performance Rules

Before implementing an operation, consider:

1. Does it require loading all records?
2. Can it use a cursor?
3. Can it process data incrementally?
4. Are allocations necessary?
5. Can memory usage be bounded?
6. Does it hold a transaction longer than necessary?
7. How does it behave with millions of records?

Prefer the simplest solution satisfying correctness and memory constraints.

---

## 10. Testing Requirements

All important functionality must have automated tests.

### Unit Tests

Cover:

* Database creation and opening.
* Database closing and deletion.
* Bucket management.
* Record CRUD.
* Exact-key lookups.
* Prefix and range searches.
* Pagination.
* Import and export.
* Error handling.
* Cancellation.
* Multiple active database handles.

Use temporary directories for database tests.

Do not rely on external database services.

### Edge Cases

Test:

* Empty databases.
* Empty buckets.
* Empty keys and values where permitted.
* Binary keys and values.
* Large values.
* Missing keys.
* Missing buckets.
* Invalid paths.
* Read-only databases.
* Concurrent readers.
* Interrupted operations.
* Corrupted database files.
* Partial import failures.

### Fuzz Testing

Use Go fuzz tests for suitable input-processing boundaries:

* Command argument parsing.
* Import file parsing.
* Binary encoding and decoding.
* Key filtering.
* Pagination tokens.
* Serialization and deserialization.

Fuzz tests must be bounded and must not create unbounded resources.

### Benchmarks

Implement benchmarks for:

* Sequential record iteration.
* Prefix searching.
* Range queries.
* Paginated listing.
* Batch imports.
* Streaming exports.
* Memory allocations.

Use `b.ReportAllocs()` where appropriate.

---

## 11. Performance Evaluation

Performance evaluation is an essential part of the thesis.

The application should be compared with existing solutions such as:

* boltbrowser
* bbolt-cli
* bolt-shell

Compare only equivalent operations and clearly document differences in functionality.

### Evaluation Metrics

Measure:

* Peak resident memory (RSS).
* Go heap allocations.
* Total allocated bytes.
* Execution time.
* Throughput in records per second.
* Database size.
* CPU utilization where relevant.

### Test Datasets

Use databases of varying sizes, for example:

* 10 MB
* 100 MB
* 1 GB
* 5 GB

Include datasets with:

* Small keys and values.
* Large values.
* Different bucket counts.
* Nested buckets.
* Large record counts.

### Benchmark Fairness

* Use identical or equivalent datasets.
* Compare equivalent operations.
* Record hardware specifications.
* Record software versions.
* Control for warm and cold filesystem caches.
* Repeat measurements.
* Report statistical variability.
* Do not invent benchmark results.

The goal is to demonstrate measurable improvements rather than assume them.

---

## 12. Security and Data Integrity

Database management operations must prioritize data integrity.

* Never delete databases without explicit user intent.
* Validate filesystem paths.
* Avoid overwriting files accidentally.
* Respect filesystem permissions.
* Preserve binary data exactly.
* Use transactions for atomic modifications.
* Handle failed writes safely.
* Avoid logging sensitive record values.
* Avoid modifying database files outside supported bbolt APIs.

For long-running write operations, document whether partial changes can remain committed after cancellation.

---

## 13. Documentation Requirements

Maintain documentation describing:

* Installation.
* Building from source.
* CLI usage.
* Command examples.
* Supported formats.
* Database lifecycle.
* Memory optimization techniques.
* Known limitations.
* Performance benchmarks.
* Architectural decisions.

Documentation must accurately reflect implemented functionality.

Do not describe planned features as completed features.

---

## 14. AI Agent Instructions

When working on this repository, AI coding agents must follow these rules.

### Before Making Changes

1. Inspect the existing project structure.
2. Read `go.mod` and relevant source files.
3. Identify existing coding conventions.
4. Understand the affected database operations.
5. Consider memory usage and transaction lifetime.

### During Implementation

1. Preserve existing architecture where reasonable.
2. Implement the smallest correct change.
3. Prefer cursor-based processing.
4. Avoid full-database materialization.
5. Avoid unnecessary dependencies.
6. Keep resource ownership explicit.
7. Respect bbolt transaction and concurrency rules.
8. Ensure code works across supported platforms.

### After Implementation

1. Format changed Go files.
2. Run relevant tests.
3. Run `go vet` where practical.
4. Add tests for new behavior.
5. Add benchmarks for performance-critical changes.
6. Verify that memory consumption remains bounded where required.
7. Update documentation if public behavior changes.

### Agent Response Expectations

When proposing or implementing a solution:

* Explain significant architectural decisions briefly.
* Highlight memory-related tradeoffs.
* Mention transaction lifetime implications.
* Identify potential performance bottlenecks.
* Avoid unsupported performance claims.
* Clearly distinguish measured improvements from expected improvements.

Do not perform unrelated refactoring.

Do not replace an efficient streaming implementation with an allocation-heavy alternative.

---

## 15. Definition of Done

A feature is considered complete when:

* It behaves correctly.
* Errors are handled.
* Relevant tests pass.
* Resources are closed properly.
* Memory usage is appropriate for the operation.
* Large datasets do not require full materialization.
* CLI behavior is consistent.
* Documentation is updated where necessary.
* No known data integrity regressions are introduced.

For performance-sensitive features, completion also requires representative benchmarks or profiling evidence.

---

## 16. Project Priorities

When tradeoffs arise, prioritize:

1. Data integrity and correctness.
2. Memory efficiency.
3. Predictable resource usage.
4. Reliability and error handling.
5. Performance.
6. CLI usability.
7. Simplicity and maintainability.
8. Additional functionality.

**Core principle:**

> The application must remain usable with databases significantly larger than the available RAM, without requiring the entire database or its records to be materialized in application memory.

This principle must guide architectural decisions, feature implementation, testing, and performance evaluation.

## 17. Terminal User Interface (TUI)

The application must provide a **modern, beautiful, intuitive, and responsive Terminal User Interface (TUI)**.

A high-quality TUI is one of the core requirements of the project, not an optional feature.

### Design Requirements

* Modern and visually appealing interface.
* Clean layout with consistent colors, borders, spacing, and typography.
* Professional appearance inspired by modern terminal applications.
* Intuitive keyboard navigation and shortcuts.
* Support for light and dark terminal themes.
* Responsive layout adapting to terminal dimensions.
* Clear visual feedback for loading, errors, warnings, and successful operations.
* Status bar, contextual help, and keyboard shortcut hints.
* Interactive dialogs for creating, editing, and deleting records.
* Searchable and scrollable tables.
* Syntax highlighting for JSON and other structured values where practical.

### Main Interface

The TUI should provide:

* **Database Explorer:** Sidebar displaying opened databases and their bucket hierarchy.
* **Record Browser:** Paginated table for browsing keys and values.
* **Record Editor:** Interactive editor for viewing and modifying records.
* **Search Panel:** Interactive search and filtering by key, prefix, and range.
* **Statistics Dashboard:** Visual representation of database and bucket statistics.
* **Import/Export Interface:** Progress indicators for long-running operations.
* **Status Bar:** Active database, current bucket, record count, and operation status.

### Recommended Technology

Prefer the Charm ecosystem:

* `Bubble Tea` — TUI application framework.
* `Lip Gloss` — terminal styling and layout.
* `Bubbles` — reusable interactive components.

### Performance Requirements

The TUI must remain responsive when working with multi-gigabyte databases.

* Never load all database records into a TUI table.
* Implement lazy loading and cursor-based pagination.
* Render only visible records.
* Use asynchronous operations for expensive tasks.
* Support cancellation of long-running operations.
* Avoid unnecessary re-rendering and memory allocations.
* Never block the UI event loop with database operations.

### UX Principles

The interface should feel like a polished standalone application rather than a collection of terminal commands.

**The goal is to combine the usability of a graphical database manager with the speed, portability, and memory efficiency of a CLI application.**

Visual quality, usability, and performance must receive equal attention during development.
