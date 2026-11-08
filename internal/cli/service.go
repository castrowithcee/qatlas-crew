package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/background"
	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-crew/internal/client"
	"github.com/castrowithcee/qatlas-crew/internal/service"
)

const (
	// codeAlreadyRunning is the error code of a start while the service runs.
	codeAlreadyRunning output.Code = "already-running"
	// stopWait bounds how long service stop waits for the service to give up its channel.
	stopWait = 10 * time.Second
)

// errAlreadyRunning is the cause of the already-running failure.
var errAlreadyRunning = errors.New("the qcrew service is already running")

// serviceCommand is the process service start launches: this program's hidden service run command. Tests
// replace it, because the test binary has no such command.
var serviceCommand = func() background.Command {
	return background.Command{Args: []string{"service", "run"}}
}

// serviceAccess is how commands reach the service. Tests replace its functions.
type serviceAccess struct {
	// noAutostart makes connect fail instead of starting a service that is not running.
	noAutostart bool
	// configFlag is the value of --config.
	configFlag string
	dial       func(ctx context.Context) (*client.Client, error)
	start      func(ctx context.Context) (pid int, err error)
}

func defaultServiceAccess() *serviceAccess {
	a := &serviceAccess{
		dial: func(ctx context.Context) (*client.Client, error) { return client.Dial(ctx, localipc.DialOptions{}) },
	}
	a.start = func(ctx context.Context) (int, error) {
		config, err := a.configPath()
		if err != nil {
			return 0, err
		}
		return startService(ctx, config)
	}
	return a
}

// connect returns a connection to the service. A service that is not running is started first, unless
// noAutostart is set; commands that need the service use connect, the service commands themselves do not.
func (a *serviceAccess) connect(ctx context.Context) (*client.Client, error) {
	c, err := a.dial(ctx)
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, client.ErrNotRunning) {
		return nil, serviceFailure(err)
	}
	if a.noAutostart {
		return nil, &cli.Failure{
			Code:     output.CodeUnreachable,
			Message:  "the qcrew service is not running and --no-autostart forbids starting it",
			NextStep: "Run 'qcrew service start', or leave out --no-autostart.",
			Cause:    err,
		}
	}
	if _, err := a.start(ctx); err != nil && !errors.Is(err, errAlreadyRunning) {
		return nil, err
	}
	c, err = a.dial(ctx)
	if err != nil {
		return nil, serviceFailure(err)
	}
	return c, nil
}

// serviceFailure turns an error of reaching the service into a diagnostic.
func serviceFailure(err error) error {
	var failure *cli.Failure
	var peer *localipc.PeerError
	switch {
	case errors.As(err, &failure):
		return err
	case errors.Is(err, client.ErrNotRunning):
		return &cli.Failure{Code: output.CodeUnreachable, Message: "the qcrew service is not running",
			NextStep: "Run 'qcrew service start'.", Cause: err}
	case errors.As(err, &peer):
		return &cli.Failure{Code: output.CodePermission,
			Message:  "the process on the qcrew service channel is not this user's qcrew service of this program file: " + peer.Error(),
			NextStep: "If qcrew was updated, the old service ends by itself shortly; then run 'qcrew service start'.", Cause: err}
	case errors.Is(err, context.DeadlineExceeded):
		return &cli.Failure{Code: output.CodeTimeout, Message: "the qcrew service did not answer in time",
			NextStep: "Run 'qcrew service status'.", Cause: err}
	}
	return &cli.Failure{Code: output.CodeRuntime, Message: "cannot reach the qcrew service: " + err.Error(),
		NextStep: "Run 'qcrew service status'.", Cause: err}
}

// startService starts the service in the background on the configuration file config and waits until it
// listens. It returns the process id of the service, or an already-running failure (whose cause is
// errAlreadyRunning) if one runs.
func startService(ctx context.Context, config string) (int, error) {
	cmd := serviceCommand()
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	// The resolved path travels as QCREW_CONFIG, so that --config and a relative path reach the service,
	// which has no flags and works in another directory.
	cmd.Env = append(slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		return strings.HasPrefix(kv, configFileEnv+"=")
	}), configFileEnv+"="+config)
	res, err := background.Start(ctx, cmd, nil, background.Options{})
	if err != nil {
		var report *background.ReportError
		if errors.As(err, &report) {
			return 0, &cli.Failure{Code: output.CodeRuntime, Message: "the qcrew service did not start: " + report.Reason,
				NextStep: "See the service log below ~/.qatlas/qcrew/logs, then run 'qcrew service start' again.", Cause: err}
		}
		return 0, &cli.Failure{Code: output.CodeRuntime, Message: "the qcrew service did not start: " + err.Error(),
			NextStep: "Run 'qcrew service start' again.", Cause: err}
	}
	if res.Report == background.ReportRunning {
		return 0, &cli.Failure{Code: codeAlreadyRunning, Message: errAlreadyRunning.Error(),
			NextStep: "Run 'qcrew service status' to see it, or 'qcrew service stop' to end it.", Cause: errAlreadyRunning}
	}
	return res.PID, nil
}

