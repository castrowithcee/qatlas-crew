package client

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-crew/internal/control"
)

func TestFailureMapsCodes(t *testing.T) {
	cases := map[string]output.Code{
		"invalid-request":     output.CodeInvalidRequest,
		"unsupported-version": output.CodeInvalidRequest,
		"unknown-operation":   output.CodeNotFound,
		"runtime":             output.CodeRuntime,
		"invalid-argument":    output.CodeInvalidRequest,
		"already-exists":      CodeAlreadyExists,
		"not-found":           output.CodeNotFound,
		"config-invalid":      output.CodeConfigInvalid,
		"conflict":            CodeConflict,
		"in-use":              CodeInUse,
		"dependency-missing":  CodeDependencyMissing,
		"tool-missing":        CodeToolMissing,
		"something-new":       output.CodeRuntime,
		"":                    output.CodeRuntime,
	}
	for in, want := range cases {
		f := Failure(in, "m")
		if f.Code != want {
			t.Errorf("code %q -> %q, want %q", in, f.Code, want)
		}
		if !output.ValidCode(f.Code) || f.NextStep == "" || f.Message == "" {
			t.Errorf("code %q gives an invalid failure: %+v", in, f)
		}
	}
}

func TestFailureCleansMessage(t *testing.T) {
	f := Failure("runtime", "bad\x1b[31m\nline"+strings.Repeat("x", 500))
	if strings.ContainsAny(f.Message, "\x1b\n") || len(f.Message) > 250 {
		t.Errorf("message not cleaned: %q", f.Message)
	}
	if got := Failure("runtime", " \n").Message; got != "the service reported an error" {
		t.Errorf("empty message: %q", got)
	}
}

func TestEveryServiceCodeIsMapped(t *testing.T) {
	for _, code := range []string{control.CodeInvalidRequest, control.CodeUnsupportedVersion, control.CodeUnknownOperation,
		control.CodeRuntime, control.CodeInvalidArgument, control.CodeAlreadyExists, control.CodeNotFound,
		control.CodeConfigInvalid, control.CodeConflict, control.CodeInUse, control.CodeDependencyMissing,
		control.CodeToolMissing} {
		kind, ok := codes[code]
		if !ok || !output.ValidCode(kind.code) || kind.next == "" {
			t.Errorf("service code %q: %+v, mapped %v", code, kind, ok)
		}
	}
}

// A refusal of the user's request exits with the usage code and shows the service's message as it is; a
// fault exits with the runtime code.
func TestFailureExitCodes(t *testing.T) {
	refused := Failure(control.CodeAlreadyExists, "the name is taken")
	if cli.ExitCode(refused) != cli.ExitUsage || refused.Message != "the name is taken" {
		t.Errorf("refusal: exit %d, %+v", cli.ExitCode(refused), refused)
	}
	fault := Failure(control.CodeConfigInvalid, "bad file")
	if cli.ExitCode(fault) != cli.ExitRuntime || !strings.HasPrefix(fault.Message, "the service answered: ") {
		t.Errorf("fault: exit %d, %+v", cli.ExitCode(fault), fault)
	}
	missing := Failure(control.CodeDependencyMissing, "tmux is not installed")
	if cli.ExitCode(missing) != cli.ExitRuntime || missing.Message != "tmux is not installed" ||
		!strings.Contains(missing.NextStep, "qcrew setup") {
		t.Errorf("missing dependency: exit %d, %+v", cli.ExitCode(missing), missing)
	}
}
