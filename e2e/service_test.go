//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// runIn runs the binary in env, so that several runs share one home and one runtime directory.
func runIn(t *testing.T, env []string, home string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = home
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		t.Fatalf("running %v: %v", args, err)
	}
	return result{stdout.String(), stderr.String(), code}
}

type serviceStatus struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

func TestServiceLifecycle(t *testing.T) {
	e := testenv.Setup(t)
	runtimeDir := testenv.ShortRuntime(t)
	env := testenv.WithRuntime(e.Environ(), runtimeDir)
	sock := filepath.Join(runtimeDir, "qcrew", "control.sock")
	sh := func(args ...string) result { return runIn(t, env, e.Home, args...) }

	var pid int
	// Whatever the test does, the service must not outlive it.
	t.Cleanup(func() {
		sh("service", "stop")
		if pid > 0 && !waitFor(func() bool { return testenv.ProcessGone(pid) }) {
			t.Errorf("service process %d is still running", pid)
		}
	})

	r := sh("service", "start")
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("start: code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
	r = sh("service", "status", "--output", "json")
	var st serviceStatus
	if err := json.Unmarshal([]byte(r.stdout), &st); err != nil || r.code != 0 {
		t.Fatalf("status: code=%d stdout=%q: %v", r.code, r.stdout, err)
	}
	if !st.Running || st.PID <= 0 || st.Version != "dev" {
		t.Fatalf("status: %+v", st)
	}
	pid = st.PID
	if _, err := os.Lstat(sock); err != nil {
		t.Fatalf("socket while running: %v", err)
	}

	r = sh("service", "start")
	if r.code != 1 || !strings.Contains(r.stderr, "qcrew: already-running:") {
		t.Fatalf("second start: code=%d stderr=%q", r.code, r.stderr)
	}

	if r = sh("service", "stop"); r.code != 0 {
		t.Fatalf("stop: code=%d stderr=%q", r.code, r.stderr)
	}
	if !waitFor(func() bool { return testenv.ProcessGone(pid) }) {
		t.Fatalf("service process %d is still running after stop", pid)
	}
	r = sh("service", "status", "--output", "json")
	if r.code != 0 || strings.TrimSpace(r.stdout) != `{"running":false}` {
		t.Fatalf("status after stop: code=%d stdout=%q", r.code, r.stdout)
	}
	if _, err := os.Lstat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket after stop: %v", err)
	}
}

func waitFor(cond func() bool) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
