//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	r := run(t, "--version")
	if r.code != 0 || r.stdout != "qcrew dev\n" || r.stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
}

func TestHelp(t *testing.T) {
	r := run(t, "help")
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("code=%d stderr=%q", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "qcrew") || !strings.Contains(r.stdout, "Available Commands") {
		t.Fatalf("stdout lacks the qcrew overview: %q", r.stdout)
	}
}

func TestNoArguments(t *testing.T) {
	r := run(t)
	if r.code != 0 || r.stderr != "" {
		t.Fatalf("code=%d stderr=%q", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "qcrew is a program") || !strings.Contains(r.stdout, "Usage:") {
		t.Fatalf("stdout is not the overview: %q", r.stdout)
	}
}
