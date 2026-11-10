//go:build ext

package e2e

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// credentialVars are the variables a real claude may log in with; the test takes them from its own
// environment and never writes them to a file or the log.
var credentialVars = []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"}

type extSession struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// extRun runs the qcrew binary with env and returns stdout; a failure ends the test.
func extRun(t *testing.T, bin string, env []string, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir, cmd.Env = dir, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("qcrew %s: %v: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
}

func extWaitFor(cond func() bool) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func extDigest(t *testing.T, paths ...string) string {
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

// TestClaudeHooksWithTheRealTool proves that the hooks of a --settings file reach the service from a real
// claude: waiting at a permission question, running after the answer, idle at the end of the turn. It needs
// credentials in the environment and claude and tmux in PATH, and uses a configuration directory and home
// of its own. The pre-filled configuration (finished onboarding, trusted folder, approved key) is written
// from what is known of Claude Code's files and may need adjusting for a newer version.
func TestClaudeHooksWithTheRealTool(t *testing.T) {
	creds := map[string]string{}
	for _, v := range credentialVars {
		if val := os.Getenv(v); val != "" {
			creds[v] = val
		}
	}
	if len(creds) == 0 {
		t.Skip("no credentials: set ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN to run this test")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude is not in PATH")
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

	// Home, configuration directory and project are all below the test's temporary directory.
	configDir := filepath.Join(e.Home, ".claude")
	project := filepath.Join(e.Root, "app")
	marker := filepath.Join(e.Root, "project-hook-ran")
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	userSettings := filepath.Join(configDir, "settings.json")
	projSettings := filepath.Join(project, ".claude", "settings.json")
	write(userSettings, `{"theme":"dark"}`+"\n")
	write(projSettings, fmt.Sprintf(`{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"touch '%s'"}]}]}}`+"\n", marker))
	approved := []string{}
	if key := creds["ANTHROPIC_API_KEY"]; len(key) >= 20 {
		approved = append(approved, key[len(key)-20:])
	}
	state, _ := json.Marshal(map[string]any{
		"hasCompletedOnboarding": true, "theme": "dark",
		"customApiKeyResponses": map[string]any{"approved": approved, "rejected": []string{}},
		"projects":              map[string]any{project: map[string]any{"hasTrustDialogAccepted": true}},
	})
	write(filepath.Join(configDir, ".claude.json"), string(state))
	before := extDigest(t, userSettings, projSettings)

	// The panes get their variables from the tmux server, not from the commands that start the session.
	paneEnv := map[string]string{"HOME": e.Home, "XDG_RUNTIME_DIR": runtimeDir, "CLAUDE_CONFIG_DIR": configDir}
	for k, v := range creds {
		paneEnv[k] = v
	}
	for k, v := range paneEnv {
		if _, err := srv.Run("set-environment", "-g", k, v); err != nil {
			t.Fatalf("set %s for the panes: %v", k, strings.Split(err.Error(), ": ")[0])
		}
	}
	env := append(testenv.WithRuntime(e.Environ(), runtimeDir), "QCREW_TMUX_SOCKET="+srv.Socket(), "CLAUDE_CONFIG_DIR="+configDir)
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
	var s extSession
	if err := json.Unmarshal([]byte(q("session", "start", "--project", "app", "--tool", "claude", "--output", "json")), &s); err != nil {
		t.Fatal(err)
	}
	target := "=p-" + p.ID + ":s-" + s.ID
	status := func() string {
		var list []extSession
		_ = json.Unmarshal([]byte(q("session", "list", "--output", "json")), &list)
		for _, x := range list {
			if x.ID == s.ID {
				return x.Status
			}
		}
		return ""
	}
	screen := func() string { out, _ := srv.Run("capture-pane", "-p", "-t", target); return out }
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("screen of the session:\n%s", screen())
		}
		extRun(t, bin, env, e.Home, "session", "stop", s.ID, "--confirm")
	})
	until := func(what string, limit time.Duration, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(limit)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out after %v waiting for %s (status %q)", limit, what, status())
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// SessionStart reports idle: the hooks of the --settings file work at all.
	until("the status idle after the start", 90*time.Second, func() bool { return status() == "idle" })

	keys := func(args ...string) {
		t.Helper()
		if _, err := srv.Run(append([]string{"send-keys", "-t", target}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	keys("-l", `Use the Bash tool to run exactly this command and nothing else: echo qcrew-ext-ok`)
	keys("Enter")

	// (a) The permission question: the status is waiting within 2 s of the dialog showing.
	var shown time.Time
	until("the status waiting", 120*time.Second, func() bool {
		if status() == "waiting" {
			return true
		}
		if strings.Contains(screen(), "Do you want") {
			if shown.IsZero() {
				shown = time.Now()
			} else if time.Since(shown) > 2*time.Second {
				t.Fatalf("the permission dialog has shown for %v and the status is %q", time.Since(shown), status())
			}
		}
		return false
	})
	if marker, err := os.Stat(marker); err == nil {
		t.Logf("RESULT project hooks: the hooks of <project>/.claude/settings.json ran in addition to the --settings hooks (marker at %v)", marker.ModTime())
	} else {
		t.Logf("RESULT project hooks: the hooks of <project>/.claude/settings.json did NOT run next to the --settings hooks")
	}

	// (b) The inbox holds a permission entry with the screen lines; the answer yes chooses the first option
	// through the key mapped to it (the mapping is an assumption until this test proves it), the session works
	// on, and the entry is picked.
	var entry struct {
		ID    string   `json:"id"`
		Kind  string   `json:"kind"`
		Lines []string `json:"lines"`
	}
	until("the permission entry in the inbox", 10*time.Second, func() bool {
		var list []struct {
			ID    string   `json:"id"`
			Kind  string   `json:"kind"`
			Lines []string `json:"lines"`
		}
		_ = json.Unmarshal([]byte(q("inbox", "list", "--output", "json")), &list)
		if len(list) == 0 {
			return false
		}
		entry = list[0]
		return true
	})
	if entry.Kind != "permission" || len(entry.Lines) == 0 {
		t.Errorf("inbox entry: kind %q with %d screen lines", entry.Kind, len(entry.Lines))
	}
	inboxStatus := func() string {
		var list []struct{ ID, Status string }
		_ = json.Unmarshal([]byte(q("inbox", "list", "--all", "--output", "json")), &list)
		for _, e := range list {
			if e.ID == entry.ID {
				return e.Status
			}
		}
		return ""
	}
	q("inbox", "answer", entry.ID, "--text", "yes")
	sawRunning := false
	until("the status idle after the turn", 180*time.Second, func() bool {
		switch status() {
		case "running":
			sawRunning = true
		case "idle":
			return true
		}
		return false
	})
	if !sawRunning {
		t.Error("the status never showed running between the answer and the end of the turn")
	}
	until("the inbox entry to be picked", 10*time.Second, func() bool { return inboxStatus() == "picked" })
	t.Log("RESULT AskUserQuestion: not probed; PreToolUse with tool AskUserQuestion is mapped to waiting without a check against the real tool")

	// (d) qcrew changed none of the settings files.
	if after := extDigest(t, userSettings, projSettings); after != before {
		t.Error("the user settings or the project settings changed")
	}
}
