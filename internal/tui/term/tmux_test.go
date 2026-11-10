//go:build integration || e2e

package term

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// newWindow creates a detached session that runs script in its only window, without
// status line, and returns the attach command for it.
func newWindow(t *testing.T, script string) (*testenv.TmuxServer, string, []string) {
	t.Helper()
	srv := testenv.Tmux(t)
	name := srv.NewSession(t)
	// The session of NewSession runs a shell; replace its window by the script.
	if _, err := srv.Run("respawn-window", "-k", "-t", name+":0", "sh", "-c", script); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Run("set-option", "-t", name, "status", "off"); err != nil {
		t.Fatal(err)
	}
	return srv, name, srv.AttachCommand(name + ":0")
}

func attach(t *testing.T, argv []string, cols, rows int) *Term {
	t.Helper()
	tm, err := Start(argv, cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	return tm
}

func TestResizeReachesTheProgramInTheWindow(t *testing.T) {
	const script = `last=; while :; do c=$(tput cols); [ "$c" = "$last" ] || { echo "W:$c"; last=$c; }; sleep 0.1; done`
	_, _, argv := newWindow(t, script)
	tm := attach(t, argv, 80, 24)
	waitFor(t, "width 80", func() bool { return strings.Contains(plain(tm), "W:80") })
	if err := tm.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "width 100", func() bool { return strings.Contains(plain(tm), "W:100") })
	if tm.em.Width() != 100 || tm.em.Height() != 30 {
		t.Errorf("VT size is %dx%d", tm.em.Width(), tm.em.Height())
	}
	if err := tm.Resize(0, 5); err == nil {
		t.Error("zero width accepted")
	}
}

func TestClosingTheWindowDeliversExitedMsg(t *testing.T) {
	srv, name, argv := newWindow(t, `echo up; exec sleep 600`)
	tm := attach(t, argv, 80, 24)
	waitFor(t, "output", func() bool { return strings.Contains(plain(tm), "up") })
	if _, err := srv.Run("kill-window", "-t", name+":0"); err != nil {
		t.Fatal(err)
	}
	for {
		m := tm.Next()()
		if e, ok := m.(ExitedMsg); ok {
			if e.ID != tm.ID() {
				t.Fatalf("exit of another instance: %+v", e)
			}
			break
		}
	}
	assertEnded(t, tm)
}

func TestCloseEndsTheClientButNotTheSession(t *testing.T) {
	srv, name, argv := newWindow(t, `echo up; exec sleep 600`)
	tm := attach(t, argv, 80, 24)
	waitFor(t, "output", func() bool { return strings.Contains(plain(tm), "up") })
	waitFor(t, "client attached", func() bool {
		out, _ := srv.Run("list-clients", "-t", name, "-F", "#{client_name}")
		return strings.TrimSpace(out) != ""
	})
	if err := tm.Close(); err != nil {
		t.Fatal(err)
	}
	assertEnded(t, tm)
	if _, err := srv.Run("has-session", "-t", name); err != nil {
		t.Fatalf("session ended with the client: %v", err)
	}
	waitFor(t, "client detached", func() bool {
		out, _ := srv.Run("list-clients", "-t", name, "-F", "#{client_name}")
		return strings.TrimSpace(out) == ""
	})
}
