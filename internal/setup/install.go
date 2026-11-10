package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// ErrNoTerminal means an installation was asked for without a terminal to confirm it at.
var ErrNoTerminal = errors.New("installing needs a terminal")

// StoppedError means the user did not confirm a command, or no confirmation could be asked. This command and
// the ones after it did not run.
type StoppedError struct {
	Command Command
	// Cause is the error of the confirmation, whose type tells a refusal from a missing terminal.
	Cause error
}

func (e *StoppedError) Error() string { return "not confirmed: " + e.Command.Display }
func (e *StoppedError) Unwrap() error { return e.Cause }

// RunError means a command ended with an error; the ones after it did not run.
type RunError struct {
	Command  Command
	ExitCode int // -1 if the command did not start
	Cause    error
}

func (e *RunError) Error() string {
	return fmt.Sprintf("command failed: %s: %v", e.Command.Display, e.Cause)
}
func (e *RunError) Unwrap() error { return e.Cause }

// Runner runs a command of an installation.
type Runner interface {
	Run(ctx context.Context, c Command) error
}

// TerminalRunner runs commands with the standard streams of the process, so that the installer, sudo and its
// password prompt use the user's terminal.
type TerminalRunner struct{}

// Run starts the command and waits for it.
func (TerminalRunner) Run(ctx context.Context, c Command) error {
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Installer carries out plans. It has no usable zero value.
type Installer struct {
	Runner Runner
	// Confirm asks the question and returns nil only for a yes.
	Confirm func(question string) error
	// Terminal reports whether a terminal is there to ask at.
	Terminal func() bool
}

// Install runs the commands of the plan one after the other. Before each it shows the command in full and
// asks; before a sudo command it asks again on its own. A refusal or a failure ends the installation. It
// returns how many commands ran to the end. Without a terminal nothing runs.
func (i Installer) Install(ctx context.Context, title string, plan Plan) (int, error) {
	if !i.Terminal() {
		return 0, ErrNoTerminal
	}
	for n, c := range plan.Commands {
		q := fmt.Sprintf("Install %s? This command will run:\n\n  $ %s\n\nRun it?", title, c.Display)
		if err := i.Confirm(q); err != nil {
			return n, &StoppedError{Command: c, Cause: err}
		}
		if c.Sudo {
			q = fmt.Sprintf("This command runs with sudo, with full rights on this system:\n\n  $ %s\n\nAllow sudo for it?", c.Display)
			if err := i.Confirm(q); err != nil {
				return n, &StoppedError{Command: c, Cause: err}
			}
		}
		if err := i.Runner.Run(ctx, c); err != nil {
			code := -1
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			}
			return n, &RunError{Command: c, ExitCode: code, Cause: err}
		}
	}
	return len(plan.Commands), nil
}
