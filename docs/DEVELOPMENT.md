# Development and releases

[Back to README](../README.md)

## Build and run

### Install with Go

Install directly from GitHub without cloning the repository:

```sh
go install github.com/islandsexit/BltCli/cmd/boltcli@latest
boltcli --help
```

The executable is named `boltcli` (`boltcli.exe` on Windows). Go installs
it in `GOBIN` when configured, otherwise in `bin` under `GOPATH`
(usually `$HOME/go/bin` on Linux/macOS or `%USERPROFILE%\go\bin` on Windows).
Add that directory to PATH to run the command from any directory.

Use `@main` to install the current main branch or a release tag such as
`@v0.2.0` to install a specific version. `@latest` prefers the latest release
tag, so publish a new tag after changes to the module path and command layout.
Existing release tags retain their original code.

Modern Go uses [`go install` for executables](https://go.dev/doc/go-get-install-deprecation);
`go get` manages dependencies and does not install the application.

For a local checkout, run `make install` or `go install ./cmd/boltcli`.

Use the Go version specified by `go.mod` (currently 1.27.2) or a compatible newer toolchain.

```sh
go build -o boltcli ./cmd/boltcli
./boltcli --help
./boltcli tui
./boltcli tui first.db second.db
./boltcli tui --read-only first.db
```

On Windows, build with `go build -o boltcli.exe ./cmd/boltcli` and run `./boltcli.exe`.

The terminal controls the font size; choose a smaller size in its profile
settings (for example, 12 pt). BoltCLI uses bold accent-colored record keys,
regular muted values, and pastel red errors. Selected records retain their
highlighted background, with bold keys and regular values.

## Development and releases

GNU Make provides `make build`, `make run ARGS=--help`, `make test`,
`make vet`, `make check`, `make fmt`, `make bench`, `make coverage`,
`make race`, `make tidy`, and `make clean`. Builds go into `bin/`.
Race tests require CGO and a compatible C compiler. On Windows, use GNU
Make with `cmd.exe` available; Git must also be on PATH for release tasks.

Install [GoReleaser v2.8 or newer](https://goreleaser.com/getting-started/install/oss/).
With a GitHub remote configured, `make release-check` validates
`.goreleaser.yaml`; `make snapshot` builds
local archives in `dist/` without publishing. The release matrix includes
macOS, Linux, and Windows on amd64 and arm64. Windows archives are ZIP;
macOS and Linux archives are tar.gz. Archives include the README and all
documentation guides, with SHA-256 checksums in `checksums.txt`.

To publish, configure a GitHub remote, push the commits and a semantic
version tag such as `v0.1.0`, set `GITHUB_TOKEN` with release write access,
then run `make release` from a clean checkout of that tag. Release hooks
run tests and vet before building. `make snapshot` and `make release`
replace generated contents of `dist/`.

Signature: Written by Dem4ik.

### First GitHub release

The [release workflow](../.github/workflows/release.yml) runs on pushed `v*`
tags. It tests on Linux, macOS ARM64 (`macos-14`), and Windows, then uses
GoReleaser on Linux to cross-compile all six configured platform/architecture
combinations and upload their archives and checksums to GitHub Releases.
It uses GitHub's automatic `GITHUB_TOKEN`; no personal token is needed.

After committing the workflow, publish the first release with:

```sh
git push origin main
git tag -a v0.1.0 -m "First release"
git push origin v0.1.0
```

Follow the **Release** run in the repository's **Actions** tab. Once it
succeeds, download the archives from **Releases**. A prerelease tag such as
`v0.2.0-rc.1` is marked as a prerelease automatically.

## Verification and performance

```sh
go test ./...
go vet ./...
go test -race ./...
go test ./repository/... -run '^$' -bench . -benchmem
go test ./repository/adapters -run '^$' -bench BenchmarkCursorWalk -memprofile heap.out -cpuprofile cpu.out
go tool pprof heap.out
go test ./utils -fuzz FuzzDecode -fuzztime 10s
go test ./repository -fuzz FuzzImport -fuzztime 10s
```

See [performance methodology](PERFORMANCE.md) for dataset creation, memory interpretation, and comparison requirements. The race detector requires a working C compiler on Windows. Unit tests use temporary files and cover binary data, empty values, queries, transaction rollback, readers, lifecycle, malformed imports, and command workflows. Benchmarks report measured allocations; they do not establish multi-gigabyte performance by themselves.
