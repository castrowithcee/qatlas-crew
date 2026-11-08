package adapter

import (
	"errors"
	"slices"
	"testing"
)

func TestCommand(t *testing.T) {
	env := map[string]string{"SHELL": "/usr/bin/zsh"}
	getenv := func(k string) string { return env[k] }
	cases := []struct {
		tool string
		want []string
	}{
		{Claude, []string{"claude"}},
		{Codex, []string{"codex"}},
		{Shell, []string{"/usr/bin/zsh", "-l"}},
	}
	for _, c := range cases {
		got, err := Command(c.tool, getenv)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: %q, %v; want %q", c.tool, got, err, c.want)
		}
	}
	for _, shell := range []string{"", "zsh"} {
		env["SHELL"] = shell
		if got, _ := Command(Shell, getenv); !slices.Equal(got, []string{"/bin/sh", "-l"}) {
			t.Errorf("SHELL=%q: %q", shell, got)
		}
	}
	if _, err := Command("vim", getenv); !errors.Is(err, ErrUnknownTool) {
		t.Errorf("unknown tool: %v", err)
	}
}
