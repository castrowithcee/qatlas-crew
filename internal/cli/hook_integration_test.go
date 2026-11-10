//go:build integration

package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/session"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

const (
	// programEnv names the wrapper script that stands for the qcrew program in the hook commands.
	programEnv = "QCREW_TEST_PROGRAM"
	outEnv     = "QCREW_TEST_OUT"
)

// payloads are synthetic hook payloads: the event whose hook entry runs, and what it reads on stdin.
var payloads = map[string]struct{ event, body string }{
	"start":      {"SessionStart", `{"session_id":"s-1","cwd":"/work","hook_event_name":"SessionStart","source":"startup","model":"m"}`},
	"prompt":     {"UserPromptSubmit", `{"session_id":"s-1","cwd":"/work","hook_event_name":"UserPromptSubmit","prompt":"do it"}`},
	"permission": {"PermissionRequest", `{"session_id":"s-1","cwd":"/work","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"}}`},
	"post-tool":  {"PostToolUse", `{"session_id":"s-1","cwd":"/work","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{},"tool_response":{}}`},
	"stop":       {"Stop", `{"session_id":"s-1","cwd":"/work","hook_event_name":"Stop","last_assistant_message":"Done.\n\u001b]0;x\u0007"}`},
	"end":        {"SessionEnd", `{"session_id":"s-1","cwd":"/work","hook_event_name":"SessionEnd","reason":"other"}`},
	"garbage":    {"Stop", `{"session_id":`},
}

// fakeClaude stands for the claude program. It records its arguments, reads the hooks out of the settings
// file it was given, and then runs the hook commands named by the lines on its input: a payload name, or
// status:N for the status line, or exit. It records every call with the exit code and the size of the output.
func fakeClaude(args []string, in io.Reader, out io.Writer) int {
	outDir := os.Getenv(outEnv)
	var settings string
	for i, a := range args {
		if a == "--settings" && i+1 < len(args) {
			settings = args[i+1]
		}
	}
	if err := os.WriteFile(filepath.Join(outDir, "argv"), []byte(strings.Join(args, "\n")+"\n"), 0o600); err != nil {
		return 3
	}
	data, err := os.ReadFile(settings)
	if err != nil {
		return 4
	}
	var cfg struct {
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct{ Command string }
		}
		StatusLine *struct{ Command string }
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return 5
	}
	calls, _ := os.OpenFile(filepath.Join(outDir, "calls"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	defer calls.Close()
	call := func(label, command, payload string) {
		cmd := exec.Command("/bin/sh", "-c", command)
		cmd.Stdin = strings.NewReader(payload)
		var got bytes.Buffer
		cmd.Stdout, cmd.Stderr = &got, &got
		code := 0
		if err := cmd.Run(); err != nil {
			code = 1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
		}
		fmt.Fprintf(calls, "%s exit=%d output=%d\n", label, code, got.Len())
	}
	fmt.Fprintln(out, "fake claude ready")
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "exit":
			return 0
		case strings.HasPrefix(line, "status:"):
			if cfg.StatusLine == nil {
				fmt.Fprintf(calls, "%s skipped\n", line)
				continue
			}
			call(line, cfg.StatusLine.Command,
				`{"model":{"id":"m"},"context_window":{"used_percentage":`+strings.TrimPrefix(line, "status:")+`}}`)
		default:
			p, ok := payloads[line]
			if !ok || len(cfg.Hooks[p.event]) == 0 {
				fmt.Fprintf(calls, "%s unknown\n", line)
				continue
			}
			call(line, cfg.Hooks[p.event][0].Hooks[0].Command, p.body)
		}
	}
	return 0
}

// wrapper writes a script that runs this test binary as the helper mode, with extra environment variables.
func wrapper(t *testing.T, path, mode string, env ...string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, kv := range append([]string{helperEnv + "=" + mode}, env...) {
		name, value, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&b, "%s=%s\nexport %s\n", name, shellLine([]string{value}), name)
	}
	fmt.Fprintf(&b, "exec %s -test.run='^TestHelperProcess$' -- \"$@\"\n", shellLine([]string{exe}))
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		t.Fatal(err)
	}
}

func digest(t *testing.T, paths ...string) string {
	t.Helper()
	h := sha256.New()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s\n%d\n", p, len(data))
		h.Write(data)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

type claudeSessionJSON struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	Since          string `json:"since"`
	ContextPercent *int   `json:"context_percent"`
}

