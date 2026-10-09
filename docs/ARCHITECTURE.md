# Architecture and limitations

[Back to README](../README.md)

## Architecture and memory behavior

* `domain`: storage interface and independent session handles.
* `repository/adapters`: bbolt lifecycle, transactions, cursor queries, CRUD, and statistics.
* `repository`: bounded streaming import and export.
* `cli` / `config`: command dispatch, flags, clean output and explicit confirmations.
* `tui`: Bubble Tea event loop, asynchronous work and Lip Gloss styling.
* `utils`: binary argument decoding and escaped previews.

Listing memory is proportional to page size and record sizes. Import memory is bounded by batch bytes, record count, and maximum input line size. Export serializes one record at a time with a fixed 64 KiB output buffer; serialization requires memory proportional to the largest record. No operation materializes an entire bucket of records. bbolt itself also allocates engine metadata and maps the database file; application heap bounds do not imply constant RSS.

Cursor bytes never escape their transaction unless copied. A lifecycle lock prevents a handle from closing during use. The TUI cancels and waits for active workers before closing handles. It renders at most 100 rows and retains at most 128 previous-page keys. Database operations run outside the event loop. Interactive value editing is limited to 1 MiB; larger values can use JSONL import.

Exports hold one read snapshot for consistency. Slow output can prolong this transaction and delay page reclamation while another writer is active. Imports commit small batches instead of one database-sized write transaction. The TUI uses separate read transactions for child names and records; refresh shows external changes. File lock acquisition has a one-second timeout. Cancellation is checked during cursor iteration and between import records, but cannot interrupt an arbitrary blocking stdin/stdout call, bbolt page-statistics traversal, or an in-progress filesystem sync.

`--timeout` bounds cooperative operations. Ctrl+C cancels through `context.Context`. Read-only sessions reject mutations. Avoid opening the same database twice in one session or modifying files outside bbolt APIs.

## Current limitations

The TUI uses a bounded value preview and a simple two-field editor, without JSON syntax highlighting or a multiline editing widget. Long operations show activity and cancellation rather than a percentage, because counting all matches would require additional work. `db info` exposes file size, page size, and free/pending pages without a record scan; `db stats` traverses engine pages to count records/buckets and aggregate page utilization. Bucket statistics also traverse pages and include descendants. Used/allocated bucket bytes exclude database metadata and unused tail space, so they are not a whole-file utilization percentage. Exports are per-bucket JSONL rather than recursive database backups. Previous-page history is intentionally bounded. Multi-gigabyte comparisons with other tools remain an evaluation task; no competitive performance claims are made.
