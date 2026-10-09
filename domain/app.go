package domain

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// App manages independent database handles in a session.
type App struct {
	mu      sync.Mutex
	handles map[string]BoltDbAdapter
	active  string
}

// NewApp constructs an empty session.
func NewApp() *App { return &App{handles: make(map[string]BoltDbAdapter)} }

// Add registers and selects a database.
func (a *App) Add(name string, db BoltDbAdapter) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if name == "" || db == nil {
		return errors.New("name and database required")
	}
	if _, ok := a.handles[name]; ok {
		return fmt.Errorf("database %q already open", name)
	}
	a.handles[name] = db
	a.active = name
	return nil
}

// Select switches the active database.
func (a *App) Select(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.handles[name]; !ok {
		return fmt.Errorf("database %q not open", name)
	}
	a.active = name
	return nil
}

// Active returns the selected handle.
func (a *App) Active() (string, BoltDbAdapter, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	db, ok := a.handles[a.active]
	if !ok {
		return "", nil, ErrClosed
	}
	return a.active, db, nil
}

// Lookup returns a named handle without changing the active database.
func (a *App) Lookup(name string) (BoltDbAdapter, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	db, ok := a.handles[name]
	return db, ok
}

// Names returns sorted open database names.
func (a *App) Names() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	names := make([]string, 0, len(a.handles))
	for n := range a.handles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Close removes a handle after closing it successfully.
func (a *App) Close(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	db, ok := a.handles[name]
	if !ok {
		return ErrClosed
	}
	if err := db.Close(); err != nil {
		return err
	}
	delete(a.handles, name)
	if a.active == name {
		a.active = ""
		for n := range a.handles {
			a.active = n
			break
		}
	}
	return nil
}

// CloseAll releases the session's resources.
func (a *App) CloseAll() error {
	var errs []error
	for _, n := range a.Names() {
		errs = append(errs, a.Close(n))
	}
	return errors.Join(errs...)
}
