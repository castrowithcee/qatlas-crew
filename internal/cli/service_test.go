package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-crew/internal/client"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// fakeService stands for the service: it is up once start ran.
type fakeService struct {
	up           bool
	dials, start int
	startErr     error
	dialErr      error // returned instead of ErrNotRunning while the service is down
}

func (f *fakeService) access(noAutostart bool) *serviceAccess {
	return &serviceAccess{
		noAutostart: noAutostart,
		dial: func(context.Context) (*client.Client, error) {
			f.dials++
			if f.up {
				return &client.Client{}, nil
			}
			if f.dialErr != nil {
				return nil, f.dialErr
			}
			return nil, fmt.Errorf("%w: gone", client.ErrNotRunning)
		},
		start: func(context.Context) (int, error) {
			f.start++
			if f.startErr == nil || errors.Is(f.startErr, errAlreadyRunning) {
				f.up = true
			}
			return 7, f.startErr
		},
	}
}

func TestConnectRunningDoesNotStart(t *testing.T) {
	f := &fakeService{up: true}
	if _, err := f.access(false).connect(context.Background()); err != nil || f.start != 0 {
		t.Fatalf("err=%v starts=%d", err, f.start)
	}
}

func TestConnectStartsOnDemand(t *testing.T) {
	f := &fakeService{}
	if _, err := f.access(false).connect(context.Background()); err != nil || f.start != 1 || f.dials != 2 {
		t.Fatalf("err=%v starts=%d dials=%d", err, f.start, f.dials)
	}
}

func TestConnectNoAutostart(t *testing.T) {
	f := &fakeService{}
	_, err := f.access(true).connect(context.Background())
	var failure *cli.Failure
	if !errors.As(err, &failure) || failure.Code != output.CodeUnreachable || !strings.Contains(failure.Message, "--no-autostart") {
		t.Fatalf("err=%v", err)
	}
	if f.start != 0 {
		t.Fatalf("started %d times despite --no-autostart", f.start)
	}
}

func TestConnectRaceWithOtherStart(t *testing.T) {
	f := &fakeService{startErr: fmt.Errorf("wrapped: %w", errAlreadyRunning)}
	if _, err := f.access(false).connect(context.Background()); err != nil {
		t.Fatalf("err=%v", err)
	}
}

func TestConnectStartFailure(t *testing.T) {
	boom := &cli.Failure{Code: output.CodeRuntime, Message: "boom", NextStep: "x"}
	f := &fakeService{startErr: boom}
	if _, err := f.access(false).connect(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
}

func TestConnectOtherDialErrorDoesNotStart(t *testing.T) {
	f := &fakeService{dialErr: &localipc.PeerError{PID: 5, Err: localipc.ErrRefused}}
	_, err := f.access(false).connect(context.Background())
	var failure *cli.Failure
	if !errors.As(err, &failure) || failure.Code != output.CodePermission || f.start != 0 {
		t.Fatalf("err=%v starts=%d", err, f.start)
	}
}

func TestServiceStatusWithoutServiceDoesNotStart(t *testing.T) {
	// The real dial in an empty runtime directory: not running, and nothing is started.
	testenv.Setup(t)
	testenv.ShortRuntime(t)
	code, out, errOut := run(t, "service", "status", "--output", "json")
	if code != 0 || strings.TrimSpace(out) != `{"running":false}` {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestNoAutostartIsAGlobalFlag(t *testing.T) {
	code, out, _ := run(t, "service", "status", "--help")
	if code != 0 || !strings.Contains(out, "--no-autostart") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}
