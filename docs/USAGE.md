# Usage

[Back to README](../README.md)

## CLI examples

Flags precede positional arguments. Each standalone command owns and closes its database handle; opened handles persist only within a TUI session.

```sh
boltcli db create example.db
boltcli bucket create --db example.db --bucket users
boltcli bucket create --db example.db --bucket users/settings
boltcli record put --db example.db --bucket users user:1 '{"name":"Ada"}'
boltcli record get --db example.db --bucket users user:1
boltcli record update --db example.db --bucket users user:1 '{"name":"Grace"}'
boltcli record export --db example.db --bucket users --output one.jsonl user:1
boltcli record exists --db example.db --bucket users user:1
boltcli record list --db example.db --bucket users --limit 50
boltcli record search --db example.db --bucket users --prefix user:
boltcli bucket list --db example.db --contains user
boltcli record search --db example.db --bucket users --contains user --value-contains Ada
boltcli record list --db example.db --bucket users --start user:1 --end user:9
boltcli record list --db example.db --bucket users --after user:1
boltcli record put --db example.db --bucket users hex:00ff base64:AP8=
boltcli db info --db example.db
boltcli db stats --db example.db
boltcli bucket stats --db example.db --bucket users
boltcli export --db example.db --bucket users --output users.jsonl
boltcli import --db example.db --bucket users --input users.jsonl --duplicates skip
boltcli record delete --db example.db --bucket users --yes user:1
boltcli bucket delete --db example.db --bucket users/settings --yes
boltcli db delete --yes example.db
```

`put` inserts or replaces a value atomically; `update` requires an existing key. Empty values are valid; empty keys are rejected by bbolt. A missing key is distinct from an empty value. Exact search uses `record get`; prefix and range searches use `record search`. Bounds follow bytewise lexical order, with an inclusive start and an exclusive end. Child buckets are excluded from record lists. `record import` is an alias for JSONL import.

Plain arguments are text. Prefix an argument with `text:` to treat a literal `hex:` or `base64:` prefix as text. Use `hex:` or `base64:` for arbitrary binary keys and values. Bucket paths use slash-separated names; use **hex** for components containing literal slashes or base64 encodings containing `/`.

List commands default to 100 records and allow 1–10,000 results. When more records exist, stderr prints an exclusive `--after base64:...` continuation argument. Bucket listing also accepts `--after`; a full bucket page may suggest a final empty continuation page. Results and metadata are JSON on stdout; help and diagnostics use separate streams. Failures exit with code 1, success with 0.

## TUI controls

| Key | Action |
| --- | --- |
| `o` / `c` | Open / exclusively create a database |
| `[` / `]` / `x` | Switch databases / close active database |
| `Tab`, arrows or `j` / `k` | Change pane and select a row; arrows automatically cross page boundaries |
| `Enter` / `Backspace` | Open child bucket or inspect record / go to parent |
| `Esc` | From records or a record preview, focus buckets if populated; otherwise go directly to parent |
| `n` / `p` or Page Down / Up | Next / previous page in focused pane |
| `b` / `D` | Create child bucket / delete current bucket and descendants |
| `r` / `e` / `d` | Insert or replace / edit / delete record |
| `/` | Filter bucket names in BUCKETS; filter record keys and values in RECORDS |
| `v` / `P` / `f` | Value substring search / key prefix / inclusive-start, exclusive-end range |
| `g` / `Ctrl+F` | Search the entire active database: record keys, record values, or bucket names |
| `s` | Database and current bucket statistics |
| `i` / `E` | Import JSONL / export current search |
| `Ctrl+R` | Refresh |
| `h` / `F1` | Open scrollable help (`F1` also works inside a dialog) |
| `Esc` | Cancel operation or close dialog |
| `q` / `Ctrl+C` | Quit |

The focused pane has a highlighted border and an `[ACTIVE]` heading; `›` marks the selected row. The bucket pane expands to about two thirds of the available width when focused; focusing records restores the wider record pane. The persistent `Root:` label shows the root bucket name even while browsing its descendants. Footer hints show actions for the current pane or dialog. Up/Down automatically load adjacent pages at the first/last row; returning with Up selects the previous page's last row. Explicit page shortcuts also remain available. Forward browsing is unlimited; the most recent 128 page boundaries are kept for going back.

