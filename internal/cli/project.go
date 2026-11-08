package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/configfile"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/projectpath"

	"github.com/castrowithcee/qatlas-crew/internal/client"
	"github.com/castrowithcee/qatlas-crew/internal/control"
)

// The environment variables that choose the configuration file when --config is not given.
const (
	configFileEnv = "QCREW_CONFIG"
	configDirEnv  = "QCREW_DIR"
)

// codeOtherConfig is the error code of a command whose configuration file is not the running service's.
const codeOtherConfig output.Code = "config-mismatch"

// configPath resolves the configuration file from --config, QCREW_CONFIG, QCREW_DIR and the qcrew data
// directory, in that order, as an absolute path. The service reads and writes the file; the command line
// only resolves the path, hands it to the service it starts and checks it against the one that runs.
func (a *serviceAccess) configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", &cli.Failure{Code: output.CodeRuntime, Message: "cannot find the home directory: " + err.Error(),
			NextStep: "Set HOME, or give the configuration file with --config.", Cause: err}
	}
	path, err := configfile.ResolvePath(a.configFlag, configFileEnv, configDirEnv, control.DataDir(home), control.ConfigFile)
	if err == nil {
		path, err = absolute(path)
	}
	if err != nil {
		return "", &cli.Failure{Code: output.CodeUsage, Message: "cannot resolve the configuration file: " + err.Error(),
			NextStep: "Give an absolute path with --config or QCREW_CONFIG.", Usage: true, Cause: err}
	}
	return path, nil
}

// absolute expands a leading "~" and makes path absolute against this process's working directory, which
// the service does not share.
func absolute(path string) (string, error) {
	path, err := projectpath.Expand(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

// projectClient connects to the service and checks that it works on this command's configuration file.
func (a *serviceAccess) projectClient(ctx context.Context) (*client.Client, error) {
	want, err := a.configPath()
	if err != nil {
		return nil, err
	}
	c, err := a.connect(ctx)
	if err != nil {
		return nil, err
	}
	st, err := c.Status(ctx)
	if err != nil {
		_ = c.Close()
		return nil, serviceFailure(err)
	}
	if st.Config != want {
		_ = c.Close()
		return nil, &cli.Failure{Code: codeOtherConfig,
			Message:  "the running qcrew service uses the configuration file " + st.Config + ", not " + want,
			NextStep: "Run 'qcrew service stop' and repeat the command, or repeat it without --config, QCREW_CONFIG and QCREW_DIR."}
	}
	return c, nil
}

// projectFailure names the project commands in the next step of a refusal.
func projectFailure(err error) error {
	var failure *cli.Failure
	if errors.As(err, &failure) && failure.Usage {
		switch failure.Code {
		case client.CodeAlreadyExists:
			failure.NextStep = "Run 'qcrew project list' to see the projects; give another name with --name."
		case output.CodeNotFound:
			failure.NextStep = "Run 'qcrew project list' to see the ids and names of the projects."
		case output.CodeInvalidRequest:
			failure.NextStep = "Give an existing directory, or add --create to make it; give a name with --name."
		}
	}
	return serviceFailure(err)
}

var projectColumns = []string{"id", "name", "path"}

func projectObject(p control.Project) output.Object {
	return output.Object{Fields: []output.Field{{Name: "id", Value: p.ID}, {Name: "name", Value: p.Name}, {Name: "path", Value: p.Path}}}
}

func projectCollection(projects []control.Project) output.Collection {
	rows := make([]output.Row, 0, len(projects))
	for _, p := range projects {
		rows = append(rows, output.Row{"id": p.ID, "name": p.Name, "path": p.Path})
	}
	return output.Collection{Columns: projectColumns, Rows: rows}
}

func writeProjects(w io.Writer, format output.Format, projects []control.Project) error {
	return output.Encode(w, format, projectCollection(projects))
}

func newProjectCommand(program *cobracli.Program, access *serviceAccess) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Register, list and remove the directories qcrew works in",
		Long: "A project is a directory registered by its path. The list lives in config.yaml in the qcrew\n" +
			"directory, which the qcrew service writes; see 'qcrew help project-guide'.",
	}

	var name string
	var create bool
	add := &cobra.Command{
		Use:   "add <path>",
		Short: "Register a directory as a project",
		Long: "add registers the directory at path, relative to the current directory or absolute, with symbolic\n" +
			"links resolved. The name defaults to the directory's base name and must be unique. The directory must\n" +
			"exist, unless --create makes it. Nothing inside the directory is read or changed.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			path, err := absolute(args[0])
			if err != nil {
				return &cli.Failure{Code: output.CodeUsage, Message: "cannot make the path absolute: " + err.Error(),
					NextStep: "Give the directory as an absolute path.", Usage: true, Cause: err}
			}
			cl, err := access.projectClient(c.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			p, err := cl.AddProject(c.Context(), control.ProjectAddArgs{Path: path, Name: name, Create: create})
			if err != nil {
				return projectFailure(err)
			}
			return output.Encode(c.OutOrStdout(), program.Format(), projectObject(p))
		},
	}
	add.Flags().StringVar(&name, "name", "", "name of the project instead of the directory's base name")
	add.Flags().BoolVar(&create, "create", false, "create the directory, with its parents, if it does not exist")

	list := &cobra.Command{
		Use:   "list",
		Short: "List the projects with id, name and path",
		Long:  "list shows the registered projects in the order they were added (--output table, json or compact).",
		RunE: func(c *cobra.Command, _ []string) error {
			cl, err := access.projectClient(c.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			projects, err := cl.Projects(c.Context())
			if err != nil {
				return projectFailure(err)
			}
			return writeProjects(c.OutOrStdout(), program.Format(), projects)
		},
	}

	remove := &cobra.Command{
		Use:   "remove <id|name>",
		Short: "Remove a project from the list; its directory stays as it is",
		Long:  "remove unregisters the project with the given id or name. It deletes no file. It needs --confirm.",
		Args:  cobra.ExactArgs(1),
	}
	gate := program.AddGate(remove, cobracli.Gates{Change: true})
	remove.RunE = func(c *cobra.Command, args []string) error {
		return gate.Run(cobracli.Request{Changes: true}, func() error {
			cl, err := access.projectClient(c.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			p, err := cl.RemoveProject(c.Context(), args[0])
			if err != nil {
				return projectFailure(err)
			}
			return output.Encode(c.OutOrStdout(), program.Format(), projectObject(p))
		})
	}

	cmd.AddCommand(add, list, remove)
	return cmd
}
