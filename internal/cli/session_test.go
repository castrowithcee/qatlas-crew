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

var sampleSessions = []control.Session{
	{ID: "aaaaaaaaaaa2", Project: "abcdefghijk2", Tool: "claude", Title: "claude", Status: "running",
		StartedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
	{ID: "bbbbbbbbbbb3", Project: "mnopqrstuvw7", Tool: "shell", Title: "build log", Status: "exited",
		StartedAt: time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)},
}

func TestSessionOutputGolden(t *testing.T) {
	for _, format := range []output.Format{output.FormatTable, output.FormatJSON} {
		var list, empty, one bytes.Buffer
		if err := writeSessions(&list, format, sampleSessions); err != nil {
			t.Fatal(err)
		}
		if err := writeSessions(&empty, format, nil); err != nil {
			t.Fatal(err)
		}
		if err := output.Encode(&one, format, sessionObject(sampleSessions[0])); err != nil {
			t.Fatal(err)
		}
		golden(t, "session-list."+string(format), list.Bytes())
		golden(t, "session-list-empty."+string(format), empty.Bytes())
		golden(t, "session-start."+string(format), one.Bytes())
	}
}

func TestShellLine(t *testing.T) {
	got := shellLine([]string{"prog", "-S", "/tmp/a b/it's", "attach", "-t", "p-x:s-y"})
	if want := `prog -S '/tmp/a b/it'\''s' attach -t p-x:s-y`; got != want {
		t.Errorf("shellLine = %q, want %q", got, want)
	}
}

func TestSessionStopNeedsConfirm(t *testing.T) {
	testenv.Setup(t)
	code, _, errOut := run(t, "session", "stop", "abcdefghijk2")
	if code != 2 || !strings.Contains(errOut, "confirmation-required") {
		t.Fatalf("stop without --confirm: code=%d err=%q", code, errOut)
	}
}

func TestSessionStartNeedsProjectAndTool(t *testing.T) {
	testenv.Setup(t)
	code, _, errOut := run(t, "session", "start", "--tool", "shell")
	if code != 2 || !strings.Contains(errOut, "project") {
		t.Fatalf("start without --project: code=%d err=%q", code, errOut)
	}
}
