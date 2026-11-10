package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/session"
)

func TestSetupAreaShowsTheProgramsFromTheService(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	if b.setupReads != 0 {
		t.Fatalf("the programs were checked before the area was shown: %d", b.setupReads)
	}
	send(m, "5")
	if m.area != areaSetup || b.setupReads != 1 {
		t.Fatalf("area %s, checks %d", m.area, b.setupReads)
	}
	contains(t, m, session.Program, "installed", "tmux 3.4", "claude", "missing", "[install]",
		"see https://github.com/openai/codex", "Result")
	// The periodic read leaves the check alone, since it starts the programs.
	m.tick()
	timersOf[m].fire(t, m)
	if b.setupReads != 1 {
		t.Fatalf("the periodic read checked the programs: %d", b.setupReads)
	}
	send(m, "r")
	if b.setupReads != 2 {
		t.Fatalf("reload did not check again: %d", b.setupReads)
	}
}

func TestSetupInstallDialogShowsThePlanAndCancelStartsNothing(t *testing.T) {
	b := demoBackend()
	b.setup[1].Install, b.setup[1].Sudo = "sudo dnf install claude", true
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "5", "down", "i")
	if m.modal == nil {
		t.Fatal("no dialog opened")
	}
	contains(t, m, "Install claude?", "$ sudo dnf install claude", "runs with sudo", "qcrew setup install claude")
	send(m, "n")
	if m.modal != nil || len(execsOf[m].calls) != 0 {
		t.Fatalf("cancel: modal %v, calls %v", m.modal, execsOf[m].calls)
	}
	// Enter and stray keys cancel as well.
	send(m, "i", "enter")
	if m.modal != nil || len(execsOf[m].calls) != 0 {
		t.Fatalf("enter: modal %v, calls %v", m.modal, execsOf[m].calls)
	}
}

func TestSetupConfirmHandsTheTerminalOverAndChecksAgain(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "5", "down", "i", "y")
	want := []string{"/fake/qcrew", "setup", "install", "claude"}
	calls := execsOf[m].calls
	if len(calls) != 1 || len(calls[0]) != 4 || calls[0][1] != want[1] || calls[0][2] != want[2] || calls[0][3] != want[3] || calls[0][0] != want[0] {
		t.Fatalf("calls = %v", calls)
	}
	if b.setupReads != 2 {
		t.Fatalf("the programs were not checked after the return: %d", b.setupReads)
	}
	contains(t, m, "claude: the install command ended with exit code 0.", "claude: found now, v1.0.",
		"Next step: none, claude is ready to use.")
	if m.setup.installing != "" || m.setup.pending != nil {
		t.Fatalf("state left over: %+v", m.setup)
	}
	// It is installed now: no button, and the key says so.
	send(m, "i")
	if m.modal != nil {
		t.Fatal("a dialog opened for an installed program")
	}
	contains(t, m, "claude is installed already.")
}

func TestSetupResultsOfAnInstallationThatDoesNotWork(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want []string
	}{
		{"declined", exitCode(2), []string{"exit code 2.", "Exit code 2 means"}},
		{"failed", exitCode(1), []string{"exit code 1.", "fix the cause and press i"}},
		{"not started", errors.New("no such file"), []string{"could not be started: no such file", "fix the cause"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := demoBackend()
			m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
			execsOf[m].err = c.err
			send(m, "5", "down", "i", "y")
			contains(t, m, c.want...)
			contains(t, m, "missing")
			lacks(t, m, "found now")
		})
	}
}

func TestSetupProgramStillMissingAfterSuccessNamesPath(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	execsOf[m].b = demoBackend() // the installation changed another machine
	send(m, "5", "down", "i", "y")
	contains(t, m, "claude: not found yet.", "add the folder the installer named to PATH")
}

func TestSetupNothingToInstallHere(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	send(m, "5", "i")
	contains(t, m, "tmux is installed already.")
	send(m, "down", "down", "i")
	if m.modal != nil || len(execsOf[m].calls) != 0 {
		t.Fatal("a program without a supported way opened a dialog")
	}
	contains(t, m, "no supported way to install codex", "https://github.com/openai/codex")
}

func TestSetupFailedCheckShowsTheError(t *testing.T) {
	b := demoBackend()
	b.setupErr = &cli.Failure{Code: "unknown-operation", Message: "unknown operation", NextStep: "Restart the service."}
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "5")
	contains(t, m, "unknown operation Restart the service.")
}

