package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"

	"github.com/castrowithcee/qatlas-crew/internal/tui/term"
)

// rawBytes is a session that asks for bracketed paste and the mouse, then prints every byte it reads as <hex>.
// The terminal is raw and silent, so what the screen shows is exactly what arrived.
const rawBytes = `stty raw -echo opost; printf '\033[?2004h\033[?1000h\033[?1006h'; echo READY; ` +
	`while :; do h=$(dd bs=1 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'); printf '<%s>' "$h"; done`

// rawBytesPlain is rawBytes without the requests: the program has no paste markers and no mouse.
const rawBytesPlain = `stty raw -echo opost; echo READY; ` +
	`while :; do h=$(dd bs=1 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'); printf '<%s>' "$h"; done`

func hexOf(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(&b, "<%02x>", s[i])
	}
	return b.String()
}

// arrived is what the session printed after READY, without the line breaks of the screen.
func arrived(m *Model) string {
	_, after, _ := strings.Cut(viewText(m), "READY")
	return strings.NewReplacer("\n", "", " ", "").Replace(after)
}

func waitArrived(t *testing.T, m *Model, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for arrived(m) != want {
		if time.Now().After(deadline) {
			t.Fatalf("the session got %q, want %q", arrived(m), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// focusedModel shows the sessions of the first project, waits for the script and gives its view the focus.
func focusedModel(t *testing.T, lang i18n.Lang, script string) *Model {
	t.Helper()
	m := newViewModel(t, lang, demoBackend(), 120, 40, shellAttach(script))
	step(m, "enter")
	waitViewHas(t, m, "READY")
	step(m, "enter")
	if !m.viewFocus || m.input() != kittui.InputNativeTerminal {
		t.Fatalf("enter on the session does not focus the view: focus %v input %v", m.viewFocus, m.input())
	}
	return m
}

// quits tells whether the message ends the program. A command that waits for a frame is not run.
func quits(m *Model, msg tea.Msg) bool {
	_, cmd := m.Update(msg)
	m.render()
	if cmd == nil {
		return false
	}
	res := make(chan bool, 1)
	go func() { _, ok := cmd().(tea.QuitMsg); res <- ok }()
	select {
	case ok := <-res:
		return ok
	case <-time.After(300 * time.Millisecond):
		return false
	}
}

func keyMsg(t *testing.T, name string) tea.KeyPressMsg {
	t.Helper()
	msg, ok := parseKey(name)
	if !ok {
		t.Fatalf("unknown key %q", name)
	}
	return msg
}

// nextMsg is the next message of the terminal; it fails the test instead of waiting for good.
func nextMsg(t *testing.T, tm *term.Term) tea.Msg {
	t.Helper()
	got := make(chan tea.Msg, 1)
	go func() { got <- tm.Next()() }()
	select {
	case msg := <-got:
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("the terminal sends no message")
		return nil
	}
}

// screenOrigin is the cell of the screen of the session view where the program's first cell is drawn.
func screenOrigin(t *testing.T, m *Model) (x, y int) { return cellOf(t, m, "READY") }

func TestFocusGivesEveryKeyToTheSession(t *testing.T) {
	m := focusedModel(t, i18n.English, rawBytes)
	// Letters, q, esc, ctrl+c, enter, ? and the arrows are the session's, and none of them acts in the interface.
	for _, k := range []string{"a", "q", "esc", "ctrl+c", "enter", "?", "f", "x", "1", "tab", "left", "down"} {
		if quits(m, keyMsg(t, k)) {
			t.Fatalf("%s ends the interface although the session has the focus", k)
		}
	}
	waitArrived(t, m, hexOf("aq\x1b\x03\r?fx1\t\x1b[D\x1b[B"))
	if !m.viewFocus || m.area != areaSessions || m.full || m.helpOpen || m.modal != nil {
		t.Fatalf("the interface reacted: focus %v area %s full %v help %v", m.viewFocus, m.area, m.full, m.helpOpen)
	}
	if id, _ := m.sessList.Selected(); id != "s1" {
		t.Errorf("a key moved the session list to %q", id)
	}
	// A key release is never sent.
	m.Update(tea.KeyReleaseMsg{Code: 'a', Text: "a"})
	if got := arrived(m); got != hexOf("aq\x1b\x03\r?fx1\t\x1b[D\x1b[B") {
		t.Errorf("a release reached the session: %q", got)
	}
}

func TestExitKeyNeverReachesTheSessionAndReturnsToTheInterface(t *testing.T) {
	m := focusedModel(t, i18n.English, rawBytes)
	step(m, "a", "ctrl+]")
	if m.viewFocus || !m.viewSel || m.input() != kittui.InputNavigation {
		t.Fatalf("the exit key leaves focus %v sel %v input %v", m.viewFocus, m.viewSel, m.input())
	}
	// The keys of the interface work again; enter focuses again.
	step(m, "enter")
	step(m, "b")
	waitArrived(t, m, hexOf("ab"))
	if m.view.term == nil {
		t.Fatal("the client is gone")
	}
	step(m, "ctrl+]")
	if !quits(m, keyMsg(t, "q")) {
		t.Error("q does not end the interface after the exit key")
	}
	waitArrived(t, m, hexOf("ab"))
}

// The exit key works from inside a batch of the input filter, and what follows it in the batch is the
// interface's.
func TestExitKeyInABatchSplitsTheInput(t *testing.T) {
	m := focusedModel(t, i18n.English, rawBytes)
	_, _ = m.Update(kittui.InputBatch{keyMsg(t, "a"), keyMsg(t, "ctrl+]"), keyMsg(t, "f")})
	m.render()
	waitArrived(t, m, hexOf("a"))
	if m.viewFocus || !m.full {
		t.Fatalf("after the exit key f toggles the full screen: focus %v full %v", m.viewFocus, m.full)
	}
}

func TestExtraExitKeyFromThePreferences(t *testing.T) {
	m := focusedModel(t, i18n.English, rawBytes)
	if err := m.keys.Bind(map[kittui.ActionID]string{actViewExit: "ctrl+x"}); err != nil {
		t.Fatal(err)
	}
	contains(t, m, "ctrl+]/ctrl+x back to qcrew")
	step(m, "ctrl+x")
	if m.viewFocus {
		t.Fatal("the extra key does not leave the view")
	}
	step(m, "enter", "ctrl+]", "enter", "ctrl+x", "enter", "z")
	// Neither exit key reached the session.
	waitArrived(t, m, hexOf("z"))
}

func TestExitKeyFromTheSavedPreferences(t *testing.T) {
	schema, err := PrefsSchema()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), uiprefs.FileName)
	if err := os.WriteFile(path, []byte("keys:\n  viewexit: ctrl+x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prefs, warnings := uiprefs.Load(path, schema)
	if len(warnings) > 0 || prefs.Keys[actViewExit] != "ctrl+x" {
		t.Fatalf("the binding is not loaded: %v %v", prefs.Keys, warnings)
	}
	m, err := newModel(config{ctx: context.Background(), lang: i18n.English, theme: kittui.ThemeTerminal,
		charset: kittui.CharsetASCII, prefs: prefs})
	if err != nil {
		t.Fatal(err)
	}
	if !m.keys.Is(keyMsg(t, "ctrl+x"), actViewExit, kittui.InputNavigation) ||
		!m.keys.Is(keyMsg(t, "ctrl+]"), actViewExit, kittui.InputNavigation) {
		t.Error("the model does not bind both exit keys")
	}
	if m.notice != "" {
		t.Errorf("a valid binding gives a warning: %q", m.notice)
	}

	// Keys that collide or that the key table forbids are dropped with a warning and never change a key.
	for _, bad := range []string{"q", "ctrl+c", "ctrl+z", "f2"} {
		if err := os.WriteFile(path, []byte("keys:\n  viewexit: "+bad+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		prefs, warnings := uiprefs.Load(path, schema)
		if len(warnings) == 0 || prefs.Keys[actViewExit] != "" {
			t.Errorf("%s was accepted as the exit key: %v %v", bad, prefs.Keys, warnings)
		}
	}
	// Keys of other actions cannot be rebound.
	if err := os.WriteFile(path, []byte("keys:\n  quit: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if prefs, warnings := uiprefs.Load(path, schema); len(warnings) == 0 || len(prefs.Keys) != 0 {
		t.Errorf("another action was rebound: %v %v", prefs.Keys, warnings)
	}
}

func TestFocusPassesAPasteOnceWithItsMarkers(t *testing.T) {
	m := focusedModel(t, i18n.English, rawBytes)
	// The markers of the outer terminal are no input; the content comes as one message.
	m.Update(tea.PasteStartMsg{})
	m.Update(tea.PasteMsg{Content: "one\ntwo\nthree"})
	m.Update(tea.PasteEndMsg{})
	step(m, "z")
	waitArrived(t, m, hexOf("\x1b[200~one\ntwo\nthree\x1b[201~z"))
	// A paste inside a batch of the filter is taken as well, once.
	m.Update(kittui.InputBatch{keyMsg(t, "a"), tea.PasteMsg{Content: "p"}})
	step(m, "z")
	waitArrived(t, m, hexOf("\x1b[200~one\ntwo\nthree\x1b[201~z")+hexOf("a\x1b[200~p\x1b[201~z"))
}

func TestFocusPastesPlainTextWhereTheProgramAsksForNoMarkers(t *testing.T) {
	m := focusedModel(t, i18n.English, rawBytesPlain)
	m.Update(tea.PasteMsg{Content: "a\nb"})
	waitArrived(t, m, hexOf("a\nb"))
}

func TestFocusPassesClickAndWheelInsideTheScreen(t *testing.T) {
	m := focusedModel(t, i18n.English, rawBytes)
	ox, oy := screenOrigin(t, m)
	at := func(dx, dy int) (int, int) { return ox + dx, oy + dy }
	var want string

	x, y := at(4, 2)
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	want += hexOf("\x1b[<0;5;3M\x1b[<0;5;3m")
	x, y = at(0, 0)
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	want += hexOf("\x1b[<64;1;1M")
	x, y = at(m.view.cols-1, m.view.rows-1)
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	want += hexOf(fmt.Sprintf("\x1b[<65;%d;%dM", m.view.cols, m.view.rows))

	// Motion, also with a button held, is never passed on. Neither is anything outside the screen: the title
	// line, the status line, the area left of the view, the list above it.
	x, y = at(3, 3)
	m.Update(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	for _, c := range [][2]int{{0, -1}, {0, m.view.rows}, {-1, 0}, {m.view.cols, 0}, {2, -oy + 1}} {
		x, y = at(c[0], c[1])
		m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	}
	m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft}) // the header of the interface
	step(m, "z")
	waitArrived(t, m, want+hexOf("z"))
	if !m.viewFocus {
		t.Error("a click outside the view ended the focus")
	}
}

// The click that gives the view the focus stays with the interface: neither its press nor its release reaches
// the session.
func TestFocusingClickDoesNotReachTheSession(t *testing.T) {
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, shellAttach(rawBytes))
	step(m, "enter")
	waitViewHas(t, m, "READY")
	x, y := screenOrigin(t, m)
	m.Update(tea.MouseClickMsg{X: x + 2, Y: y + 1, Button: tea.MouseLeft})
	m.render()
	if !m.viewFocus {
		t.Fatal("a click in the view does not focus it")
	}
	m.Update(tea.MouseReleaseMsg{X: x + 2, Y: y + 1, Button: tea.MouseLeft})
	step(m, "z")
	waitArrived(t, m, hexOf("z"))
}

// A real ctrl+] is the byte 0x1d, which the input reader names as the key table does.
func TestExitKeyIsTheByteOfCtrlBracket(t *testing.T) {
	var d uv.EventDecoder
	_, ev := d.Decode([]byte{0x1d})
	key, ok := ev.(uv.KeyPressEvent)
	if !ok {
		t.Fatalf("0x1d decodes to %T", ev)
	}
	if name := kittui.KeyName(tea.KeyPressMsg(key)); name != "ctrl+]" {
		t.Fatalf("0x1d is %q", name)
	}
}

// The wheel does nothing in the interface while the view has the focus; without the focus it scrolls the
// list under it.
func TestFocusKeepsTheWheelFromTheInterface(t *testing.T) {
	m := focusedModel(t, i18n.English, rawBytes)
	x, y := cellOf(t, m, "refactor parser")
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	if id, _ := m.sessList.Selected(); id != "s1" {
		t.Errorf("the wheel moved the list to %q during the focus", id)
	}
	step(m, "ctrl+]")
	x, y = cellOf(t, m, "refactor parser")
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	m.render()
	if m.viewFocus {
		t.Error("the wheel gave the focus back")
	}
}

func TestEnterOnTheSelectedViewFocusesIt(t *testing.T) {
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, shellAttach(rawBytes))
	step(m, "enter", "right")
	waitViewHas(t, m, "READY")
	if m.viewFocus || !m.viewSel {
		t.Fatalf("the right arrow only selects: focus %v sel %v", m.viewFocus, m.viewSel)
	}
	contains(t, m, "enter control the session")
	step(m, "enter")
	if !m.viewFocus {
		t.Fatal("enter does not focus the selected view")
	}
	contains(t, m, "ctrl+] back to qcrew")
	lacks(t, m, "control the session", "q quit")
	step(m, "ctrl+]")
	contains(t, m, "enter control the session")
	waitArrived(t, m, "")
}

func TestFocusFooterIsGerman(t *testing.T) {
	m := focusedModel(t, i18n.German, rawBytes)
	contains(t, m, "ctrl+] zurück zu qcrew")
	step(m, "ctrl+]")
	contains(t, m, "enter Session steuern")
}

// Without a running client enter and a click only select the view, as before.
func TestNoFocusWithoutAClient(t *testing.T) {
	b := demoBackend()
	b.sessions[0].Status = "exited"
	m := newViewModel(t, i18n.English, b, 120, 40, shellAttach(rawBytes))
	step(m, "enter", "enter")
	if m.viewFocus {
		t.Fatal("focus without a client")
	}
	step(m, "right")
	room := m.frame.Workspace(m.frameSpec())
	view, _, _ := m.sessionSplit(room.Width, room.Height)
	click(m, room.X+view.X+3, room.Y+view.Y+3)
	if m.viewFocus || !m.viewSel {
		t.Fatalf("click without a client: focus %v sel %v", m.viewFocus, m.viewSel)
	}
	lacks(t, m, "control the session")
}

func TestFocusEndsWhenTheClientEnds(t *testing.T) {
	m := focusedModel(t, i18n.English, `stty raw -echo opost; echo READY; dd bs=1 count=1 >/dev/null 2>&1`)
	t0 := m.view.term
	step(m, "a")
	for {
		msg := nextMsg(t, t0)
		m.Update(msg)
		if _, ok := msg.(term.ExitedMsg); ok {
			break
		}
	}
	if m.viewFocus || m.input() != kittui.InputNavigation {
		t.Fatalf("the focus outlives the client: focus %v input %v", m.viewFocus, m.input())
	}
	contains(t, m, "The session has ended.")
	if !quits(m, keyMsg(t, "q")) {
		t.Error("the keys of the interface do not work after the client ended")
	}
}

func TestFocusEndsWhenTheSessionChangesOrTheViewIsGone(t *testing.T) {
	m := focusedModel(t, i18n.English, rawBytes)
	b := m.conn.(*fakeBackend)
	b.mu.Lock()
	b.sessions = b.sessions[1:] // s1 disappears, the list moves on to s2
	b.mu.Unlock()
	load(t, m)
	if m.viewFocus || m.view == nil || m.view.id != "s2" {
		t.Fatalf("a new session inherits the focus: focus %v view %+v", m.viewFocus, m.view)
	}
	step(m, "enter")
	if !m.viewFocus {
		t.Fatal("no focus on the new session")
	}
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	if m.viewFocus || m.view != nil || m.input() == kittui.InputNativeTerminal {
		t.Fatalf("the focus outlives the view: focus %v view %v", m.viewFocus, m.view)
	}
}

// A paste far larger than the PTY takes, to a program that reads nothing, must not hold the interface.
func TestFocusedInputNeverBlocksUpdate(t *testing.T) {
	m := focusedModel(t, i18n.English, `stty raw -echo; echo READY; exec sleep 600`)
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Update(tea.PasteMsg{Content: strings.Repeat("0123456789abcdef", 1<<16)})
		step(m, "a", "ctrl+]")
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Update blocked on the input of a session that does not read")
	}
	if m.viewFocus {
		t.Error("the exit key did not work behind the stuck paste")
	}
}

// Without the focus the interface is what it was: q quits and the wheel scrolls the lists.
func TestWithoutFocusKeysStayWithTheInterface(t *testing.T) {
	m := newViewModel(t, i18n.English, demoBackend(), 120, 40, shellAttach(rawBytes))
	step(m, "enter")
	waitViewHas(t, m, "READY")
	if m.viewFocus {
		t.Fatal("a view without the focus took it")
	}
	// A paste is dropped without the focus.
	m.Update(tea.PasteMsg{Content: "x"})
	step(m, "down")
	if id, _ := m.sessList.Selected(); id != "s2" {
		t.Errorf("down did not move the list: %q", id)
	}
	if !quits(m, keyMsg(t, "q")) {
		t.Error("q does not quit without the focus")
	}
}
