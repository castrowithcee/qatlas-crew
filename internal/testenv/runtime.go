package testenv

import (
	"os"
	"testing"
)

// ShortRuntime points XDG_RUNTIME_DIR at a new private directory with a short path and returns it. A Unix
// socket path is limited to about 100 bytes, which a directory below t.TempDir can exceed. The directory is
// removed when the test ends. Call it after Setup, whose TMPDIR is ignored here on purpose.
func ShortRuntime(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "qc-")
	if err != nil {
		t.Fatalf("testenv: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	return dir
}

// WithRuntime returns env with its XDG_RUNTIME_DIR entry set to dir, for a child process.
func WithRuntime(env []string, dir string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if len(kv) < len("XDG_RUNTIME_DIR=") || kv[:len("XDG_RUNTIME_DIR=")] != "XDG_RUNTIME_DIR=" {
			out = append(out, kv)
		}
	}
	return append(out, "XDG_RUNTIME_DIR="+dir)
}
