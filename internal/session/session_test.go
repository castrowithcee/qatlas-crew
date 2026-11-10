package session

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPlain(t *testing.T) {
	const rule = "────────"
	cases := []struct{ name, in, want string }{
		{"text", "a\nb", "a\nb"},
		{"sgr and cursor", "\x1b[1;31mred\x1b[0m \x1b[2Kx", "red x"},
		{"osc", "\x1b]0;title\x07a\x1b]8;;http://x\x1b\\b", "ab"},
		{"two-character escape", "\x1b(Ba\x1b=b", "ab"},
		{"lone escape", "\x1b", "\x1b"},
		{"claude suggestion dropped",
			rule + "\n❯ \x1b[2mtry this\x1b[22mtyped\n" + rule,
			rule + "\n❯ typed\n" + rule},
		{"claude coloured text dropped",
			rule + "\n❯ \x1b[38;5;244mgrey\x1b[39m ok\n" + rule,
			rule + "\n❯  ok\n" + rule},
		{"claude truecolor arguments skipped",
			rule + "\n❯ \x1b[38;2;1;2;3mgrey\x1b[0mok\x1b[48;2;1;2;3m!\n" + rule,
			rule + "\n❯ ok!\n" + rule},
		{"claude colon form", rule + "\n❯ \x1b[38:5:244mgrey\x1b[mok\n" + rule, rule + "\n❯ ok\n" + rule},
		{"glyph without rules kept", "❯ \x1b[2mhint", "❯ hint"},
		{"codex suggestion dropped", "› \x1b[2mhint\x1b[0mtyped", "› typed"},
		{"codex bright colour dropped", "›\x1b[90mx\x1b[0my", "›y"},
		{"faint state spans lines", "\x1b[2ma\n› b", "a\n› "},
		{"reset by empty parameter", "\x1b[2m\x1b[;m› b", "› b"},
	}
	for _, c := range cases {
		if got := Plain(c.in); got != c.want {
			t.Errorf("%s: Plain(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestLastLines(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		max  int
		want string
	}{
		{"a\nb\nc\n\n  \n", 2, 100, "b\nc"},
		{"a  \nb", 5, 100, "a\nb"},
		{"", 5, 100, ""},
		{"aaaa\nbbbb\ncccc", 5, 10, "bbbb\ncccc"},
		{"aaaa\nbbbb", 5, 3, ""},
	}
	for _, c := range cases {
		if got := lastLines(c.in, c.n, c.max); got != c.want {
			t.Errorf("lastLines(%q, %d, %d) = %q, want %q", c.in, c.n, c.max, got, c.want)
		}
	}
}

func TestServerSelection(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	s, err := ServerFromEnv(getenv)
	if err != nil || !slices.Equal(s.args(), []string{"-L", "qcrew"}) {
		t.Fatalf("default server: %v, %v", s.args(), err)
	}
	if got := strings.Join(s.AttachCommand("abcdefghijk2", "mnopqrstuvw7"), " "); got != "tmux -L qcrew attach -t p-abcdefghijk2:s-mnopqrstuvw7" {
		t.Errorf("attach: %q", got)
	}
	env[SocketEnv] = "/run/x/../sock"
	if s, err = ServerFromEnv(getenv); err != nil || !slices.Equal(s.args(), []string{"-S", "/run/sock"}) {
		t.Fatalf("socket server: %v, %v", s.args(), err)
	}
	env[SocketEnv] = "rel/sock"
	if _, err = ServerFromEnv(getenv); err == nil {
		t.Fatal("a relative socket path was accepted")
	}
}

func TestTmuxEnv(t *testing.T) {
	got := tmuxEnv([]string{"A=1", "TMUX=/s,1,0", "TMUX_PANE=%1", "TERM="})
	if !slices.Equal(got, []string{"A=1", "TERM=", "TERM=xterm-256color"}) {
		t.Errorf("tmuxEnv: %q", got)
	}
	if got := tmuxEnv([]string{"TERM=screen"}); !slices.Equal(got, []string{"TERM=screen"}) {
		t.Errorf("tmuxEnv keeps TERM: %q", got)
	}
}

func TestParseList(t *testing.T) {
	out := "@1\t0\t\t\t\t\t\t\n" + // a window of someone else
		"@2\t1\tbbbbbbbbbbbb\tpppppppppppp\tshell\t2026-01-02T03:04:06Z\t\tsecond\ttab\n" +
		"@3\t0\taaaaaaaaaaaa\tpppppppppppp\tcodex\t2026-01-02T03:04:05Z\t1\tfirst\n" +
		"broken\n"
	got := parseList(out)
	want := []Session{
		{ID: "aaaaaaaaaaaa", Project: "pppppppppppp", Tool: "codex", Title: "first", Status: StatusRunning,
			Started: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Bell: true, window: "@3"},
		{ID: "bbbbbbbbbbbb", Project: "pppppppppppp", Tool: "shell", Title: "second\ttab", Status: StatusExited,
			Started: time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC), window: "@2"},
	}
	if !slices.EqualFunc(got, want, func(a, b Session) bool { return a == b }) {
		t.Errorf("parseList:\n got %+v\nwant %+v", got, want)
	}
}

func TestStartRefusals(t *testing.T) {
	dir := t.TempDir()
	m := New(Server{Socket: "/nonexistent/sock"})
	m.getenv = func(string) string { return "" }
	missing := func(name string) func(string) (string, error) {
		return func(file string) (string, error) {
			if file == name {
				return "", exec.ErrNotFound
			}
			return "/bin/" + file, nil
		}
	}
	cases := []struct {
		name string
		look func(string) (string, error)
		req  StartRequest
		kind error
	}{
		{"unknown tool", missing(""), StartRequest{ProjectID: "p", Dir: dir, Tool: "vim"}, ErrInvalid},
		{"control title", missing(""), StartRequest{ProjectID: "p", Dir: dir, Tool: "shell", Title: "a\nb"}, ErrInvalid},
		{"long title", missing(""), StartRequest{ProjectID: "p", Dir: dir, Tool: "shell", Title: strings.Repeat("x", MaxTitle+1)}, ErrInvalid},
		{"no tmux", missing("tmux"), StartRequest{ProjectID: "p", Dir: dir, Tool: "claude"}, ErrNoTmux},
		{"no tool", missing("codex"), StartRequest{ProjectID: "p", Dir: dir, Tool: "codex"}, ErrToolMissing},
		{"no directory", missing(""), StartRequest{ProjectID: "p", Dir: dir + "/gone", Tool: "claude"}, ErrInvalid},
	}
	for _, c := range cases {
		m.lookPath = c.look
		_, err := m.Start(context.Background(), c.req)
		var refusal *Error
		if !errors.Is(err, c.kind) || !errors.As(err, &refusal) || refusal.Message == "" {
			t.Errorf("%s: %v, want %v", c.name, err, c.kind)
		}
	}
}

func TestWithoutTmuxThereAreNoSessions(t *testing.T) {
	m := New(Server{Socket: "/nonexistent/sock"})
	m.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if list, err := m.List(context.Background()); err != nil || len(list) != 0 {
		t.Errorf("List: %v, %v", list, err)
	}
	if _, err := m.Stop(context.Background(), "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stop: %v", err)
	}
	if _, err := m.Capture(context.Background(), "x", -1); !errors.Is(err, ErrInvalid) {
		t.Errorf("Capture with negative lines: %v", err)
	}
}

func TestNewUUID(t *testing.T) {
	v4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for range 50 {
		id, err := newUUID()
		if err != nil || !v4.MatchString(id) || seen[id] {
			t.Fatalf("newUUID = %q, %v", id, err)
		}
		seen[id] = true
	}
}