`/` opens a filter for the focused pane. Bucket filters match substrings of child names, or root bucket names at the database root. Record filters match key and value substrings; both conditions must match, along with any prefix/range filters. `v` edits only the value filter, while `P` edits the key prefix. All filters search across the current bucket, including records outside the visible page. Matches are case-sensitive byte substrings, and accept the standard binary encodings. Empty fields clear their filter. Changing the bucket or database resets filters. Exports apply the active record filters.

CLI substring flags are `--contains` (record key or bucket name) and `--value-contains` (record value). Prefix queries use indexed cursor navigation; substring/value searches may scan the selected bucket. Scans run outside the TUI event loop, check cancellation between records, and copy only the bounded result page. They hold a read transaction during the scan, which can delay page reclamation while a writer is active.

Record keys use the full row width and wrap over up to three table lines; values use up to two additional lines. The table scrolls to keep the selected record visible. Enter displays the **complete readable key**, including keys too long for the table, and a wrapped value preview of up to 64 KiB. Up/Down, Page Up/Down, and Home/End scroll the preview. Export includes the complete value. When the child-bucket panel is empty inside a bucket, focus automatically moves to records; Esc from records or their preview goes directly to the parent bucket. `h` opens the full usage guide and preserves the underlying panel or preview when closed.

`g` (or Ctrl+F) opens **database-wide search**. Use Left/Right in the first field to choose record keys, record values, or bucket names; Tab moves to the substring field and Enter runs the search. It includes every root and nested bucket in the active database, independent of the current local filters. Matches are case-sensitive byte substrings and accept text, hex, or base64 input. An empty term lists all entries of the chosen type. Results are paginated, show their bucket path, and support the same automatic arrow navigation. Enter opens a bucket result or shows a record at its location; `o` opens the result's bucket. Esc closes search results. Results are read-only until a match is opened in the normal browser.

Database-wide search retains only the requested page and a bounded value preview, with at most 128 previous-page checkpoints. It closes its read transaction after at most 1,024 inspected entries and resumes with a binary-safe path/key checkpoint. This bounds per-transaction bucket caches and permits page reclamation during long scans. Searches check cancellation during traversal. Results across scan blocks and later pages are not a single atomic snapshot: concurrent writes can change later matches. No entire database or record collection is loaded into memory.

Dialogs accept the same binary encodings as the CLI. Use Tab to change fields, arrows/Home/End to move the cursor, Ctrl+U to clear a field, and Enter to submit. Long fields wrap and scroll to keep the cursor visible. In a text field, `h` types a character; `F1` opens help. Deletions require typing `yes`. Editing opens printable UTF-8 as `text:` and other data as `hex:` to preserve bytes exactly. Terminal controls in stored data are escaped. Themes adapt to the terminal background.

## Import and export format

Record previews pretty-print complete JSON with two-space indentation.
UTF-8 text, including YAML, keeps its line breaks and indentation without
extra quotes or escaped newlines. Binary values and terminal controls are
escaped. Previews remain bounded; formatting never modifies stored data.

JSONL contains one record per line. Both fields are **base64 strings**, including text data. An empty value is `""`; `null` is rejected during import.

```json
{"key":"dXNlcjox","value":"eyJuYW1lIjoiQWRhIn0="}
{"key":"AP8=","value":""}
```

Export covers direct records in the selected bucket and applies prefix/range filters. It streams all matches unless `--limit` is explicitly supplied. Nested buckets are managed separately and are not included in a bucket export. Use `--input -` or `--output -` for stdin/stdout. Output files are created exclusively; existing files are never overwritten.

Imports default to 500 records and 8 MiB of decoded data per write transaction, with a 16 MiB maximum JSONL line. Configure `--batch-size`, `--batch-bytes`, and `--max-record-bytes`. A single record exceeding the batch byte limit is rejected. Duplicate modes are `overwrite`, `skip`, and `error`.

Each import batch is atomic. Earlier batches remain committed after malformed input, duplicate errors, I/O failures, or cancellation. The diagnostic count reports **committed input records**, including skipped duplicates. Records pending in an uncommitted batch are discarded. Failed exports can leave a partial file, which must be removed or renamed before reusing that output path.
