package term

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// FrameMsg tells that the screen of the Term with this ID changed. Draw it with [Term.View].
type FrameMsg struct{ ID int }

// ExitedMsg tells that the client of the Term with this ID ended, for example because its tmux window was
// closed. Code is the exit status of the client, -1 when it was ended by a signal.
type ExitedMsg struct {
	ID   int
	Code int
}

// Option changes how Start sets up a Term.
type Option func(*Term)

// WithClock replaces the clock of the frame throttle.
func WithClock(c Clock) Option { return func(t *Term) { t.clock = c } }

// Term is one command running in a PTY together with the VT screen of its output.
type Term struct {
	id    int
	clock Clock

	cmd  *exec.Cmd
	ptmx *os.File
	em   *vt.SafeEmulator

	out   outbox
	input inputQueue

	dirty  chan struct{} // capacity 1: the screen changed since the last frame
	frames chan struct{} // capacity 1: a frame is waiting for Next
	stop   chan struct{} // closed when the child is gone and the output is read
	done   chan struct{} // closed when every goroutine of the Term has ended

	closeOnce sync.Once
	exitCode  int
}

var nextID atomic.Int64

// Start runs argv in a new PTY of cols x rows cells. The command sees the environment of the process without
// TMUX, so a tmux client does not refuse to attach from inside tmux, and with the terminal type and color
// support of the VT instead of those of the outer terminal.
func Start(argv []string, cols, rows int, opts ...Option) (*Term, error) {
	if len(argv) == 0 {
		return nil, errors.New("term: empty command")
	}
	if cols < 1 || rows < 1 {
		return nil, errors.New("term: size must be positive")
	}
	t := &Term{
		id:     int(nextID.Add(1)),
		clock:  realClock{},
		dirty:  make(chan struct{}, 1),
		frames: make(chan struct{}, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	for _, o := range opts {
		o(t)
	}
	t.cmd = exec.Command(argv[0], argv[1:]...)
	t.cmd.Env = childEnv(os.Environ())
	ptmx, err := pty.StartWithSize(t.cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}
	if t.ptmx, err = pollable(ptmx); err != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
		return nil, err
	}
	// No callbacks are set: titles, clipboard and similar requests of the program are dropped here and
	// never forwarded to the outer terminal.
	t.em = vt.NewSafeEmulator(cols, rows)

	t.out.wake = make(chan struct{}, 1)
	t.input.wake = make(chan struct{}, 1)

	var wg sync.WaitGroup
	wg.Add(5)
	go func() { defer wg.Done(); t.readLoop() }()
	go func() { defer wg.Done(); t.replyLoop() }()
	go func() { defer wg.Done(); t.writeLoop() }()
	go func() { defer wg.Done(); t.inputLoop() }()
	go func() { defer wg.Done(); throttle(t.clock, FrameInterval, t.dirty, t.stop, t.emitFrame) }()
	go func() { wg.Wait(); close(t.done) }()
	return t, nil
}

// childEnv returns env without the tmux variables and with the terminal type and color support of the VT.
// COLORTERM tells a tmux client that the VT takes 24-bit colors.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") || strings.HasPrefix(kv, "TERM=") ||
			strings.HasPrefix(kv, "COLORTERM=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "TERM=xterm-256color", "COLORTERM=truecolor")
}

// ID identifies this instance in its messages.
func (t *Term) ID() int { return t.id }

// readLoop feeds the output of the child to the VT until the child ends.
func (t *Term) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := t.ptmx.Read(buf)
		if n > 0 {
			_, _ = t.em.Write(buf[:n])
			select {
			case t.dirty <- struct{}{}:
			default:
			}
		}
		if err != nil {
			break
		}
	}
	// The child has closed its end of the PTY or Close hung it up.
	t.exitCode = exitCode(t.cmd.Wait())
	_ = t.ptmx.Close()
	// Closing the answer pipe ends the reply loop. The pipe is closed directly because Emulator.Close sets a
	// flag the reply loop reads without synchronization. The throttle ends after the final state is emitted.
	if c, ok := t.em.InputPipe().(io.Closer); ok {
		_ = c.Close()
	}
	// The throttle may stop before it emits the state of the last output, so deliver one more frame.
	t.emitFrame()
	close(t.stop)
}

