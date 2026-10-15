# Database comparison

[Back to README](../README.md)

```sh
boltcli tui --diff base.db other.db
boltcli diff base.db other.db
```

Both commands open the comparison TUI. In a normal session, press `F` or
`Ctrl+D`, enter the other database's file path or its open session name,
and press Enter. The active database is BASE. File paths with spaces work
in the dialog; quote them when launching from a shell.

BASE is green; OTHER is pastel red. The legend names both databases.
Only differences appear: added/removed records, changed values, missing
buckets (including empty ones), record/bucket type changes, and bucket
sequence changes. The comparison includes every root and nested bucket
and ignores current browser filters. File layout, free pages, and insertion
order do not affect equality. Keys and values are compared byte for byte.

The list follows bucket nesting and shows only branches containing changes.
Enter or Right opens a selected bucket; Left or Backspace returns to its parent.
Escape returns to the parent, or closes comparison at the root; Q closes from any level.
Up/Down select entries and automatically cross page boundaries.
Page Up/Down (or P/N) jump pages; Home returns to the first page. Enter on a record shows
both sides with their colors, byte counts, and readable previews. For long
changed values, previews begin near the first differing byte, so a change
beyond the initial preview is still visible. Escape closes the preview;
The original browser location is restored when comparison closes.

Equal databases display **Databases are equal**. Completely different
databases display a text summary without a result list. Here “completely
different” means no identical record exists at the same bucket path and
key, even if some bucket names are shared. If both databases contain no
records, it means no identical bucket exists. Two empty databases are equal.

## Resources and cancellation

The comparison runs asynchronously and can be cancelled with Escape.
The progress screen shows elapsed time, checked entries, differences, a progress
bar, and estimated remaining time for the current phase. Comparison progress
uses processed logical bytes against file size minus engine-reported free bytes;
it is approximate, may finish before reaching 100%, and is capped below 100%
until traversal finishes. Page overhead, spare file capacity, data distribution,
cache state, and concurrent writes affect the estimate. No extra counting scan
is performed. Initially the ETA says “calculating”. Building bucket navigation
is a separate final phase with an exact difference count and an estimated ETA.
Additional files open read-only; existing session handles are reused and
stay open. Comparison does not modify either database.

Sorted cursors are merged directly; full values remain in mapped memory.
Read transactions close after at most 1,024 inspected entries. Large values
are compared in 64 KiB chunks with cancellation checks. Application memory
depends on nesting depth, key sizes, bounded previews, and the visible page,
rather than the total number of differences.
At most 128 previous diff-page boundaries are retained; Home returns to the
first page when older boundaries are no longer available.

Differences and a fixed-size offset index are spooled to private temporary
files. Disk usage grows with the number of differences; each side retains
at most 4 KiB of value preview. Disk exhaustion is reported as an error.
After comparison closes its source transactions, the bucket hierarchy is built
in write transactions of at most 512 differences or 4 MiB of input (plus at most
one bounded entry). The hierarchy stores eight-byte spool offsets instead of
duplicate JSON previews. This replaces a synchronized transaction for every
difference; source database write/durability settings are unchanged. Equal and
summary-only comparisons skip hierarchy construction entirely.
Files are removed after closing comparison, cancellation/failure, or normal
application shutdown. A forced process termination can leave temporary files.

Each block is consistent within each database's read transaction. Blocks
are not one atomic snapshot of both databases: concurrent external writes
can affect later blocks. Compare files that are not changing when you need
a reproducible result. bbolt's mmap and engine metadata also consume memory;
bounded application buffers do not imply constant RSS.

## Errors

Errors wrap in the footer in pastel red. Press `F2` to read the complete
message, including while a dialog is open. Use arrows, Page Up/Down, or
Home/End to scroll; `F2` or Escape restores the previous screen.
