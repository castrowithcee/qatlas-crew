package term

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const waitLimit = 10 * time.Second

// waitFor polls cond until it holds or the limit passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitLimit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// plain returns the visible text of the screen without any styling.
func plain(t *Term) string {
	return ansi.Strip(t.View(t.em.Width(), t.em.Height()))
}

// startDirect runs a shell script in a PTY of its own, without tmux.
func startDirect(t *testing.T, script string, cols, rows int, opts ...Option) *Term {
	t.Helper()
	tm, err := Start([]string{"sh", "-c", script}, cols, rows, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	return tm
}

// termGoroutines returns the goroutines that run code of a Term.
func termGoroutines() []string {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	var found []string
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "term.(*Term).") || strings.Contains(g, "term.throttle(") {
			found = append(found, g)
		}
	}
	return found
}

func assertEnded(t *testing.T, tm *Term) {
	t.Helper()
	select {
	case <-tm.done:
	default:
		t.Fatal("goroutines of the Term still running")
	}
	if g := termGoroutines(); len(g) > 0 {
		t.Fatalf("goroutines left after the end:\n%s", strings.Join(g, "\n\n"))
	}
}
