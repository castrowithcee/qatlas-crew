//go:build integration

package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/session"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

const codexAgent = "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"

// codexPayloads are synthetic hook payloads of Codex that the fake tool passes to the hook command on stdin.
var codexPayloads = map[string]string{
	"prompt":     `{"session_id":"` + codexAgent + `","cwd":"/work","hook_event_name":"UserPromptSubmit","model":"m","prompt":"do it"}`,
	"permission": `{"session_id":"` + codexAgent + `","cwd":"/work","hook_event_name":"PermissionRequest","tool_name":"shell","tool_input":{"command":"ls -l"}}`,
	"post-tool":  `{"session_id":"` + codexAgent + `","cwd":"/work","hook_event_name":"PostToolUse","tool_name":"shell","tool_input":{},"tool_response":{}}`,
	"question":   `{"session_id":"` + codexAgent + `","cwd":"/work","hook_event_name":"PreToolUse","tool_name":"request_user_input","tool_input":{}}`,
	"stop":       `{"session_id":"` + codexAgent + `","cwd":"/work","hook_event_name":"Stop","last_assistant_message":"Done.\n\u001b]0;x\u0007"}`,
	"end":        `{"session_id":"` + codexAgent + `","cwd":"/work","hook_event_name":"SessionEnd","reason":"other"}`,
	"garbage":    `{"session_id":`,
}

const codexNotify = `{"type":"agent-turn-complete","thread-id":"` + codexAgent + `","turn-id":"t-1","cwd":"/work","client":"codex-tui","input-messages":["do it"],"last-assistant-message":"Done."}`

var tomlItem = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

// notifyProgram reads the notify list of the arguments the fake codex was started with: the argv of the
// notify program without its payload.
func notifyProgram(args []string) []string {
	for i, a := range args {
		if a == "-c" && i+1 < len(args) && strings.HasPrefix(args[i+1], "notify=[") {
			var argv []string
			for _, m := range tomlItem.FindAllStringSubmatch(args[i+1], -1) {
				word, err := strconv.Unquote(`"` + m[1] + `"`)
				if err != nil {
					return nil
				}
				argv = append(argv, word)
			}
			return argv
		}
	}
	return nil
}

// fakeCodex stands for the codex program. It records its arguments and the session variable it got, and runs
// what the lines on its input say: hook:<payload> calls the hook command with the payload on stdin,
// notify calls the notify program of its arguments with the payload as the last argument, bell prints a
// prompt and rings the terminal bell, exit ends it. Any other line is recorded as typed. Every call is
// recorded with its exit code and the size of its output.
func fakeCodex(args []string, in io.Reader, out io.Writer) int {
	outDir := os.Getenv(outEnv)
	record, err := os.OpenFile(filepath.Join(outDir, "calls"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 3
	}
	defer record.Close()
	if os.WriteFile(filepath.Join(outDir, "argv"), []byte(strings.Join(args, "\n")+"\n"), 0o600) != nil ||
		os.WriteFile(filepath.Join(outDir, "env"), []byte(os.Getenv("QCREW_SESSION")), 0o600) != nil {
		return 4
	}
	call := func(label string, cmd *exec.Cmd) {
		var got bytes.Buffer
		cmd.Stdout, cmd.Stderr = &got, &got
		code := 0
		if err := cmd.Run(); err != nil {
			code = 1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
		}
		fmt.Fprintf(record, "%s exit=%d output=%d\n", label, code, got.Len())
	}
	fmt.Fprintln(out, "fake codex ready")
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "exit":
			return 0
		case line == "bell":
			fmt.Fprint(out, "Allow command ls -l? [y/n]\a\n")
		case line == "notify":
			argv := notifyProgram(args)
			if len(argv) != 4 {
				fmt.Fprintf(record, "notify skipped\n")
				continue
			}
			call("notify", exec.Command(argv[0], append(argv[1:], codexNotify)...))
		case strings.HasPrefix(line, "hook:"):
			body, ok := codexPayloads[strings.TrimPrefix(line, "hook:")]
			if !ok {
				fmt.Fprintf(record, "%s unknown\n", line)
				continue
			}
			cmd := exec.Command("/bin/sh", "-c", shellLine([]string{os.Getenv(programEnv)})+" hook codex")
			cmd.Stdin = strings.NewReader(body)
			call(line, cmd)
		default:
			fmt.Fprintf(record, "typed %q\n", line)
		}
	}
	return 0
}

