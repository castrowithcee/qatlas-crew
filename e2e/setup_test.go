//go:build e2e

package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/ptytest"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

const foreignHooks = `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "say done"}]}]}}` + "\n"

// codexTestHome gives a test home whose Codex home holds a hooks.json with a hook of someone else and no
// config.toml. CODEX_HOME is unset there, so the binary takes .codex in the test home.
func codexTestHome(t *testing.T) (*testenv.Env, string) {
	t.Helper()
	e := testenv.Setup(t)
	home := filepath.Join(e.Home, ".codex")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "hooks.json"), []byte(foreignHooks), 0o600); err != nil {
		t.Fatal(err)
	}
	return e, home
}

// unchanged fails the test unless the Codex home holds just the foreign hooks.json and nothing was backed up.
func unchanged(t *testing.T, e *testenv.Env, home string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "hooks.json"))
	if err != nil || string(data) != foreignHooks {
		t.Errorf("hooks.json changed: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); err == nil {
		t.Errorf("config.toml was written")
	}
	if _, err := os.Stat(filepath.Join(e.Home, ".qatlas", "qcrew", "backups")); err == nil {
		t.Errorf("a backup was made")
	}
}

func TestCodexHooksWithoutTerminalWriteNothing(t *testing.T) {
	e, home := codexTestHome(t)
	for _, args := range [][]string{{"setup", "codex-hooks"}, {"setup", "codex-hooks", "--remove"}} {
		cmd := exec.Command(binary, args...)
		cmd.Dir, cmd.Env = e.Home, e.Environ()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(stderr.String(), "needs a terminal") {
			t.Errorf("%v: %v, stderr %q", args, err, stderr.String())
		}
	}
	unchanged(t, e, home)
}

// atTerminal runs the binary at a pseudo terminal, answers its question with answer and returns the exit
// code and what it printed.
func atTerminal(t *testing.T, e *testenv.Env, answer string, args ...string) (int, string) {
	t.Helper()
	term, err := ptytest.Start(ptytest.Command{Path: binary, Args: args, Dir: e.Home,
		Env: append(e.Environ(), "TERM=xterm-256color"), Size: ptytest.Size{Columns: 160, Rows: 60}})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	if err := term.WaitForText(10*time.Second, "[y/N]"); err != nil {
		t.Fatalf("no question: %v\n%s", err, term.Output())
	}
	if _, err := term.Write([]byte(answer + "\n")); err != nil {
		t.Fatal(err)
	}
	code, err := term.Wait(10 * time.Second)
	if err != nil {
		t.Fatalf("wait: %v\n%s", err, term.Output())
	}
	return code, term.Output()
}

func TestCodexHooksDeclinedAtTheTerminalWriteNothing(t *testing.T) {
	e, home := codexTestHome(t)
	code, out := atTerminal(t, e, "n", "setup", "codex-hooks")
	if code != 2 || !strings.Contains(out, "+++ "+filepath.Join(home, "hooks.json")) || !strings.Contains(out, "nothing was written") {
		t.Fatalf("code %d:\n%s", code, out)
	}
	unchanged(t, e, home)
}

func TestCodexHooksConfirmedAndRemoved(t *testing.T) {
	e, home := codexTestHome(t)
	if code, out := atTerminal(t, e, "y", "setup", "codex-hooks"); code != 0 || !strings.Contains(out, "Codex hooks: configured") {
		t.Fatalf("set up: code %d:\n%s", code, out)
	}
	data, err := os.ReadFile(filepath.Join(home, "hooks.json"))
	if err != nil || !strings.Contains(string(data), binary+" hook codex") || !strings.HasPrefix(string(data), `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "say done"}]},`) {
		t.Fatalf("hooks.json: %s %v", data, err)
	}
	cmd := exec.Command(binary, "setup", "--check", "--output", "json")
	cmd.Dir, cmd.Env = e.Home, e.Environ()
	if out, err := cmd.Output(); err != nil || !strings.Contains(string(out), `"name":"codex"`) ||
		!strings.Contains(string(out), `"integration":"configured"`) {
		t.Fatalf("check: %s %v", out, err)
	}
	if code, out := atTerminal(t, e, "y", "setup", "codex-hooks", "--remove"); code != 0 || !strings.Contains(out, "Codex hooks: missing") {
		t.Fatalf("remove: code %d:\n%s", code, out)
	}
	if data, err := os.ReadFile(filepath.Join(home, "hooks.json")); err != nil || string(data) != foreignHooks {
		t.Errorf("hooks.json not restored: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); err == nil {
		t.Errorf("config.toml left behind")
	}
}
