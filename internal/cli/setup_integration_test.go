//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/adapter/codex/integration"
	"github.com/castrowithcee/qatlas-crew/internal/setup"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// codexHome points the Codex hooks of the fake host into a test home and returns that configuration. The
// paths come from the test environment itself, never from the process environment.
func codexHome(t *testing.T) integration.Config {
	t.Helper()
	e := testenv.Setup(t)
	cfg := integration.Config{CodexHome: filepath.Join(e.Home, ".codex"),
		DataDir: filepath.Join(e.Home, ".qatlas", "qcrew"), Program: filepath.Join(e.Root, "bin", "qcrew")}
	if !strings.HasPrefix(cfg.CodexHome, e.Root+string(filepath.Separator)) {
		t.Fatalf("%s is outside the test directory", cfg.CodexHome)
	}
	setupHost.codex = func() (integration.Config, error) { return cfg, nil }
	return cfg
}

const foreignHooksFile = `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "say done"}]}]}}` + "\n"

func readOr(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "<none>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCodexHooksDeclinedWritesNothing(t *testing.T) {
	atTerminal(t, true)
	h := &fakeHost{found: []string{setup.Tmux, setup.Claude, setup.Codex}, replies: []error{declinedForTest()}}
	h.install(t, "linux")
	cfg := codexHome(t)
	if err := os.MkdirAll(cfg.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(cfg.CodexHome, integration.HooksFile)
	if err := os.WriteFile(hooksPath, []byte(foreignHooksFile), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "setup", "codex-hooks")
	if code != 2 || len(h.questions) != 1 || !strings.Contains(errOut, "nothing was written") ||
		!strings.Contains(out, "+++ "+hooksPath) {
		t.Fatalf("code %d, questions %q, out %q, err %q", code, h.questions, out, errOut)
	}
	if got := readOr(t, hooksPath); got != foreignHooksFile {
		t.Errorf("hooks.json changed: %q", got)
	}
	if got := readOr(t, filepath.Join(cfg.CodexHome, integration.ConfigFile)); got != "<none>" {
		t.Errorf("config.toml written: %q", got)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "backups")); err == nil {
		t.Errorf("a backup was made")
	}
}

func TestCodexHooksSetUpAndRemoved(t *testing.T) {
	atTerminal(t, true)
	h := &fakeHost{found: []string{setup.Tmux, setup.Claude, setup.Codex}}
	h.install(t, "linux")
	cfg := codexHome(t)
	if err := os.MkdirAll(cfg.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(cfg.CodexHome, integration.HooksFile)
	if err := os.WriteFile(hooksPath, []byte(foreignHooksFile), 0o600); err != nil {
		t.Fatal(err)
	}

	// setup offers the hooks of an installed Codex and writes them after the yes.
	code, out, errOut := run(t, "setup")
	if code != 0 || len(h.questions) != 1 || !strings.Contains(out, "Codex hooks: configured") {
		t.Fatalf("setup: code %d, questions %q, out %q, err %q", code, h.questions, out, errOut)
	}
	if r, err := cfg.Inspect(); err != nil || r.Status != integration.Configured {
		t.Fatalf("after setup: %+v %v", r, err)
	}
	if code, out, _ := run(t, "setup", "--check"); code != 0 || !strings.Contains(out, "configured") {
		t.Errorf("check: code %d, out %q", code, out)
	}
	// Nothing to do: no question.
	h.questions = nil
	if code, out, _ := run(t, "setup", "codex-hooks"); code != 0 || len(h.questions) != 0 || !strings.Contains(out, "nothing was written") {
		t.Errorf("again: code %d, out %q", code, out)
	}

	code, out, errOut = run(t, "setup", "codex-hooks", "--remove")
	if code != 0 || len(h.questions) != 1 || !strings.Contains(out, "Codex hooks: missing") {
		t.Fatalf("remove: code %d, out %q, err %q", code, out, errOut)
	}
	if got := readOr(t, hooksPath); got != foreignHooksFile {
		t.Errorf("hooks.json not restored: %q", got)
	}
	backups, err := os.ReadDir(filepath.Join(cfg.DataDir, "backups", "codex"))
	if err != nil || len(backups) != 3 {
		t.Errorf("backups: %v %v", backups, err)
	}
}

func TestSetupWithoutTerminalWritesNoHooks(t *testing.T) {
	atTerminal(t, false)
	h := &fakeHost{found: []string{setup.Tmux, setup.Claude, setup.Codex}}
	h.install(t, "linux")
	cfg := codexHome(t)
	for _, args := range [][]string{{"setup"}, {"setup", "codex-hooks"}, {"setup", "codex-hooks", "--remove"}} {
		code, _, errOut := run(t, args...)
		if code != 2 || len(h.questions) != 0 || !strings.Contains(errOut, "needs a terminal") {
			t.Errorf("%v: code %d, err %q", args, code, errOut)
		}
	}
	if _, err := os.Stat(cfg.CodexHome); err == nil {
		t.Errorf("the Codex home was created")
	}
	// Looking needs no terminal.
	if code, out, _ := run(t, "setup", "--check"); code != 0 || !strings.Contains(out, "missing") {
		t.Errorf("check: code %d, out %q", code, out)
	}
}