// replyLoop moves what the VT sends to the child, the answers to terminal queries and the input of the person,
// into the outbox. It never waits for the PTY: the VT holds its lock while it writes into the pipe this loop
// reads, so a loop that blocked on a full PTY would stop readLoop, and a child that waits for its output to
// be read would never read its input.
func (t *Term) replyLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := t.em.Read(buf)
		if n > 0 {
			t.out.add(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// writeLoop puts the outbox into the PTY in order. When the PTY no longer accepts input, what is left is
// discarded.
func (t *Term) writeLoop() {
	for {
		chunk := t.out.take()
		if len(chunk) == 0 {
			select {
			case <-t.out.wake:
				continue
			case <-t.stop:
				t.out.discard()
				return
			}
		}
		if _, err := t.ptmx.Write(chunk); err != nil {
			t.out.discard()
			return
		}
	}
}

// inputLoop turns the queued input into bytes for the child, one item at a time in the order of arrival.
func (t *Term) inputLoop() {
	for {
		send := t.input.take()
		if send == nil {
			select {
			case <-t.input.wake:
				continue
			case <-t.stop:
				t.input.discard()
				return
			}
		}
		send(t.em)
	}
}

func (t *Term) emitFrame() {
	select {
	case t.frames <- struct{}{}:
	default:
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// Next returns a command that waits for the next change of the screen and yields a [FrameMsg], or an
// [ExitedMsg] once the client has ended and its last frame was delivered. Issue it again after every message
// to keep receiving. Frames come at most 60 times a second.
func (t *Term) Next() tea.Cmd {
	return func() tea.Msg {
		select {
		case <-t.frames:
			return FrameMsg{ID: t.id}
		case <-t.done:
			select {
			case <-t.frames:
				return FrameMsg{ID: t.id}
			default:
			}
			return ExitedMsg{ID: t.id, Code: t.exitCode}
		}
	}
}

// View returns the screen as the top left width x height cells, one line per row, joined by "\n". Colors and
// attributes are SGR sequences, and the cursor is a cell in reverse video. Draw it into a rectangle of that
// size, after [Term.Resize] to the same size.
func (t *Term) View(width, height int) string {
	return render(t.em, width, height)
}

// Write sends raw input to the program in the PTY.
func (t *Term) Write(p []byte) (int, error) { return t.ptmx.Write(p) }

// SendKey passes a key press to the program, encoded as the VT of the program expects it. It never blocks,
// keeps the order of the calls and does nothing once the client has ended.
func (t *Term) SendKey(msg tea.KeyPressMsg) {
	key := vt.KeyPressEvent{Text: msg.Text, Mod: msg.Mod, Code: msg.Code}
	t.input.add(func(em *vt.SafeEmulator) { em.SendKey(key) })
}

// Paste passes pasted text to the program once, between the markers of a bracketed paste if the program has
// asked for them. It never blocks.
func (t *Term) Paste(text string) {
	t.input.add(func(em *vt.SafeEmulator) { em.Paste(text) })
}

// SendMouse passes a click, its release or a wheel step to the program at the cell x, y of its screen,
// counted from 0. The VT encodes it only in a mouse mode the program has asked for. Other events, motion
// included, are dropped. It never blocks.
func (t *Term) SendMouse(msg tea.MouseMsg, x, y int) {
	m := msg.Mouse()
	var ev vt.Mouse
	switch msg.(type) {
	case tea.MouseClickMsg:
		ev = vt.MouseClick{X: x, Y: y, Button: m.Button, Mod: m.Mod}
	case tea.MouseReleaseMsg:
		ev = vt.MouseRelease{X: x, Y: y, Button: m.Button, Mod: m.Mod}
	case tea.MouseWheelMsg:
		ev = vt.MouseWheel{X: x, Y: y, Button: m.Button, Mod: m.Mod}
	default:
		return
	}
	t.input.add(func(em *vt.SafeEmulator) { em.SendMouse(ev) })
}

// Resize sets the size of the PTY and of the VT screen.
func (t *Term) Resize(cols, rows int) error {
	if cols < 1 || rows < 1 {
		return errors.New("term: size must be positive")
	}
	t.em.Resize(cols, rows)
	return setSize(t.ptmx, cols, rows)
}

// Close ends the client and the PTY and waits until every goroutine of the Term has ended. It affects the
// started command only. It may be called more than once.
func (t *Term) Close() error {
	t.closeOnce.Do(func() {
		// Hanging up the PTY ends a client that reads its terminal; the signal covers one that does not.
		_ = t.cmd.Process.Signal(syscall.SIGHUP)
		_ = t.ptmx.Close()
	})
	select {
	case <-t.done:
	case <-time.After(3 * time.Second):
		_ = t.cmd.Process.Kill()
		<-t.done
	}
	return nil
}

var _ io.Writer = (*Term)(nil)
