package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
	"github.com/castrowithcee/qatlas-crew/internal/tui/term"
)

// shellAttach gives every session a shell script instead of a tmux client: script runs with the session ID as
// $1 and the project as $2.
func shellAttach(script string) func(project, id string) []string {
	return func(project, id string) []string { return []string{"sh", "-c", script, "sh", id, project} }
}

// newViewModel is a model with a session view, at the size of the terminal.
func newViewModel(t *testing.T, lang i18n.Lang, b *fakeBackend, width, height int, attach func(project, id string) []string) *Model {
	t.Helper()
	m := newTestModel(t, lang, b, width, height)
	m.attach = attach
	t.Cleanup(m.closeTerm)
	return m
}

// step presses keys and drops the commands they return: a command of the terminal waits for the next frame,
// which a test does not need because the screen is read from the terminal itself.
func step(m *Model, keys ...string) {
	for _, k := range keys {
		msg, ok := parseKey(k)
		if !ok {
			panic("unknown key " + k)
		}
		m.Update(msg)
		m.render()
	}
}

// click is clickAt without following the commands, which would wait for frames of the terminal for good.
func click(m *Model, x, y int) {
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.render()
}

// viewText is the visible text of the terminal in the session view.
func viewText(m *Model) string {
	if m.view == nil || m.view.term == nil {
		return ""
	}
	return ansi.Strip(m.view.term.View(m.view.cols, m.view.rows))
}

func waitView(t *testing.T, m *Model, what string, cond func(text string) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond(viewText(m)) {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s; the view shows %q", what, viewText(m))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitViewHas(t *testing.T, m *Model, text string) {
	t.Helper()
	waitView(t, m, text, func(v string) bool { return strings.Contains(v, text) })
}

// The shell reports its size whenever it changes.
const sizeScript = `last=; while :; do s="$(tput cols)x$(tput lines)"; [ "$s" = "$last" ] || { echo "SIZE:$s"; last=$s; }; sleep 0.05; done`

func TestSessionViewHasTheSizeOfItsPane(t *testing.T) {
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, shellAttach(sizeScript))
	step(m, "enter")
	want := func() string { return fmt.Sprintf("SIZE:%dx%d", m.view.cols, m.view.rows) }
	waitViewHas(t, m, want())
	first := want()

	m.Update(tea.WindowSizeMsg{Width: 110, Height: 34})
	if got := want(); got == first {
		t.Fatalf("the size of the session did not change from %s", first)
	}
	waitViewHas(t, m, want())

	// The full screen shows the view alone: the whole workspace less the title and the status line.
	step(m, "right", "f")
	room := m.frame.Workspace(m.frameSpec())
	if m.view.cols != room.Width || m.view.rows != room.Height-2 {
		t.Fatalf("full screen: the session is %dx%d, the room %dx%d", m.view.cols, m.view.rows, room.Width, room.Height)
	}
	waitViewHas(t, m, want())
	step(m, "f")
	if m.view.cols == room.Width {
		t.Fatalf("back in the columns the session keeps the full width %d", m.view.cols)
	}
	waitViewHas(t, m, want())
}

func TestSessionViewFollowsTheChosenSession(t *testing.T) {
	dir := t.TempDir()
	script := `echo $$ > "` + dir + `/$1"; echo "I am $1"; exec sleep 600`
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, shellAttach(script))
	step(m, "enter")
	waitViewHas(t, m, "I am s1")
	pid := func(id string) int {
		t.Helper()
		var n int
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if b, err := os.ReadFile(filepath.Join(dir, id)); err == nil {
				if n, err = strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
					return n
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("session %s wrote no pid", id)
		return 0
	}
	old := pid("s1")
	step(m, "down")
	waitViewHas(t, m, "I am s2")
	if !testenv.ProcessGone(old) {
		t.Error("the client of the first session still runs")
	}
	if testenv.ProcessGone(pid("s2")) {
		t.Error("the client of the second session is gone")
	}
	// Leaving the sessions closes the terminal; quitting does the same through closeTerm.
	cur := pid("s2")
	step(m, "esc")
	if m.view != nil {
		t.Error("the terminal stays attached outside the sessions area")
	}
	if !testenv.ProcessGone(cur) {
		t.Error("the client of the second session still runs")
	}
}

func TestSessionViewShowsAHintWhenTheClientEnds(t *testing.T) {
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, shellAttach(`echo bye`))
	step(m, "enter")
	t0 := m.view.term
	for {
		msg := t0.Next()()
		_, cmd := m.Update(msg)
		if _, ok := msg.(term.ExitedMsg); ok {
			if cmd != nil {
				t.Error("a wait follows the end of the client")
			}
			break
		}
	}
	contains(t, m, "The session has ended.")
	if m.view.term != nil {
		t.Error("the ended terminal is kept")
	}
	// A late message of another terminal is dropped and starts no wait.
	if _, cmd := m.Update(term.FrameMsg{ID: -1}); cmd != nil {
		t.Error("a frame of an unknown terminal is answered")
	}
}

