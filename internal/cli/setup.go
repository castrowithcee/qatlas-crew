package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-crew/internal/adapter/codex/integration"
	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/setup"
)

// setupHost is what the setup commands look at and act with; tests replace it.
var setupHost = struct {
	env    setup.Env
	runner setup.Runner
	// confirm asks one question at the terminal and returns nil only for a yes.
	confirm func(question string) error
	// codex names where the Codex hooks live and the program they call.
	codex func() (integration.Config, error)
}{
	runner: setup.TerminalRunner{},
	codex:  codexConfig,
	confirm: func(question string) error {
		return cli.ConfirmRequest(cli.Confirmation{Question: question})
	},
}

var setupColumns = []string{"name", "status", "version", "install", "integration"}

// codexConfig is where this run sets up the Codex hooks: the Codex home as Codex resolves it, the qcrew data
// folder for the backups, and this program, as the adapter names it in the notify flag of its sessions.
func codexConfig() (integration.Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return integration.Config{}, err
	}
	codexHome, err := integration.CodexHome(os.Getenv, home)
	if err != nil {
		return integration.Config{}, err
	}
	data, err := dataDir()
	if err != nil {
		return integration.Config{}, err
	}
	program, err := os.Executable()
	if err != nil {
		return integration.Config{}, err
	}
	return integration.Config{CodexHome: codexHome, DataDir: data, Program: program}, nil
}

// codexState is the state of the Codex hooks, one of the control.Integration values, with what is stale or
// why the state is unknown.
type codexState struct {
	status   string
	problems []string
}

func inspectCodex() codexState {
	cfg, err := setupHost.codex()
	var r integration.Report
	if err == nil {
		r, err = cfg.Inspect()
	}
	if err != nil {
		return codexState{status: control.IntegrationUnknown, problems: []string{err.Error()}}
	}
	return codexState{status: string(r.Status), problems: r.Problems}
}

// integrationOf is what the integration column shows for a program: nil, shown empty and as null, for one
// that has no integration.
func integrationOf(name string, codex codexState) any {
	switch name {
	case setup.Claude:
		return control.IntegrationNotNeeded
	case setup.Codex:
		return codex.status
	}
	return nil
}

// writeProblems names on stderr what is stale about the Codex hooks, or why their state is unknown.
func writeProblems(w io.Writer, codex codexState) {
	for _, p := range codex.problems {
		fmt.Fprintln(w, "codex hooks: "+p)
	}
}

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

