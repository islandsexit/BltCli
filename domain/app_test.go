package domain_test

import (
	"bltCli/domain"
	"bltCli/repository/adapters"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestMultipleHandles(t *testing.T) {
	app := domain.NewApp()
	defer app.CloseAll()
	for _, name := range []string{"first", "second"} {
		db, e := adapters.Open(filepath.Join(t.TempDir(), "db"), true, false)
		if e != nil {
			t.Fatal(e)
		}
		if e = app.Add(name, db); e != nil {
			t.Fatal(e)
		}
		if e = db.CreateBucket(domain.Path{[]byte(name)}); e != nil {
			t.Fatal(e)
		}
	}
	if e := app.Select("first"); e != nil {
		t.Fatal(e)
	}
	name, db, e := app.Active()
	if e != nil || name != "first" {
		t.Fatalf("%s %v", name, e)
	}
	names, e := db.Buckets(context.Background(), nil, nil, 100)
	if e != nil || len(names) != 1 || string(names[0]) != "first" {
		t.Fatal("database handles share state")
	}
	if e = app.Close("first"); e != nil {
		t.Fatal(e)
	}
	name, _, e = app.Active()
	if e != nil || name != "second" {
		t.Fatalf("%s %v", name, e)
	}
	if e = app.Select("missing"); e == nil {
		t.Fatal("selected unknown handle")
	}
	if e = app.CloseAll(); e != nil {
		t.Fatal(e)
	}
	if _, _, e = app.Active(); !errors.Is(e, domain.ErrClosed) {
		t.Fatal(e)
	}
}
