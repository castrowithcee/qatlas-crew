package codex

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
)

const (
	bell = `tui.notifications=["approval-requested","plan-mode-prompt","async-question"]`
)

func session(mod ...func(*adapter.Session)) adapter.Session {
	s := adapter.Session{ID: "abcdefghij23", Program: "/opt/qcrew/bin/qcrew"}
	for _, m := range mod {
		m(&s)
	}
	return s
}

func TestCommand(t *testing.T) {
	notify := func(path string) string { return `notify=[` + path + `,"hook","codex","--notify"]` }
	cases := []struct {
		name string
		s    adapter.Session
		want []string
	}{
		{"default", session(), []string{"codex", "--no-daemon", "-c", notify(`"/opt/qcrew/bin/qcrew"`),
			"-c", bell, "-c", `tui.notification_method="bel"`, "-c", `tui.notification_condition="always"`}},
		{"model and effort", session(func(s *adapter.Session) { s.Model, s.Effort = "gpt-x", "high" }),
			[]string{"codex", "--no-daemon", "-m", "gpt-x", "-c", `model_reasoning_effort="high"`,
				"-c", notify(`"/opt/qcrew/bin/qcrew"`), "-c", bell, "-c", `tui.notification_method="bel"`,
				"-c", `tui.notification_condition="always"`}},
		{"effort only", session(func(s *adapter.Session) { s.Effort = "low" }),
			[]string{"codex", "--no-daemon", "-c", `model_reasoning_effort="low"`,
				"-c", notify(`"/opt/qcrew/bin/qcrew"`), "-c", bell, "-c", `tui.notification_method="bel"`,
				"-c", `tui.notification_condition="always"`}},
		{"path with a space, a quote and a backslash", session(func(s *adapter.Session) { s.Program = `/my bin/q"c\rew` }),
			[]string{"codex", "--no-daemon", "-c", notify(`"/my bin/q\"c\\rew"`), "-c", bell,
				"-c", `tui.notification_method="bel"`, "-c", `tui.notification_condition="always"`}},
		{"further arguments", session(func(s *adapter.Session) { s.Args = []string{"--x", "y"} }),
			[]string{"codex", "--no-daemon", "-c", notify(`"/opt/qcrew/bin/qcrew"`), "-c", bell,
				"-c", `tui.notification_method="bel"`, "-c", `tui.notification_condition="always"`, "--x", "y"}},
		{"no program known yet", adapter.Session{}, []string{"codex", "--no-daemon", "-c", bell,
			"-c", `tui.notification_method="bel"`, "-c", `tui.notification_condition="always"`}},
	}
	for _, c := range cases {
		if got := (Adapter{}).Command(c.s); !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestEnvAndPrepare(t *testing.T) {
	a := Adapter{}
	if a.Name() != adapter.Codex {
		t.Errorf("name %q", a.Name())
	}
	if got := a.Env(session()); !slices.Equal(got, []string{"QCREW_SESSION=abcdefghij23"}) {
		t.Errorf("env %q", got)
	}
	if err := a.Prepare(session()); err != nil {
		t.Errorf("prepare: %v", err)
	}
	a.Cleanup(session())
	for name, s := range map[string]adapter.Session{
		"bad id":         session(func(s *adapter.Session) { s.ID = "x" }),
		"relative path":  session(func(s *adapter.Session) { s.Program = "qcrew" }),
		"no path":        adapter.Session{ID: "abcdefghij23"},
		"model quote":    session(func(s *adapter.Session) { s.Model = `a"b` }),
		"model option":   session(func(s *adapter.Session) { s.Model = "-x" }),
		"effort spaced":  session(func(s *adapter.Session) { s.Effort = "very high" }),
		"effort long":    session(func(s *adapter.Session) { s.Effort = strings.Repeat("a", 17) }),
		"effort control": session(func(s *adapter.Session) { s.Effort = "a\nb" }),
	} {
		if err := a.Prepare(s); err == nil {
			t.Errorf("%s: prepare did not refuse", name)
		}
	}
}

func TestTomlString(t *testing.T) {
	if got := tomlString("a\x1b\n\"\\ä"); got != `"a\u001B\u000A\"\\ä"` {
		t.Errorf("tomlString = %s", got)
	}
}

func TestReadScreen(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		ready bool
		err   string
	}{
		{"empty", nil, false, ""},
		{"loading", []string{"", "starting"}, false, ""},
		{"ready footer", []string{"› ", "", "  ? for shortcuts      100% context left", ""}, true, ""},
		{"send hint", []string{"› hello", "  ⏎ send   ⌃J newline"}, true, ""},
		{"footer far above is no footer", []string{"? for shortcuts", "a", "b", "c", "d"}, false, ""},
		{"error line", []string{"› go", "■ stream error: connection lost", "", "  ? for shortcuts"}, true, "■ stream error: connection lost"},
		{"unexpected status", []string{"■ Unexpected Status 500"}, false, "■ Unexpected Status 500"},
		{"the last error line counts", []string{"■ error one", "x", "■ error two"}, false, "■ error two"},
		{"glyph without an error word", []string{"■ stopped"}, false, ""},
		{"error word without the glyph", []string{"an error occurred"}, false, ""},
		{"old error far above", append([]string{"■ error old"}, strings.Split(strings.Repeat("x\n", 15), "\n")...), false, ""},
	}
	for _, c := range cases {
		if ready, line := ReadScreen(c.lines); ready != c.ready || line != c.err {
			t.Errorf("%s: %v, %q; want %v, %q", c.name, ready, line, c.ready, c.err)
		}
	}
}
