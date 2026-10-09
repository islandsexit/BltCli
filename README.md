# BoltCLI

A CLI and terminal UI for browsing and managing BoltDB databases.
Supports nested buckets, binary keys and values, database-wide search,
and JSONL import/export.
Compare databases with `boltcli tui --diff base.db other.db`.

## Install

With Go installed:

```sh
go install github.com/islandsexit/BltCli/cmd/boltcli@latest
```

Ensure your Go bin directory is on PATH. See [installation details](docs/DEVELOPMENT.md#install-with-go).

Download an archive for Linux, macOS, or Windows from
[Releases](https://github.com/islandsexit/BltCli/releases), extract it,
and add `boltcli` to your PATH. Builds are available for amd64 and arm64.

To build from source, use the Go version specified in `go.mod`:

```sh
go build -o boltcli ./cmd/boltcli
```

On Windows, use `-o boltcli.exe`.

## Quick start

```sh
boltcli tui
boltcli tui --read-only example.db
boltcli --help
```

Press `o` to open a database, `c` to create one, and `h` for help in the TUI.

## Documentation

- [Usage: CLI, TUI, and import/export](docs/USAGE.md)
- [Database comparison](docs/DIFF.md)
- [Development, testing, and releases](docs/DEVELOPMENT.md)
- [Architecture and limitations](docs/ARCHITECTURE.md)
- [Performance measurements](docs/PERFORMANCE.md)

Written by **Dem4ik**.
