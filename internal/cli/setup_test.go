package cli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-crew/internal/setup"
)

var sampleSetup = []setup.Status{
	{Dependency: setup.Dependency{Name: setup.Tmux, Title: setup.Tmux}, Installed: true, Version: "tmux 3.4"},
	{Dependency: setup.Dependency{Name: setup.Claude, Title: "Claude Code", Source: "https://code.claude.com/docs/en/setup"},
		Plan: setup.Plan{Commands: []setup.Command{{Display: "curl -fsSL https://claude.ai/install.sh | bash"}}}},
	{Dependency: setup.Dependency{Name: setup.Codex, Title: "Codex", Source: "https://github.com/openai/codex"}},
}

func TestSetupOutputGolden(t *testing.T) {
	for _, format := range []output.Format{output.FormatTable, output.FormatJSON} {
		var out bytes.Buffer
		if err := writeSetup(&out, format, sampleSetup); err != nil {
			t.Fatal(err)
		}
		golden(t, "setup-check."+string(format), out.Bytes())
	}
}

// fakeHost is a machine on which the programs in found exist and the given package manager does; running a
// command "installs" what the command names, and nothing real runs.
type fakeHost struct {
	found     []string
	ran       []string
	questions []string
	// replies are the answers to the questions in order; a missing one is yes.
	replies []error
	// failRun makes a command fail.
	failRun error
	// noPath makes a command succeed without making its program appear.
	noPath bool
}

