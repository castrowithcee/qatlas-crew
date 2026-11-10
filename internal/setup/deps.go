package setup

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/castrowithcee/qatlas-crew/internal/session"
)

// The dependencies, by the name of their program.
const (
	Tmux   = session.Program
	Claude = "claude"
	Codex  = "codex"
)

// Dependency is a program qcrew needs.
type Dependency struct {
	// Name is the program's name, which the command line and the channel use to address it.
	Name  string
	Title string
	// VersionArgs make the program print its version.
	VersionArgs []string
	// Source is the page of the maker that documents the installation.
	Source string
}

// Dependencies returns the programs qcrew needs, in the order they are shown.
func Dependencies() []Dependency {
	return []Dependency{
		{Name: Tmux, Title: Tmux, VersionArgs: []string{"-V"}, Source: "https://github.com/tmux/tmux/wiki/Installing"},
		{Name: Claude, Title: "Claude Code", VersionArgs: []string{"--version"}, Source: "https://code.claude.com/docs/en/setup"},
		{Name: Codex, Title: "Codex", VersionArgs: []string{"--version"}, Source: "https://github.com/openai/codex"},
	}
}

// Find returns the dependency with the given name.
func Find(name string) (Dependency, bool) {
	for _, d := range Dependencies() {
		if d.Name == name {
			return d, true
		}
	}
	return Dependency{}, false
}

// Env is what detection and planning look at. Tests replace its parts; a zero part is the real one.
type Env struct {
	// LookPath finds a program in PATH.
	LookPath func(name string) (string, error)
	// Version runs the program at path with args and returns its output.
	Version func(ctx context.Context, path string, args []string) (string, error)
	// GOOS is the operating system, as runtime.GOOS names it.
	GOOS string
}

func (e Env) filled() Env {
	if e.LookPath == nil {
		e.LookPath = exec.LookPath
	}
	if e.Version == nil {
		e.Version = runVersion
	}
	if e.GOOS == "" {
		e.GOOS = runtime.GOOS
	}
	return e
}

// Status is the state of one dependency.
type Status struct {
	Dependency
	Installed bool
	// Path is where the program was found.
	Path string
	// Version is the first line of the program's version output, made safe to show; empty if unknown.
	Version string
	// Plan is how a missing dependency can be installed here.
	Plan Plan
}

const (
	versionTimeout = 5 * time.Second
	maxOutput      = 4096
	maxVersionLen  = 80
)

// Detect looks for every dependency. It runs the version command of the ones it finds and installs nothing.
func Detect(ctx context.Context, env Env) []Status {
	env = env.filled()
	deps := Dependencies()
	out := make([]Status, 0, len(deps))
	for _, d := range deps {
		st := Status{Dependency: d}
		if path, err := env.LookPath(d.Name); err == nil {
			st.Installed, st.Path = true, path
			vctx, cancel := context.WithTimeout(ctx, versionTimeout)
			raw, err := env.Version(vctx, path, d.VersionArgs)
			cancel()
			if err == nil {
				st.Version = Sanitize(raw)
			}
		} else {
			st.Plan = PlanFor(d, env)
		}
		out = append(out, st)
	}
	return out
}

// Sanitize makes the output of a program fit to show: the first line only, control characters replaced, and
// shortened. The output belongs to a program qcrew does not control, so it is data.
func Sanitize(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, s)
	if r := []rune(s); len(r) > maxVersionLen {
		s = string(r[:maxVersionLen]) + "..."
	}
	return strings.TrimSpace(s)
}

// limitBuffer keeps the first maxOutput bytes and drops the rest.
type limitBuffer struct{ bytes.Buffer }

func (b *limitBuffer) Write(p []byte) (int, error) {
	if room := maxOutput - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func runVersion(ctx context.Context, path string, args []string) (string, error) {
	var out limitBuffer
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}
