package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

func TestQueriesAndScreen(t *testing.T) {
	e := vt.NewEmulator(40, 4)
	var seen observation
	seen.observe(e)
	responses := make(chan []byte, 1)
	go func() {
		var output bytes.Buffer
		_, _ = io.Copy(&output, e)
		responses <- output.Bytes()
	}()
	_, err := e.Write([]byte("Hello\x1b[c\x1b[>c\x1b[6n\x1b]10;?\a\x1b]11;?\a\x1b[?u\x1b[>q\x1b[?2004h\x1b[?1000h"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(e.String()); got != "Hello" {
		t.Fatalf("screen = %q", got)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-responses:
		for _, want := range []string{"\x1b[?62;1;6;22c", "\x1b[>1;10;0c", "\x1b[1;6R", "\x1b]10;", "\x1b]11;", "\x1b[?0u", "\x1bP>|crew-prototype(0)\x1b\\"} {
			if !bytes.Contains(reply, []byte(want)) {
				t.Errorf("missing response %q in %q", want, reply)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("query responses blocked")
	}
	if got := strings.Join(seen.modes, "\n"); !strings.Contains(got, "2004") || !strings.Contains(got, "1000") {
		t.Errorf("missing modes: %s", got)
	}
}

func TestExpectedResponseAfterInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("synthetic host uses a POSIX shell")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", dir)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		initial string
		answer  string
		want    bool
	}{
		{"answer", "", "ANSWER_ONLY", true},
		{"no answer", "", "different", false},
		{"preexisting marker", "ANSWER_ONLY", "different", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldArgs := os.Args
			defer func() { os.Args = oldArgs }()
			os.Args = []string{"pty-emulator", "-dir", dir, "-duration", "4s", "-input", "synthetic prompt", "-expect", "ANSWER_ONLY", "--", "sh", "-c", "printf '%s\\n' " + tc.initial + "; IFS= read -r line; printf '%s\\n' " + tc.answer}
			var output bytes.Buffer
			if err := run(&output); err != nil {
				t.Fatal(err)
			}
			got := output.String()
			if !strings.Contains(got, "expect_seen="+strconv.FormatBool(tc.want)) {
				t.Fatalf("unexpected result: %s", got)
			}
			if strings.Contains(got, "expect_after=") != tc.want {
				t.Fatalf("unexpected timing: %s", got)
			}
			if strings.Contains(got, "ANSWER_ONLY") || strings.Contains(got, "synthetic prompt") {
				t.Fatalf("marker or input leaked: %s", got)
			}
		})
	}
}
