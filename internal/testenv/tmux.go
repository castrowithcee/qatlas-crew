package testenv

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// ErrNoTmux reports that the tmux binary is not installed.
var ErrNoTmux = errors.New("tmux not found in PATH")

var (
	shared  *TmuxServer
	session atomic.Int64
)

// TmuxServer is a private tmux server with its own socket. Every tmux call of this package names that socket
// with -S, so no other server, in particular the one of the user, is ever addressed.
type TmuxServer struct {
	dir    string
	socket string
}

// StartTmux starts a private server. The socket lives in a short temporary directory because socket paths
// have a small length limit. It returns ErrNoTmux when tmux is missing.
func StartTmux() (*TmuxServer, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, ErrNoTmux
	}
	dir, err := os.MkdirTemp("", "qcrew-test-")
	if err != nil {
		return nil, err
	}
	s := &TmuxServer{dir: dir, socket: filepath.Join(dir, "sock")}
	// The anchor session keeps the server alive while no test session exists.
	if _, err := s.Run("new-session", "-d", "-s", "anchor"); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return s, nil
}

// Socket returns the path of the server socket.
func (s *TmuxServer) Socket() string { return s.socket }

// Run executes a tmux command against this server only and returns its combined output.
func (s *TmuxServer) Run(args ...string) (string, error) {
	// -f /dev/null ignores the user's tmux configuration.
	cmd := exec.Command("tmux", append([]string{"-S", s.socket, "-f", "/dev/null"}, args...)...)
	// The first call starts the server, whose environment every pane inherits: only PATH comes from the
	// real environment, and the home directory is the server's own. Without TMUX the call is not nested;
	// TERM must exist for a detached session.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + s.dir, "SHELL=/bin/sh", "LANG=C.UTF-8",
		"TERM=xterm-256color", "TMUX_TMPDIR=" + s.dir}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), err
}

// AttachCommand returns the command line of a tmux client that attaches to target, a session or a
// session:window, on this server only. The caller runs it in a terminal of its own.
func (s *TmuxServer) AttachCommand(target string) []string {
	// -u: the client has no UTF-8 locale of its own in a test.
	return []string{"tmux", "-S", s.socket, "-f", "/dev/null", "-u", "attach-session", "-t", target}
}

// Stop ends this server and removes its directory. It never touches another server.
func (s *TmuxServer) Stop() {
	_, _ = s.Run("kill-server")
	_ = os.RemoveAll(s.dir)
}

// NewSession creates a detached session for one test and removes it when the test ends. It returns the
// session name.
func (s *TmuxServer) NewSession(t testing.TB) string {
	t.Helper()
	name := fmt.Sprintf("t%d", session.Add(1))
	if _, err := s.Run("new-session", "-d", "-s", name, "-x", Columns, "-y", Lines); err != nil {
		t.Fatalf("testenv: %v", err)
	}
	t.Cleanup(func() { _, _ = s.Run("kill-session", "-t", name) })
	return name
}

// TmuxMain is for TestMain: it starts one private server for the whole test run, runs the tests and stops
// that server. Without tmux the tests that call Tmux skip.
func TmuxMain(m *testing.M) int {
	s, err := StartTmux()
	if err != nil {
		fmt.Fprintf(os.Stderr, "testenv: no tmux server (%v); tests needing tmux will skip\n", err)
		return m.Run()
	}
	shared = s
	defer s.Stop()
	return m.Run()
}

// Tmux returns the server started by TmuxMain, or skips the test when there is none.
func Tmux(t testing.TB) *TmuxServer {
	t.Helper()
	if shared == nil {
		t.Skip("tmux is not available")
	}
	return shared
}