func listSessions(t *testing.T) map[string]claudeSessionJSON {
	t.Helper()
	code, out, errOut := run(t, "session", "list", "--output", "json")
	var list []claudeSessionJSON
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil {
		t.Fatalf("session list: code=%d out=%q err=%q", code, out, errOut)
	}
	byID := map[string]claudeSessionJSON{}
	for _, x := range list {
		byID[x.ID] = x
	}
	return byID
}

func waitStatus(t *testing.T, id, want string) claudeSessionJSON {
	t.Helper()
	var last claudeSessionJSON
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if last = listSessions(t)[id]; last.Status == want {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session %s: status %q, want %q", id, last.Status, want)
	return last
}

func TestClaudeHooksReportTheStatus(t *testing.T) {
	srv := testenv.Tmux(t)
	env := serviceTestEnv(t)
	t.Setenv(session.SocketEnv, srv.Socket())
	t.Setenv("SHELL", "/bin/sh")

	// The fake claude and the stand-in for qcrew live in directories with a space, to exercise the quoting.
	bin := filepath.Join(env.Root, "my bin")
	outDir := filepath.Join(env.Root, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(bin, "qcrew")
	wrapper(t, program, "hook")
	wrapper(t, filepath.Join(bin, "claude"), "claude", "HOME="+env.Home, "XDG_RUNTIME_DIR="+os.Getenv("XDG_RUNTIME_DIR"),
		outEnv+"="+outDir)
	t.Setenv(programEnv, program)
	// tmux hands the PATH of the client that creates a project's tmux session, here the service, to the panes.
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	// Settings of the user and of the project, which qcrew must leave as they are.
	dir := filepath.Join(env.Root, "app")
	userSettings := filepath.Join(env.Home, ".claude", "settings.json")
	projSettings := filepath.Join(dir, ".claude", "settings.json")
	localSettings := filepath.Join(dir, ".claude", "settings.local.json")
	for path, content := range map[string]string{
		userSettings:  "{\n  \"theme\": \"dark\",\n  \"hooks\": {}\n}\n",
		projSettings:  `{"model":"synthetic"}`,
		localSettings: "{ \"permissions\": { \"allow\": [] } }",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := digest(t, userSettings, projSettings, localSettings)

	code, out, errOut := run(t, "project", "add", dir, "--output", "json")
	var p projectJSON
	if code != 0 || json.Unmarshal([]byte(out), &p) != nil {
		t.Fatalf("project add: code=%d out=%q err=%q", code, out, errOut)
	}
	t.Cleanup(func() { _, _ = srv.Run("kill-session", "-t", "="+session.SessionName(p.ID)) })

	start := func() claudeSessionJSON {
		t.Helper()
		_ = os.Remove(filepath.Join(outDir, "argv"))
		_ = os.Remove(filepath.Join(outDir, "calls"))
		code, out, errOut := run(t, "session", "start", "--project", "app", "--tool", "claude", "--output", "json")
		var s claudeSessionJSON
		if code != 0 || json.Unmarshal([]byte(out), &s) != nil || s.Status != "running" || s.Since == "" || s.ContextPercent != nil {
			t.Fatalf("start: code=%d out=%q err=%q", code, out, errOut)
		}
		defer func() {
			if t.Failed() {
				pane, _ := srv.Run("capture-pane", "-p", "-t", "="+session.SessionName(p.ID)+":"+session.WindowName(s.ID))
				t.Logf("screen of the session:\n%s", pane)
			}
		}()
		waitFor(t, "the fake claude to start", func() bool { _, err := os.Stat(filepath.Join(outDir, "argv")); return err == nil })
		return s
	}
	send := func(s claudeSessionJSON, line string) {
		t.Helper()
		target := "=" + session.SessionName(p.ID) + ":" + session.WindowName(s.ID)
		if _, err := srv.Run("send-keys", "-t", target, "-l", line); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.Run("send-keys", "-t", target, "Enter"); err != nil {
			t.Fatal(err)
		}
	}

	s := start()
	// The command line of the tool.
	argv, _ := os.ReadFile(filepath.Join(outDir, "argv"))
	settingsFile := filepath.Join(env.Home, ".qatlas", "qcrew", "sessions", s.ID, "claude-settings.json")
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	got := strings.Split(strings.TrimSpace(string(argv)), "\n")
	if len(got) != 6 || got[0] != "--session-id" || !uuid.MatchString(got[1]) || got[2] != "--prompt-suggestions" ||
		got[3] != "false" || got[4] != "--settings" || got[5] != settingsFile {
		t.Fatalf("command line: %q", got)
	}
	if info, err := os.Stat(settingsFile); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings file: %v, %v", info, err)
	}
	if info, err := os.Stat(filepath.Dir(settingsFile)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("session folder: %v, %v", info, err)
	}
	// The tool's session id outlives the service in a window option.
	if out, err := srv.Run("list-windows", "-a", "-F", "#{@qcrew-session} #{@qcrew-agent-session}"); err != nil ||
		!strings.Contains(out, s.ID+" "+got[1]+"\n") {
		t.Fatalf("tmux windows: %q, %v", out, err)
	}

	// running (nothing reported yet) -> waiting -> running -> idle -> exited
	prev, _ := time.Parse(time.RFC3339, s.Since)
	for _, step := range []struct{ line, status string }{
		{"permission", "waiting"}, {"post-tool", "running"}, {"stop", "idle"}, {"prompt", "running"},
		{"garbage", "error"}, {"start", "idle"}, {"permission", "waiting"}, {"post-tool", "running"},
		{"stop", "idle"}, {"end", "exited"},
	} {
		send(s, step.line)
		x := waitStatus(t, s.ID, step.status)
		since, err := time.Parse(time.RFC3339, x.Since)
		if err != nil || since.Before(prev) {
			t.Fatalf("%s: since %q after %v", step.line, x.Since, prev)
		}
		prev = since
		if step.status == "waiting" {
			// The status line reports the context window without changing the status.
			send(s, "status:42")
			waitFor(t, "the context percentage", func() bool {
				c := listSessions(t)[s.ID].ContextPercent
				return c != nil && *c == 42
			})
			if x := listSessions(t)[s.ID]; x.Status != "waiting" {
				t.Fatalf("a status line report changed the status to %s", x.Status)
			}
		}
	}
	// The program ends after SessionEnd: still exited.
	send(s, "exit")
	waitFor(t, "the pane to end", func() bool {
		out, _ := srv.Run("list-windows", "-a", "-F", "#{@qcrew-session} #{pane_dead}")
		return strings.Contains(out, s.ID+" 1\n")
	})
	time.Sleep(2500 * time.Millisecond) // a refresh of the service has looked by now
	if x := listSessions(t)[s.ID]; x.Status != "exited" {
		t.Fatalf("after the program ended: %s", x.Status)
	}
	calls, _ := os.ReadFile(filepath.Join(outDir, "calls"))
	for line := range strings.Lines(string(calls)) {
		if !strings.HasSuffix(strings.TrimSpace(line), "exit=0 output=0") {
			t.Errorf("a hook call did not end silently with exit code 0: %q", line)
		}
	}
	if n := strings.Count(string(calls), "exit=0 output=0"); n != 12 {
		t.Errorf("%d hook calls recorded, want 12:\n%s", n, calls)
	}

	// A program that ends without SessionEnd is an error.
	lost := start()
	send(lost, "stop")
	waitStatus(t, lost.ID, "idle")
	send(lost, "exit")
	waitStatus(t, lost.ID, "error")
	if x := listSessions(t)[s.ID]; x.Status != "exited" {
		t.Errorf("the first session changed to %s", x.Status)
	}

	for _, id := range []string{s.ID, lost.ID} {
		if code, _, errOut := run(t, "session", "stop", id, "--confirm"); code != 0 {
			t.Fatalf("stop %s: code=%d err=%q", id, code, errOut)
		}
		if _, err := os.Stat(filepath.Join(env.Home, ".qatlas", "qcrew", "sessions", id)); !os.IsNotExist(err) {
			t.Errorf("the session folder of %s is still there: %v", id, err)
		}
	}
	if after := digest(t, userSettings, projSettings, localSettings); after != before {
		t.Error("the settings of the user or of the project changed")
	}
	for d, want := range map[string]string{
		filepath.Join(env.Home, ".claude"): "settings.json",
		filepath.Join(dir, ".claude"):      "settings.json settings.local.json",
	} {
		var names []string
		entries, err := os.ReadDir(d)
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if got := strings.Join(names, " "); err != nil || got != want {
			t.Errorf("%s holds %q, want %q (%v)", d, got, want, err)
		}
	}
}

// waitFor polls until cond holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
