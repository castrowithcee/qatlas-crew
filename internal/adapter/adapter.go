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

// fallbackShell is the login shell when SHELL names no absolute path.
const fallbackShell = "/bin/sh"

// Command returns the command line that starts tool. getenv reads the environment the session inherits; the
// shell tool starts the login shell of SHELL.
func Command(tool string, getenv func(string) string) ([]string, error) {
	switch tool {
	case Claude, Codex:
		return []string{tool}, nil
	case Shell:
		shell := getenv("SHELL")
		if !filepath.IsAbs(shell) {
			shell = fallbackShell
		}
		return []string{shell, "-l"}, nil
	}
	return nil, ErrUnknownTool
}
