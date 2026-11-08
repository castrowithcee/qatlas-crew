//go:build integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/session"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// TestMain starts the private tmux server for the run, except in the processes the tests start from this
// binary: a service process must neither start nor end a server of its own.
func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) != "" {
		os.Exit(m.Run())
	}
	os.Exit(testenv.TmuxMain(m))
}

type sessionJSON struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Tool    string `json:"tool"`
	Title   string `json:"title"`
	Status  string `json:"status"`
}

func TestSessionCommandsAgainstTheService(t *testing.T) {
	srv := testenv.Tmux(t)
	env := serviceTestEnv(t)
	t.Setenv(session.SocketEnv, srv.Socket())
	t.Setenv("SHELL", "/bin/sh")
	dir := filepath.Join(env.Root, "app")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "project", "add", dir, "--output", "json")
	var p projectJSON
	if code != 0 || json.Unmarshal([]byte(out), &p) != nil {
		t.Fatalf("project add: code=%d out=%q err=%q", code, out, errOut)
	}
	t.Cleanup(func() { _, _ = srv.Run("kill-session", "-t", "="+session.SessionName(p.ID)) })

	code, out, errOut = run(t, "session", "start", "--project", "app", "--tool", "shell", "--output", "json")
	var s sessionJSON
	if code != 0 || json.Unmarshal([]byte(out), &s) != nil || s.Project != p.ID || s.Status != "running" || s.Title != "shell" {
		t.Fatalf("start: code=%d out=%q err=%q", code, out, errOut)
	}

	code, out, errOut = run(t, "session", "list", "--project", p.ID, "--output", "json")
	var list []sessionJSON
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list) != 1 || list[0] != s {
		t.Fatalf("list: code=%d out=%q err=%q", code, out, errOut)
	}

	// The test's output is no terminal, so attach only prints the command.
	code, out, errOut = run(t, "session", "attach", s.ID, "--output", "json")
	want := `{"command":"` + strings.Join(session.Server{Socket: srv.Socket()}.AttachCommand(p.ID, s.ID), " ") + `"}`
	if code != 0 || strings.TrimSpace(out) != want {
		t.Fatalf("attach: code=%d out=%q err=%q, want %s", code, out, errOut, want)
	}

	code, _, errOut = run(t, "session", "start", "--project", "app", "--tool", "vim")
	if code != 2 || !strings.HasPrefix(errOut, "qcrew: invalid-request: ") {
		t.Fatalf("unknown tool: code=%d err=%q", code, errOut)
	}
	code, _, errOut = run(t, "project", "remove", "app", "--confirm")
	if code != 2 || !strings.HasPrefix(errOut, "qcrew: in-use: ") || !strings.Contains(errOut, "qcrew session stop") {
		t.Fatalf("remove with a running session: code=%d err=%q", code, errOut)
	}

	if code, _, errOut = run(t, "session", "stop", s.ID, "--confirm"); code != 0 {
		t.Fatalf("stop: code=%d err=%q", code, errOut)
	}
	code, out, _ = run(t, "session", "list", "--output", "json")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("list after stop: code=%d out=%q", code, out)
	}
	code, _, errOut = run(t, "session", "stop", s.ID, "--confirm")
	if code != 2 || !strings.Contains(errOut, "not-found") {
		t.Fatalf("second stop: code=%d err=%q", code, errOut)
	}
	if code, _, errOut = run(t, "project", "remove", "app", "--confirm"); code != 0 {
		t.Fatalf("remove after stop: code=%d err=%q", code, errOut)
	}
}
