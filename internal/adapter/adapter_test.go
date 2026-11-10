package adapter

import (
	"errors"
	"slices"
	"testing"
)

func TestPlainCommand(t *testing.T) {
	env := map[string]string{"SHELL": "/usr/bin/zsh"}
	getenv := func(k string) string { return env[k] }
	cases := []struct {
		tool string
		want []string
	}{
		{Codex, []string{"codex"}},
		{Shell, []string{"/usr/bin/zsh", "-l"}},
	}
	for _, c := range cases {
		a, err := Plain(c.tool, getenv)
		if err != nil || a.Name() != c.tool || !slices.Equal(a.Command(Session{}), c.want) {
			t.Errorf("%s: %v, %v; want %q", c.tool, a, err, c.want)
		}
		if err := a.Prepare(Session{}); err != nil {
			t.Errorf("%s: prepare: %v", c.tool, err)
		}
		a.Cleanup(Session{})
	}
	for _, shell := range []string{"", "zsh"} {
		env["SHELL"] = shell
		a, _ := Plain(Shell, getenv)
		if got := a.Command(Session{}); !slices.Equal(got, []string{"/bin/sh", "-l"}) {
			t.Errorf("SHELL=%q: %q", shell, got)
		}
	}
	a, _ := Plain(Codex, getenv)
	if got := a.Command(Session{Args: []string{"--x"}}); !slices.Equal(got, []string{"codex", "--x"}) {
		t.Errorf("with arguments: %q", got)
	}
	for _, tool := range []string{"vim", Claude} {
		if _, err := Plain(tool, getenv); !errors.Is(err, ErrUnknownTool) {
			t.Errorf("%s: %v", tool, err)
		}
	}
}

func TestHasHooks(t *testing.T) {
	for tool, want := range map[string]bool{Claude: true, Codex: false, Shell: false} {
		if HasHooks(tool) != want {
			t.Errorf("HasHooks(%s) != %v", tool, want)
		}
	}
}
