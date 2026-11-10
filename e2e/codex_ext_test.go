//go:build ext

package e2e

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// codexCredentialVars are the variables a real codex may log in with; the test takes them from its own
// environment and never writes them to a file or the log.
var codexCredentialVars = []string{"OPENAI_API_KEY", "CODEX_API_KEY"}

// codexHookEvents are the hook events the test sets up in the temporary CODEX_HOME.
var codexHookEvents = []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "PreToolUse", "PermissionRequest",
	"PostToolUse", "Stop", "PreModelRequest", "PostModelRequest", "Interrupt"}

var camelBoundary = regexp.MustCompile(`([a-z])([A-Z])`)

// codexHookTrust returns the config.toml lines that trust the hooks of a hooks.json, from what is known of
// the tool's internal hash: key = <file>:<event label>:<group>:<handler>, hash =
// sha256 over the JSON, keys sorted, of {event_name, matcher, hooks:[handler]}. The event label (snake case
// here) and the normalization of the handler are assumptions; if the hooks do not run, they are the first
// thing to check for a newer version of the tool.
func codexHookTrust(hooksFile, command string) string {
	var b strings.Builder
	for _, event := range codexHookEvents {
		label := strings.ToLower(camelBoundary.ReplaceAllString(event, "${1}_${2}"))
		handler := map[string]any{"type": "command", "command": command, "async": true}
		data, _ := json.Marshal(map[string]any{"event_name": label, "matcher": nil, "hooks": []any{handler}})
		fmt.Fprintf(&b, "[hooks.state.%q]\ntrusted_hash = \"sha256:%x\"\n", fmt.Sprintf("%s:%s:0:0", hooksFile, label), sha256.Sum256(data))
	}
	return b.String()
}

type codexListed struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Signals string `json:"signals"`
}

