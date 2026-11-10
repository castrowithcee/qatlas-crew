//go:build integration || e2e

package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.TmuxMain(m)) }

// windowSet is a group of windows on the private test server, one per session ID.
type windowSet struct {
	srv   *testenv.TmuxServer
	names map[string]string // session ID -> tmux session name
}

// newWindows creates for every id a detached session that runs script in its window, without status line.
func newWindows(t *testing.T, script string, ids ...string) *windowSet {
	t.Helper()
	srv := testenv.Tmux(t)
	w := &windowSet{srv: srv, names: map[string]string{}}
	for _, id := range ids {
		name := srv.NewSession(t)
		if _, err := srv.Run("respawn-window", "-k", "-t", name+":0", "sh", "-c", script, "sh", id); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.Run("set-option", "-t", name, "status", "off"); err != nil {
			t.Fatal(err)
		}
		w.names[id] = name
	}
	return w
}

func (w *windowSet) attach(_, id string) []string { return w.srv.AttachCommand(w.names[id] + ":0") }

// clients is the number of clients attached to the session of id.
func (w *windowSet) clients(id string) int {
	out, _ := w.srv.Run("list-clients", "-t", w.names[id], "-F", "#{client_name}")
	return len(strings.Fields(out))
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The program in the tmux window sees the size of the pane of the session view, also after a resize of the
// interface and in the full screen.
func TestSessionViewResizesTheTmuxWindow(t *testing.T) {
	w := newWindows(t, sizeScript, "s1")
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, w.attach)
	step(m, "enter")
	want := func() string { return fmt.Sprintf("SIZE:%dx%d", m.view.cols, m.view.rows) }
	waitViewHas(t, m, want())

	m.Update(tea.WindowSizeMsg{Width: 108, Height: 33})
	waitViewHas(t, m, want())
	step(m, "right", "f")
	room := m.frame.Workspace(m.frameSpec())
	if got, exp := want(), fmt.Sprintf("SIZE:%dx%d", room.Width, room.Height-2); got != exp {
		t.Fatalf("full screen: %s, want %s", got, exp)
	}
	waitViewHas(t, m, want())
	waitFor(t, "the window to follow", func() bool {
		out, _ := w.srv.Run("display-message", "-p", "-t", w.names["s1"]+":0", "#{window_width}x#{window_height}")
		return strings.TrimSpace(out) == fmt.Sprintf("%dx%d", room.Width, room.Height-2)
	})
}

// Choosing another session closes the client at the old window and attaches one at the new window.
func TestSessionViewSwitchClosesTheOldClient(t *testing.T) {
	w := newWindows(t, `echo "I am $1"; exec sleep 600`, "s1", "s2")
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, w.attach)
	step(m, "enter")
	waitViewHas(t, m, "I am s1")
	waitFor(t, "a client at s1", func() bool { return w.clients("s1") == 1 })

	step(m, "down")
	waitViewHas(t, m, "I am s2")
	waitFor(t, "the client at s1 to end", func() bool { return w.clients("s1") == 0 })
	waitFor(t, "a client at s2", func() bool { return w.clients("s2") == 1 })
	if _, err := w.srv.Run("has-session", "-t", w.names["s1"]); err != nil {
		t.Fatalf("closing the view ended the session: %v", err)
	}

	step(m, "esc")
	waitFor(t, "the client at s2 to end", func() bool { return w.clients("s2") == 0 })
}

// A window that ends shows the hint instead of a dead image.
func TestSessionViewShowsTheHintWhenTheWindowEnds(t *testing.T) {
	w := newWindows(t, `echo up; exec sleep 600`, "s1")
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, w.attach)
	step(m, "enter")
	waitViewHas(t, m, "up")
	t0 := m.view.term
	if _, err := w.srv.Run("kill-session", "-t", w.names["s1"]); err != nil {
		t.Fatal(err)
	}
	for {
		msg := t0.Next()()
		m.Update(msg)
		if m.view.ended {
			break
		}
	}
	contains(t, m, "The session has ended.")
}

// Typed keys and a wheel step reach a program in the tmux window that asks for the mouse, while the mouse
// option of tmux stays off: tmux passes the reports on to a pane that asked for them.
func TestFocusedViewReachesTheTmuxWindow(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "s1")
	w := newWindows(t, `stty raw -echo; printf '\033[?1000h\033[?1006h'; echo READY; exec cat > "`+dir+`/$1"`, "s1")
	if out, err := w.srv.Run("show-options", "-gv", "mouse"); err != nil || strings.TrimSpace(out) != "off" {
		t.Fatalf("the mouse option of tmux is %q, %v", out, err)
	}
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, w.attach)
	step(m, "enter")
	waitViewHas(t, m, "READY")
	step(m, "enter")
	if !m.viewFocus {
		t.Fatal("enter does not focus the view")
	}
	step(m, "h", "i")
	x, y := screenOrigin(t, m)
	m.Update(tea.MouseWheelMsg{X: x + 2, Y: y + 1, Button: tea.MouseWheelDown})
	const want = "hi\x1b[<65;3;2M"
	waitFor(t, "the input in the window", func() bool {
		got, _ := os.ReadFile(log)
		return string(got) == want
	})
}