func writeSetup(w io.Writer, format output.Format, statuses []setup.Status, codex codexState) error {
	rows := make([]output.Row, 0, len(statuses))
	for _, st := range statuses {
		status := "missing"
		if st.Installed {
			status = "installed"
		}
		rows = append(rows, output.Row{"name": st.Name, "status": status, "version": st.Version, "install": installWay(st),
			"integration": integrationOf(st.Name, codex)})
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
	return usageFailure("installing or setting up needs a terminal on standard input and standard output; nothing was run or written",
		"Run 'qcrew setup' in a terminal, or look with 'qcrew setup --check'.")
}

// changeCodexHooks shows the change an action makes to the Codex configuration, asks, and writes it. A refusal
// writes nothing.
func changeCodexHooks(c *cobra.Command, a integration.Action) error {
	cfg, err := setupHost.codex()
	var ch *integration.Change
	if err == nil {
		ch, err = cfg.Plan(a)
	}
	if err != nil {
		return &cli.Failure{Code: output.CodeRuntime, Message: "the Codex hooks cannot be changed: " + err.Error(),
			NextStep: "Fix the cause, then run 'qcrew setup codex-hooks' again; nothing was written.", Cause: err}
	}
	out := c.OutOrStdout()
	if ch.Empty() {
		if a == integration.Remove {
			fmt.Fprintln(out, "There are no Codex hooks of qcrew to remove; nothing was written.")
		} else {
			fmt.Fprintln(out, "The Codex hooks of qcrew are set up; nothing was written.")
		}
		return nil
	}
	fmt.Fprint(out, ch.Preview())
	for _, w := range ch.Warnings {
		fmt.Fprintln(out, "warning: "+w)
	}
	fmt.Fprintln(out)
	q := "Write these changes to the Codex configuration? Both files are backed up first; qcrew approves only its own hooks."
	if a == integration.Remove {
		q = "Remove the Codex hooks of qcrew and their approvals as shown? Both files are backed up first."
	}
	if err := setupHost.confirm(q); err != nil {
		var failure *cli.Failure
		if errors.As(err, &failure) && failure.Code == output.CodeConfirmationDeclined {
			return &cli.Failure{Code: output.CodeConfirmationDeclined, Usage: true,
				Message:  "the Codex hooks were not changed: the change was not confirmed; nothing was written",
				NextStep: "Run 'qcrew setup codex-hooks' again when you want the change."}
		}
		if failure != nil {
			return failure
		}
		return &cli.Failure{Code: output.CodeRuntime, Message: "cannot ask for confirmation: " + err.Error(),
			NextStep: "Run 'qcrew setup codex-hooks' in a terminal.", Cause: err}
	}
	if err := ch.Apply(time.Now()); err != nil {
		return &cli.Failure{Code: output.CodeRuntime, Message: "writing the Codex hooks failed: " + err.Error(),
			NextStep: "Run 'qcrew setup codex-hooks' again; the backups are in backups/codex in the qcrew data folder.",
			Cause:    err}
	}
	st := inspectCodex()
	fmt.Fprintln(out, "Codex hooks: "+st.status)
	writeProblems(c.ErrOrStderr(), st)
	return nil
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
		Short: "Check the programs qcrew needs, install missing ones and set up the Codex hooks",
		Long: "setup shows, for tmux, Claude Code and Codex, whether the program is installed, its version, for a\n" +
			"missing one the command that installs it (--output table, json or compact), and in the integration\n" +
			"column whether the hooks through which Codex reports to qcrew are configured, missing or stale; Claude\n" +
			"Code needs none, its sessions bring their own. What is stale is named on standard error.\n\n" +
			"At a terminal, setup then offers to install what is missing, using the maker's own way only: the package\n" +
			"manager for tmux (apt, dnf, pacman or brew, the first found in PATH) and the installer script of the maker\n" +
			"for Claude Code and Codex. Each command is shown in full and needs your yes before it runs; a command with\n" +
			"sudo asks a second time. A refusal ends the installation. When Codex is there and its hooks are missing\n" +
			"or stale, setup then shows the change to the Codex configuration and asks before it writes, as\n" +
			"'qcrew setup codex-hooks' does. Without a terminal setup installs and writes nothing and exits with\n" +
			"code 2.\n\n" +
			"--check, and --output json, only look and never install or write; the exit code is 0 whether or not\n" +
			"something is missing, so read the status and integration columns or the JSON. 'qcrew setup install\n" +
			"<name>' installs one program with the same questions; 'qcrew setup codex-hooks --remove' takes the hooks\n" +
			"out. Without a supported package manager tmux is only detected and the output names where to read how to\n" +
			"install it.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			statuses := setup.Detect(c.Context(), setupHost.env)
			codex := inspectCodex()
			if err := writeSetup(c.OutOrStdout(), program.Format(), statuses, codex); err != nil {
				return err
			}
			if program.Format() != output.FormatJSON {
				writeProblems(c.ErrOrStderr(), codex)
			}
			if check || program.Format() == output.FormatJSON {
				return nil
			}
			var missing []setup.Status
			codexInstalled := false
			for _, st := range statuses {
				if !st.Installed {
					missing = append(missing, st)
				}
				codexInstalled = codexInstalled || st.Name == setup.Codex && st.Installed
			}
			hooks := codex.status == control.IntegrationMissing || codex.status == control.IntegrationStale
			if len(missing) == 0 && !(codexInstalled && hooks) {
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
			after := setup.Detect(c.Context(), setupHost.env)
			for _, st := range after {
				// Hooks are offered for a Codex that is there, also one installed just now.
				if st.Name == setup.Codex && st.Installed && hooks {
					fmt.Fprintln(c.OutOrStdout())
					if err := changeCodexHooks(c, integration.Setup); err != nil {
						return err
					}
				}
			}
			fmt.Fprintln(c.OutOrStdout())
			return writeSetup(c.OutOrStdout(), program.Format(), after, inspectCodex())
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
					return writeSetup(c.OutOrStdout(), program.Format(), []setup.Status{st}, inspectCodex())
				}
				if err := install(c.Context(), c, st); err != nil {
					return err
				}
				for _, a := range setup.Detect(c.Context(), setupHost.env) {
					if a.Name == st.Name {
						return writeSetup(c.OutOrStdout(), program.Format(), []setup.Status{a}, inspectCodex())
					}
				}
			}
			return nil
		},
	}
	var remove bool
	hooks := &cobra.Command{
		Use:   "codex-hooks",
		Short: "Set up, renew or remove the hooks of qcrew in the Codex configuration",
		Long: "codex-hooks sets up the hooks through which Codex reports the status of its sessions to qcrew: an entry\n" +
			"for each event qcrew reads in hooks.json in the Codex home (CODEX_HOME, else ~/.codex), and the approval\n" +
			"of each of these entries in config.toml there, so that Codex runs them. A stale setup, after a move of\n" +
			"qcrew or a change by hand, is renewed; --remove takes the entries and their approvals out.\n\n" +
			"It first shows the change to both files as a diff and asks; nothing is written without your yes. Both\n" +
			"files are then backed up to backups/codex in the qcrew data folder, of which the newest ten are kept,\n" +
			"and replaced in one step each. Only the entries of qcrew change: every other hook, table and comment\n" +
			"stays byte for byte, and qcrew never approves a hook of anyone else. It needs a terminal; without one\n" +
			"it writes nothing and exits with code 2. 'qcrew setup --check' shows the state.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if _, _, ok := terminalStreams(c.OutOrStdout()); !ok {
				return usageFailure("changing the Codex hooks needs a terminal on standard input and standard output; nothing was written",
					"Run 'qcrew setup codex-hooks' in a terminal, or look with 'qcrew setup --check'.")
			}
			action := integration.Setup
			if remove {
				action = integration.Remove
			}
			return changeCodexHooks(c, action)
		},
	}
	hooks.Flags().BoolVar(&remove, "remove", false, "take the hooks of qcrew and their approvals out")
	cmd.AddCommand(one, hooks)
	return cmd
}
