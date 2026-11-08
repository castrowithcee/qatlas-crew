//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/localipc"

	"github.com/castrowithcee/qatlas-crew/internal/client"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

const helperEnv = "QCREW_TEST_HELPER"

// helperCommand starts this test binary as the service process: TestHelperProcess runs the hidden command.
func helperCommand() background.Command {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	return background.Command{Program: exe, Args: []string{"-test.run=^TestHelperProcess$"},
		Env: append(os.Environ(), helperEnv+"=service")}
}

// TestHelperProcess is not a test: it is the body of the processes the tests below start from this binary.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		return
	}
	serviceCommand = helperCommand
	switch mode {
	case "service":
		os.Exit(Run([]string{"service", "run"}, os.Stdout, os.Stderr))
	case "start":
		os.Exit(Run([]string{"service", "start", "--output", "json"}, os.Stdout, os.Stderr))
	case "dial":
		fmt.Println(helperDial())
		os.Exit(0)
	}
	os.Exit(3)
}

// helperDial reports how a client of this binary fares: through client.Dial, and over the raw socket.
func helperDial() string {
	var out []string
	_, err := client.Dial(context.Background(), localipc.DialOptions{})
	var peer *localipc.PeerError
	switch {
	case errors.As(err, &peer):
		out = append(out, "dial=peer-error")
	case err == nil:
		out = append(out, "dial=accepted")
	default:
		out = append(out, "dial=error:"+err.Error())
	}
	home, _ := os.UserHomeDir()
	path, err := client.Endpoint(home).Path()
	if err != nil {
		return strings.Join(append(out, "raw=error:"+err.Error()), " ")
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return strings.Join(append(out, "raw=error:"+err.Error()), " ")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = conn.Write([]byte(`{"v":1,"op":"ping"}` + "\n"))
	if data, _ := io.ReadAll(conn); len(data) > 0 {
		return strings.Join(append(out, "raw=answered"), " ")
	}
	return strings.Join(append(out, "raw=refused"), " ")
}

// serviceTestEnv gives the test a hermetic home and a short runtime directory, starts the service from
// this binary on demand, and makes sure that no service outlives the test.
func serviceTestEnv(t *testing.T) *testenv.Env {
	t.Helper()
	env := testenv.Setup(t)
	testenv.ShortRuntime(t)
	old := serviceCommand
	serviceCommand = helperCommand
	t.Cleanup(func() { serviceCommand = old })
	t.Cleanup(func() { stopAndVerify(t) })
	return env
}

// stopAndVerify ends the service of the test environment, if one runs, and checks that its process is gone.
func stopAndVerify(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := client.Dial(ctx, localipc.DialOptions{})
	if errors.Is(err, client.ErrNotRunning) {
		return
	}
	if err != nil {
		t.Errorf("cleanup: cannot reach the service: %v", err)
		return
	}
	st, err := c.Status(ctx)
	if err != nil {
		t.Errorf("cleanup: status: %v", err)
		return
	}
	if err := c.Stop(ctx); err != nil {
		t.Errorf("cleanup: stop: %v", err)
	}
	_ = c.Close()
	waitGone(t, st.PID)
}

// waitGone waits for the process pid, which the test started and verified through the service's own status
// answer, to end.
func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if testenv.ProcessGone(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("process %d is still running", pid)
}

type statusJSON struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid"`
	Version string `json:"version"`
}

func statusOf(t *testing.T) statusJSON {
	t.Helper()
	code, out, errOut := run(t, "service", "status", "--output", "json")
	if code != 0 {
		t.Fatalf("status: code=%d stderr=%q", code, errOut)
	}
	var st statusJSON
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatalf("status output %q: %v", out, err)
	}
	return st
}

