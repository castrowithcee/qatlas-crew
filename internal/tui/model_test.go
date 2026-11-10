package tui

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"

	"github.com/castrowithcee/qatlas-crew/internal/client"
	"github.com/castrowithcee/qatlas-crew/internal/control"
)

const (
	wideWidth, wideHeight     = 120, 30
	narrowWidth, narrowHeight = 80, 24
)

func TestCatalogsAreComplete(t *testing.T) {
	if err := kittui.CheckCatalogs(catalog()); err != nil {
		t.Fatal(err)
	}
	keys, err := kittui.NewKeymap(keyTable(), kittui.CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		if _, err := kittui.NewTranslator(lang, catalog(), keys); err != nil {
			t.Errorf("%s: %v", lang, err)
		}
	}
}

func TestKeyTableHoldsTheRules(t *testing.T) {
	for _, charset := range []kittui.CharsetMode{kittui.CharsetUnicode, kittui.CharsetASCII} {
		if _, err := kittui.NewKeymap(keyTable(), charset); err != nil {
			t.Errorf("charset %v: %v", charset, err)
		}
	}
	if len(areaKeys) != len(areaIDs()) {
		t.Fatalf("%d area keys for %d areas", len(areaKeys), len(areaIDs()))
	}
}

func TestAreasAndFrameAreDrawable(t *testing.T) {
	for _, icons := range []string{uiprefs.IconsUnicode, uiprefs.IconsNerdFont, uiprefs.IconsNone} {
		m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
		m.icons = icons
		if err := kittui.CheckAreas(m.areas()); err != nil {
			t.Errorf("icons %s: %v", icons, err)
		}
		for _, area := range areaIDs() {
			m.area = area
			if err := kittui.CheckFrame(m.frameSpec()); err != nil {
				t.Errorf("icons %s, area %s: %v", icons, area, err)
			}
		}
	}
}

func TestPrefsSchemaAcceptsTheAreas(t *testing.T) {
	schema, err := PrefsSchema()
	if err != nil {
		t.Fatal(err)
	}
	if schema.DefaultArea != string(areaProjects) || len(schema.Areas) != len(areaIDs()) || len(schema.Themes) == 0 {
		t.Fatalf("schema = %+v", schema)
	}
	if got := uiprefs.Default(schema); got.Start != "projects" {
		t.Fatalf("default = %+v", got)
	}
}

func TestProjectsShowTheirOpenInboxCount(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	contains(t, m, "PROJECT", "INBOX", "alpha", "beta", "Sessions of alpha")
	screen := m.render()
	if !regexp.MustCompile(`alpha +3 `).MatchString(screen) || !regexp.MustCompile(`beta +0 `).MatchString(screen) {
		t.Fatalf("the rows show no counts:\n%s", screen)
	}
}

func TestChoosingAProjectSwitchesTheSessions(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	contains(t, m, "TOOL", "TITLE", "STATUS", "SINCE", "refactor parser", "write docs", "running", "waiting", "1m", "3h")
	lacks(t, m, "build watcher")
	send(m, "down")
	contains(t, m, "Sessions of beta", "build watcher", "idle", "5s")
	lacks(t, m, "refactor parser")
	send(m, "up")
	contains(t, m, "refactor parser")
}

func TestEnterOpensTheSessionsAndBackReturns(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	send(m, "enter")
	if m.area != areaSessions || m.focus() != kittui.ZoneRows {
		t.Fatalf("area %s focus %s", m.area, m.focus())
	}
	send(m, "down")
	if id, _ := m.sessList.Selected(); id != "s2" {
		t.Fatalf("selected %q", id)
	}
	send(m, "esc")
	if m.area != areaProjects {
		t.Fatalf("area %s", m.area)
	}
	send(m, "right")
	if m.area != areaSessions {
		t.Fatalf("right: area %s", m.area)
	}
	send(m, "left")
	if m.area != areaProjects {
		t.Fatalf("left: area %s", m.area)
	}
}

func TestKeysMoveBetweenAreasAndQuit(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	send(m, "2")
	contains(t, m, "follows in a later release", "Open entries: 3")
	send(m, "3")
	if m.area != areaSessions {
		t.Fatalf("3: area %s", m.area)
	}
	send(m, "4")
	contains(t, m, "theme", "language", "mouse")
	send(m, "1")
	if m.area != areaProjects {
		t.Fatalf("1: area %s", m.area)
	}
	if !isQuit(press(m, "q")) || !isQuit(press(m, "ctrl+c")) {
		t.Fatal("q and ctrl+c do not quit")
	}
}

