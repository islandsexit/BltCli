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

Closing the editor, completed comparison, or a database requires a confirmation
popup. Esc at the database root asks to close the active handle; `x` opens the
same popup. `q` asks before quitting with databases open. Stopping a running
comparison with Esc also requires confirmation. The default is **Stay**:
Left/Right or Tab selects the action, Enter applies it, and Esc cancels the
popup. `y` confirms, `n` cancels; command aliases follow the current layout.
Canceling preserves the editor draft, cursor and comparison results. Returning
from a value preview, navigating to a parent bucket, and closing help/error
overlays remain immediate. Ctrl+C is an immediate application quit.

Command shortcuts work at the corresponding positions in English QWERTY,
Russian ЙЦУКЕН, and standard Ukrainian/Belarusian layouts. For example, `o`/`щ`
opens a database, `e`/`у` edits, `h`/`р` opens help, and `F`/`А` opens diff.
Shift remains significant: `d`/`в` deletes a record, while `D`/`В` deletes a
bucket. This also applies to navigation, database switching, overlays, and
search-type selection. Text fields and pasted text are never translated.
Terminal key events expose characters rather than physical scan codes, so
other layouts require their own character mappings.

| Key | Action |
| --- | --- |
| `o` / `c` | Open / exclusively create a database |
| `[` / `]` / `x` | Switch databases / close active database |
| `Tab`, arrows or `j` / `k` | Change pane and select a row; at a page boundary, arrows load the next or previous page without blending pages |
| `Enter` / `Backspace` | Open child bucket or inspect record / go to parent |
| `Esc` | From records or a record preview, focus buckets if populated; otherwise go directly to parent |
| `n` / `p` or Page Down / Up | Next / previous page in focused pane |
| `b` / `D` | Create child bucket / delete current bucket and descendants |
| `r` / `e` / `d` | Insert or replace / edit / delete record |
| `/` | Filter bucket names in BUCKETS; filter record keys and values in RECORDS |
| `v` / `P` / `f` | Value substring search / key prefix / inclusive-start, exclusive-end range |
| `g` / `Ctrl+F` | Search the entire active database: record keys, record values, or bucket names |
| `F` / `Ctrl+D` | Compare the active database with another file or open database name |
| `F2` | Open the complete error in a scrollable view |
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

`g` (or Ctrl+F) opens **database-wide search**. Use Left/Right in the first field to choose record keys, record values, or bucket names; Tab moves to the substring field and Enter runs the search. It includes every root and nested bucket in the active database, independent of the current local filters. Matches are case-sensitive byte substrings and accept text, hex, or base64 input. An empty term lists all entries of the chosen type. Results are paginated, show their bucket path, and support the same automatic arrow navigation. Enter opens a bucket result or shows a record at its location; `o` opens the result's bucket. Esc closes search results. Press `e` in a record preview to edit that exact match.

Database-wide search retains only the requested page and a bounded value preview, with at most 128 previous-page checkpoints. It closes its read transaction after at most 1,024 inspected entries and resumes with a binary-safe path/key checkpoint. This bounds per-transaction bucket caches and permits page reclamation during long scans. Searches check cancellation during traversal. Results across scan blocks and later pages are not a single atomic snapshot: concurrent writes can change later matches. No entire database or record collection is loaded into memory.

Ordinary dialogs accept the same binary encodings as the CLI. Use Tab to change fields, arrows/Home/End to move the cursor, Ctrl+U to clear a field, and Enter to submit. Long fields wrap and scroll to keep the cursor visible. In a text field, `h` types a character; `F1` opens help, and `h` or Esc closes help while preserving the dialog. Deletions require typing `yes`. Terminal controls in stored data are escaped. Themes adapt to the terminal background.

### Record editor

`r` creates/replaces a record; `e` opens the selected record in the multiline editor. Keys accept `text:`, `hex:`, and `base64:` encodings. Values use explicit text, JSON, or hex modes; prefixes typed into a text value are literal text. UTF-8 values retain line breaks, and binary values open in hex mode. The decoded value limit is 1 MiB (up to 2 MiB of hex characters). Larger values can be modified through the CLI.

`e` / `у` also opens the editor directly from a record's content preview,
including previews opened from database-wide search. The complete current value
of the viewed key is loaded asynchronously, up to the same 1 MiB edit limit;
the table selection and truncated preview are not used as editor input. Esc
asks before discarding the draft and returning to the preview; Ctrl+S saves and refreshes it.
Read-only databases and BASE/OTHER diff previews remain non-editable.

Valid JSON opens with two-space indentation and syntax highlighting. Formatting is only for editing: saving valid JSON from JSON or text mode uses compact JSON. Whitespace inside strings, numeric tokens (including large integers), duplicate fields, and field order are preserved. Invalid JSON in JSON mode blocks saving and reports a line and column. Hex mode preserves bytes exactly, including JSON whitespace.

| Key | Action |
|---|---|
| Enter in key field / Tab | Focus the value |
| Enter in value | New line with existing indentation |
| Tab in value | Insert two spaces |
| Shift+Tab | Focus the key |
| Arrows / Page Up/Down | Move within the document |
| Home/End / Ctrl+Home/End | Line start/end / document start/end |
| F4 / Ctrl+F | Validate and format as JSON |
| F6 | Switch JSON → text → hex → UTF-8 text/JSON |
| Ctrl+S | Save atomically |
| Esc | Confirm discarding draft and closing |

The editor renders and highlights a bounded window around the cursor. Indentation expansion is also limited to 1 MiB. Saving runs asynchronously; a failed or canceled write leaves the draft open. Opening or formatting a value does not write to the database. CLI put/import behavior is unchanged.

Read-only databases show `READ ONLY` in the context line. Mutation hotkeys explain the restriction immediately; browsing, search, diff and export remain available. Error messages adapt to the available screen space; F2 opens the complete message when it does not fit.

## Import and export format

Record previews pretty-print complete JSON with two-space indentation.
Complete JSON value previews use syntax colors for keys, strings, numbers,
booleans and null in record details, database-wide search details, and both
BASE/OTHER diff details. Colors adapt to light/dark terminals. Highlighting is
cached for the bounded preview and rebuilt on resize; viewing never writes data.
Ordinary record previews are limited to 64 KiB; oversized ordinary previews
remain plain. Diff lists show 4 KiB previews per side. Enter on a diff record
opens the [complete two-column value viewer](DIFF.md#complete-value-viewer),
with red removals, green additions and unchanged context. `/` in comparison
searches changed keys across all buckets without running comparison again.
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
