package cli

import (
	"bytes"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestVersionFlag(t *testing.T) {
	code, out, _ := run(t, "--version")
	if code != 0 || strings.TrimSpace(out) != "qcrew "+version {
		t.Fatalf("--version = %d %q", code, out)
	}
}

func TestNoArgumentsShowsOverview(t *testing.T) {
	code, out, _ := run(t)
	if code != 0 || !strings.Contains(out, "self-update") || strings.Contains(out, "follows in a later release") || !strings.Contains(out, "opens the terminal interface") {
		t.Fatalf("no arguments = %d %q", code, out)
	}
}

func TestSelfUpdateNeedsConfirm(t *testing.T) {
	old := selfUpdate
	t.Cleanup(func() { selfUpdate = old })
	selfUpdate.apiURL = "http://127.0.0.1:1"
	if code, _, _ := run(t, "self-update"); code == 0 {
		t.Fatal("self-update without --confirm succeeded")
	}
}
