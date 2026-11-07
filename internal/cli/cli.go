// Package cli defines the command surface of the qcrew binary. Commands stay thin adapters: they parse
// options, hand typed results to the encoders, and leave errors and exit codes to the kit.
package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"

	"github.com/castrowithcee/qatlas-crew/internal/helptopics"
)

// version is overridden at build time with
// -ldflags "-X github.com/castrowithcee/qatlas-crew/internal/cli.version=<v>".
var version = "dev"

// Run executes the command line against the given streams and returns the process exit code. It never
// terminates the process, so callers and tests share the same path.
func Run(args []string, stdout, stderr io.Writer) int {
	program, err := newRoot()
	if err != nil {
		return cli.ExitRuntime
	}
	return program.Execute(args, stdout, stderr)
}

func newRoot() (*cobracli.Program, error) {
	root := &cobra.Command{
		Use:   "qcrew",
		Short: "Work with several AI agent command line tools in parallel across projects",
		Long: "qcrew is a program with a terminal interface for working with several AI agent command line tools,\n" +
			"such as Claude Code and Codex, in parallel across several projects.\n" +
			"qcrew is in an early state: the terminal interface follows in a later release, so without a command\n" +
			"qcrew shows this overview.",
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	program, err := cobracli.New(root, cobracli.Options{Version: version, Topics: helptopics.All(), Redactor: &redact.Redactor{}})
	if err != nil {
		return nil, err
	}
	root.AddCommand(newVersionCommand(program), newSelfUpdateCommand(program), newManpageCommand(program))
	return program, nil
}

func newVersionCommand(program *cobracli.Program) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of qcrew",
		RunE: func(c *cobra.Command, _ []string) error {
			return output.Encode(c.OutOrStdout(), program.Format(),
				output.Object{Fields: []output.Field{{Name: "version", Value: version}}})
		},
	}
}
