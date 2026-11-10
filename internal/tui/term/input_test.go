package term

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// echoBytes is a program that asks for bracketed paste and for mouse reports, then prints every byte it reads
// as <hex>.
const echoBytes = `stty raw -echo opost; printf '\033[?2004h\033[?1000h\033[?1006h'; echo READY; ` +
	`while :; do h=$(dd bs=1 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'); printf '<%s>' "$h"; done`

// hexOf spells bytes the way echoBytes prints them.
func hexOf(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(&b, "<%02x>", s[i])
	}
	return b.String()
}

// received is what echoBytes printed after READY, without the line breaks of the screen.
func received(tm *Term) string {
	text := plain(tm)
	_, after, _ := strings.Cut(text, "READY")
	return strings.NewReplacer("\n", "", " ", "").Replace(after)
}

func waitReceived(t *testing.T, tm *Term, want string) {
	t.Helper()
	waitFor(t, "the bytes "+want, func() bool { return received(tm) == want })
}

func keyOf(s string) tea.KeyPressMsg {
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func TestInputReachesTheProgramInOrder(t *testing.T) {
	tm := startDirect(t, echoBytes, 120, 10)
	waitFor(t, "READY", func() bool { return strings.Contains(plain(tm), "READY") })

	tm.SendKey(keyOf("a"))
	tm.SendKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	tm.SendKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	tm.SendKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	// A paste comes once, between the markers the program asked for.
	tm.Paste("l1\nl2")
	tm.SendKey(keyOf("b"))
	want := hexOf("a\x1b\x03\r\x1b[200~l1\nl2\x1b[201~b")
	waitReceived(t, tm, want)
}

func TestMouseReachesTheProgramAsAsked(t *testing.T) {
	tm := startDirect(t, echoBytes, 120, 10)
	waitFor(t, "READY", func() bool { return strings.Contains(plain(tm), "READY") })

	tm.SendMouse(tea.MouseClickMsg{Button: tea.MouseLeft}, 4, 2)
	tm.SendMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft}, 4, 2)
	tm.SendMouse(tea.MouseWheelMsg{Button: tea.MouseWheelUp}, 0, 0)
	tm.SendMouse(tea.MouseMotionMsg{Button: tea.MouseLeft}, 9, 9)
	tm.SendKey(keyOf("z"))
	want := hexOf("\x1b[<0;5;3M\x1b[<0;5;3m\x1b[<64;1;1M") + hexOf("z")
	waitReceived(t, tm, want)
}

// A program that does not ask for the mouse gets nothing, and one that does not ask for bracketed paste gets
// the text alone.
func TestInputFollowsTheModesOfTheProgram(t *testing.T) {
	plainMode := strings.Replace(echoBytes, `printf '\033[?2004h\033[?1000h\033[?1006h'; `, "", 1)
	tm := startDirect(t, plainMode, 120, 10)
	waitFor(t, "READY", func() bool { return strings.Contains(plain(tm), "READY") })
	tm.SendMouse(tea.MouseClickMsg{Button: tea.MouseLeft}, 1, 1)
	tm.Paste("x")
	tm.SendKey(keyOf("z"))
	waitReceived(t, tm, hexOf("xz"))
}

// A program that never reads must not hold the caller or the screen: the paste is far larger than the PTY
// takes, and Close still ends everything, also while a process that outlives the client keeps the PTY open.
func TestHugePasteNeverBlocks(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the PTY stays blocking outside Linux, where Close cannot interrupt a stuck write")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	tm := startDirect(t, `trap '' HUP; stty raw -echo; sleep 30 & echo $! > "`+pidFile+`"; echo READY; wait`, 80, 5)
	waitFor(t, "READY", func() bool { return strings.Contains(plain(tm), "READY") })
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tm.Paste(strings.Repeat("0123456789abcdef", 1<<16)) // 1 MiB
		tm.SendKey(keyOf("a"))
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Paste blocked")
	}
	// The screen still takes output while the input is stuck.
	_, _ = tm.em.Write([]byte("\x1b[2J\x1b[Hstill alive"))
	if !strings.Contains(plain(tm), "still alive") {
		t.Error("the VT is held by the stuck input")
	}
	closed := make(chan error, 1)
	go func() { closed <- tm.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close hangs on the stuck input")
	}
	assertEnded(t, tm)
}

// A child that echoes what it reads while the paste is larger than both PTY buffers must not deadlock the
// two directions.
func TestPasteThroughAnEchoingProgramDoesNotDeadlock(t *testing.T) {
	tm := startDirect(t, `stty raw -echo opost; exec cat`, 80, 5)
	var text strings.Builder
	for i := range 20000 {
		fmt.Fprintf(&text, "line %05d\n", i)
	}
	text.WriteString("END")
	tm.Paste(text.String())
	waitFor(t, "the end of the paste", func() bool { return strings.Contains(plain(tm), "END") })
}

func TestInputAfterTheEndIsDropped(t *testing.T) {
	tm := startDirect(t, `echo bye`, 40, 5)
	for {
		if _, ok := tm.Next()().(ExitedMsg); ok {
			break
		}
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				tm.SendKey(keyOf("a"))
				tm.Paste("text")
				tm.SendMouse(tea.MouseClickMsg{Button: tea.MouseLeft}, 1, 1)
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("input after the end blocks")
	}
	if err := tm.Close(); err != nil {
		t.Fatal(err)
	}
	assertEnded(t, tm)
}