func (h *fakeHost) install(t *testing.T, goos string) {
	t.Helper()
	old := setupHost
	t.Cleanup(func() { setupHost = old })
	setupHost.env = setup.Env{
		GOOS: goos,
		LookPath: func(name string) (string, error) {
			if slices.Contains(h.found, name) {
				return "/fake/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		Version: func(context.Context, string, []string) (string, error) { return "v1\n", nil },
	}
	setupHost.runner = runnerFunc(func(c setup.Command) error {
		h.ran = append(h.ran, c.Display)
		if h.failRun != nil {
			return h.failRun
		}
		for _, name := range []string{setup.Tmux, setup.Claude, setup.Codex} {
			if !h.noPath && strings.Contains(c.Display, name) {
				h.found = append(h.found, name)
			}
		}
		return nil
	})
	setupHost.confirm = func(q string) error {
		h.questions = append(h.questions, q)
		if len(h.replies) == 0 {
			return nil
		}
		r := h.replies[0]
		h.replies = h.replies[1:]
		return r
	}
}

// declinedForTest is the refusal the kit's confirmation returns for a no.
func declinedForTest() error {
	return &cli.Failure{Code: output.CodeConfirmationDeclined, Usage: true, Message: "confirmation was declined"}
}

type runnerFunc func(setup.Command) error

func (f runnerFunc) Run(_ context.Context, c setup.Command) error { return f(c) }

func TestSetupCheckListsAndExitsZero(t *testing.T) {
	atTerminal(t, true)
	h := &fakeHost{found: []string{setup.Tmux, "apt"}}
	h.install(t, "linux")
	code, out, errOut := run(t, "setup", "--check")
	if code != 0 || len(h.ran) != 0 || len(h.questions) != 0 {
		t.Fatalf("code %d, ran %v, err %q", code, h.ran, errOut)
	}
	for _, want := range []string{setup.Tmux, "installed", "claude", "missing", "curl -fsSL https://claude.ai/install.sh | bash"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestSetupJSONOnlyLists(t *testing.T) {
	atTerminal(t, true)
	h := &fakeHost{}
	h.install(t, "linux")
	code, out, _ := run(t, "setup", "--output", "json")
	if code != 0 || len(h.ran) != 0 || len(h.questions) != 0 || !strings.Contains(out, `"status":"missing"`) {
		t.Fatalf("code %d, ran %v, out %q", code, h.ran, out)
	}
}

func TestSetupWithoutTerminalRunsNothing(t *testing.T) {
	atTerminal(t, false)
	h := &fakeHost{found: []string{"apt"}}
	h.install(t, "linux")
	code, _, errOut := run(t, "setup")
	if code != 2 || len(h.ran) != 0 || len(h.questions) != 0 || !strings.Contains(errOut, "needs a terminal") || !strings.Contains(errOut, "qcrew setup --check") {
		t.Fatalf("code %d, ran %v, err %q", code, h.ran, errOut)
	}
	code, _, errOut = run(t, "setup", "install", setup.Codex)
	if code != 2 || len(h.ran) != 0 || len(h.questions) != 0 {
		t.Fatalf("install: code %d, ran %v, err %q", code, h.ran, errOut)
	}
}

func TestSetupNothingMissingNeedsNoTerminal(t *testing.T) {
	atTerminal(t, false)
	h := &fakeHost{found: []string{setup.Tmux, setup.Claude, setup.Codex}}
	h.install(t, "linux")
	if code, _, errOut := run(t, "setup"); code != 0 {
		t.Fatalf("code %d, err %q", code, errOut)
	}
}

func TestSetupInstallsAfterConfirmation(t *testing.T) {
	atTerminal(t, true)
	h := &fakeHost{found: []string{setup.Claude, setup.Codex, "dnf"}}
	h.install(t, "linux")
	code, out, errOut := run(t, "setup")
	if code != 0 || !slices.Equal(h.ran, []string{"sudo dnf install " + setup.Tmux}) {
		t.Fatalf("code %d, ran %v, err %q", code, h.ran, errOut)
	}
	// The command question and the sudo question.
	if len(h.questions) != 2 || !strings.Contains(h.questions[1], "sudo") {
		t.Errorf("questions %q", h.questions)
	}
	if strings.Count(out, "installed") < 3 {
		t.Errorf("the final state does not show everything installed:\n%s", out)
	}
}

func TestSetupSudoDeclinedStopsTheInstallation(t *testing.T) {
	atTerminal(t, true)
	h := &fakeHost{found: []string{"apt"}}
	h.install(t, "linux")
	setupHost.confirm = func(q string) error {
		h.questions = append(h.questions, q)
		if strings.Contains(q, "sudo") && strings.Contains(q, "Allow") {
			return declinedForTest()
		}
		return nil
	}
	code, _, errOut := run(t, "setup")
	if code != 2 || len(h.ran) != 0 || !strings.Contains(errOut, "confirmation-declined") {
		t.Fatalf("code %d, ran %v, err %q", code, h.ran, errOut)
	}
}

func TestSetupInstallOne(t *testing.T) {
	atTerminal(t, true)
	h := &fakeHost{found: []string{setup.Tmux}}
	h.install(t, "linux")
	code, out, errOut := run(t, "setup", "install", setup.Codex)
	if code != 0 || len(h.ran) != 1 || !strings.Contains(h.ran[0], "chatgpt.com/codex/install.sh") || !strings.Contains(out, "installed") {
		t.Fatalf("code %d, ran %v, out %q, err %q", code, h.ran, out, errOut)
	}
	// Already there: nothing runs.
	h.ran, h.questions = nil, nil
	if code, _, _ := run(t, "setup", "install", setup.Codex); code != 0 || len(h.ran) != 0 || len(h.questions) != 0 {
		t.Fatalf("again: code %d, ran %v", code, h.ran)
	}
}

func TestSetupInstallUnknownName(t *testing.T) {
	atTerminal(t, true)
	h := &fakeHost{}
	h.install(t, "linux")
	code, _, errOut := run(t, "setup", "install", "npm")
	if code != 2 || len(h.ran) != 0 || !strings.Contains(errOut, "Name one of") {
		t.Fatalf("code %d, err %q", code, errOut)
	}
}

func TestSetupNoPackageManager(t *testing.T) {
	atTerminal(t, true)
	h := &fakeHost{found: []string{setup.Claude, setup.Codex}}
	h.install(t, "linux")
	code, _, errOut := run(t, "setup")
	if code != 0 || len(h.ran) != 0 || !strings.Contains(errOut, "https://github.com/tmux/tmux/wiki/Installing") {
		t.Fatalf("code %d, ran %v, err %q", code, h.ran, errOut)
	}
	code, _, errOut = run(t, "setup", "install", setup.Tmux)
	if code != 1 || len(h.ran) != 0 || !strings.Contains(errOut, "Installing") {
		t.Fatalf("install: code %d, err %q", code, errOut)
	}
}

func TestSetupCommandFailureAndMissingPath(t *testing.T) {
	atTerminal(t, true)
	h := &fakeHost{found: []string{setup.Tmux, setup.Codex}, failRun: errors.New("exit status 7")}
	h.install(t, "linux")
	if code, _, errOut := run(t, "setup", "install", setup.Claude); code != 1 || !strings.Contains(errOut, "failed") {
		t.Fatalf("failing command: code %d, err %q", code, errOut)
	}
	h.failRun, h.noPath = nil, true
	if code, _, errOut := run(t, "setup", "install", setup.Claude); code != 1 || !strings.Contains(errOut, "not found in PATH") {
		t.Fatalf("not in PATH: code %d, err %q", code, errOut)
	}
}
