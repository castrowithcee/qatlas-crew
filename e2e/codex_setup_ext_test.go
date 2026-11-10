//go:build ext

package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/ptytest"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// TestCodexHooksSetUpByQcrewAreTrusted proves against a real codex, in a CODEX_HOME of its own that holds a
// config.toml with comments and a table of the user and a hooks.json with a hook of someone else: that
// 'qcrew setup codex-hooks' adds only its own entries, that codex then runs the hooks of qcrew (the session
// reports signals hooks and the service shows no integration hint), and that it still does not run the hook
// of someone else (its marker file stays absent). It needs credentials in the environment and codex and tmux
// in PATH.
func TestCodexHooksSetUpByQcrewAreTrusted(t *testing.T) {
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
	codexHome := filepath.Join(e.Root, "codex-home")
	for _, d := range []string{project, codexHome} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(e.Root, "foreign-hook-ran")
	config := fmt.Sprintf("# settings of the user\napproval_policy = \"never\"\nsandbox_mode = \"read-only\"\n\n"+
		"[projects.%q]\ntrust_level = \"trusted\"\n\n# kept as it is\n[history]\npersistence = \"none\"\n", project)
	hooks := fmt.Sprintf(`{"hooks": {"UserPromptSubmit": [{"hooks": [{"type": "command", "command": "touch %s"}]}]}}`+"\n", marker)
	for name, content := range map[string]string{"config.toml": config, "hooks.json": hooks} {
		if err := os.WriteFile(filepath.Join(codexHome, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := append(testenv.WithRuntime(e.Environ(), runtimeDir), "QCREW_TMUX_SOCKET="+srv.Socket(), "CODEX_HOME="+codexHome)

	// Set up the hooks as a person does: at a terminal, after the preview, with a yes.
	term, err := ptytest.Start(ptytest.Command{Path: bin, Args: []string{"setup", "codex-hooks"}, Dir: e.Home,
		Env: append(env, "TERM=xterm-256color"), Size: ptytest.Size{Columns: 160, Rows: 60}})
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
	if _, err := term.Write([]byte("y\n")); err != nil {
		t.Fatal(err)
	}
	if code, err := term.Wait(10 * time.Second); err != nil || code != 0 {
		t.Fatalf("setup codex-hooks: %d %v\n%s", code, err, term.Output())
	}
	gotConfig, _ := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	gotHooks, _ := os.ReadFile(filepath.Join(codexHome, "hooks.json"))
	if !strings.HasPrefix(string(gotConfig), config) || !strings.HasPrefix(string(gotHooks), strings.TrimSuffix(hooks, "]}}\n")) {
		t.Fatalf("foreign content changed:\n%s\n%s", gotHooks, gotConfig)
	}

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
	if out := q("setup", "--check", "--output", "json"); !strings.Contains(out, `"integration":"configured"`) {
		t.Fatalf("check: %s", out)
	}
	var p struct{ ID string }
	if err := json.Unmarshal([]byte(q("project", "add", project, "--output", "json")), &p); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"HOME": e.Home, "XDG_RUNTIME_DIR": runtimeDir, "CODEX_HOME": codexHome} {
		creds[k] = v
	}
	for k, v := range creds {
		if _, err := srv.Run("set-environment", "-g", k, v); err != nil {
			t.Fatalf("set %s for the panes: %v", k, strings.Split(err.Error(), ": ")[0])
		}
	}
	var s struct{ ID string }
	if err := json.Unmarshal([]byte(q("session", "start", "--project", "app", "--tool", "codex", "--output", "json")), &s); err != nil {
		t.Fatal(err)
	}
	target := "=p-" + p.ID + ":s-" + s.ID
	screen := func() string { out, _ := srv.Run("capture-pane", "-p", "-t", target); return out }
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("screen of the session:\n%s", screen())
		}
		q("session", "stop", s.ID, "--confirm")
	})
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
	until := func(what string, limit time.Duration, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(limit)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out after %v waiting for %s (status %q, signals %q)", limit, what, current().Status, current().Signals)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	until("codex to show its prompt", 60*time.Second, func() bool { return strings.Contains(screen(), "›") })
	if _, err := srv.Run("send-keys", "-t", target, "-l", "Reply with the single word ok."); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Run("send-keys", "-t", target, "Enter"); err != nil {
		t.Fatal(err)
	}
	until("the hooks of qcrew to report", 180*time.Second, func() bool { return current().Signals == "hooks" })
	until("the turn to end", 180*time.Second, func() bool { return current().Status == "idle" })
	var listed []map[string]any
	_ = json.Unmarshal([]byte(q("session", "list", "--output", "json")), &listed)
	for _, x := range listed {
		if x["id"] == s.ID && x["integration"] != nil {
			t.Errorf("the service shows an integration hint: %v", x["integration"])
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("codex ran the hook of someone else, which qcrew must not approve")
	}
	t.Logf("RESULT: the hooks set up by qcrew are trusted by codex; the foreign hook is not")
}
