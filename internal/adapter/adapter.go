package adapter

import (
	"errors"
	"path/filepath"
)

// The tools a session can run.
const (
	Claude = "claude"
	Codex  = "codex"
	Shell  = "shell"
)

// Tools lists the tools in the order help texts name them.
var Tools = []string{Claude, Codex, Shell}

// ErrUnknownTool means the tool is none of Tools.
var ErrUnknownTool = errors.New("unknown tool")

// HasHooks reports whether the tool tells qcrew its status through hooks. The status of the other tools is
// what tmux shows.
func HasHooks(tool string) bool { return tool == Claude }

// Session is what an adapter needs to know about one session.
type Session struct {
	// ID is the qcrew session id.
	ID string
	// Dir is the project directory the tool runs in.
	Dir string
	// DataDir is the data directory of qcrew; an adapter keeps its files for the session below it.
	DataDir string
	// Program is the absolute path of the qcrew program, which hooks call.
	Program string
	// AgentSession is the id the tool knows the session by, for tools that take one.
	AgentSession string
	// Args are further arguments for the tool's command line.
	Args []string
}

// Adapter is the knowledge about one agent command line tool.
type Adapter interface {
	// Name is the tool, one of Tools.
	Name() string
	// Command returns the command line that starts the tool for s.
	Command(s Session) []string
	// Prepare creates what the command line needs, before the tool starts.
	Prepare(s Session) error
	// Cleanup removes what Prepare created. It never fails: a rest is harmless.
	Cleanup(s Session)
}

// fallbackShell is the login shell when SHELL names no absolute path.
const fallbackShell = "/bin/sh"

// plain is a tool that qcrew starts as it is and that needs no preparation.
type plain struct {
	name    string
	command func() []string
}

func (p plain) Name() string { return p.name }
func (p plain) Command(s Session) []string {
	return append(p.command(), s.Args...)
}
func (plain) Prepare(Session) error { return nil }
func (plain) Cleanup(Session)       {}

// Plain returns the adapter of a tool without hooks: codex or shell. getenv reads the environment the
// session inherits; the shell tool starts the login shell of SHELL. Claude has its own adapter in package
// claude.
func Plain(tool string, getenv func(string) string) (Adapter, error) {
	switch tool {
	case Codex:
		return plain{name: tool, command: func() []string { return []string{tool} }}, nil
	case Shell:
		return plain{name: tool, command: func() []string {
			shell := getenv("SHELL")
			if !filepath.IsAbs(shell) {
				shell = fallbackShell
			}
			return []string{shell, "-l"}
		}}, nil
	}
	return nil, ErrUnknownTool
}
