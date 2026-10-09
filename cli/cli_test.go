package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/islandsexit/BltCli/cli"
	"github.com/islandsexit/BltCli/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandWorkflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	var out, diag bytes.Buffer
	run := func(args ...string) error {
		out.Reset()
		diag.Reset()
		return cli.Run(context.Background(), args, strings.NewReader(""), &out, &diag)
	}
	must := func(args ...string) {
		t.Helper()
		if e := run(args...); e != nil {
			t.Fatal(args, e)
		}
	}
	must("db", "create", path)
	if e := run("db", "create", path); e == nil {
		t.Fatal("overwrote database")
	}
	must("bucket", "create", "--db", path, "--bucket", "users")
	must("record", "put", "--db", path, "--bucket", "users", "hex:ff", "hex:00ff")
	must("record", "put", "--db", path, "--bucket", "users", "a", "")
	if e := run("record", "update", "--db", path, "--bucket", "users", "missing", "v"); e == nil {
		t.Fatal("update accepted missing key")
	}
	must("record", "update", "--db", path, "--bucket", "users", "a", "")
	must("record", "get", "--db", path, "--bucket", "users", "a")
	var record domain.Record
	if e := json.Unmarshal(out.Bytes(), &record); e != nil || len(record.Value) != 0 {
		t.Fatalf("%s %v", out.String(), e)
	}
	must("record", "list", "--db", path, "--bucket", "users", "--limit", "1")
	if !strings.Contains(diag.String(), "--after") {
		t.Fatal("missing continuation")
	}
	if strings.Contains(out.String(), "continue") {
		t.Fatal("diagnostics polluted stdout")
	}
	must("record", "exists", "--db", path, "--bucket", "users", "missing")
	if !strings.Contains(out.String(), "false") {
		t.Fatal(out.String())
	}
	if e := run("record", "delete", "--db", path, "--bucket", "users", "a"); e == nil {
		t.Fatal("unconfirmed delete")
	}
	must("record", "delete", "--db", path, "--bucket", "users", "--yes", "a")
	export := filepath.Join(t.TempDir(), "export.jsonl")
	single := filepath.Join(t.TempDir(), "single.jsonl")
	must("record", "export", "--db", path, "--bucket", "users", "--output", single, "hex:ff")
	if data, e := os.ReadFile(single); e != nil || bytes.Count(data, []byte("\n")) != 1 {
		t.Fatalf("single export: %s %v", data, e)
	}
	must("export", "--db", path, "--bucket", "users", "--output", export)
	if e := run("export", "--db", path, "--bucket", "users", "--output", export); e == nil {
		t.Fatal("overwrote export")
	}
	must("bucket", "create", "--db", path, "--bucket", "copy")
	must("import", "--db", path, "--bucket", "copy", "--input", export)
	must("record", "import", "--db", path, "--bucket", "copy", "--input", single, "--duplicates", "skip")
	must("db", "stats", "--db", path)
	must("record", "get", "--db", path, "--bucket", "copy", "hex:ff")
	if e := json.Unmarshal(out.Bytes(), &record); e != nil || !bytes.Equal(record.Value, []byte{0, 255}) {
		t.Fatalf("%s %v", out.String(), e)
	}
	if e := run("db", "delete", path); e == nil {
		t.Fatal("unconfirmed database delete")
	}
	must("db", "delete", "--yes", path)
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
func TestHelpAndValidation(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"record", "list", "--help"}} {
		if e := cli.Run(context.Background(), args, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); e != nil {
			t.Fatal(e)
		}
	}
	for _, args := range [][]string{{"record"}, {"record", "get"}, {"nonsense"}, {"tui", "--limit", "0"}} {
		if e := cli.Run(context.Background(), args, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestExportLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	var output bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		output.Reset()
		if e := cli.Run(context.Background(), args, strings.NewReader(""), &output, &bytes.Buffer{}); e != nil {
			t.Fatal(e)
		}
	}
	run("db", "create", path)
	run("bucket", "create", "--db", path, "--bucket", "b")
	for _, k := range []string{"a", "b", "c"} {
		run("record", "put", "--db", path, "--bucket", "b", k, "v")
	}
	run("export", "--db", path, "--bucket", "b", "--limit=1")
	if bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatal(output.String())
	}
	run("export", "--db", path, "--bucket", "b", "-limit", "1")
	if bytes.Count(output.Bytes(), []byte("\n")) != 1 {
		t.Fatal("single-dash limit ignored")
	}
	run("export", "--db", path, "--bucket", "b")
	if bytes.Count(output.Bytes(), []byte("\n")) != 3 {
		t.Fatal(output.String())
	}
}

func TestSearchFilterCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	var out bytes.Buffer
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		if e := cli.Run(context.Background(), args, strings.NewReader(""), &out, &bytes.Buffer{}); e != nil {
			t.Fatal(e)
		}
	}
	run("db", "create", path)
	for _, name := range []string{"team:alpha", "team:beta", "other"} {
		run("bucket", "create", "--db", path, "--bucket", name)
	}
	run("bucket", "list", "--db", path, "--contains", "alpha")
	if bytes.Count(out.Bytes(), []byte("\n")) != 1 {
		t.Fatal(out.String())
	}
	run("record", "put", "--db", path, "--bucket", "other", "user:01", "blue")
	run("record", "put", "--db", path, "--bucket", "other", "user:02", "red")
	run("record", "search", "--db", path, "--bucket", "other", "--contains", "user:", "--value-contains", "red")
	var got domain.Record
	if e := json.Unmarshal(out.Bytes(), &got); e != nil || string(got.Key) != "user:02" {
		t.Fatalf("%s %v", out.String(), e)
	}
	run("export", "--db", path, "--bucket", "other", "--value-contains", "red")
	if bytes.Count(out.Bytes(), []byte("\n")) != 1 {
		t.Fatal("export ignored value filter")
	}
}
