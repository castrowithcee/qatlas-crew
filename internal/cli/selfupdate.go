package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/selfupdate"

	"github.com/castrowithcee/qatlas-crew/internal/release"
)

const (
	// releaseAPI is the GitHub API base of the public repository the releases come from.
	releaseAPI = "https://api.github.com/repos/castrowithcee/qatlas-crew"
	// minimumVersion is the oldest release that can be installed.
	minimumVersion = "v0.1.0"
	// programName is the executable and the release archive prefix.
	programName = "qcrew"
	// manEntry is the main manpage inside the release archive, below the installation prefix.
	manEntry = "share/man/man1/qcrew.1"
)

// selfUpdateHost is everything the self-update takes from the host. Production code never changes it;
// tests replace it so that no real release server or installed file is involved.
type selfUpdateHost struct {
	apiURL     string
	keys       func() []ssh.PublicKey
	executable func() (string, error)
}

var selfUpdate = selfUpdateHost{
	apiURL:     releaseAPI,
	keys:       release.TrustedKeys,
	executable: os.Executable,
}

func (h selfUpdateHost) client() *selfupdate.Client {
	return &selfupdate.Client{
		RepositoryAPI:  strings.TrimRight(h.apiURL, "/"),
		CurrentVersion: version,
		MinimumVersion: minimumVersion,
		ArchiveName: func(v string) string {
			switch runtime.GOOS + "/" + runtime.GOARCH {
			case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64":
				return fmt.Sprintf("%s_%s_%s_%s.tar.gz", programName, v, runtime.GOOS, runtime.GOARCH)
			}
			return ""
		},
		UserAgent:      programName + "/" + version,
		Namespace:      release.Namespace,
		SignatureAsset: release.SignatureAsset,
		TrustedKeys:    h.keys(),
		ProgramName:    programName,
	}
}

// companions returns the manpage that lives beside an installation of the form <prefix>/bin/qcrew, which is
// what the installer makes. Any other location gets the program alone.
func companions(exe string) []selfupdate.Companion {
	bin := filepath.Dir(exe)
	if filepath.Base(bin) != "bin" {
		return nil
	}
	return []selfupdate.Companion{{Entry: manEntry, Path: filepath.Join(filepath.Dir(bin), filepath.FromSlash(manEntry))}}
}

func usageFailure(message, next string) error {
	return &cli.Failure{Code: output.CodeUsage, Message: message, NextStep: next, Usage: true}
}

// newSelfUpdateCommand is the self-update of the program.
func newSelfUpdateCommand(program *cobracli.Program) *cobra.Command {
	var gate *cobracli.Gate
	cmd := &cobra.Command{
		Use:   "self-update [version]",
		Short: "Update qcrew to the latest stable release or to a given newer one",
		Long: "self-update replaces the installed program and its main manpage with those of a signed release.\n" +
			"Without an argument it installs the newest stable release if that is newer than the running version;\n" +
			"with a version such as v1.2.3 it installs exactly that release, which must be newer than the running\n" +
			"one. Updating never installs an older release.\n" +
			"The signature over the release's checksums.txt is verified against the release key built into\n" +
			"qcrew, then the archive's SHA-256 checksum; any failure leaves the installation unchanged.\n" +
			"Installing needs --confirm and asks for confirmation on a terminal; --yes answers the question.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageFailure("self-update takes at most one argument, the release version",
					"Run 'qcrew self-update [version]'.")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			tag := ""
			if len(args) == 1 {
				tag = args[0]
			}
			return runSelfUpdate(c, program, gate, tag)
		},
	}
	gate = program.AddGate(cmd, cobracli.Gates{Question: true, Change: true})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show the installed version, the latest stable release and whether an update is available",
		Long: "status asks the release server for the newest stable release and compares it with the running\n" +
			"version. It changes nothing.",
		RunE: func(c *cobra.Command, _ []string) error {
			res, err := selfUpdate.client().Check(c.Context())
			if err != nil {
				return selfUpdateFailure(err)
			}
			return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
				{Name: "installed", Value: res.Current},
				{Name: "latest", Value: res.Latest},
				{Name: "update_available", Value: res.UpdateAvailable},
			}})
		},
	})
	return cmd
}

func runSelfUpdate(c *cobra.Command, program *cobracli.Program, gate *cobracli.Gate, tag string) error {
	// A request without --confirm fails before the release server is asked.
	if err := gate.Run(cobracli.Request{Changes: true}, func() error { return nil }); err != nil {
		return err
	}
	exe, err := selfUpdate.executable()
	if err != nil {
		return selfUpdateFailure(fmt.Errorf("cannot find the running program: %w", err))
	}
	updater := selfUpdate.client()
	updater.Executable = exe
	updater.Companions = companions(exe)
	if updater.ArchiveName("v0.0.0") == "" {
		return selfUpdateFailure(fmt.Errorf("no release is published for %s/%s", runtime.GOOS, runtime.GOARCH))
	}
	ctx := c.Context()
	if tag == "" {
		check, err := updater.Check(ctx)
		if err != nil {
			return selfUpdateFailure(err)
		}
		if !check.UpdateAvailable {
			return emitSelfUpdate(c, program, version, version, false)
		}
		tag = check.Latest
	}
	target, err := updater.Target(ctx, tag)
	if err != nil {
		var te *selfupdate.TargetError
		if errors.As(err, &te) {
			return usageFailure(err.Error(), "Run 'qcrew self-update status' to see the latest stable release.")
		}
		return selfUpdateFailure(err)
	}
	if target.Current {
		return emitSelfUpdate(c, program, version, version, false)
	}
	if target.Older {
		return usageFailure("release "+target.Version+" is older than the running version "+version+"; self-update never installs an older release",
			"Run 'qcrew self-update status' to see the latest stable release.")
	}
	question := fmt.Sprintf("Update qcrew from %s to %s.", version, target.Version)
	return gate.Run(cobracli.Request{Question: question, Changes: true}, func() error {
		return installRelease(ctx, updater, target, func(result selfupdate.Result) error {
			return emitSelfUpdate(c, program, result.Current, result.Latest, true)
		})
	})
}

func installRelease(ctx context.Context, updater *selfupdate.Client, target selfupdate.Target, emit func(selfupdate.Result) error) error {
	prepared, err := updater.Prepare(ctx, target)
	if err != nil {
		return selfUpdateFailure(err)
	}
	result, err := updater.InstallPrepared(ctx, prepared)
	if err != nil {
		return selfUpdateFailure(err)
	}
	return emit(result)
}

// selfUpdateFailure is a failed update or release lookup; the message says whether the installation changed.
func selfUpdateFailure(err error) error {
	message := "self-update: " + err.Error()
	if errors.Is(err, selfupdate.ErrDevelopmentBuild) {
		message = "self-update: self-update is unavailable in a development build; install a tagged release first"
	}
	return &cli.Failure{Code: output.CodeRuntime, Message: message, Cause: err,
		NextStep: "Read the message: the installation is unchanged unless it says otherwise. Fix the cause, then run the command again."}
}

func emitSelfUpdate(c *cobra.Command, program *cobracli.Program, previous, current string, updated bool) error {
	return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
		{Name: "previous", Value: previous},
		{Name: "version", Value: current},
		{Name: "updated", Value: updated},
	}})
}
