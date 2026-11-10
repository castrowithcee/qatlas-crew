package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/uiprefs"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
	"github.com/castrowithcee/qatlas-crew/internal/tui"
)

// atTerminal makes the command line believe it runs at a terminal and replaces the interface with a recorder.
func atTerminal(t *testing.T, at bool) *[]tui.Options {
	t.Helper()
	oldInteractive, oldRun := interactive, runTUI
	t.Cleanup(func() { interactive, runTUI = oldInteractive, oldRun })
	var runs []tui.Options
	interactive = func(io.Writer) bool { return at }
	runTUI = func(o tui.Options) error {
		runs = append(runs, o)
		return nil
	}
	return &runs
}

func TestNoArgumentsAtATerminalOpensTheInterface(t *testing.T) {
	testenv.Setup(t)
	runs := atTerminal(t, true)
	code, out, _ := run(t)
	if code != 0 || len(*runs) != 1 || strings.Contains(out, "self-update") {
		t.Fatalf("code %d, runs %d, out %q", code, len(*runs), out)
	}
}

func TestNoArgumentsWithoutATerminalShowsTheOverview(t *testing.T) {
	runs := atTerminal(t, false)
	code, out, _ := run(t)
	if code != 0 || len(*runs) != 0 || !strings.Contains(out, "self-update") {
		t.Fatalf("code %d, runs %d, out %q", code, len(*runs), out)
	}
}

func TestTUICommandNeedsATerminal(t *testing.T) {
	runs := atTerminal(t, false)
	code, _, errOut := run(t, "tui")
	if code != 2 || len(*runs) != 0 || !strings.Contains(errOut, "needs a terminal") {
		t.Fatalf("code %d, runs %d, err %q", code, len(*runs), errOut)
	}
	atTerminal(t, true)
	if code, _, _ := run(t, "tui"); code != 0 {
		t.Fatalf("tui at a terminal = %d", code)
	}
}

func TestInterfaceFailureIsAFailure(t *testing.T) {
	atTerminal(t, true)
	runTUI = func(tui.Options) error { return errors.New("no tty") }
	code, _, errOut := run(t)
	if code == 0 || !strings.Contains(errOut, "the terminal interface failed: no tty") {
		t.Fatalf("code %d, err %q", code, errOut)
	}
}

func TestPreferencesLiveInTheDataFolder(t *testing.T) {
	env := testenv.Setup(t)
	runs := atTerminal(t, true)
	if code, _, _ := run(t); code != 0 {
		t.Fatalf("code %d", code)
	}
	opts := (*runs)[0]
	if len(opts.PrefsWarnings) != 0 || opts.SavePrefs == nil || !opts.Prefs.MouseEnabled {
		t.Fatalf("options %+v", opts)
	}
	prefs := opts.Prefs
	prefs.Language = uiprefs.LanguageGerman
	if err := opts.SavePrefs(prefs); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(env.Home, ".qatlas", "qcrew", "tui.yaml")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("the preferences are not in %s: %v", want, err)
	}
}

// The session view attaches with the server the environment names and forces UTF-8 for the embedded client.
func TestInterfaceGetsTheEmbeddedAttachCommand(t *testing.T) {
	env := testenv.Setup(t)
	runs := atTerminal(t, true)
	if code, _, _ := run(t); code != 0 {
		t.Fatalf("code %d", code)
	}
	opts := (*runs)[0]
	if opts.Attach == nil {
		t.Fatal("no attach command is given")
	}
	got := strings.Join(opts.Attach("proj", "sess"), " ")
	want := "tmux -S " + filepath.Join(env.Root, "no-tmux", "socket") + " -u attach -t p-proj:s-sess"
	if got != want {
		t.Errorf("attach = %q, want %q", got, want)
	}
	t.Setenv("QCREW_TMUX_SOCKET", "relative/sock")
	*runs = nil
	if code, _, _ := run(t); code != 0 {
		t.Fatalf("code %d", code)
	}
	if o := (*runs)[0]; o.Attach != nil || len(o.PrefsWarnings) == 0 {
		t.Errorf("a bad socket path leaves the session view on: %+v", o)
	}
}

func TestPreferencesFollowQcrewDir(t *testing.T) {
	testenv.Setup(t)
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	runs := atTerminal(t, true)
	if code, _, _ := run(t); code != 0 {
		t.Fatalf("code %d", code)
	}
	if err := (*runs)[0].SavePrefs((*runs)[0].Prefs); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tui.yaml")); err != nil {
		t.Fatalf("the preferences are not in QCREW_DIR: %v", err)
	}
	// A saved language is read back at the next start.
	saved := (*runs)[0].Prefs
	saved.Language = uiprefs.LanguageGerman
	if err := (*runs)[0].SavePrefs(saved); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t); code != 0 || (*runs)[1].Prefs.Language != uiprefs.LanguageGerman {
		t.Fatalf("second start: %+v", (*runs)[1].Prefs)
	}
}

func TestInterfaceConnectsThroughTheServiceAccess(t *testing.T) {
	testenv.Setup(t)
	runs := atTerminal(t, true)
	f := &fakeService{}
	access := f.access(true)
	root := newTUICommand(access)
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	if err := root.RunE(root, nil); err != nil {
		t.Fatal(err)
	}
	_, err := (*runs)[0].Connect(context.Background())
	var failure *cli.Failure
	if !errors.As(err, &failure) || failure.Code != output.CodeUnreachable || f.start != 0 {
		t.Fatalf("err %v, starts %d", err, f.start)
	}
	access = f.access(false)
	root = newTUICommand(access)
	if err := root.RunE(root, nil); err != nil {
		t.Fatal(err)
	}
	conn, err := (*runs)[1].Connect(context.Background())
	if err != nil || conn == nil || f.start != 1 {
		t.Fatalf("conn %v, err %v, starts %d", conn, err, f.start)
	}
}
