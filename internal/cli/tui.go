package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/uiprefs"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/tui"
)

// runTUI shows the interface; tests replace it.
var runTUI = tui.Run

// terminalStreams returns standard input and the output stream when both are terminals. A buffer or a pipe is
// no terminal.
func terminalStreams(stdout io.Writer) (in, out *os.File, ok bool) {
	if !interactive(stdout) {
		return nil, nil, false
	}
	out, _ = stdout.(*os.File)
	return os.Stdin, out, true
}

// dataDir is the folder of the qcrew data: QCREW_DIR, else ~/.qatlas/qcrew, the folder that holds config.yaml
// unless that file is named on its own.
func dataDir() (string, error) {
	if dir := os.Getenv(configDirEnv); dir != "" {
		return absolute(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return control.DataDir(home), nil
}

// prefsPath is the device-local preferences file of the interface, tui.yaml in the qcrew data folder.
func prefsPath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return uiprefs.Path(dir), nil
}

// savePrefs writes the preferences file at path; the Settings area saves through it.
func savePrefs(path string, schema uiprefs.Schema) func(uiprefs.Prefs) error {
	return func(p uiprefs.Prefs) error { return uiprefs.Save(path, p, schema) }
}

func newTUICommand(access *serviceAccess) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal interface",
		Long: "tui opens the terminal interface, the same as qcrew without a command at a terminal. On the left it lists\n" +
			"the projects with the number of open inbox entries; on the right it lists the sessions of the project\n" +
			"under the cursor with tool, title, status and the time since the status last changed. Areas are switched\n" +
			"with 1 to 4, or with tab and the left and right keys: Projects, Inbox (it follows in a later release),\n" +
			"Sessions and Settings. f shows the pane of an area alone and back, r reads the service again, ? opens the\n" +
			"help and q quits. The lists refresh every two seconds.\n\n" +
			"n adds a project: type the path of a folder (~ is your home, tab completes, the list below shows its\n" +
			"folders and .. goes up), then choose that folder or a new folder in it and the name. d removes the chosen\n" +
			"project after asking; its folder stays. c, x and s start a Claude Code, Codex or shell session in the\n" +
			"chosen project and mark it; t stops the session under the cursor after asking.\n\n" +
			"The mouse works too: click a tab, a button, a key in the header or footer, or a row, and turn the wheel.\n" +
			"A click does what the key does, so a stop still asks first. With the mouse on, hold Shift to select text.\n\n" +
			"It needs a terminal on both standard input and output, and starts the service when it is not running,\n" +
			"unless --no-autostart is given. Without tmux the projects still show, and the interface names the next\n" +
			"step, 'qcrew setup'. Preferences (theme, language, start area, icons and mouse) are kept in tui.yaml in\n" +
			"~/.qatlas/qcrew, or in QCREW_DIR; change them in the Settings area instead of editing the file by hand.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if _, _, ok := terminalStreams(c.OutOrStdout()); !ok {
				return usageFailure("the terminal interface needs a terminal on standard input and standard output",
					"Run 'qcrew' in a terminal, or use commands such as 'qcrew project list' in scripts.")
			}
			return openTUI(c, access)
		},
	}
}

// openTUI shows the interface at the terminal. The service is reached, and started where allowed, from inside
// the interface, so a service that cannot start shows its next step there instead of ending the program.
func openTUI(c *cobra.Command, access *serviceAccess) error {
	in, out, _ := terminalStreams(c.OutOrStdout())
	schema, err := tui.PrefsSchema()
	if err != nil {
		return &cli.Failure{Code: output.CodeRuntime, Message: "the terminal interface cannot start: " + err.Error(),
			NextStep: "Report this as a bug of qcrew.", Cause: err}
	}
	opts := tui.Options{In: in, Out: out, Prefs: uiprefs.Default(schema)}
	opts.Connect = func(ctx context.Context) (tui.Connection, error) {
		cl, err := access.connect(ctx)
		if err != nil {
			return nil, err
		}
		return cl, nil
	}
	if path, err := prefsPath(); err != nil {
		opts.PrefsWarnings = []string{fmt.Sprintf("no data folder, using the defaults: %v", err)}
	} else {
		opts.Prefs, opts.PrefsWarnings = uiprefs.Load(path, schema)
		opts.SavePrefs = savePrefs(path, schema)
	}
	if err := runTUI(opts); err != nil {
		return &cli.Failure{Code: output.CodeRuntime, Message: "the terminal interface failed: " + err.Error(),
			NextStep: "Run 'qcrew' again; if it keeps failing, use commands such as 'qcrew project list'.", Cause: err}
	}
	return nil
}
