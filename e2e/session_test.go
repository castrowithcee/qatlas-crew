//go:build e2e

package e2e

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

type sessionEntry struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Tool    string `json:"tool"`
	Status  string `json:"status"`
}

// setEnv returns env with the variable key set to value.
func setEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return append(out, key+"="+value)
}

func TestSessions(t *testing.T) {
	srv, err := testenv.StartTmux()
	if errors.Is(err, testenv.ErrNoTmux) {
		t.Skip("tmux is not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	e := testenv.Setup(t)
	env := setEnv(testenv.WithRuntime(e.Environ(), testenv.ShortRuntime(t)), "QCREW_TMUX_SOCKET", srv.Socket())
	sh := func(args ...string) result { return runIn(t, env, e.Home, args...) }
	t.Cleanup(func() {
		r := sh("service", "status", "--output", "json")
		var st serviceStatus
		_ = json.Unmarshal([]byte(r.stdout), &st)
		sh("service", "stop")
		if st.PID > 0 && !waitFor(func() bool { return testenv.ProcessGone(st.PID) }) {
			t.Errorf("service process %d is still running", st.PID)
		}
	})

	dir := filepath.Join(e.Root, "app")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := sh("project", "add", dir, "--output", "json")
	var p projectEntry
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &p) != nil {
		t.Fatalf("project add: code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}

	r = sh("session", "start", "--project", "app", "--tool", "shell", "--output", "json")
	var s sessionEntry
	if r.code != 0 || r.stderr != "" || json.Unmarshal([]byte(r.stdout), &s) != nil || s.Project != p.ID || s.Status != "running" {
		t.Fatalf("session start: code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
	out, err := srv.Run("list-windows", "-a", "-F", "#{window_name} #{@qcrew-project}")
	if err != nil || !strings.Contains(out, "s-"+s.ID+" "+p.ID+"\n") {
		t.Fatalf("tmux windows: %q, %v", out, err)
	}

	listed := func(what string) {
		t.Helper()
		r := sh("session", "list", "--output", "json")
		var list []sessionEntry
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &list) != nil || len(list) != 1 || list[0] != s {
			t.Fatalf("session list %s: code=%d stdout=%q stderr=%q", what, r.code, r.stdout, r.stderr)
		}
	}
	listed("after start")

	// The session lives in tmux, not in the service.
	if r = sh("service", "stop"); r.code != 0 {
		t.Fatalf("service stop: code=%d stderr=%q", r.code, r.stderr)
	}
	if r = sh("service", "start"); r.code != 0 {
		t.Fatalf("service start: code=%d stderr=%q", r.code, r.stderr)
	}
	listed("after a service restart")

	r = sh("session", "attach", s.ID)
	if want := "COMMAND  tmux -S " + srv.Socket() + " attach -t p-" + p.ID + ":s-" + s.ID + "\n"; r.code != 0 || r.stdout != want {
		t.Fatalf("session attach: code=%d stdout=%q stderr=%q, want %q", r.code, r.stdout, r.stderr, want)
	}

	if r = sh("session", "stop", s.ID, "--confirm"); r.code != 0 {
		t.Fatalf("session stop: code=%d stderr=%q", r.code, r.stderr)
	}
	if out, err := srv.Run("list-windows", "-a", "-F", "#{@qcrew-session}"); err != nil || strings.Contains(out, s.ID) {
		t.Fatalf("tmux windows after stop: %q, %v", out, err)
	}
	if _, err := srv.Run("has-session", "-t", "=p-"+p.ID); err == nil {
		t.Fatal("the empty project session is still there")
	}
}
