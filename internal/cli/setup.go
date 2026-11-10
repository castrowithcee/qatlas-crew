package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-crew/internal/setup"
)

// setupHost is what the setup commands look at and act with; tests replace it.
var setupHost = struct {
	env    setup.Env
	runner setup.Runner
	// confirm asks one question at the terminal and returns nil only for a yes.
	confirm func(question string) error
}{
	runner: setup.TerminalRunner{},
	confirm: func(question string) error {
		return cli.ConfirmRequest(cli.Confirmation{Question: question})
	},
}

var setupColumns = []string{"name", "status", "version", "install"}

// installWay is what the install column shows: the command for a missing program, or where to read about it.
func installWay(st setup.Status) string {
	switch {
	case st.Installed:
		return ""
	case len(st.Plan.Commands) > 0:
		return st.Plan.Display()
	}
	return "see " + st.Source
}

func writeSetup(w io.Writer, format output.Format, statuses []setup.Status) error {
	rows := make([]output.Row, 0, len(statuses))
	for _, st := range statuses {
		status := "missing"
		if st.Installed {
			status = "installed"
		}
		rows = append(rows, output.Row{"name": st.Name, "status": status, "version": st.Version, "install": installWay(st)})
	}
	return output.Encode(w, format, output.Collection{Columns: setupColumns, Rows: rows})
}

// setupInstaller is the installer at the terminal of this run.
func setupInstaller(c *cobra.Command) setup.Installer {
	return setup.Installer{
		Runner:  setupHost.runner,
		Confirm: setupHost.confirm,
		Terminal: func() bool {
			_, _, ok := terminalStreams(c.OutOrStdout())
			return ok
		},
	}
}

func noTerminalFailure() error {
	return usageFailure("installing needs a terminal on standard input and standard output; nothing was run",
		"Run 'qcrew setup' in a terminal, or look with 'qcrew setup --check'.")
}

// installFailure turns what stopped an installation into the failure the command ends with.
func installFailure(title string, ran int, err error) error {
	var stopped *setup.StoppedError
	var run *setup.RunError
	switch {
	case errors.Is(err, setup.ErrNoTerminal):
		return noTerminalFailure()
	case errors.As(err, &stopped):
		var failure *cli.Failure
		if errors.As(stopped.Cause, &failure) && failure.Code == output.CodeConfirmationDeclined {
			return &cli.Failure{Code: output.CodeConfirmationDeclined, Usage: true,
				Message:  fmt.Sprintf("installing %s stopped: the command was not confirmed (%d ran before)", title, ran),
				NextStep: "Run 'qcrew setup' again to continue; nothing further runs without your confirmation."}
		}
		if failure != nil {
			return failure
		}
		return &cli.Failure{Code: output.CodeRuntime, Message: "cannot ask for confirmation: " + stopped.Cause.Error(),
			NextStep: "Run 'qcrew setup' in a terminal.", Cause: err}
	case errors.As(err, &run):
		msg := fmt.Sprintf("installing %s failed: the command did not start: %v", title, run.Cause)
		if run.ExitCode >= 0 {
			msg = fmt.Sprintf("installing %s failed: the command ended with exit code %d", title, run.ExitCode)
		}
		return &cli.Failure{Code: output.CodeRuntime, Message: msg,
			NextStep: "Read the output above, fix the cause, and run 'qcrew setup' again.", Cause: err}
	}
	return &cli.Failure{Code: output.CodeRuntime, Message: "installing " + title + " failed: " + err.Error(),
		NextStep: "Run 'qcrew setup' again.", Cause: err}
}

// install carries out the plan of one missing dependency and checks that it is found afterwards.
func install(ctx context.Context, c *cobra.Command, st setup.Status) error {
	if len(st.Plan.Commands) == 0 {
		return &cli.Failure{Code: output.CodeRuntime,
			Message:  fmt.Sprintf("qcrew knows no supported way to install %s on this system", st.Title),
			NextStep: "Install it by hand as described at " + st.Source + ", then run 'qcrew setup --check'."}
	}
	ran, err := setupInstaller(c).Install(ctx, st.Title, st.Plan)
	if err != nil {
		return installFailure(st.Title, ran, err)
	}
	after := setup.Detect(ctx, setupHost.env)
	for _, a := range after {
		if a.Name == st.Name && !a.Installed {
			return &cli.Failure{Code: output.CodeRuntime,
				Message:  fmt.Sprintf("the install command of %s finished, but %s is not found in PATH", st.Title, st.Name),
				NextStep: "Open a new shell or add the folder the installer names to PATH, then run 'qcrew setup --check'."}
		}
	}
	return nil
}

