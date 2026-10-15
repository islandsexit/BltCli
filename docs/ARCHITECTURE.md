# Architecture and limitations

[Back to README](../README.md)

## Architecture and memory behavior

* `domain`: storage interface and independent session handles.
* `repository/adapters`: bbolt lifecycle, transactions, cursor queries, CRUD, and statistics.
* `repository`: bounded streaming import/export and disk-backed comparison, search and aligned value documents.
* `cli` / `config`: command dispatch, flags, clean output and explicit confirmations.
* `tui`: Bubble Tea event loop, asynchronous work and Lip Gloss styling.
* `utils`: binary argument decoding and escaped previews.

Listing memory is proportional to page size and record sizes. Import memory is bounded by batch bytes, record count, and maximum input line size. Export serializes one record at a time with a fixed 64 KiB output buffer; serialization requires memory proportional to the largest record. No operation materializes an entire bucket of records. bbolt itself also allocates engine metadata and maps the database file; application heap bounds do not imply constant RSS.

Cursor bytes never escape their transaction unless copied or streamed to a writer before the transaction closes. A lifecycle lock prevents a handle from closing during use. The TUI cancels and waits for active workers before closing handles. Browser pages contain at most 100 records; full value diff pages contain at most 200 aligned rows. Previous-page histories retain at most 128 keys. Database operations run outside the event loop. Interactive value editing is limited to 1 MiB; larger values can use JSONL import.

Comparison merge-walks source cursors in short read transactions. Complete changed values are copied to a disk snapshot in 64 KiB chunks; list previews remain bounded. Batched hierarchy and hashed-key indexes reference spool offsets. The complete value viewer incrementally formats values and aligns two 64-row lookahead windows, splitting long lines into bounded segments; rows and row/change indexes stay on disk. Exact-key search seeks the completed hash index and verifies binary keys. Substring search streams the result spool. Neither viewer nor search needs to reopen the source databases. Disk usage grows with changed bytes and displayed document size; live application buffers do not grow with the number of rows.

Exports hold one read snapshot for consistency. Slow output can prolong this transaction and delay page reclamation while another writer is active. Imports commit small batches instead of one database-sized write transaction. The TUI uses separate read transactions for child names and records; refresh shows external changes. File lock acquisition has a one-second timeout. Cancellation is checked during cursor iteration and between import records, but cannot interrupt an arbitrary blocking stdin/stdout call, bbolt page-statistics traversal, or an in-progress filesystem sync.

`--timeout` bounds cooperative operations. Ctrl+C cancels through `context.Context`. Read-only sessions reject mutations. Avoid opening the same database twice in one session or modifying files outside bbolt APIs.

## Current limitations

The multiline editor supports JSON syntax highlighting, display formatting and compact saves, with a 1 MiB decoded value limit. Ordinary record previews remain bounded. Comparison shows approximate byte progress and estimated phase time; exact source totals would require an extra scan. Full value alignment uses local anchors, so distant moves can be grouped more coarsely than by a global edit-distance algorithm. Complete value snapshots and formatted documents require temporary disk space. `db info` exposes file size, page size, and free/pending pages without a record scan; `db stats` traverses engine pages to count records/buckets and aggregate page utilization. Bucket statistics also traverse pages and include descendants. Used/allocated bucket bytes exclude database metadata and unused tail space, so they are not a whole-file utilization percentage. Exports are per-bucket JSONL rather than recursive database backups. Previous-page history is intentionally bounded. Multi-gigabyte comparisons with other tools remain an evaluation task; no competitive performance claims are made.