func TestSetupClicksActLikeKeys(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "5")
	// A click on the row marks it; a second click opens the question as enter does.
	x, y := cellOf(t, m, "[install]")
	clickAt(m, x, y)
	if m.modal != nil {
		t.Fatal("the first click opened the question")
	}
	clickAt(m, x, y)
	if m.modal == nil {
		t.Fatal("the second click did not open the question")
	}
	x, y = cellOf(t, m, "n/esc/enter")
	clickAt(m, x, y)
	if m.modal != nil || len(execsOf[m].calls) != 0 {
		t.Fatal("cancel by click started something")
	}
	// The key in the footer does what the key does.
	x, y = cellOf(t, m, "i install")
	clickAt(m, x, y)
	if m.modal == nil {
		t.Fatal("the footer key did not open the question")
	}
	x, y = cellOf(t, m, "y install")
	clickAt(m, x, y)
	if len(execsOf[m].calls) != 1 {
		t.Fatalf("the answer did not hand the terminal over: %v", execsOf[m].calls)
	}
}

func TestSetupSurvivesAChangeOfLanguage(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "5", "down", "i", "y")
	m.relocalize(i18n.German)
	m.fillSetup()
	contains(t, m, "Ergebnis", "installiert", "claude: the install command ended with exit code 0.")
	if id, _ := m.setup.list.Selected(); id != "claude" {
		t.Fatalf("selected %q", id)
	}
}

func TestSetupDefaultExecutableIsTheRunningProgram(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	s, err := m.newSetup(nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.executable()
	if err != nil || got == "" {
		t.Fatalf("executable %q, %v", got, err)
	}
}

func TestSetupHelpAndAreaKeys(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), narrowWidth, narrowHeight)
	send(m, "5")
	if m.focus() != kittui.ZoneRows {
		t.Fatalf("focus %s", m.focus())
	}
	send(m, "esc")
	if m.area != areaProjects {
		t.Fatalf("esc: area %s", m.area)
	}
}

func TestSetupHooksPreviewThenHandOver(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "5")
	contains(t, m, "INTEGRATION", "not needed", "missing [configure]")
	send(m, "down", "down", "h")
	if m.modal == nil || len(b.hooksAsked) != 1 || b.hooksAsked[0] {
		t.Fatalf("modal %v, asks %v", m.modal, b.hooksAsked)
	}
	contains(t, m, "Set up the Codex hooks of qcrew?", "/home/u/.codex/hooks.json", "/home/u/.codex/config.toml",
		"+{", "... 2 more changed lines", "must be approved", "qcrew setup codex-hooks")
	send(m, "n")
	if m.modal != nil || len(execsOf[m].calls) != 0 {
		t.Fatalf("cancel: modal %v, calls %v", m.modal, execsOf[m].calls)
	}
	send(m, "h", "y")
	calls := execsOf[m].calls
	if len(calls) != 1 || strings.Join(calls[0], " ") != "/fake/qcrew setup codex-hooks" {
		t.Fatalf("calls = %v", calls)
	}
	contains(t, m, "Codex hooks: the command ended with exit code 0.", "Codex hooks now: configured.",
		"configured [remove]")
	if m.setup.hooking || m.setup.pendingHooks != nil || m.setup.previewing {
		t.Fatalf("state left over: %+v", m.setup)
	}

	// Configured: the same key removes, and a failed command names the next step.
	execsOf[m].err = exitCode(2)
	send(m, "h")
	if len(b.hooksAsked) != 3 || !b.hooksAsked[2] {
		t.Fatalf("asks %v", b.hooksAsked)
	}
	contains(t, m, "Remove the Codex hooks of qcrew?", "only the entries")
	send(m, "y")
	if calls := execsOf[m].calls; strings.Join(calls[len(calls)-1], " ") != "/fake/qcrew setup codex-hooks --remove" {
		t.Fatalf("calls = %v", calls)
	}
	contains(t, m, "exit code 2.", "Codex hooks now: configured.", "press h to try again")
}

func TestSetupHooksRefusals(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "5", "h")
	if m.modal != nil || len(b.hooksAsked) != 0 {
		t.Fatal("tmux has no hooks")
	}
	contains(t, m, "Only Codex has hooks to set up here.")

	b.setup[2].Integration, b.setup[2].IntegrationProblems = control.IntegrationUnknown, []string{"CODEX_HOME is not a folder"}
	send(m, "r", "down", "down", "h")
	if m.modal != nil || len(b.hooksAsked) != 0 {
		t.Fatal("asked although the state is unknown")
	}
	contains(t, m, "unknown", "CODEX_HOME is not a folder")

	b.setup[2].Integration = control.IntegrationStale
	b.hooks = control.SetupIntegrationResult{Empty: true}
	send(m, "r", "h")
	if m.modal != nil || len(b.hooksAsked) != 1 {
		t.Fatalf("modal %v, asks %v", m.modal, b.hooksAsked)
	}
	contains(t, m, "stale [configure]", "The Codex hooks need no change.")

	b.hooksErr = errors.New("boom")
	send(m, "h")
	if m.modal != nil || m.setup.previewing {
		t.Fatalf("modal %v after a failed preview", m.modal)
	}
}

func TestSessionsShowMissingHooks(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.German, b, wideWidth, wideHeight)
	send(m, "enter")
	contains(t, m, ", ohne Hooks")
}