func newSetupCommand(program *cobracli.Program) *cobra.Command {
	var check bool
	names := make([]string, 0, 3)
	for _, d := range setup.Dependencies() {
		names = append(names, d.Name)
	}
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Check the programs qcrew needs and install missing ones",
		Long: "setup shows, for tmux, Claude Code and Codex, whether the program is installed, its version and, for a\n" +
			"missing one, the command that installs it (--output table, json or compact).\n\n" +
			"At a terminal, setup then offers to install what is missing, using the maker's own way only: the package\n" +
			"manager for tmux (apt, dnf, pacman or brew, the first found in PATH) and the installer script of the maker\n" +
			"for Claude Code and Codex. Each command is shown in full and needs your yes before it runs; a command with\n" +
			"sudo asks a second time. A refusal ends the installation. Without a terminal setup installs nothing and\n" +
			"exits with code 2.\n\n" +
			"--check, and --output json, only look and never install; the exit code is 0 whether or not something is\n" +
			"missing, so read the status column or the JSON. 'qcrew setup install <name>' installs one program with the\n" +
			"same questions. Without a supported package manager tmux is only detected and the output names where to\n" +
			"read how to install it.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			statuses := setup.Detect(c.Context(), setupHost.env)
			if err := writeSetup(c.OutOrStdout(), program.Format(), statuses); err != nil {
				return err
			}
			if check || program.Format() == output.FormatJSON {
				return nil
			}
			var missing []setup.Status
			for _, st := range statuses {
				if !st.Installed {
					missing = append(missing, st)
				}
			}
			if len(missing) == 0 {
				return nil
			}
			if _, _, ok := terminalStreams(c.OutOrStdout()); !ok {
				return noTerminalFailure()
			}
			for _, st := range missing {
				if len(st.Plan.Commands) == 0 {
					fmt.Fprintf(c.ErrOrStderr(), "%s: no supported way to install it here; see %s\n", st.Title, st.Source)
					continue
				}
				if err := install(c.Context(), c, st); err != nil {
					return err
				}
			}
			fmt.Fprintln(c.OutOrStdout())
			return writeSetup(c.OutOrStdout(), program.Format(), setup.Detect(c.Context(), setupHost.env))
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "only look and list; install nothing")

	one := &cobra.Command{
		Use:       "install <" + strings.Join(names, "|") + ">",
		Short:     "Install one missing program with the same confirmations",
		Long:      "install installs the one named program if it is missing, by the same way and with the same questions as\n'qcrew setup': every command in full, a second question before sudo, a refusal ends it. It needs a terminal.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: names,
		RunE: func(c *cobra.Command, args []string) error {
			if _, ok := setup.Find(args[0]); !ok {
				return usageFailure(fmt.Sprintf("%q is not a program setup knows", args[0]),
					"Name one of: "+strings.Join(names, ", ")+".")
			}
			if _, _, ok := terminalStreams(c.OutOrStdout()); !ok {
				return noTerminalFailure()
			}
			for _, st := range setup.Detect(c.Context(), setupHost.env) {
				if st.Name != args[0] {
					continue
				}
				if st.Installed {
					return writeSetup(c.OutOrStdout(), program.Format(), []setup.Status{st})
				}
				if err := install(c.Context(), c, st); err != nil {
					return err
				}
				for _, a := range setup.Detect(c.Context(), setupHost.env) {
					if a.Name == st.Name {
						return writeSetup(c.OutOrStdout(), program.Format(), []setup.Status{a})
					}
				}
			}
			return nil
		},
	}
	cmd.AddCommand(one)
	return cmd
}