func socketPath(t *testing.T, env *testenv.Env) string {
	t.Helper()
	path, err := client.Endpoint(env.Home).Path()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServiceLifecycle(t *testing.T) {
	env := serviceTestEnv(t)
	sock := socketPath(t, env)

	if st := statusOf(t); st.Running {
		t.Fatalf("running before start: %+v", st)
	}
	if _, err := os.Lstat(sock); err == nil {
		t.Fatal("status created the socket")
	}

	code, out, errOut := run(t, "service", "start", "--output", "json")
	if code != 0 {
		t.Fatalf("start: code=%d stderr=%q", code, errOut)
	}
	var started statusJSON
	if err := json.Unmarshal([]byte(out), &started); err != nil || !started.Running || started.PID <= 0 {
		t.Fatalf("start output %q: %v", out, err)
	}

	st := statusOf(t)
	if !st.Running || st.PID != started.PID || st.Version != version {
		t.Fatalf("status %+v after start %+v", st, started)
	}
	if st.PID == os.Getpid() {
		t.Fatal("the service runs in the test process")
	}

	c, err := client.Dial(context.Background(), localipc.DialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	_ = c.Close()

	code, _, errOut = run(t, "service", "start")
	if code != 1 || !strings.Contains(errOut, "already-running") {
		t.Fatalf("second start: code=%d stderr=%q", code, errOut)
	}
	if got := statusOf(t); got.PID != started.PID {
		t.Fatalf("the second start disturbed the service: %+v", got)
	}

	code, out, errOut = run(t, "service", "stop", "--output", "json")
	if code != 0 || !strings.Contains(out, `"stopped":true`) {
		t.Fatalf("stop: code=%d out=%q stderr=%q", code, out, errOut)
	}
	waitGone(t, started.PID)
	if _, err := os.Lstat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket after stop: %v", err)
	}
	if st := statusOf(t); st.Running {
		t.Fatalf("running after stop: %+v", st)
	}
	code, out, _ = run(t, "service", "stop", "--output", "json")
	if code != 0 || !strings.Contains(out, `"stopped":false`) {
		t.Fatalf("stop without service: code=%d out=%q", code, out)
	}
	if _, err := os.Stat(filepath.Join(env.Home, ".qatlas", "qcrew", "logs", "service.log")); err != nil {
		t.Errorf("service log: %v", err)
	}
}

// The service must outlive the process that started it.
func TestServiceOutlivesStarter(t *testing.T) {
	env := serviceTestEnv(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"=start")
	out, err := cmd.Output() // returns only after the starter ended
	if err != nil {
		t.Fatalf("starter: %v", err)
	}
	var started statusJSON
	if err := json.Unmarshal(out, &started); err != nil || started.PID <= 0 {
		t.Fatalf("starter output %q: %v", out, err)
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatal("the starter did not end")
	}
	time.Sleep(200 * time.Millisecond)
	if st := statusOf(t); !st.Running || st.PID != started.PID {
		t.Fatalf("status after the starter ended: %+v, started %+v", st, started)
	}
	_ = env
}

// A process of another program file must not reach the service, nor may the client reach a service of another
// program file.
func TestServiceRefusesOtherProgram(t *testing.T) {
	serviceTestEnv(t)
	if code, _, errOut := run(t, "service", "start"); code != 0 {
		t.Fatalf("start: code=%d stderr=%q", code, errOut)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "qc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	other := filepath.Join(dir, "other")
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(other, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(other, "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"=dial")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("other program: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "dial=peer-error raw=refused" {
		t.Fatalf("other program result: %q", got)
	}
	// The service is unharmed.
	if st := statusOf(t); !st.Running {
		t.Fatalf("status: %+v", st)
	}
}

func TestAutostartAndNoAutostartWithRealService(t *testing.T) {
	serviceTestEnv(t)
	real := defaultServiceAccess()
	real.noAutostart = true
	if _, err := real.connect(context.Background()); err == nil || !strings.Contains(err.Error(), "--no-autostart") {
		t.Fatalf("no-autostart: %v", err)
	}
	if st := statusOf(t); st.Running {
		t.Fatal("--no-autostart started the service")
	}
	real.noAutostart = false
	c, err := real.connect(context.Background())
	if err != nil {
		t.Fatalf("autostart: %v", err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if st := statusOf(t); !st.Running {
		t.Fatal("autostart did not leave a running service")
	}
}
