package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

var inboxClock = time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC)

var sampleInbox = []control.InboxEntry{
	{ID: "eeeeeeeeeee2", Project: "abcdefghijk2", Session: "aaaaaaaaaaa2", Kind: "permission", Text: "Bash",
		Lines:     []string{"Bash command", "  ls -la", "Do you want to proceed?", "❯ 1. Yes"},
		CreatedAt: time.Date(2026, 1, 2, 3, 59, 15, 0, time.UTC), Status: "open"},
	{ID: "fffffffffff3", Project: "mnopqrstuvw7", Session: "bbbbbbbbbbb3", Kind: "question",
		Text:  "Which database should the migration use, and should it keep the old tables or drop them after the copy?",
		Lines: []string{"Which database?"}, CreatedAt: time.Date(2026, 1, 2, 1, 0, 0, 0, time.UTC), Status: "delivered",
		Answer: "postgres", AnsweredAt: new(time.Date(2026, 1, 2, 3, 30, 0, 0, time.UTC))},
	{ID: "ggggggggggg4", Project: "abcdefghijk2", Session: "aaaaaaaaaaa2", Kind: "idle", Text: "the session waits for input",
		CreatedAt: time.Date(2025, 12, 28, 3, 0, 0, 0, time.UTC), Status: "closed"},
}

func TestInboxOutputGolden(t *testing.T) {
	for _, format := range []output.Format{output.FormatTable, output.FormatJSON} {
		var list, empty, show, answered bytes.Buffer
		if err := writeInbox(&list, format, sampleInbox, inboxClock); err != nil {
			t.Fatal(err)
		}
		if err := writeInbox(&empty, format, nil, inboxClock); err != nil {
			t.Fatal(err)
		}
		if err := writeInboxEntry(&show, format, sampleInbox[0]); err != nil {
			t.Fatal(err)
		}
		if err := output.Encode(&answered, format, inboxObject(sampleInbox[1], false)); err != nil {
			t.Fatal(err)
		}
		golden(t, "inbox-list."+string(format), list.Bytes())
		golden(t, "inbox-list-empty."+string(format), empty.Bytes())
		golden(t, "inbox-show."+string(format), show.Bytes())
		golden(t, "inbox-answer."+string(format), answered.Bytes())
	}
}

// Text and screen lines come from the session's tool: no control character reaches the output.
func TestInboxOutputMasksForeignText(t *testing.T) {
	hostile := control.InboxEntry{ID: "eeeeeeeeeee2", Project: "p", Session: "s", Kind: "permission",
		Text: "ok\x1b[2J\x1b]0;x\x07 done", Lines: []string{"\x1b[31mred\x00", "next\r\nline"},
		CreatedAt: inboxClock, Status: "open", Answer: "a\x1bb"}
	for _, format := range []output.Format{output.FormatTable, output.FormatJSON, output.FormatCompact} {
		var buf bytes.Buffer
		if err := writeInbox(&buf, format, []control.InboxEntry{hostile}, inboxClock); err != nil {
			t.Fatal(err)
		}
		if err := writeInboxEntry(&buf, format, hostile); err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(buf.String(), "\x1b\x00\x07\r") {
			t.Errorf("%s output holds a control character: %q", format, buf.String())
		}
	}
}

func TestAge(t *testing.T) {
	for d, want := range map[time.Duration]string{-time.Second: "0s", 0: "0s", 59 * time.Second: "59s", time.Minute: "1m",
		59 * time.Minute: "59m", time.Hour: "1h", 47 * time.Hour: "47h", 48 * time.Hour: "2d", 100 * time.Hour: "4d"} {
		if got := age(d); got != want {
			t.Errorf("age(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestInboxCommandsNeedTheirArguments(t *testing.T) {
	testenv.Setup(t)
	for _, args := range [][]string{{"inbox", "answer", "eeeeeeeeeee2"}, {"inbox", "answer", "--text", "x"}, {"inbox", "show"},
		{"inbox", "close"}, {"inbox", "list", "extra"}} {
		code, out, errOut := run(t, append([]string{"--no-autostart"}, args...)...)
		if code != 2 || out != "" || !strings.Contains(errOut, "usage") {
			t.Errorf("%v: code=%d out=%q err=%q", args, code, out, errOut)
		}
	}
}