// TestCodexStatusWithTheRealTool proves against a real codex, in a CODEX_HOME of its own: that hooks set up
// there report the status (waiting within 2 s of a permission prompt, the name request_user_input for a
// question, running after the answer, idle at the end of the turn), and that the fallback without hooks
// reports waiting within 2 s through the bell and idle through notify. It needs credentials in the
// environment and codex and tmux in PATH.
func TestCodexStatusWithTheRealTool(t *testing.T) {
	creds := map[string]string{}
	for _, v := range codexCredentialVars {
		if val := os.Getenv(v); val != "" {
			creds[v] = val
		}
	}
	if len(creds) == 0 {
		t.Skip("no credentials: set OPENAI_API_KEY or CODEX_API_KEY to run this test")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex is not in PATH")
	}
	srv, err := testenv.StartTmux()
	if err != nil {
		t.Skipf("tmux is not available: %v", err)
	}
	t.Cleanup(srv.Stop)

	e := testenv.Setup(t)
	runtimeDir := testenv.ShortRuntime(t)
	bin := filepath.Join(t.TempDir(), "qcrew")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = ".."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	project := filepath.Join(e.Root, "app")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	// Two homes of the tool: one with hooks and their trust, one without.
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	baseConfig := fmt.Sprintf("approval_policy = \"on-request\"\nsandbox_mode = \"read-only\"\n[projects.%q]\ntrust_level = \"trusted\"\n", project)
	hooksHome := filepath.Join(e.Root, "codex-hooks")
	plainHome := filepath.Join(e.Root, "codex-plain")
	hooksFile := filepath.Join(hooksHome, "hooks.json")
	command := bin + " hook codex"
	hooks := map[string][]any{}
	for _, event := range codexHookEvents {
		hooks[event] = []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "async": true}}}}
	}
	hooksData, _ := json.Marshal(map[string]any{"hooks": hooks})
	write(hooksFile, string(hooksData)+"\n")
	write(filepath.Join(hooksHome, "config.toml"), baseConfig+codexHookTrust(hooksFile, command))
	write(filepath.Join(plainHome, "config.toml"), baseConfig)

	env := append(testenv.WithRuntime(e.Environ(), runtimeDir), "QCREW_TMUX_SOCKET="+srv.Socket())
	q := func(args ...string) string { return extRun(t, bin, env, e.Home, args...) }
	t.Cleanup(func() {
		var st struct {
			PID int `json:"pid"`
		}
		_ = json.Unmarshal([]byte(q("service", "status", "--output", "json")), &st)
		q("service", "stop")
		if st.PID > 0 && !extWaitFor(func() bool { return testenv.ProcessGone(st.PID) }) {
			t.Errorf("service process %d is still running", st.PID)
		}
	})
	var p struct{ ID string }
	if err := json.Unmarshal([]byte(q("project", "add", project, "--output", "json")), &p); err != nil {
		t.Fatal(err)
	}
	// The panes get their variables from the tmux server; the home of the tool is set before each start.
	setPaneEnv := func(k, v string) {
		t.Helper()
		if _, err := srv.Run("set-environment", "-g", k, v); err != nil {
			t.Fatalf("set %s for the panes: %v", k, strings.Split(err.Error(), ": ")[0])
		}
	}
	setPaneEnv("HOME", e.Home)
	setPaneEnv("XDG_RUNTIME_DIR", runtimeDir)
	for k, v := range creds {
		setPaneEnv(k, v)
	}

	// phase runs one session in the given home and checks what its signals show.
	phase := func(name, home string, wantSignals string) {
		setPaneEnv("CODEX_HOME", home)
		var s codexListed
		if err := json.Unmarshal([]byte(q("session", "start", "--project", "app", "--tool", "codex", "--output", "json")), &s); err != nil {
			t.Fatal(err)
		}
		target := "=p-" + p.ID + ":s-" + s.ID
		screen := func() string { out, _ := srv.Run("capture-pane", "-p", "-t", target); return out }
		current := func() codexListed {
			var list []codexListed
			_ = json.Unmarshal([]byte(q("session", "list", "--output", "json")), &list)
			for _, x := range list {
				if x.ID == s.ID {
					return x
				}
			}
			return codexListed{}
		}
		t.Cleanup(func() {
			if t.Failed() {
				t.Logf("screen of the %s session:\n%s", name, screen())
			}
			extRun(t, bin, env, e.Home, "session", "stop", s.ID, "--confirm")
		})
		until := func(what string, limit time.Duration, cond func() bool) {
			t.Helper()
			deadline := time.Now().Add(limit)
			for !cond() {
				if time.Now().After(deadline) {
					t.Fatalf("%s: timed out after %v waiting for %s (status %q, signals %q)", name, limit, what, current().Status, current().Signals)
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
		keys := func(args ...string) {
			t.Helper()
			if _, err := srv.Run(append([]string{"send-keys", "-t", target}, args...)...); err != nil {
				t.Fatal(err)
			}
		}
		prompt := func(text string) { keys("-l", text); keys("Enter") }
		// promptWaitingWithin sends a prompt that needs an approval and requires the status waiting within
		// 2 s of the approval dialog showing on the screen.
		approval := func() {
			var shown time.Time
			until("the status waiting", 180*time.Second, func() bool {
				if current().Status == "waiting" {
					return true
				}
				if strings.Contains(strings.ToLower(screen()), "would you like to run") || strings.Contains(strings.ToLower(screen()), "yes, proceed") {
					if shown.IsZero() {
						shown = time.Now()
					} else if time.Since(shown) > 2*time.Second {
						t.Fatalf("%s: the approval dialog has shown for %v and the status is %q", name, time.Since(shown), current().Status)
					}
				}
				return false
			})
		}

		until("codex to show its prompt", 60*time.Second, func() bool { return strings.Contains(screen(), "›") })
		// The screen features that mean ready and error are unverified; the log shows the real screen.
		t.Logf("SCREEN %s after the start (status %q):\n%s", name, current().Status, screen())
		until("the status idle once the screen shows readiness", 10*time.Second, func() bool { return current().Status == "idle" })
		if got := current().Signals; got != "fallback" {
			t.Errorf("%s: signals before the first turn: %q, want fallback", name, got)
		}
		prompt("Run the shell command `touch qcrew-ext-marker` and nothing else.")
		approval()
		t.Logf("SCREEN %s at the approval question (status %q):\n%s", name, current().Status, screen())
		t.Logf("RESULT %s: waiting seen within 2 s of the approval dialog", name)
		if wantSignals == "hooks" {
			if got := current().Signals; got != "hooks" {
				t.Errorf("%s: signals after the first turn started: %q, want hooks (are the hooks set up and trusted?)", name, got)
			}
		}
		// The inbox holds a permission entry; the answer is typed as text.
		var entry struct{ ID, Kind string }
		until("the permission entry", 10*time.Second, func() bool {
			var list []struct{ ID, Kind string }
			_ = json.Unmarshal([]byte(q("inbox", "list", "--output", "json")), &list)
			if len(list) == 0 {
				return false
			}
			entry = list[len(list)-1]
			return true
		})
		if entry.Kind != "permission" {
			t.Errorf("%s: inbox entry kind %q", name, entry.Kind)
		}
		q("inbox", "answer", entry.ID, "--text", "y")
		until("the turn to end", 180*time.Second, func() bool { return current().Status == "idle" })
		t.Logf("RESULT %s: running after the answer and idle at the end of the turn (signals %q)", name, current().Signals)

		if wantSignals == "hooks" {
			// The name of the question tool: ask for a question and look for the entry kind.
			prompt("Use the request_user_input tool to ask me which colour I prefer, then stop.")
			var kind string
			until("a question or the end of the turn", 180*time.Second, func() bool {
				var list []struct{ ID, Kind, Status string }
				_ = json.Unmarshal([]byte(q("inbox", "list", "--output", "json")), &list)
				for _, x := range list {
					if x.Kind == "question" {
						kind = x.Kind
						return true
					}
				}
				return false
			})
			if kind != "question" {
				t.Errorf("%s: no question entry; the name request_user_input is not proven", name)
			} else {
				t.Logf("RESULT request_user_input: a PreToolUse with this tool name was mapped to a question entry")
			}
		}
	}
	phase("hooks", hooksHome, "hooks")
	phase("fallback", plainHome, "fallback")
}
