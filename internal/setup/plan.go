package setup

import (
	"strings"
)

// Command is one command of an installation.
type Command struct {
	// Argv is what runs.
	Argv []string
	// Display is the command as the user sees and approves it, in full.
	Display string
	// Sudo is set for a command that runs with elevated rights.
	Sudo bool
}

// Plan is the list of commands that install a dependency, in the order they run. It is empty if there is no
// supported way on this system.
type Plan struct {
	Commands []Command
}

// Display joins the commands for a one-line view.
func (p Plan) Display() string {
	lines := make([]string, len(p.Commands))
	for i, c := range p.Commands {
		lines[i] = c.Display
	}
	return strings.Join(lines, "; ")
}

// Sudo reports whether any command of the plan runs with sudo.
func (p Plan) Sudo() bool {
	for _, c := range p.Commands {
		if c.Sudo {
			return true
		}
	}
	return false
}

// managers are the ways to install tmux, in the order the first one that is found is taken.
var managers = []struct {
	program string
	argv    []string
	sudo    bool
}{
	// Source: https://github.com/tmux/tmux/wiki/Installing
	{"apt", []string{"sudo", "apt", "install", Tmux}, true},
	// Source: https://github.com/tmux/tmux/wiki/Installing
	{"dnf", []string{"sudo", "dnf", "install", Tmux}, true},
	// Source: https://github.com/tmux/tmux/wiki/Installing
	{"pacman", []string{"sudo", "pacman", "-S", Tmux}, true},
	// Source: https://github.com/tmux/tmux/wiki/Installing
	{"brew", []string{"brew", "install", Tmux}, false},
}

// installScripts are the installers of the agent tools. They run in sh, and what the user approves is the
// script text itself.
var installScripts = map[string]string{
	// Source: https://code.claude.com/docs/en/setup
	Claude: "curl -fsSL https://claude.ai/install.sh | bash",
	// Source: https://github.com/openai/codex
	Codex: "curl -fsSL https://chatgpt.com/codex/install.sh | sh",
}

// PlanFor returns how to install d on this system. For tmux it takes the first package manager that is in
// PATH; with none the plan is empty and the user is left to the source. The agent tools have installers for
// Linux, macOS and WSL only.
func PlanFor(d Dependency, env Env) Plan {
	env = env.filled()
	if d.Name == Tmux {
		for _, m := range managers {
			if _, err := env.LookPath(m.program); err == nil {
				return Plan{Commands: []Command{{Argv: m.argv, Display: strings.Join(m.argv, " "), Sudo: m.sudo}}}
			}
		}
		return Plan{}
	}
	script, ok := installScripts[d.Name]
	if !ok || (env.GOOS != "linux" && env.GOOS != "darwin") {
		return Plan{}
	}
	return Plan{Commands: []Command{{Argv: []string{"sh", "-c", script}, Display: script}}}
}
