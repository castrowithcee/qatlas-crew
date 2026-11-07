//go:build e2e

// Package e2e runs the built qcrew binary from the outside and checks stdout, stderr and the exit code.
package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// binary is built once per test run in TestMain.
var binary string

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "qcrew-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	binary = filepath.Join(dir, "qcrew")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = ".." // the module root; the tests run in the e2e directory
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: build failed: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

type result struct {
	stdout, stderr string
	code           int
}

// run starts the binary in a hermetic environment and waits for it.
func run(t *testing.T, args ...string) result {
	t.Helper()
	env := testenv.Setup(t)
	cmd := exec.Command(binary, args...)
	cmd.Dir = env.Home
	cmd.Env = env.Environ()
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
