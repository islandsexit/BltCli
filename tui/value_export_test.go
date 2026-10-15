package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/islandsexit/BltCli/domain"
	"github.com/islandsexit/BltCli/repository/adapters"
)

func TestValueJSONFilename(t *testing.T) {
	if got := valueJSONFilename(domain.Path{[]byte("users"), []byte("profiles")}, []byte("alice")); got != "users_profiles_alice.json" {
		t.Fatal(got)
	}
	for _, key := range [][]byte{[]byte("../../bad:name?"), {0, 255}, []byte("CON"), []byte(strings.Repeat("界", 300))} {
		got := valueJSONFilename(nil, key)
		if !utf8.ValidString(got) || len(got) > 186 || strings.ContainsAny(got, `/\:*?"<>|`) || got == "CON.json" {
			t.Fatalf("unsafe name: %q", got)
		}
	}
}

func TestPreviewValueExport(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "demo.db")
	db, err := adapters.Open(dbpath, true, false)
	if err != nil {
		t.Fatal(err)
	}
	path := domain.Path{[]byte("users")}
	if err := db.CreateBucket(path); err != nil {
		t.Fatal(err)
	}
	value := []byte(`{"data":"` + strings.Repeat("x", 100<<10) + `"}`)
	key := []byte("alice")
	if err := db.Put(path, key, value); err != nil {
		t.Fatal(err)
	}
	if err := db.Put(path, []byte("invalid"), []byte("not JSON")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = adapters.Open(dbpath, false, true)
	if err != nil {
		t.Fatal(err)
	}
	app := domain.NewApp()
	if err := app.Add("demo", db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.CloseAll(); err != nil {
			t.Error(err)
		}
	})
	m := model{ctx: context.Background(), app: app, workers: &workers{}, width: 100, height: 30, path: path, info: domain.Info{ReadOnly: true}}
	m.showRecordDetail("Value:\n", []byte("truncated preview"))
	m.detailKey = key
	next, _ := m.Update(runeKey("E"))
	m = next.(model)
	if m.modal == nil || len(m.modal.fields) != 1 {
		t.Fatal("export dialog/default missing")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if m.modal.fields[0].value != filepath.Join(cwd, "users_alice.json") {
		t.Fatal("output path must default to a full filename in the application's working directory")
	}
	m.modal.fields[0].value = ""
	next, cmd := m.submit()
	m = next.(model)
	if cmd != nil || !m.statusError || m.modal == nil {
		t.Fatal("empty path must keep dialog open")
	}
	file := filepath.Join(dir, "chosen.json")
	m.modal.fields[0].value = file
	next, cmd = m.submit()
	m = completeLoad(t, next.(model), cmd)
	got, err := os.ReadFile(file)
	if err != nil || string(got) != string(value) || m.modal != nil {
		t.Fatalf("complete value export failed: %v", err)
	}
	next, _ = m.Update(runeKey("E"))
	m = next.(model)
	m.modal.fields[0].value = file
	next, cmd = m.submit()
	m = completeLoad(t, next.(model), cmd)
	if !m.statusError || m.modal == nil {
		t.Fatal("overwrite must fail and keep dialog")
	}
	got, err = os.ReadFile(file)
	if err != nil || string(got) != string(value) {
		t.Fatal("existing file changed")
	}
	m.detailKey = []byte("invalid")
	badFile := filepath.Join(dir, "invalid.json")
	m.modal.fields[0].value = badFile
	next, cmd = m.submit()
	m = completeLoad(t, next.(model), cmd)
	if !m.statusError || m.modal == nil {
		t.Fatal("invalid JSON must keep dialog")
	}
	if _, err := os.Stat(badFile); !os.IsNotExist(err) {
		t.Fatalf("failed output was not removed: %v", err)
	}
}
