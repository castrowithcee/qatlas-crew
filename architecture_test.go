package main

import (
	"os"
	"regexp"
	"testing"

	"github.com/castrowithcee/qatlas-kit/archtest"
)

// domain lists the packages that hold the product logic. They sit below the service layer.
var domain = []string{"internal/project", "internal/session", "internal/adapter", "internal/inbox",
	"internal/team", "internal/store", "internal/notify", "internal/setup"}

func TestArchitecture(t *testing.T) {
	archtest.Check(t, os.DirFS("."), []archtest.Rule{
		{
			Name:  "service-stays-below-the-command-surface",
			Files: archtest.Files{Under: []string{"internal/service"}, Tests: archtest.WithTests},
			Check: archtest.ForbidImports("internal/cli", "internal/client"),
			Hint:  "the command surface calls the service through the client, never the other way round",
		},
		{
			Name:  "control-holds-the-protocol-only",
			Files: archtest.Files{Under: []string{"internal/control"}, Tests: archtest.WithTests},
			Check: archtest.AllowImports(),
			Hint:  "the protocol imports the standard library only and sits below every other layer",
		},
		{
			Name:  "client-talks-protocol-only",
			Files: archtest.Files{Under: []string{"internal/client"}, Tests: archtest.WithTests},
			Check: archtest.ForbidImports("internal/service", "internal/cli"),
			Hint:  "the client reaches the service over the channel, never by import",
		},
		{
			Name:  "tui-reaches-the-domain-over-the-client-only",
			Files: archtest.Files{Under: []string{"internal/tui"}, Tests: archtest.SkipTests},
			Check: archtest.ForbidImports(append([]string{"internal/service", "internal/cli"}, domain...)...),
			Hint:  "the terminal interface asks the service through the client and imports no domain package",
		},
		{
			Name:  "domain-knows-no-service-layer",
			Files: archtest.Files{Under: domain, Tests: archtest.WithTests},
			Check: archtest.ForbidImports("internal/cli", "internal/tui", "internal/client", "internal/service"),
			Hint:  "domain packages sit below the service layer and know none of it",
		},
		{
			// The test helper runs its own servers, each with -S and a socket in the test's temporary directory.
			Name:  "only-the-session-package-runs-tmux",
			Files: archtest.Files{Under: []string{"."}, Except: []string{"internal/session", "internal/testenv"}, Tests: archtest.WithTests},
			Check: archtest.ForbidLiterals(archtest.Pattern(regexp.MustCompile(`^(.*/)?tmux$`))),
			Hint:  "call tmux through internal/session, which names the qcrew server in every call and never reaches the user's",
		},
	}, nil)
}
