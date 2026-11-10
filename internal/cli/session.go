package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/session"
)

// sessionFailure names the session commands in the next step of a refusal.
func sessionFailure(err error) error {
	var failure *cli.Failure
	if errors.As(err, &failure) && failure.Usage {
		switch failure.Code {
		case output.CodeNotFound:
			failure.NextStep = "Run 'qcrew session list' to see the sessions, or 'qcrew project list' to see the projects."
		case output.CodeInvalidRequest:
			failure.NextStep = "Give a project with --project and one of the tools " + strings.Join(adapter.Tools, ", ") + " with --tool."
		}
	}
	return serviceFailure(err)
}

var sessionColumns = []string{"id", "project", "tool", "title", "status", "since", "context_percent", "started_at"}

// contextValue is the fill of the context window, or nil, shown empty and as null, while it is unknown.
func contextValue(x control.Session) any {
	if x.ContextPercent == nil {
		return nil
	}
	return int64(*x.ContextPercent)
}

// signalsValue is where the status comes from, or nil, shown empty and as null, for a tool with one source.
func signalsValue(x control.Session) any {
	if x.Signals == "" {
		return nil
	}
	return x.Signals
}

// integrationValue is the state of the hooks a session depends on while they are not in place, or nil.
func integrationValue(x control.Session) any {
	if x.Integration == "" {
		return nil
	}
	return x.Integration
}

func sessionObject(x control.Session) output.Object {
	o := output.Object{Fields: []output.Field{
		{Name: "id", Value: x.ID}, {Name: "project", Value: x.Project}, {Name: "tool", Value: x.Tool},
		{Name: "title", Value: x.Title}, {Name: "status", Value: x.Status},
		{Name: "since", Value: x.Since.Format(time.RFC3339)}, {Name: "context_percent", Value: contextValue(x)},
		{Name: "started_at", Value: x.StartedAt.Format(time.RFC3339)},
	}}
	if x.Signals != "" {
		o.Fields = append(o.Fields, output.Field{Name: "signals", Value: x.Signals})
	}
	if x.Integration != "" {
		o.Fields = append(o.Fields, output.Field{Name: "integration", Value: x.Integration})
	}
	return o
}

func writeSessions(w io.Writer, format output.Format, sessions []control.Session) error {
	// The columns signals and integration exist only while a session has the field, which keeps the output for
	// the other tools as it was.
	columns := sessionColumns[:len(sessionColumns):len(sessionColumns)]
	signals, integ := false, false
	for _, x := range sessions {
		signals, integ = signals || x.Signals != "", integ || x.Integration != ""
	}
	if signals {
		columns = append(columns, "signals")
	}
	if integ {
		columns = append(columns, "integration")
	}
	rows := make([]output.Row, 0, len(sessions))
	for _, x := range sessions {
		row := output.Row{"id": x.ID, "project": x.Project, "tool": x.Tool, "title": x.Title,
			"status": x.Status, "since": x.Since.Format(time.RFC3339), "context_percent": contextValue(x),
			"started_at": x.StartedAt.Format(time.RFC3339)}
		if signals {
			row["signals"] = signalsValue(x)
		}
		if integ {
			row["integration"] = integrationValue(x)
		}
		rows = append(rows, row)
	}
	return output.Encode(w, format, output.Collection{Columns: columns, Rows: rows})
}

