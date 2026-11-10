package setup

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// fakeEnv finds exactly the programs in found and answers a version command with versions[name].
func fakeEnv(goos string, versions map[string]string, found ...string) Env {
	return Env{
		GOOS: goos,
		LookPath: func(name string) (string, error) {
			if slices.Contains(found, name) {
				return "/fake/bin/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		Version: func(_ context.Context, path string, _ []string) (string, error) {
			v, ok := versions[strings.TrimPrefix(path, "/fake/bin/")]
			if !ok {
				return "", errors.New("no version")
			}
			return v, nil
		},
	}
}

func byName(t *testing.T, sts []Status, name string) Status {
	t.Helper()
	for _, st := range sts {
		if st.Name == name {
			return st
		}
	}
	t.Fatalf("no status for %s", name)
	return Status{}
}

func TestDetectPresentAndMissing(t *testing.T) {
	env := fakeEnv("linux", map[string]string{Tmux: "tmux 3.4\n", Claude: "2.1.0 (Claude Code)\nmore"}, Tmux, Claude, "apt")
	sts := Detect(context.Background(), env)
	if len(sts) != 3 {
		t.Fatalf("got %d statuses", len(sts))
	}
	if st := byName(t, sts, Tmux); !st.Installed || st.Version != "tmux 3.4" || st.Path != "/fake/bin/"+Tmux || len(st.Plan.Commands) != 0 {
		t.Errorf("tmux: %+v", st)
	}
	if st := byName(t, sts, Claude); !st.Installed || st.Version != "2.1.0 (Claude Code)" {
		t.Errorf("claude: %+v", st)
	}
	st := byName(t, sts, Codex)
	if st.Installed || st.Version != "" || len(st.Plan.Commands) != 1 || st.Plan.Commands[0].Display != installScripts[Codex] {
		t.Errorf("codex: %+v", st)
	}
}

func TestDetectUnknownVersionStillInstalled(t *testing.T) {
	sts := Detect(context.Background(), fakeEnv("linux", nil, Codex))
	if st := byName(t, sts, Codex); !st.Installed || st.Version != "" {
		t.Errorf("codex: %+v", st)
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"tmux 3.4\n", "tmux 3.4"},
		{"first\nsecond", "first"},
		{"\x1b[31mred\x1b[0m\x07", "?[31mred?[0m?"},
		{"a\rb", "a?b"},
		{"", ""},
		{strings.Repeat("x", 200), strings.Repeat("x", maxVersionLen) + "..."},
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPlanPerManager(t *testing.T) {
	d, _ := Find(Tmux)
	cases := []struct {
		found []string
		want  string
		sudo  bool
	}{
		{[]string{"apt"}, "sudo apt install " + Tmux, true},
		{[]string{"dnf"}, "sudo dnf install " + Tmux, true},
		{[]string{"pacman"}, "sudo pacman -S " + Tmux, true},
		{[]string{"brew"}, "brew install " + Tmux, false},
		{[]string{"brew", "pacman", "dnf", "apt"}, "sudo apt install " + Tmux, true},
		{[]string{"brew", "pacman"}, "sudo pacman -S " + Tmux, true},
	}
	for _, c := range cases {
		p := PlanFor(d, fakeEnv("linux", nil, c.found...))
		if len(p.Commands) != 1 || p.Commands[0].Display != c.want || p.Commands[0].Sudo != c.sudo || p.Sudo() != c.sudo {
			t.Errorf("%v: %+v, want %q", c.found, p, c.want)
		}
		if strings.Join(p.Commands[0].Argv, " ") != c.want {
			t.Errorf("%v: argv %v differs from the display", c.found, p.Commands[0].Argv)
		}
	}
}

func TestPlanWithoutManager(t *testing.T) {
	d, _ := Find(Tmux)
	if p := PlanFor(d, fakeEnv("linux", nil, "zypper")); len(p.Commands) != 0 || p.Display() != "" {
		t.Errorf("plan without a manager: %+v", p)
	}
	sts := Detect(context.Background(), fakeEnv("linux", nil))
	if st := byName(t, sts, Tmux); st.Installed || len(st.Plan.Commands) != 0 || st.Source == "" {
		t.Errorf("tmux: %+v", st)
	}
}

func TestPlanOfTheAgentTools(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		for _, name := range []string{Claude, Codex} {
			d, _ := Find(name)
			p := PlanFor(d, fakeEnv(goos, nil))
			if len(p.Commands) != 1 || p.Commands[0].Sudo || p.Commands[0].Display != installScripts[name] ||
				!slices.Equal(p.Commands[0].Argv, []string{"sh", "-c", installScripts[name]}) {
				t.Errorf("%s on %s: %+v", name, goos, p)
			}
		}
	}
	d, _ := Find(Claude)
	if p := PlanFor(d, fakeEnv("windows", nil)); len(p.Commands) != 0 {
		t.Errorf("claude on windows: %+v", p)
	}
}

func TestInstallWays(t *testing.T) {
	if got := installScripts[Claude]; got != "curl -fsSL https://claude.ai/install.sh | bash" {
		t.Errorf("claude: %q", got)
	}
	if got := installScripts[Codex]; got != "curl -fsSL https://chatgpt.com/codex/install.sh | sh" {
		t.Errorf("codex: %q", got)
	}
}

type fakeRunner struct {
	ran  []string
	fail error
}

func (r *fakeRunner) Run(_ context.Context, c Command) error {
	r.ran = append(r.ran, c.Display)
	return r.fail
}

// answers confirms the questions in order with the given replies and records them.
type answers struct {
	replies   []error
	questions []string
}

func (a *answers) confirm(q string) error {
	a.questions = append(a.questions, q)
	if len(a.replies) == 0 {
		return nil
	}
	r := a.replies[0]
	a.replies = a.replies[1:]
	return r
}

var errNo = errors.New("declined")

func installer(r *fakeRunner, a *answers, terminal bool) Installer {
	return Installer{Runner: r, Confirm: a.confirm, Terminal: func() bool { return terminal }}
}

func sudoPlan() Plan {
	return Plan{Commands: []Command{
		{Argv: []string{"sudo", "apt", "install", Tmux}, Display: "sudo apt install " + Tmux, Sudo: true},
		{Argv: []string{"second"}, Display: "second"},
	}}
}

func TestInstallAsksAndRuns(t *testing.T) {
	r, a := &fakeRunner{}, &answers{}
	n, err := installer(r, a, true).Install(context.Background(), Tmux, sudoPlan())
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if !slices.Equal(r.ran, []string{"sudo apt install " + Tmux, "second"}) {
		t.Errorf("ran %v", r.ran)
	}
	// command, sudo, command: the sudo command asks twice.
	if len(a.questions) != 3 || !strings.Contains(a.questions[0], "$ sudo apt install "+Tmux) ||
		!strings.Contains(a.questions[1], "sudo") || !strings.Contains(a.questions[1], "$ sudo apt install "+Tmux) ||
		!strings.Contains(a.questions[2], "$ second") {
		t.Errorf("questions %q", a.questions)
	}
}

func TestInstallSudoDeclinedRunsNothing(t *testing.T) {
	r, a := &fakeRunner{}, &answers{replies: []error{nil, errNo}}
	n, err := installer(r, a, true).Install(context.Background(), Tmux, sudoPlan())
	var stopped *StoppedError
	if !errors.As(err, &stopped) || !errors.Is(err, errNo) || n != 0 || len(r.ran) != 0 {
		t.Fatalf("n=%d err=%v ran=%v", n, err, r.ran)
	}
	if len(a.questions) != 2 {
		t.Errorf("asked %d questions", len(a.questions))
	}
}

func TestInstallCommandDeclinedSkipsSudoQuestion(t *testing.T) {
	r, a := &fakeRunner{}, &answers{replies: []error{errNo}}
	if _, err := installer(r, a, true).Install(context.Background(), Tmux, sudoPlan()); !errors.Is(err, errNo) {
		t.Fatalf("err=%v", err)
	}
	if len(r.ran) != 0 || len(a.questions) != 1 {
		t.Errorf("ran=%v questions=%d", r.ran, len(a.questions))
	}
}

func TestInstallLaterDeclineStopsFollowing(t *testing.T) {
	r, a := &fakeRunner{}, &answers{replies: []error{nil, nil, errNo}}
	n, err := installer(r, a, true).Install(context.Background(), Tmux, sudoPlan())
	if !errors.Is(err, errNo) || n != 1 || len(r.ran) != 1 {
		t.Fatalf("n=%d err=%v ran=%v", n, err, r.ran)
	}
}

func TestInstallWithoutTerminalRunsNothing(t *testing.T) {
	r, a := &fakeRunner{}, &answers{}
	n, err := installer(r, a, false).Install(context.Background(), Tmux, sudoPlan())
	if !errors.Is(err, ErrNoTerminal) || n != 0 || len(r.ran) != 0 || len(a.questions) != 0 {
		t.Fatalf("n=%d err=%v ran=%v questions=%d", n, err, r.ran, len(a.questions))
	}
}

func TestInstallFailureStopsFollowing(t *testing.T) {
	boom := errors.New("boom")
	r, a := &fakeRunner{fail: boom}, &answers{}
	n, err := installer(r, a, true).Install(context.Background(), Tmux, sudoPlan())
	var run *RunError
	if !errors.As(err, &run) || !errors.Is(err, boom) || run.ExitCode != -1 || n != 0 || len(r.ran) != 1 {
		t.Fatalf("n=%d err=%v ran=%v", n, err, r.ran)
	}
}