func TestTabLineArrowsStepTheAreas(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	send(m, "tab")
	if m.focus() != kittui.ZoneTabs {
		t.Fatalf("focus %s", m.focus())
	}
	send(m, "right")
	if m.area != areaInbox || m.focus() != kittui.ZoneTabs {
		t.Fatalf("area %s focus %s", m.area, m.focus())
	}
	send(m, "left", "left")
	if m.area != areaSetup {
		t.Fatalf("area %s", m.area)
	}
	send(m, "left")
	if m.area != areaSettings {
		t.Fatalf("area %s", m.area)
	}
}

func TestFullScreenToggles(t *testing.T) {
	for _, size := range [][2]int{{wideWidth, wideHeight}, {narrowWidth, narrowHeight}} {
		m := newTestModel(t, i18n.English, demoBackend(), size[0], size[1])
		contains(t, m, "alpha", "refactor parser")
		send(m, "f")
		if !m.full {
			t.Fatal("f did not switch to the full screen")
		}
		contains(t, m, "alpha")
		lacks(t, m, "refactor parser")
		send(m, "f")
		if m.full {
			t.Fatal("f did not switch back")
		}
		contains(t, m, "alpha", "refactor parser")
		send(m, "enter", "f")
		contains(t, m, "refactor parser", "Sessions of alpha")
		lacks(t, m, "beta")
		send(m, "esc")
		if m.full || m.area != areaSessions {
			t.Fatalf("esc: full %v area %s", m.full, m.area)
		}
		contains(t, m, "beta")
	}
}

func TestStackedBelowOneHundredColumns(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), 99, 24)
	lines := strings.Split(m.render(), "\n")
	var proj, sess int
	for i, l := range lines {
		if strings.Contains(l, "alpha") && proj == 0 {
			proj = i
		}
		if strings.Contains(l, "refactor parser") {
			sess = i
		}
	}
	if proj == 0 || sess <= proj {
		t.Fatalf("projects at row %d, sessions at row %d:\n%s", proj, sess, strings.Join(lines, "\n"))
	}
}

func TestSmallTerminalAsksForMoreRoom(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), 30, 8)
	contains(t, m, "30")
	if m.mouseMap != nil {
		t.Fatal("the mouse map is on in a terminal that is too small")
	}
}

func TestStoppingASessionAsksFirst(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "enter", "x")
	if m.modal == nil {
		t.Fatal("x opened no question")
	}
	contains(t, m, "Stop the session 'refactor parser'?", "tool: claude")
	if len(b.stops()) != 0 {
		t.Fatalf("the service was called before the answer: %v", b.stops())
	}
	send(m, "enter") // enter is no answer to a destructive question
	if m.modal != nil || len(b.stops()) != 0 {
		t.Fatalf("enter answered: modal %v, calls %v", m.modal, b.stops())
	}
	send(m, "x", "n")
	if m.modal != nil || len(b.stops()) != 0 {
		t.Fatalf("n answered: calls %v", b.stops())
	}
	send(m, "x", "y")
	if got := b.stops(); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("calls = %v", got)
	}
	contains(t, m, "The session is stopped.")
}

func TestStopFailureIsShownAndKeepsTheInterface(t *testing.T) {
	b := demoBackend()
	b.stopErr = &cli.Failure{Code: "session-gone", Message: "the session is gone", NextStep: "Run 'qcrew session list'."}
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "enter", "x", "y")
	contains(t, m, "the session is gone", "qcrew session list")
	if m.conn == nil {
		t.Fatal("an answer of the service dropped the connection")
	}
}

func TestStopNeedsASession(t *testing.T) {
	m := newTestModel(t, i18n.English, &fakeBackend{projects: []control.Project{{ID: "p", Name: "empty"}}}, wideWidth, wideHeight)
	send(m, "enter", "x")
	if m.modal != nil {
		t.Fatal("a question for no session")
	}
	contains(t, m, "no session to stop")
}

func TestTickReadsTheServiceAgain(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	m.Update(tea.WindowSizeMsg{Width: wideWidth, Height: wideHeight})
	// Init asks for a read and a timer.
	drain(m, m.Init())
	tm := timersOf[m]
	if len(tm.waits) == 0 || tm.waits[len(tm.waits)-1] != 2*time.Second {
		t.Fatalf("timers %v, want one of 2s", tm.waits)
	}
	before := b.reads
	b.sessions[0].Status = "error"
	b.projects[0].InboxOpen = 7
	tm.fire(t, m)
	if b.reads <= before {
		t.Fatal("the tick read nothing")
	}
	contains(t, m, "error", " 7")
	if len(tm.waits) == 0 {
		t.Fatal("the tick did not start the next timer")
	}
}

