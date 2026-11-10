//go:build e2e

package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/i18n"

	"github.com/castrowithcee/qatlas-crew/internal/client"
	"github.com/castrowithcee/qatlas-crew/internal/control"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// The sizes of the goldens: a narrow terminal that stacks the panes and a wide one with columns.
var goldenSizes = map[string][2]int{"narrow": {60, 30}, "wide": {120, 40}}

var goldenLangs = map[string]i18n.Lang{"en": i18n.English, "de": i18n.German}

// goldenBackend holds one project with open inbox entries and sessions of every status.
func goldenBackend() *fakeBackend {
	b := demoBackend()
	b.sessions = []control.Session{
		{ID: "s1", Project: "p-alpha", Tool: "claude", Title: "refactor parser", Status: "running", Since: epoch.Add(-90 * time.Second)},
		{ID: "s2", Project: "p-alpha", Tool: "codex", Title: "write docs", Status: "waiting", Since: epoch.Add(-3 * time.Hour)},
		{ID: "s3", Project: "p-alpha", Tool: "claude", Title: "fix flaky test", Status: "error", Since: epoch.Add(-26 * time.Hour)},
		{ID: "s4", Project: "p-beta", Tool: "shell", Title: "build watcher", Status: "idle", Since: epoch.Add(-5 * time.Second)},
		{ID: "s5", Project: "p-beta", Tool: "codex", Title: "old experiment", Status: "exited", Since: epoch.Add(-50 * time.Hour)},
	}
	return b
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	golden := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("screen differs from %s:\ngot:\n%s\nwant:\n%s", golden, got, want)
	}
}

// checkScreen holds a screen to the size of the terminal: every line exactly as wide, no line more. The
// goldens are drawn in ASCII, so a rune is a cell.
func checkScreen(t *testing.T, screen string, width, height int) {
	t.Helper()
	lines := strings.Split(screen, "\n")
	if len(lines) != height {
		t.Errorf("%d lines, want %d", len(lines), height)
	}
	for i, l := range lines {
		if w := utf8.RuneCountInString(l); w != width {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, width, l)
		}
	}
}

// The goldens fix what a person sees, byte for byte. Run `just snap-update` to rewrite them.
func TestGoldenScreens(t *testing.T) {
	missing := goldenBackend()
	missing.sessErr = &cli.Failure{Code: client.CodeDependencyMissing, Message: "tmux is not installed.",
		NextStep: "Run 'qcrew setup' to install what qcrew needs."}
	screens := []struct {
		name    string
		backend func() *fakeBackend
		keys    []string
	}{
		{"main", goldenBackend, nil},
		{"project-beta", goldenBackend, []string{"down"}},
		{"sessions", goldenBackend, []string{"enter", "down"}},
		{"inbox", goldenBackend, []string{"2"}},
		{"full-projects", goldenBackend, []string{"f"}},
		{"full-sessions", goldenBackend, []string{"enter", "f"}},
		{"stop", goldenBackend, []string{"enter", "x"}},
		{"settings", goldenBackend, []string{"4"}},
		{"help", goldenBackend, []string{"?"}},
		{"setup", goldenBackend, []string{"5"}},
		{"setup-install", goldenBackend, []string{"5", "down", "i"}},
		{"setup-result", goldenBackend, []string{"5", "down", "i", "y"}},
		{"tmux-missing", func() *fakeBackend { return missing }, nil},
	}
	for _, s := range screens {
		for lang, l := range goldenLangs {
			for size, wh := range goldenSizes {
				t.Run(s.name+"."+lang+"."+size, func(t *testing.T) {
					m := newTestModel(t, l, s.backend(), wh[0], wh[1])
					send(m, s.keys...)
					screen := m.render()
					checkScreen(t, screen, wh[0], wh[1])
					checkGolden(t, s.name+"."+lang+"."+size+".golden", screen+"\n")
				})
			}
		}
	}
}
