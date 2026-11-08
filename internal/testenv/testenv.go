// Package testenv gives tests a hermetic process environment: home, XDG and temporary directories under the
// test's own temporary directory, a fixed locale and terminal size, an injectable clock, seeded randomness and
// a private tmux server.
package testenv

import (
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Fixed terminal geometry that tests may rely on.
const (
	Columns = "80"
	Lines   = "24"
)

// Env describes the directories of one hermetic test environment.
type Env struct {
	Root    string // parent of every directory below
	Home    string
	Config  string
	Data    string
	State   string
	Cache   string
	Runtime string // XDG_RUNTIME_DIR, mode 0700
	Tmp     string
}

// Setup creates the directories under t.TempDir and points HOME, XDG_*, TMPDIR and the locale, color, terminal
// and time zone variables at them for the duration of the test, and clears QCREW_CONFIG and QCREW_DIR. It uses
// t.Setenv, so it cannot be combined with t.Parallel.
func Setup(t testing.TB) *Env {
	t.Helper()
	root := t.TempDir()
	e := &Env{
		Root:    root,
		Home:    filepath.Join(root, "home"),
		Config:  filepath.Join(root, "xdg", "config"),
		Data:    filepath.Join(root, "xdg", "data"),
		State:   filepath.Join(root, "xdg", "state"),
		Cache:   filepath.Join(root, "xdg", "cache"),
		Runtime: filepath.Join(root, "xdg", "run"),
		Tmp:     filepath.Join(root, "tmp"),
	}
	for _, d := range []string{e.Home, e.Config, e.Data, e.State, e.Cache, e.Tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("testenv: %v", err)
		}
	}
	// Runtime directories must not be accessible to others, like the real one.
	if err := os.MkdirAll(e.Runtime, 0o700); err != nil {
		t.Fatalf("testenv: %v", err)
	}
	for _, kv := range e.vars() {
		t.Setenv(kv[0], kv[1])
	}
	// The variables that point qcrew at another configuration must not reach a test from the real
	// environment; empty counts as unset.
	for _, name := range []string{"QCREW_CONFIG", "QCREW_DIR"} {
		t.Setenv(name, "")
	}
	return e
}

func (e *Env) vars() [][2]string {
	return [][2]string{
		{"HOME", e.Home},
		{"XDG_CONFIG_HOME", e.Config},
		{"XDG_DATA_HOME", e.Data},
		{"XDG_STATE_HOME", e.State},
		{"XDG_CACHE_HOME", e.Cache},
		{"XDG_RUNTIME_DIR", e.Runtime},
		{"TMPDIR", e.Tmp},
		{"LANG", "C.UTF-8"},
		{"LC_ALL", "C.UTF-8"},
		{"NO_COLOR", "1"},
		{"COLUMNS", Columns},
		{"LINES", Lines},
		{"TZ", "UTC"},
	}
}

// Environ returns the complete environment for a child process: only the hermetic variables plus PATH, so
// nothing of the real user environment leaks in.
func (e *Env) Environ() []string {
	out := []string{"PATH=" + os.Getenv("PATH")}
	for _, kv := range e.vars() {
		out = append(out, kv[0]+"="+kv[1])
	}
	return out
}

// Clock is the source of time for code under test.
type Clock interface {
	Now() time.Time
}

// SystemClock is the real clock.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now() }

// FakeClock is a manually advanced clock, safe for concurrent use.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock returns a clock standing at start.
func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{now: start} }

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set moves the clock to t.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Rand returns a random source with the given seed and logs the seed, so a failing run can be reproduced.
func Rand(t testing.TB, seed int64) *rand.Rand {
	t.Helper()
	t.Logf("random seed: %d", seed)
	return rand.New(rand.NewSource(seed))
}