func TestSessionViewOfAnEndedSessionAttachesNothing(t *testing.T) {
	b := demoBackend()
	b.sessions[0].Status = "exited"
	m := newViewModel(t, i18n.German, b, 120, 40, shellAttach(`echo never`))
	step(m, "enter")
	if m.view == nil || m.view.term != nil {
		t.Fatalf("a terminal is attached to an ended session: %+v", m.view)
	}
	contains(t, m, "Die Session ist beendet.")
}

func TestSessionViewFailureIsShown(t *testing.T) {
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40,
		func(string, string) []string { return []string{"/nonexistent/qcrew-test-client"} })
	step(m, "enter")
	contains(t, m, "The session cannot be shown:")
	// The failure stays for the session and is not retried with every message.
	v := m.view
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.view != v {
		t.Error("the failed start is repeated")
	}
}

func TestSessionViewStatusLine(t *testing.T) {
	b := demoBackend()
	pct := 42
	b.sessions[0].ContextPercent = &pct
	m := newViewModel(t, i18n.English, b, 120, 40, shellAttach(`exec sleep 600`))
	step(m, "enter")
	contains(t, m, "Tool: claude  Status: running  Context: 42%")
	step(m, "down")
	contains(t, m, "Tool: codex  Status: waiting")
	lacks(t, m, "Context:")

	de := newViewModel(t, i18n.German, b, 120, 40, shellAttach(`exec sleep 600`))
	step(de, "enter")
	contains(t, de, "Kontext: 42%")
}

// The title and the status line hold text the service sent; it is made visible, not executed.
func TestSessionViewEscapesForeignText(t *testing.T) {
	b := demoBackend()
	b.sessions[0].Title = "a\x1b]0;owned\x07b"
	b.sessions[0].Tool = "t\x1b[31mx"
	m := newViewModel(t, i18n.English, b, 120, 40, shellAttach(`exec sleep 600`))
	step(m, "enter")
	screen := m.render()
	for _, bad := range []string{"\x1b]0;owned", "\x07", "\x1b[31mx"} {
		if strings.Contains(screen, bad) {
			t.Errorf("the screen holds %q", bad)
		}
	}
}

func TestSessionViewIsSelectedByClickAndKey(t *testing.T) {
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, shellAttach(`echo hello; exec sleep 600`))
	step(m, "enter")
	if m.viewSel {
		t.Fatal("the view is selected before anything chose it")
	}
	step(m, "right")
	if !m.viewSel {
		t.Fatal("the right arrow does not select the view")
	}
	step(m, "left")
	if m.viewSel || m.area != areaSessions {
		t.Fatalf("left deselects the view and stays in the sessions: sel %v area %s", m.viewSel, m.area)
	}
	// A click in the view gives it the focus; the exit key hands the selection back to the interface.
	room := m.frame.Workspace(m.frameSpec())
	view, _, _ := m.sessionSplit(room.Width, room.Height)
	click(m, room.X+view.X+3, room.Y+view.Y+3)
	if !m.viewSel || !m.viewFocus || m.area != areaSessions {
		t.Fatalf("a click in the view: sel %v focus %v area %s", m.viewSel, m.viewFocus, m.area)
	}
	step(m, "ctrl+]")
	if !m.viewSel || m.viewFocus {
		t.Fatalf("the exit key leaves the focus and keeps the selection: sel %v focus %v", m.viewSel, m.viewFocus)
	}
	if res := probe(m, room.X+view.X+3, room.Y+view.Y+3); !res.Handled {
		t.Error("the view has no click target")
	}
	// A click on a row of the list gives the list the selection back.
	x, y := cellOf(t, m, "write docs")
	click(m, x, y)
	if m.viewSel {
		t.Error("a click on the list leaves the view selected")
	}
	// The full screen of a selected view and back with esc.
	step(m, "right", "f")
	if !m.full || !m.viewSel {
		t.Fatal("f does not show the view alone")
	}
	step(m, "esc")
	if m.full || !m.viewSel {
		t.Fatalf("esc leaves the full screen first: full %v sel %v", m.full, m.viewSel)
	}
}

func TestSessionViewIsAbsentWithoutAttachOrRoom(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), 120, 40)
	step(m, "enter")
	lacks(t, m, "Session view")
	m.attach = shellAttach(`exec sleep 600`)
	t.Cleanup(m.closeTerm)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	if m.view != nil {
		t.Error("a terminal runs although the room is too small for the view")
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.view == nil {
		t.Error("no terminal after the room is large enough")
	}
}

// With a project that has no session the view shows its hint and attaches nothing.
func TestSessionViewWithoutSession(t *testing.T) {
	b := demoBackend()
	b.sessions = []control.Session{}
	m := newViewModel(t, i18n.English, b, 120, 40, shellAttach(`exec sleep 600`))
	step(m, "enter")
	if m.view != nil {
		t.Error("a terminal runs without a session")
	}
	contains(t, m, "Choose a session to see it here.")
}
