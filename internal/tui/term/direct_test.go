package term

import (
	"strings"
	"testing"
)

func TestOSCSequencesDoNotReachTheOutput(t *testing.T) {
	tm := startDirect(t, `printf 'a\033]0;TITLE0\007\033]2;TITLE2\033\\\033]52;c;SGVsbG8=\007b\033]8;;http://x.invalid\007link\033]8;;\007\n'; echo END; exec sleep 60`, 40, 5)
	waitFor(t, "END", func() bool { return strings.Contains(plain(tm), "END") })
	view := tm.View(40, 5)
	for _, bad := range []string{"\x1b]", "\x07", "TITLE", "SGVsbG8", "x.invalid", "\x1b\\"} {
		if strings.Contains(view, bad) {
			t.Errorf("view contains %q: %q", bad, view)
		}
	}
	if !strings.Contains(plain(tm), "ablink") {
		t.Errorf("text around the sequences is lost: %q", plain(tm))
	}
}

func TestTerminalQueriesAreAnsweredToTheProgram(t *testing.T) {
	// The program asks for its device attributes and prints the first bytes of the answer. The VT answers
	// on its own pipe, which must reach the program and must not block the parser.
	tm := startDirect(t, `stty raw -echo; printf '\033[c'; dd bs=1 count=3 2>/dev/null | od -An -c; echo END; exec sleep 60`, 40, 5)
	waitFor(t, "END", func() bool { return strings.Contains(plain(tm), "END") })
	if got := plain(tm); !strings.Contains(got, "033") || !strings.Contains(got, "[") || !strings.Contains(got, "?") {
		t.Errorf("answer to the query did not arrive: %q", got)
	}
}

func TestChildEnvironment(t *testing.T) {
	t.Setenv("TMUX", "/tmp/x,1,0")
	t.Setenv("TMUX_PANE", "%1")
	t.Setenv("TERM", "dumb")
	t.Setenv("COLORTERM", "")
	tm := startDirect(t, `echo "T[${TMUX-unset}] P[${TMUX_PANE-unset}] E[$TERM] C[$COLORTERM]"; exec sleep 60`, 60, 3)
	waitFor(t, "output", func() bool { return strings.Contains(plain(tm), "C[") })
	if got := plain(tm); !strings.Contains(got, "T[unset] P[unset] E[xterm-256color] C[truecolor]") {
		t.Errorf("unexpected environment: %q", got)
	}
}

func TestExitDeliversExitedMsgAndEndsGoroutines(t *testing.T) {
	tm := startDirect(t, `echo bye; exit 3`, 40, 5)
	var sawFrame bool
	for {
		switch m := tm.Next()().(type) {
		case FrameMsg:
			if m.ID != tm.ID() {
				t.Fatalf("frame of another instance: %d", m.ID)
			}
			sawFrame = true
			continue
		case ExitedMsg:
			if m.ID != tm.ID() || m.Code != 3 {
				t.Fatalf("unexpected exit message: %+v", m)
			}
		}
		break
	}
	if !sawFrame {
		t.Error("no frame before the exit")
	}
	if !strings.Contains(plain(tm), "bye") {
		t.Errorf("last output missing: %q", plain(tm))
	}
	if err := tm.Close(); err != nil {
		t.Fatal(err)
	}
	assertEnded(t, tm)
}

func TestCloseEndsARunningClientAndGoroutines(t *testing.T) {
	tm := startDirect(t, `exec sleep 60`, 40, 5)
	if err := tm.Close(); err != nil {
		t.Fatal(err)
	}
	assertEnded(t, tm)
	// One last frame may be pending before the exit message.
	if _, ok := tm.Next()().(FrameMsg); ok {
		if _, ok := tm.Next()().(ExitedMsg); !ok {
			t.Error("no exit message after Close")
		}
	}
	if err := tm.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestStartRejectsBadArguments(t *testing.T) {
	if _, err := Start(nil, 10, 10); err == nil {
		t.Error("empty command accepted")
	}
	if _, err := Start([]string{"sh"}, 0, 10); err == nil {
		t.Error("zero width accepted")
	}
}
