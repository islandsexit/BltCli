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

BASE removals are red; OTHER additions are green. The legend names both databases.
Only differences appear: added/removed records, changed values, missing
buckets (including empty ones), record/bucket type changes, and bucket
sequence changes. The comparison includes every root and nested bucket
and ignores current browser filters. File layout, free pages, and insertion
order do not affect equality. Keys and values are compared byte for byte.

The list follows bucket nesting and shows only branches containing changes.
Enter or Right opens a selected bucket; Left or Backspace returns to its parent.
Escape returns to the parent; at the comparison root it asks before closing.
`q` asks to close the comparison from any bucket level. The popup defaults to
Stay; Left/Right or Tab chooses Close, Enter applies it, and Esc cancels.
Up/Down select entries and automatically cross page boundaries.
Page Up/Down (or P/N) jump pages; Home returns to the first page. Enter on a record opens
the complete BASE and OTHER values side by side, with aligned line numbers.
Unchanged lines remain visible as context; changed lines have red/green backgrounds,
with stronger highlighting between the common prefix and suffix of a replacement.
The list itself retains short previews near the first differing byte.
The original browser location is restored when comparison closes.

Equal databases display **Databases are equal**. Completely different
databases retain their complete searchable differences. Here “completely
different” means no identical record exists at the same bucket path and
key, even if some bucket names are shared. If both databases contain no
records, it means no identical bucket exists. Two empty databases are equal.

## Complete value viewer

| Key | Action |
|---|---|
| Up/Down or `k`/`j` | Scroll aligned rows |
| Page Up/Down or `p`/`n` | Scroll a page |
| Home/End | Beginning/end of complete values |
| Left/Right | Pan both columns horizontally |
| `0` | Reset horizontal position |
| `[` / `]` | Previous/next change block |
| F4 | Switch formatted JSON / raw text |
| F6 | Explicit hex view of both values (F4 returns to text/JSON) |
| `/`, `g`, Ctrl+F | Search the completed comparison by key |
| Esc, `q`, Enter | Return to the difference list/search results |

JSON-looking values are formatted incrementally for display, preserving string
contents and number spellings. F4 exposes original whitespace; whitespace-only
changes automatically open in raw mode. Binary values use a hex dump. If text
contains tabs, carriage returns or other controls, raw mode shows escaped markers;
F6 exposes the exact bytes on both sides. If text
representations hide a recorded byte difference, the initial viewer falls back
to hex. This is a read-only view and never saves formatted content to a database.
Long physical lines are split into segments of about 4 KiB with repeated line
numbers, allowing all content to be reached without a growing line buffer.

Alignment uses a 64-row local lookahead rather than a full-file edit-distance
matrix. Distant moved blocks or repetitive text can be grouped more coarsely
than in VS Code; all value content is retained. JSON syntax colors apply to
unchanged context; red/green backgrounds identify actual changed rows.

## Search after comparison

Press `/` (or `g`/Ctrl+F) in the diff browser or full value viewer. Enter the key
using plain text, `text:`, `hex:`, or `base64:`. Choose `exact` (default) or
`contains` in the second field. Results cover every bucket, include the bucket
path, and support Up/Down, Page Up/Down and Enter to open the full value diff.
Esc returns to the bucket browser. Input shortcuts also work in Cyrillic layouts.

Exact search seeks a SHA-256 key index and verifies the original binary key;
substring search scans the completed result spool incrementally. Neither opens
or compares the source databases again. “No recorded differences for this key”
means there is no matching changed record; it does not establish whether an
unchanged key exists in either source. Bucket metadata-only changes are browsed
through the hierarchy rather than the record-key search.

## Resources and cancellation

The comparison runs asynchronously; Escape asks before stopping and discarding
it. Escape while preparing/paging a value viewer cancels only that operation.
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

Sorted cursors are merged directly; full values are streamed from mapped memory.
Read transactions close after at most 1,024 inspected entries or about 8 MiB of
value payload (plus at most one record). Large values
are compared in 64 KiB chunks with cancellation checks. Application memory
depends on nesting depth, key sizes, bounded previews, and the visible page,
rather than the total number of differences.
At most 128 previous diff-page boundaries are retained; Home returns to the
first page when older boundaries are no longer available.

Differences, a fixed-size offset index, and complete changed value bytes are
spooled to private temporary files. Each list entry retains at most 4 KiB of
preview per side. Disk usage also includes the full bytes of changed values
from both databases, so sufficient temporary-disk space is required. Disk
exhaustion is reported as an error, without modifying either source database.
Full value bytes are written in 64 KiB chunks while the source transaction is
valid. This adds I/O and can lengthen that record's read transaction, but avoids
retaining a large value or reopening databases later for inspection.
After comparison closes its source transactions, the bucket hierarchy is built
in write transactions of at most 512 differences or 4 MiB of input (plus at most
one bounded entry). The hierarchy stores eight-byte spool offsets instead of
duplicate JSON previews. This replaces a synchronized transaction for every
difference; source database write/durability settings are unchanged. Equal
comparisons skip hierarchy construction entirely.
Opening a value builds temporary aligned rows plus direct row/change indexes
asynchronously. The two 64-row windows and a visible page bound application
memory; the complete formatted document stays on disk. Building can be canceled
with Esc. Viewer files are removed when closing/replacing the viewer; snapshot
files live until comparison closes. Viewing uses the captured value bytes even
if an external writer subsequently changes a source database.
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