// safeWord is a word a POSIX shell reads as it is.
var safeWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellLine joins a command line so that a POSIX shell reads it back word for word.
func shellLine(args []string) string {
	words := make([]string, len(args))
	for i, a := range args {
		if safeWord.MatchString(a) {
			words[i] = a
		} else {
			words[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(words, " ")
}

// interactive reports whether the command runs at a terminal it can hand to another program.
var interactive = func(stdout io.Writer) bool {
	out, ok := stdout.(*os.File)
	return ok && term.IsTerminal(int(out.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}

// runAttached runs a command on this process's terminal and waits for it. The caller's TMUX is dropped, so
// that tmux attaches to the qcrew server from inside the user's own tmux as well.
func runAttached(args []string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = withoutVar(os.Environ(), "TMUX")
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return &cli.Failure{Code: output.CodeRuntime, Message: fmt.Sprintf("the attach command ended with exit code %d", exit.ExitCode()),
				NextStep: "Run 'qcrew session list' to check the session.", Cause: err}
		}
		return &cli.Failure{Code: output.CodeRuntime, Message: "cannot run the attach command: " + err.Error(),
			NextStep: "Run the printed command yourself.", Cause: err}
	}
	return nil
}

func withoutVar(env []string, name string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, name+"=") {
			out = append(out, kv)
		}
	}
	return out
}

func newSessionCommand(program *cobracli.Program, access *serviceAccess) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Start, list, stop and attach to the agent sessions of the projects",
		Long: "An agent session runs a tool in a project's directory on the tmux server of qcrew, apart from your own\n" +
			"tmux server. Sessions keep running without qcrew, its service or the terminal; see\n" +
			"'qcrew help session-guide'.",
	}

	var projectRef, tool, title string
	start := &cobra.Command{
		Use:   "start --project P --tool T",
		Short: "Start a session of a tool in a project",
		Long: "start opens a new session that runs the tool (" + strings.Join(adapter.Tools, ", ") + ") in the directory of\n" +
			"the project with the given id or name. The tool shell is your login shell. The title defaults to the tool.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cl, err := access.projectClient(c.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			x, err := cl.StartSession(c.Context(), control.SessionStartArgs{Project: projectRef, Tool: tool, Title: title})
			if err != nil {
				return sessionFailure(err)
			}
			return output.Encode(c.OutOrStdout(), program.Format(), sessionObject(x))
		},
	}
	start.Flags().StringVar(&projectRef, "project", "", "id or name of the project")
	start.Flags().StringVar(&tool, "tool", "", "tool to run: "+strings.Join(adapter.Tools, ", "))
	start.Flags().StringVar(&title, "title", "", "title of the session instead of the tool")
	_ = start.MarkFlagRequired("project")
	_ = start.MarkFlagRequired("tool")

	var listProject string
	list := &cobra.Command{
		Use:   "list",
		Short: "List the sessions with id, project, tool, title, status, since, context and start time",
		Long: "list shows the sessions of every project, or of the project given with --project, in the order\n" +
			"they started. The status is running, waiting, idle, error or exited, and since is when the session took\n" +
			"it. Claude Code and Codex report all five; shell shows running while it runs and exited after it\n" +
			"ended. context_percent is how full the tool's context window is, empty while unknown. A column\n" +
			"signals appears if a Codex session is listed: hooks if its hooks report, fallback until then. A column\n" +
			"integration appears while the Codex hooks of qcrew are missing, stale or unknown; such a session reports\n" +
			"through the fallback. 'qcrew setup codex-hooks' sets them up.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cl, err := access.projectClient(c.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			sessions, err := cl.Sessions(c.Context(), listProject)
			if err != nil {
				return sessionFailure(err)
			}
			return writeSessions(c.OutOrStdout(), program.Format(), sessions)
		},
	}
	list.Flags().StringVar(&listProject, "project", "", "id or name of the project whose sessions to list")

	stop := &cobra.Command{
		Use:   "stop <id>",
		Short: "End a session and close its window",
		Long: "stop ends the tool of the session, if it still runs, and closes its window. It needs --confirm.\n" +
			"The project's tmux session closes with its last window.",
		Args: cobra.ExactArgs(1),
	}
	gate := program.AddGate(stop, cobracli.Gates{Change: true})
	stop.RunE = func(c *cobra.Command, args []string) error {
		return gate.Run(cobracli.Request{Changes: true}, func() error {
			cl, err := access.projectClient(c.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			x, err := cl.StopSession(c.Context(), args[0])
			if err != nil {
				return sessionFailure(err)
			}
			return output.Encode(c.OutOrStdout(), program.Format(), sessionObject(x))
		})
	}

	attach := &cobra.Command{
		Use:   "attach <id>",
		Short: "Print the tmux command that shows a session, and run it at a terminal",
		Long: "attach prints the command that attaches a terminal to the session's window on the tmux server of\n" +
			"qcrew. At a terminal it then runs the command; detach with the tmux prefix key and d. Without a\n" +
			"terminal it only prints the command.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			server, err := session.ServerFromEnv(os.Getenv)
			if err != nil {
				return &cli.Failure{Code: output.CodeUsage, Message: err.Error(),
					NextStep: "Unset " + session.SocketEnv + " or give it an absolute path.", Usage: true}
			}
			cl, err := access.projectClient(c.Context())
			if err != nil {
				return err
			}
			sessions, err := cl.Sessions(c.Context(), "")
			_ = cl.Close()
			if err != nil {
				return sessionFailure(err)
			}
			var found *control.Session
			for i := range sessions {
				if sessions[i].ID == args[0] {
					found = &sessions[i]
				}
			}
			if found == nil {
				return &cli.Failure{Code: output.CodeNotFound, Message: fmt.Sprintf("no session has the id %q", args[0]),
					NextStep: "Run 'qcrew session list' to see the sessions.", Usage: true}
			}
			command := server.AttachCommand(found.Project, found.ID)
			if err := output.Encode(c.OutOrStdout(), program.Format(),
				output.Object{Fields: []output.Field{{Name: "command", Value: shellLine(command)}}}); err != nil {
				return err
			}
			if !interactive(c.OutOrStdout()) {
				return nil
			}
			return runAttached(command)
		},
	}

	cmd.AddCommand(start, list, stop, attach)
	return cmd
}
