//go:build integration

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

const codexAgentID = "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"

// fakeCodexCat puts a codex on the PATH that prints a line and then echoes its input, so that what is typed
// into its window shows on the screen.
func fakeCodexCat(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\necho fake codex ready\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
}

func (r *inboxRig) newCodex() control.Session {
	r.t.Helper()
	var s control.Session
	if resp := r.call(control.OpSessionStart, control.SessionStartArgs{Project: "app", Tool: "codex"}, &s); !resp.OK {
		r.t.Fatalf("start: %+v", resp)
	}
	return s
}

func (r *inboxRig) status(s control.Session) control.Session {
	r.t.Helper()
	var list control.SessionListResult
	if resp := r.call(control.OpSessionList, control.SessionListArgs{}, &list); !resp.OK {
		r.t.Fatalf("list: %+v", resp)
	}
	for _, x := range list.Sessions {
		if x.ID == s.ID {
			return x
		}
	}
	r.t.Fatalf("session %s is not listed", s.ID)
	return control.Session{}
}

func (r *inboxRig) pushCodex(s control.Session, source string, ev control.EventPushArgs) control.Response {
	r.t.Helper()
	ev.Session, ev.Source = s.ID, source
	return r.call(control.OpEventPush, ev, nil)
}

func TestCodexSessionOverTheChannel(t *testing.T) {
	fakeCodexCat(t)
	clock := testenv.NewFakeClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	r := newInboxRig(t, clock)
	s := r.newCodex()
	if s.Status != "idle" || s.Signals != control.SignalsFallback {
		t.Fatalf("a started session: %+v", s)
	}
	r.waitFor("the tool on the screen", func() bool { return strings.Contains(r.screen(s), "fake codex ready") })
	hook := func(ev control.EventPushArgs) control.Response { return r.pushCodex(s, control.EventSourceHook, ev) }

	// The session id of the tool is bound by the first event that carries it, and rebound when it changes.
	if resp := hook(control.EventPushArgs{Event: "UserPromptSubmit", AgentSession: codexAgentID}); !resp.OK {
		t.Fatalf("prompt: %+v", resp)
	}
	bound := func(want string) bool {
		out, _ := r.srv.Run("list-windows", "-a", "-F", "#{@qcrew-session} [#{@qcrew-agent-session}]")
		return strings.Contains(out, s.ID+" ["+want+"]\n")
	}
	if !bound(codexAgentID) {
		t.Errorf("the tool session id is not in the window")
	}
	other := "1a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"
	hook(control.EventPushArgs{Event: "PreModelRequest", AgentSession: other})
	if !bound(other) {
		t.Errorf("the new tool session id is not in the window")
	}
	// An id out of form is refused, and nothing is bound.
	if resp := hook(control.EventPushArgs{Event: "PreModelRequest", AgentSession: "not-an-id"}); resp.OK || resp.Code != control.CodeInvalidArgument {
		t.Errorf("an invalid tool session id: %+v", resp)
	}
	if !bound(other) {
		t.Errorf("an invalid id changed the binding")
	}

	// A permission request answered through the inbox: the words are typed, not mapped to keys.
	hook(control.EventPushArgs{Event: "PermissionRequest", Tool: "shell", Text: "ls"})
	e := r.openEntry()
	if e.Kind != "permission" || e.Text != "ls" || r.status(s).Status != "waiting" {
		t.Fatalf("permission: %+v, status %s", e, r.status(s).Status)
	}
	if resp := r.call(control.OpInboxAnswer, control.InboxAnswerArgs{ID: e.ID, Text: "yes"}, nil); !resp.OK {
		t.Fatalf("answer: %+v", resp)
	}
	r.waitFor("the word yes on the screen", func() bool { return strings.Contains(r.screen(s), "\nyes\n") })
	if r.status(s).Status != "waiting" {
		t.Errorf("with hooks the answer alone does not end the wait; status %s", r.status(s).Status)
	}
	hook(control.EventPushArgs{Event: "PostToolUse", Tool: "shell"})
	if r.entry(e.ID).Status != "picked" || r.status(s).Status != "running" {
		t.Errorf("after PostToolUse: entry %s, status %s", r.entry(e.ID).Status, r.status(s).Status)
	}

	// A failed model request is an error after ten seconds without a further event.
	hook(control.EventPushArgs{Event: "PostModelRequest", Type: "failed", Text: "rate limited"})
	clock.Advance(failedRequestAfter + time.Second)
	r.waitFor("the status error", func() bool { return r.status(s).Status == "error" })
	if e := r.openEntry(); e.Kind != "error" {
		t.Errorf("entry for the failed request: %+v", e)
	}
	hook(control.EventPushArgs{Event: "UserPromptSubmit"})
	if r.status(s).Status != "running" {
		t.Errorf("after a new prompt: %s", r.status(s).Status)
	}
	// ... and is no error if an event follows in time.
	hook(control.EventPushArgs{Event: "PostModelRequest", Type: "failed"})
	clock.Advance(5 * time.Second)
	hook(control.EventPushArgs{Event: "PreModelRequest"})
	clock.Advance(time.Minute)
	time.Sleep(1500 * time.Millisecond) // the sweep has looked by now
	if st := r.status(s).Status; st != "running" {
		t.Errorf("a retried request ended in %s", st)
	}

	// Notify events are fallback signals of Codex only.
	if resp := r.pushCodex(s, control.EventSourceNotify, control.EventPushArgs{Event: "agent-turn-complete"}); !resp.OK {
		t.Errorf("notify: %+v", resp)
	}
	if st := r.status(s).Status; st != "running" {
		t.Errorf("a notify event changed the status of a session with hooks to %s", st)
	}
	var c control.Session
	if resp := r.call(control.OpSessionStart, control.SessionStartArgs{Project: "app", Tool: "claude"}, &c); !resp.OK {
		t.Fatalf("start claude: %+v", resp)
	}
	if resp := r.pushCodex(c, control.EventSourceNotify, control.EventPushArgs{Event: "agent-turn-complete"}); resp.OK || resp.Code != control.CodeInvalidArgument {
		t.Errorf("a notify event for Claude: %+v", resp)
	}
	if got := r.status(s); got.Signals != control.SignalsHooks {
		t.Errorf("signals: %q", got.Signals)
	}
}