func newServiceCommand(program *cobracli.Program, access *serviceAccess) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Start, stop and inspect the qcrew background service",
		Long: "The qcrew service runs in the background, one per user and host, and keeps running without a\n" +
			"terminal until it is stopped. qcrew commands that need it start it on demand, unless --no-autostart\n" +
			"is given.",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start the service in the background and wait until it is ready",
			Long: "start launches the service detached from the terminal, waits until it listens and prints its\n" +
				"process id. It fails with already-running if a service of this user already runs.",
			RunE: func(c *cobra.Command, _ []string) error {
				pid, err := access.start(c.Context())
				if err != nil {
					return err
				}
				return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{
					{Name: "running", Value: true}, {Name: "pid", Value: pid},
				}})
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the service",
			Long: "stop asks the service to end and waits until it has given up its channel. It succeeds, with\n" +
				"stopped false, if no service runs.",
			RunE: func(c *cobra.Command, _ []string) error { return runServiceStop(c, program, access) },
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show whether the service runs, with its process id and version",
			Long: "status reports running false and exits 0 if no service runs; it never starts the service.\n" +
				"Use --output json for the fields running, pid, version and started_at.",
			RunE: func(c *cobra.Command, _ []string) error {
				fields := []output.Field{{Name: "running", Value: false}}
				cl, err := access.dial(c.Context())
				switch {
				case errors.Is(err, client.ErrNotRunning):
				case err != nil:
					return serviceFailure(err)
				default:
					defer cl.Close()
					st, err := cl.Status(c.Context())
					if err != nil {
						return serviceFailure(err)
					}
					fields = []output.Field{
						{Name: "running", Value: true}, {Name: "pid", Value: st.PID},
						{Name: "version", Value: st.Version}, {Name: "started_at", Value: st.StartedAt.Format(time.RFC3339)},
					}
				}
				return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: fields})
			},
		},
		&cobra.Command{
			Use:    "run",
			Short:  "Run the service in this process (started by 'service start')",
			Hidden: true,
			RunE:   func(c *cobra.Command, _ []string) error { return runServiceProcess(c, access) },
		},
	)
	return cmd
}

func runServiceStop(c *cobra.Command, program *cobracli.Program, access *serviceAccess) error {
	ctx := c.Context()
	cl, err := access.dial(ctx)
	if errors.Is(err, client.ErrNotRunning) {
		return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: []output.Field{{Name: "stopped", Value: false}}})
	}
	if err != nil {
		return serviceFailure(err)
	}
	pid := 0
	if st, err := cl.Status(ctx); err == nil {
		pid = st.PID
	}
	err = cl.Stop(ctx)
	_ = cl.Close()
	if err != nil {
		return serviceFailure(err)
	}
	deadline := time.Now().Add(stopWait)
	for {
		next, err := access.dial(ctx)
		if errors.Is(err, client.ErrNotRunning) {
			break
		}
		if err == nil {
			_ = next.Close()
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return &cli.Failure{Code: output.CodeTimeout, Message: "the qcrew service did not stop in time",
				NextStep: "Run 'qcrew service status'; if it still runs, end its process.", Cause: err}
		}
		time.Sleep(50 * time.Millisecond)
	}
	fields := []output.Field{{Name: "stopped", Value: true}}
	if pid != 0 {
		fields = append(fields, output.Field{Name: "pid", Value: pid})
	}
	return output.Encode(c.OutOrStdout(), program.Format(), output.Object{Fields: fields})
}

// runServiceProcess is the hidden service run command: the body of the background process. It reports to
// the starter once the service listens, or why it could not.
func runServiceProcess(c *cobra.Command, access *serviceAccess) error {
	child, ok := background.Inherit()
	if !ok {
		return &cli.Failure{Code: output.CodeUsage, Message: "service run is not meant to be started by hand",
			NextStep: "Run 'qcrew service start'.", Usage: true}
	}
	defer child.Close()
	config, err := access.configPath()
	if err != nil {
		child.Fail(err.Error())
		return err
	}
	err = service.Run(c.Context(), service.Options{Version: version, Config: config, Ready: child.Ready})
	switch {
	case errors.Is(err, service.ErrRunning):
		child.Running()
		return nil
	case err != nil:
		child.Fail(err.Error())
		return &cli.Failure{Code: output.CodeRuntime, Message: fmt.Sprintf("the service ended: %v", err),
			NextStep: "See the service log below ~/.qatlas/qcrew/logs.", Cause: err}
	}
	return nil
}