type codexSessionJSON struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Signals string `json:"signals"`
}

type inboxEntryJSON struct {
	ID      string   `json:"id"`
	Session string   `json:"session"`
	Kind    string   `json:"kind"`
	Status  string   `json:"status"`
	Text    string   `json:"text"`
	Lines   []string `json:"lines"`
}

func listInbox(t *testing.T) []inboxEntryJSON {
	t.Helper()
	code, out, errOut := run(t, "inbox", "list", "--all", "--output", "json")
	var list []inboxEntryJSON
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil {
		t.Fatalf("inbox list: code=%d out=%q err=%q", code, out, errOut)
	}
	return list
}

func TestCodexReportsTheStatus(t *testing.T) {
	srv := testenv.Tmux(t)
	env := serviceTestEnv(t)
	t.Setenv(session.SocketEnv, srv.Socket())
	t.Setenv("SHELL", "/bin/sh")

	// The fake codex and the stand-in for qcrew live in a directory with a space, to exercise the quoting.
	bin := filepath.Join(env.Root, "my bin")
	outDir := filepath.Join(env.Root, "out")
	codexHome := filepath.Join(env.Root, "codex-home")
	for _, d := range []string{outDir, codexHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	program := filepath.Join(bin, "qcrew")
	wrapper(t, program, "hook")
	wrapper(t, filepath.Join(bin, "codex"), "codex", "HOME="+env.Home, "XDG_RUNTIME_DIR="+os.Getenv("XDG_RUNTIME_DIR"),
		"CODEX_HOME="+codexHome, outEnv+"="+outDir, programEnv+"="+program)
	t.Setenv(programEnv, program)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	// Hooks and configuration of the user, which qcrew must leave as they are.
	configToml := filepath.Join(codexHome, "config.toml")
	hooksJSON := filepath.Join(codexHome, "hooks.json")
	for path, content := range map[string]string{
		configToml: "model = \"synthetic\"\n[hooks]\n",
		hooksJSON:  "{\n  \"hooks\": {}\n}\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := digest(t, configToml, hooksJSON)

	dir := filepath.Join(env.Root, "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := run(t, "project", "add", dir, "--output", "json")
	var p projectJSON
	if code != 0 || json.Unmarshal([]byte(out), &p) != nil {
		t.Fatalf("project add: code=%d out=%q err=%q", code, out, errOut)
	}
	t.Cleanup(func() { _, _ = srv.Run("kill-session", "-t", "="+session.SessionName(p.ID)) })

	start := func() codexSessionJSON {
		t.Helper()
		for _, f := range []string{"argv", "calls", "env"} {
			_ = os.Remove(filepath.Join(outDir, f))
		}
		code, out, errOut := run(t, "session", "start", "--project", "app", "--tool", "codex", "--output", "json")
		var s codexSessionJSON
		if code != 0 || json.Unmarshal([]byte(out), &s) != nil || s.Status != "idle" || s.Signals != "fallback" {
			t.Fatalf("start: code=%d out=%q err=%q", code, out, errOut)
		}
		waitFor(t, "the fake codex to start", func() bool { _, err := os.Stat(filepath.Join(outDir, "env")); return err == nil })
		return s
	}
	target := func(s codexSessionJSON) string {
		return "=" + session.SessionName(p.ID) + ":" + session.WindowName(s.ID)
	}
	send := func(s codexSessionJSON, line string) {
		t.Helper()
		if _, err := srv.Run("send-keys", "-t", target(s), "-l", line); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.Run("send-keys", "-t", target(s), "Enter"); err != nil {
			t.Fatal(err)
		}
	}
	// within polls until cond holds, for at most limit, and returns how long it took.
	within := func(what string, limit time.Duration, cond func() bool) {
		t.Helper()
		begin := time.Now()
		for !cond() {
			if time.Since(begin) > limit {
				t.Fatalf("%s did not happen within %v", what, limit)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	waitingWithin := func(s codexSessionJSON, limit time.Duration) {
		t.Helper()
		within("status waiting", limit, func() bool { return listSessions2(t)[s.ID].Status == "waiting" })
	}
	entryWithin := func(s codexSessionJSON, kind string, limit time.Duration) inboxEntryJSON {
		t.Helper()
		var found inboxEntryJSON
		// The inbox list does not look at tmux: an entry is there if the service itself saw the signal.
		within("an inbox entry of kind "+kind, limit, func() bool {
			for _, e := range listInbox(t) {
				if e.Session == s.ID && e.Status == "open" && e.Kind == kind {
					found = e
					return true
				}
			}
			return false
		})
		return found
	}

	// First session: hooks.
	s := start()
	argv, _ := os.ReadFile(filepath.Join(outDir, "argv"))
	got := strings.Split(strings.TrimSpace(string(argv)), "\n")
	want := []string{"--no-daemon", "-c", "notify=[\"" + program + "\",\"hook\",\"codex\",\"--notify\"]", "-c",
		`tui.notifications=["approval-requested","plan-mode-prompt","async-question"]`, "-c", `tui.notification_method="bel"`,
		"-c", `tui.notification_condition="always"`}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("command line:\n got %q\nwant %q", got, want)
	}
	if env, _ := os.ReadFile(filepath.Join(outDir, "env")); string(env) != s.ID {
		t.Fatalf("QCREW_SESSION in the tool = %q, want %q", env, s.ID)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".qatlas", "qcrew", "sessions", s.ID)); !os.IsNotExist(err) {
		t.Errorf("the adapter made a folder for the session: %v", err)
	}
	// Nothing is bound before the first hook event.
	if out, _ := srv.Run("list-windows", "-a", "-F", "#{@qcrew-session} [#{@qcrew-agent-session}]"); !strings.Contains(out, s.ID+" []\n") {
		t.Fatalf("tmux windows before an event: %q", out)
	}

	send(s, "hook:prompt")
	within("status running", 2*time.Second, func() bool { return listSessions2(t)[s.ID].Status == "running" })
	if x := listSessions2(t)[s.ID]; x.Signals != "hooks" {
		t.Errorf("signals after the first hook event: %q", x.Signals)
	}
	// The session id of the tool is recorded in the window, where a later resume finds it.
	within("the tool session id in the window", 2*time.Second, func() bool {
		out, _ := srv.Run("list-windows", "-a", "-F", "#{@qcrew-session} #{@qcrew-agent-session}")
		return strings.Contains(out, s.ID+" "+codexAgent+"\n")
	})
	// A permission request: waiting within two seconds, and an entry in the inbox with the command.
	send(s, "hook:permission")
	entry := entryWithin(s, "permission", 2*time.Second)
	if entry.Text != "ls -l" {
		t.Errorf("permission entry text %q", entry.Text)
	}
	waitingWithin(s, 2*time.Second)
	send(s, "hook:post-tool")
	waitStatus2(t, s.ID, "running")
	send(s, "hook:question")
	entryWithin(s, "question", 2*time.Second)
	waitingWithin(s, 2*time.Second)
	send(s, "hook:post-tool")
	waitStatus2(t, s.ID, "running")
	send(s, "hook:stop")
	waitStatus2(t, s.ID, "idle")
	send(s, "hook:prompt")
	waitStatus2(t, s.ID, "running")
	send(s, "hook:garbage")
	waitStatus2(t, s.ID, "error")
	send(s, "hook:stop")
	waitStatus2(t, s.ID, "idle")
	// Notify and bell after hooks reported change nothing.
	send(s, "notify")
	send(s, "bell")
	send(s, "hook:permission")
	waitStatus2(t, s.ID, "waiting")
	send(s, "notify")
	time.Sleep(1500 * time.Millisecond)
	if x := listSessions2(t)[s.ID]; x.Status != "waiting" {
		t.Errorf("a notify event changed the status of a session with hooks to %s", x.Status)
	}
	send(s, "hook:end")
	waitStatus2(t, s.ID, "exited")
	send(s, "exit")
	waitFor(t, "the pane to end", func() bool {
		out, _ := srv.Run("list-windows", "-a", "-F", "#{@qcrew-session} #{pane_dead}")
		return strings.Contains(out, s.ID+" 1\n")
	})
	time.Sleep(2500 * time.Millisecond)
	if x := listSessions2(t)[s.ID]; x.Status != "exited" {
		t.Fatalf("after the program ended: %s", x.Status)
	}
	calls, _ := os.ReadFile(filepath.Join(outDir, "calls"))
	for line := range strings.Lines(string(calls)) {
		if !strings.HasSuffix(strings.TrimSpace(line), "exit=0 output=0") && !strings.HasPrefix(line, "notify skipped") {
			t.Errorf("a hook call did not end silently with exit code 0: %q", line)
		}
	}
	if n := strings.Count(string(calls), "exit=0 output=0"); n != 13 {
		t.Errorf("%d hook calls recorded, want 13:\n%s", n, calls)
	}

	// Second session: no hooks, only the fallback.
	fb := start()
	waitStatus2(t, fb.ID, "idle")
	// A bell is a prompt: waiting and an inbox entry within two seconds, found without a session list.
	send(fb, "bell")
	prompt := entryWithin(fb, "permission", 2*time.Second)
	if !strings.Contains(prompt.Text, "Allow command ls -l?") || len(prompt.Lines) == 0 {
		t.Errorf("fallback entry: text %q, %d screen lines", prompt.Text, len(prompt.Lines))
	}
	if x := listSessions2(t)[fb.ID]; x.Status != "waiting" || x.Signals != "fallback" {
		t.Errorf("after the bell: %+v", x)
	}
	// A second ring while waiting adds no entry.
	send(fb, "bell")
	time.Sleep(1200 * time.Millisecond)
	n := 0
	for _, e := range listInbox(t) {
		if e.Session == fb.ID && e.Kind == "permission" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d permission entries after a second bell, want 1: %+v", n, listInbox(t))
	}
	// The answer is typed as text, with no preset key, and the session works on.
	if code, _, errOut := run(t, "inbox", "answer", prompt.ID, "--text", "yes"); code != 0 {
		t.Fatalf("answer: code=%d err=%q", code, errOut)
	}
	waitFor(t, "the answer to be typed", func() bool {
		c, _ := os.ReadFile(filepath.Join(outDir, "calls"))
		return strings.Contains(string(c), `typed "yes"`)
	})
	waitStatus2(t, fb.ID, "running")
	// The end of the turn comes from the notify program.
	send(fb, "notify")
	waitStatus2(t, fb.ID, "idle")
	if x := listSessions2(t)[fb.ID]; x.Signals != "fallback" {
		t.Errorf("signals after notify: %q", x.Signals)
	}
	// The first hook event ends the fallback.
	send(fb, "hook:prompt")
	waitStatus2(t, fb.ID, "running")
	if x := listSessions2(t)[fb.ID]; x.Signals != "hooks" {
		t.Errorf("signals after a hook event: %q", x.Signals)
	}
	send(fb, "bell")
	time.Sleep(1500 * time.Millisecond)
	if x := listSessions2(t)[fb.ID]; x.Status != "running" {
		t.Errorf("a bell changed the status of a session with hooks to %s", x.Status)
	}
	// The ring was taken in even though it changed nothing: no stale ring is left in the window.
	if out, _ := srv.Run("list-windows", "-a", "-F", "#{@qcrew-session} [#{@qcrew-bell}]"); !strings.Contains(out, fb.ID+" []\n") {
		t.Errorf("the ring was not taken in: %q", out)
	}

	for _, id := range []string{s.ID, fb.ID} {
		if code, _, errOut := run(t, "session", "stop", id, "--confirm"); code != 0 {
			t.Fatalf("stop %s: code=%d err=%q", id, code, errOut)
		}
	}
	if after := digest(t, configToml, hooksJSON); after != before {
		t.Error("the configuration or the hooks of the tool changed")
	}
	entries, err := os.ReadDir(codexHome)
	if err != nil || len(entries) != 2 {
		t.Errorf("the tool's home holds %d entries (%v), want the 2 of the test", len(entries), err)
	}
}

func listSessions2(t *testing.T) map[string]codexSessionJSON {
	t.Helper()
	code, out, errOut := run(t, "session", "list", "--output", "json")
	var list []codexSessionJSON
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil {
		t.Fatalf("session list: code=%d out=%q err=%q", code, out, errOut)
	}
	byID := map[string]codexSessionJSON{}
	for _, x := range list {
		byID[x.ID] = x
	}
	return byID
}

func waitStatus2(t *testing.T, id, want string) {
	t.Helper()
	var last codexSessionJSON
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if last = listSessions2(t)[id]; last.Status == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session %s: status %q, want %q", id, last.Status, want)
}
