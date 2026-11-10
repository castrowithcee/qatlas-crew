//go:build e2e

package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/ptytest"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// ptyChild makes the test binary run the interface itself, at the terminal the parent test opened.
const ptyChild = "QCREW_TUI_PTY_CHILD"

// TestPtyChild is the interface in the child process of the tests below; it does nothing otherwise.
func TestPtyChild(t *testing.T) {
	if os.Getenv(ptyChild) != "1" {
		t.Skip("runs only as the child of a pty test")
	}
	schema, err := PrefsSchema()
	if err != nil {
		t.Fatal(err)
	}
	b := goldenBackend()
	err = Run(Options{In: os.Stdin, Out: os.Stdout, Prefs: uiprefs.Default(schema),
		Connect: func(context.Context) (Connection, error) { return b, nil }})
	if err != nil {
		t.Fatal(err)
	}
}

// ptyScreen is the interface at a terminal of the given size.
type ptyScreen struct {
	t    *testing.T
	term *ptytest.Terminal
}

func startPty(t *testing.T, cols, rows int, env ...string) *ptyScreen {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env = append([]string{ptyChild + "=1", "TERM=xterm-256color", "LANG=C.UTF-8"}, env...)
	term, err := ptytest.Start(ptytest.Command{Path: exe, Args: []string{"-test.run=^TestPtyChild$"}, Env: env,
		Size: ptytest.Size{Columns: cols, Rows: rows}})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	return &ptyScreen{t: t, term: term}
}

// mark is the end of the output so far; a wait that starts at it ignores what was drawn before.
func (p *ptyScreen) mark() int { return len(p.term.Output()) }

// waitAfter waits for text in what was drawn after mark. The terminal redraws only changed cells, so the
// text must be one that the action changes.
func (p *ptyScreen) waitAfter(mark int, text string) {
	p.t.Helper()
	err := p.term.WaitFor(10*time.Second, func(output string) bool {
		return mark <= len(output) && strings.Contains(output[mark:], text)
	})
	if err != nil {
		p.t.Fatalf("waiting for %q: %v\n%q", text, err, p.term.Output()[mark:])
	}
}

func (p *ptyScreen) key(s string) {
	p.t.Helper()
	if _, err := p.term.Write([]byte(s)); err != nil {
		p.t.Fatal(err)
	}
}

// click presses and releases the left button on the cell x, y, counted from 0.
func (p *ptyScreen) click(x, y int) {
	p.t.Helper()
	p.key(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1))
}

func (p *ptyScreen) quit() {
	p.t.Helper()
	p.key("q")
	if code, err := p.term.Wait(10 * time.Second); err != nil || code != 0 {
		p.t.Fatalf("exit code %d, %v\n%q", code, err, p.term.Output())
	}
}

// cellsOf are the cells of the first occurrence of needle on the screen a model of the same size draws; the
// layout does not depend on colour or on the key that led there.
func cellsOf(t *testing.T, cols, rows int, needle string, keys ...string) (x, y int) {
	t.Helper()
	m := newTestModel(t, i18n.English, goldenBackend(), cols, rows)
	send(m, keys...)
	x, y = cellOf(t, m, needle)
	return x + 1, y
}

// The interface starts at a terminal, draws the projects and ends cleanly on q.
func TestPtyStartsAndQuits(t *testing.T) {
	p := startPty(t, 120, 40)
	p.waitAfter(0, "alpha")
	p.waitAfter(0, "refactor parser")
	p.quit()
}

// A click on a project shows its sessions; a click on [?] opens the help; f shows the pane alone and back.
func TestPtyAcceptanceByMouseAndKey(t *testing.T) {
	const cols, rows = 120, 40
	p := startPty(t, cols, rows)
	p.waitAfter(0, "refactor parser")

	x, y := cellsOf(t, cols, rows, "beta")
	at := p.mark()
	p.click(x, y)
	p.waitAfter(at, "build watcher")

	x, y = cellsOf(t, cols, rows, "[?]")
	at = p.mark()
	p.click(x, y)
	p.waitAfter(at, "qcrew is a program")
	// A lone escape waits for a possible sequence, so the help key closes it.
	p.key("?")

	at = p.mark()
	p.key("f")
	p.waitAfter(at, "columns")
	at = p.mark()
	p.key("f")
	p.waitAfter(at, "full screen")
	p.quit()
}

// colorSequence matches a colour of the foreground or the background in a control sequence.
var colorSequence = regexp.MustCompile(`\x1b\[[0-9;:]*(?:3[0-79]|4[0-79]|38|48|9[0-7]|10[0-7])[0-9;:]*m`)

// With NO_COLOR the interface works at 60x30 and draws no colour.
func TestPtyNoColorAtNarrowSize(t *testing.T) {
	p := startPty(t, 60, 30, "NO_COLOR=1")
	p.waitAfter(0, "refactor parser")
	at := p.mark()
	p.key("2")
	p.waitAfter(at, "follows in a later release")
	at = p.mark()
	p.key("1")
	p.key("\r")
	p.key("e")
	p.waitAfter(at, "Stop the session")
	p.key("n")
	p.key("?")
	p.key("?")
	if loc := colorSequence.FindString(string(p.term.Output())); loc != "" {
		t.Fatalf("a colour was drawn with NO_COLOR: %q", loc)
	}
	p.quit()
}
