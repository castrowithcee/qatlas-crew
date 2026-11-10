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

type inboxJSON struct {
	ID      string   `json:"id"`
	Project string   `json:"project"`
	Session string   `json:"session"`
	Kind    string   `json:"kind"`
	Status  string   `json:"status"`
	Text    string   `json:"text"`
	Lines   []string `json:"lines"`
	Answer  *string  `json:"answer"`
}

func inboxList(t *testing.T, all bool) []inboxJSON {
	t.Helper()
	args := []string{"inbox", "list", "--output", "json"}
	if all {
		args = append(args, "--all")
	}
	code, out, errOut := run(t, args...)
	var list []inboxJSON
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil {
		t.Fatalf("inbox list: code=%d out=%q err=%q", code, out, errOut)
	}
	return list
}

func inboxStatus(t *testing.T, id string) string {
	t.Helper()
	for _, e := range inboxList(t, true) {
		if e.ID == id {
			return e.Status
		}
	}
	return ""
}

// TestInboxCommandsAgainstTheService drives the inbox commands with a fake claude whose window runs the test
// helper: it reads the payload names that are typed into it, runs the hooks for them, and shows every typed
// line on its screen.
func TestInboxCommandsAgainstTheService(t *testing.T) {
	srv := testenv.Tmux(t)
	env := serviceTestEnv(t)
	t.Setenv(session.SocketEnv, srv.Socket())
	t.Setenv("SHELL", "/bin/sh")
	bin := filepath.Join(env.Root, "bin")
	outDir := filepath.Join(env.Root, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(bin, "qcrew")
	wrapper(t, program, "hook")
	wrapper(t, filepath.Join(bin, "claude"), "claude", "HOME="+env.Home, "XDG_RUNTIME_DIR="+os.Getenv("XDG_RUNTIME_DIR"),
		outEnv+"="+outDir)
	t.Setenv(programEnv, program)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

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
	code, out, errOut = run(t, "session", "start", "--project", "app", "--tool", "claude", "--output", "json")
	var s claudeSessionJSON
	if code != 0 || json.Unmarshal([]byte(out), &s) != nil {
		t.Fatalf("start: code=%d out=%q err=%q", code, out, errOut)
	}
	target := "=" + session.SessionName(p.ID) + ":" + session.WindowName(s.ID)
	screen := func() string { out, _ := srv.Run("capture-pane", "-p", "-t", target); return out }
	send := func(line string) {
		t.Helper()
		if _, err := srv.Run("send-keys", "-t", target, "-l", line); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.Run("send-keys", "-t", target, "Enter"); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("screen of the session:\n%s", screen())
		}
		_, _, _ = run(t, "session", "stop", s.ID, "--confirm")
	})
	waitFor(t, "the fake claude to start", func() bool { return strings.Contains(screen(), "fake claude ready") })

	// An empty inbox.
	if code, out, _ := run(t, "inbox", "list", "--output", "json"); code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty inbox: code=%d out=%q", code, out)
	}

	// A permission request makes an entry of the project, with the last lines of the screen.
	send("permission")
	waitFor(t, "the permission entry", func() bool { return len(inboxList(t, false)) == 1 })
	entry := inboxList(t, false)[0]
	if entry.Kind != "permission" || entry.Status != "open" || entry.Project != p.ID || entry.Session != s.ID ||
		entry.Text != "Bash" || !strings.Contains(strings.Join(entry.Lines, "\n"), "fake claude ready") {
		t.Fatalf("entry: %+v", entry)
	}
	code, out, errOut = run(t, "inbox", "list", "--project", "app")
	if code != 0 || !strings.Contains(out, entry.ID) || !strings.Contains(out, "permission") || !strings.Contains(out, "AGE") {
		t.Fatalf("inbox list: code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = run(t, "inbox", "show", entry.ID)
	if code != 0 || !strings.Contains(out, "LAST SCREEN LINES") || !strings.Contains(out, "| fake claude ready") {
		t.Fatalf("inbox show: code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = run(t, "project", "list", "--output", "json")
	var projects []struct {
		InboxOpen int `json:"inbox_open"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &projects) != nil || len(projects) != 1 || projects[0].InboxOpen != 1 {
		t.Fatalf("project list: code=%d out=%q err=%q", code, out, errOut)
	}

	// A second wait state of the same session makes no second open entry.
	send("permission")
	waitFor(t, "the hook of the second request to end", func() bool {
		calls, _ := os.ReadFile(filepath.Join(outDir, "calls"))
		return strings.Count(string(calls), "permission exit=0") >= 2
	})
	if got := inboxList(t, true); len(got) != 1 {
		t.Fatalf("a second wait state made another entry: %+v", got)
	}

	// The answer appears in the window, and the entry is delivered, then picked after the next event.
	code, out, errOut = run(t, "inbox", "answer", entry.ID, "--text", "wort", "--output", "json")
	var answered inboxJSON
	if code != 0 || json.Unmarshal([]byte(out), &answered) != nil || answered.Status != "delivered" || answered.Answer == nil || *answered.Answer != "wort" {
		t.Fatalf("inbox answer: code=%d out=%q err=%q", code, out, errOut)
	}
	waitFor(t, "the answer on the screen", func() bool { return strings.Contains(screen(), "wort") })
	waitFor(t, "the tool to read the answer", func() bool {
		calls, _ := os.ReadFile(filepath.Join(outDir, "calls"))
		return strings.Contains(string(calls), "wort unknown")
	})
	if got := inboxStatus(t, entry.ID); got != "delivered" {
		t.Fatalf("status after the answer: %q", got)
	}
	if code, _, errOut := run(t, "inbox", "answer", entry.ID, "--text", "again"); code != 2 || !strings.Contains(errOut, "invalid-request") {
		t.Errorf("a second answer: code=%d err=%q", code, errOut)
	}
	send("post-tool")
	waitFor(t, "the entry to be picked", func() bool { return inboxStatus(t, entry.ID) == "picked" })
	if got := inboxList(t, false); len(got) != 0 {
		t.Errorf("the default list holds a picked entry: %+v", got)
	}

	// Multi-line text arrives as one line.
	send("permission")
	waitFor(t, "the next entry", func() bool { return len(inboxList(t, false)) == 1 })
	next := inboxList(t, false)[0]
	if code, _, errOut := run(t, "inbox", "answer", next.ID, "--text", "eins\nzwei"); code != 0 {
		t.Fatalf("multi-line answer: code=%d err=%q", code, errOut)
	}
	waitFor(t, "the one-line answer on the screen", func() bool { return strings.Contains(screen(), "eins zwei") })
	waitFor(t, "the tool to read one line", func() bool {
		calls, _ := os.ReadFile(filepath.Join(outDir, "calls"))
		return strings.Contains(string(calls), "eins zwei unknown")
	})
	send("post-tool")
	waitFor(t, "the entry to be picked", func() bool { return inboxStatus(t, next.ID) == "picked" })

	// Closing, and refusals with the codes of the command line.
	send("permission")
	waitFor(t, "the entry to close", func() bool { return len(inboxList(t, false)) == 1 })
	last := inboxList(t, false)[0]
	code, out, errOut = run(t, "inbox", "close", last.ID, "--output", "json")
	var closed inboxJSON
	if code != 0 || json.Unmarshal([]byte(out), &closed) != nil || closed.Status != "closed" {
		t.Fatalf("inbox close: code=%d out=%q err=%q", code, out, errOut)
	}
	if code, _, errOut := run(t, "inbox", "close", last.ID); code != 2 || !strings.Contains(errOut, "invalid-request") {
		t.Errorf("closing a closed entry: code=%d err=%q", code, errOut)
	}
	if code, _, errOut := run(t, "inbox", "show", "zzzzzzzzzzz2"); code != 2 || !strings.Contains(errOut, "not-found") {
		t.Errorf("an unknown entry: code=%d err=%q", code, errOut)
	}
	if code, _, errOut := run(t, "inbox", "answer", "zzzzzzzzzzz2", "--text", "x"); code != 2 || !strings.Contains(errOut, "not-found") {
		t.Errorf("answering an unknown entry: code=%d err=%q", code, errOut)
	}
	if code, _, errOut := run(t, "inbox", "list", "--project", "nope"); code != 2 || !strings.Contains(errOut, "not-found") {
		t.Errorf("an unknown project: code=%d err=%q", code, errOut)
	}
	if got := inboxList(t, true); len(got) != 3 {
		t.Errorf("--all lists %d entries, want 3", len(got))
	}
}