func TestRefreshKeepsTheCursorOnItsSession(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "enter", "down")
	b.sessions[0], b.sessions[1] = b.sessions[1], b.sessions[0]
	send(m, "r")
	if id, _ := m.sessList.Selected(); id != "s2" {
		t.Fatalf("selected %q", id)
	}
}

func TestMissingTmuxShowsTheNextStep(t *testing.T) {
	b := demoBackend()
	b.sessErr = &cli.Failure{Code: client.CodeDependencyMissing, Message: "tmux is not installed", NextStep: "Run 'qcrew setup'."}
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	contains(t, m, "tmux is not installed", "qcrew setup", "alpha")
	if m.conn == nil {
		t.Fatal("the connection was dropped")
	}
	b.sessErr = &cli.Failure{Code: client.CodeToolMissing, Message: "claude is not installed"}
	send(m, "r")
	contains(t, m, "claude is not installed", "qcrew setup")
	b.sessErr = nil
	send(m, "r")
	lacks(t, m, "qcrew setup")
	contains(t, m, "refactor parser")
}

func TestBrokenChannelIsOpenedAgain(t *testing.T) {
	b := demoBackend()
	b.projErr = errors.New("read the answer: EOF")
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	if m.conn != nil || !b.closed {
		t.Fatalf("conn %v closed %v", m.conn, b.closed)
	}
	contains(t, m, "does not answer")
	b.projErr, b.closed = nil, false
	send(m, "r")
	if m.conn == nil {
		t.Fatal("no new connection")
	}
	contains(t, m, "alpha")
	lacks(t, m, "does not answer")
}

func TestUnreachableServiceShowsItsNextStep(t *testing.T) {
	m, err := newModel(config{ctx: context.Background(), charset: kittui.CharsetASCII, profile: kittui.ProfileNone,
		theme: kittui.ThemeTerminal, connect: func(context.Context) (Connection, error) {
			return nil, &cli.Failure{Message: "the qcrew service is not running", NextStep: "Run 'qcrew service start'."}
		}})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: wideWidth, Height: wideHeight})
	load(t, m)
	contains(t, m, "not running", "qcrew service start")
	if !isQuit(press(m, "q")) {
		t.Fatal("the interface does not quit without a service")
	}
}

func TestForeignTextIsMasked(t *testing.T) {
	b := demoBackend()
	b.projects[0].Name = "al\x1b[31mpha"
	b.sessions[0].Title = "evil\x1b]0;x\x07title"
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	screen := m.render()
	if strings.Contains(screen, "\x1b") || strings.Contains(screen, "\x07") {
		t.Fatalf("a control character reached the screen: %q", screen)
	}
}

func TestSinceIsARelativeDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-time.Second: "0s", 0: "0s", 59 * time.Second: "59s", time.Minute: "1m", 59 * time.Minute: "59m",
		time.Hour: "1h", 23 * time.Hour: "23h", 48 * time.Hour: "2d",
	} {
		if got := since(epoch, epoch.Add(-d)); got != want {
			t.Errorf("since(%v) = %q, want %q", d, got, want)
		}
	}
	if since(epoch, time.Time{}) != "-" {
		t.Error("an unknown time is not a dash")
	}
}

func TestGermanTextsAreShown(t *testing.T) {
	m := newTestModel(t, i18n.German, demoBackend(), wideWidth, wideHeight)
	contains(t, m, "Projekte", "Postfach", "Sessions von alpha", "läuft", "wartet", "SEIT")
}

func TestHelpShowsTheTopicsAndTheKeysOfTheScreen(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	send(m, "?")
	if !m.helpOpen {
		t.Fatal("? opens no help")
	}
	contains(t, m, "qcrew")
	send(m, "right")
	if !m.helpOpen {
		t.Fatal("a topic key closed the help")
	}
	send(m, "?")
	if m.helpOpen {
		t.Fatal("? did not close the help")
	}
}

func TestStartAreaAndPreferenceWarningsApply(t *testing.T) {
	m, err := newModel(config{ctx: context.Background(), charset: kittui.CharsetASCII, profile: kittui.ProfileNone,
		theme: "no-such-theme", start: areaSessions, warnings: []string{"tui.yaml: cannot parse"}})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: wideWidth, Height: wideHeight})
	if m.area != areaSessions {
		t.Fatalf("area %s", m.area)
	}
	contains(t, m, "tui.yaml: cannot parse")
}
