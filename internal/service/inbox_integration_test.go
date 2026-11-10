//go:build integration

package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/inbox"
	"github.com/castrowithcee/qatlas-crew/internal/session"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// fakeClaudeCat puts a claude on the PATH that prints a line and then echoes its input, so that what is typed
// into its window shows on the screen, and that ends on end of input.
func fakeClaudeCat(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\necho fake claude ready\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
}

type inboxRig struct {
	t   *testing.T
	c   *conn
	srv *testenv.TmuxServer
	p   control.Project
}

func (r *inboxRig) call(op string, args any, result any) control.Response {
	r.t.Helper()
	resp := r.c.callWith(r.t, op, args)
	if resp.OK && result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			r.t.Fatalf("%s: %v: %s", op, err, resp.Result)
		}
	}
	return resp
}

func (r *inboxRig) newSession() control.Session {
	r.t.Helper()
	var s control.Session
	if resp := r.call(control.OpSessionStart, control.SessionStartArgs{Project: "app", Tool: "claude"}, &s); !resp.OK {
		r.t.Fatalf("start: %+v", resp)
	}
	return s
}

func (r *inboxRig) push(s control.Session, ev control.EventPushArgs) {
	r.t.Helper()
	ev.Session, ev.Source = s.ID, control.EventSourceHook
	if resp := r.call(control.OpEventPush, ev, nil); !resp.OK {
		r.t.Fatalf("push %s: %+v", ev.Event, resp)
	}
}

func (r *inboxRig) entries(all bool) []control.InboxEntry {
	r.t.Helper()
	var list control.InboxListResult
	if resp := r.call(control.OpInboxList, control.InboxListArgs{All: all}, &list); !resp.OK {
		r.t.Fatalf("inbox list: %+v", resp)
	}
	return list.Entries
}

// openEntry returns the one open entry.
func (r *inboxRig) openEntry() control.InboxEntry {
	r.t.Helper()
	var open []control.InboxEntry
	for _, e := range r.entries(false) {
		if e.Status == "open" {
			open = append(open, e)
		}
	}
	if len(open) != 1 {
		r.t.Fatalf("%d open entries, want 1: %+v", len(open), open)
	}
	return open[0]
}

func (r *inboxRig) entry(id string) control.InboxEntry {
	r.t.Helper()
	var e control.InboxEntry
	if resp := r.call(control.OpInboxGet, control.InboxRefArgs{ID: id}, &e); !resp.OK {
		r.t.Fatalf("inbox get: %+v", resp)
	}
	return e
}

func (r *inboxRig) screen(s control.Session) string {
	out, _ := r.srv.Run("capture-pane", "-p", "-t", "="+session.SessionName(r.p.ID)+":"+session.WindowName(s.ID))
	return out
}

func (r *inboxRig) openCount() int {
	r.t.Helper()
	var list control.ProjectListResult
	if resp := r.call(control.OpProjectList, nil, &list); !resp.OK || len(list.Projects) != 1 {
		r.t.Fatalf("project list: %+v", resp)
	}
	return list.Projects[0].InboxOpen
}

func (r *inboxRig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func newInboxRig(t *testing.T, clock *testenv.FakeClock) *inboxRig {
	t.Helper()
	srv := testenv.Tmux(t)
	fakeClaudeCat(t)
	t.Setenv("SHELL", "/bin/sh")
	r := start(t, Options{tmux: &session.Server{Socket: srv.Socket()}, now: clock.Now,
		Program: func() (string, error) { return "/opt/qcrew/bin/qcrew", nil }})
	rig := &inboxRig{t: t, c: r.dial(t), srv: srv}
	dir := filepath.Join(r.home, "work", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if resp := rig.call(control.OpProjectAdd, control.ProjectAddArgs{Path: dir}, &rig.p); !resp.OK {
		t.Fatalf("project add: %+v", resp)
	}
	t.Cleanup(func() { _, _ = srv.Run("kill-session", "-t", "="+session.SessionName(rig.p.ID)) })
	return rig
}

func TestInboxFromWaitStates(t *testing.T) {
	clock := testenv.NewFakeClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	r := newInboxRig(t, clock)
	s := r.newSession()
	r.waitFor("the tool's first output", func() bool { return strings.Contains(r.screen(s), "fake claude ready") })

	// A permission request makes one entry with the last screen lines.
	r.push(s, control.EventPushArgs{Event: "PermissionRequest", Tool: "Bash"})
	list := r.entries(false)
	if len(list) != 1 || list[0].Kind != "permission" || list[0].Text != "Bash" || list[0].Status != "open" ||
		list[0].Project != r.p.ID || list[0].Session != s.ID || !list[0].CreatedAt.Equal(clock.Now()) ||
		!strings.Contains(strings.Join(list[0].Lines, "\n"), "fake claude ready") || len(list[0].Lines) > inbox.MaxLines {
		t.Fatalf("after a permission request: %+v", list)
	}
	first := list[0]
	if n := r.openCount(); n != 1 {
		t.Errorf("project inbox_open = %d, want 1", n)
	}
	// The notification for the same prompt, and a second request, make no second entry.
	r.push(s, control.EventPushArgs{Event: "Notification", Type: "permission_prompt", Text: "Claude needs your permission"})
	r.push(s, control.EventPushArgs{Event: "PermissionRequest", Tool: "Bash"})
	if got := r.entries(true); len(got) != 1 || got[0].ID != first.ID {
		t.Fatalf("a further wait state made another entry: %+v", got)
	}

	// An answer with a line break arrives as one line, and the entry is delivered, not yet picked.
	var done control.InboxEntry
	if resp := r.call(control.OpInboxAnswer, control.InboxAnswerArgs{ID: first.ID, Text: "multi\nline\x1b[0m answer"}, &done); !resp.OK {
		t.Fatalf("answer: %+v", resp)
	}
	if done.Status != "delivered" || done.Answer != "multi line[0m answer" || done.AnsweredAt == nil {
		t.Errorf("answered entry: %+v", done)
	}
	r.waitFor("the answer on the screen", func() bool { return strings.Contains(r.screen(s), "multi line[0m answer") })
	if strings.Contains(r.screen(s), "multi\n") || strings.Contains(r.screen(s), "\x1b") {
		t.Errorf("the answer did not arrive as one plain line:\n%s", r.screen(s))
	}
	if n := r.openCount(); n != 0 {
		t.Errorf("project inbox_open = %d after the answer, want 0", n)
	}
	// An answered entry cannot be answered or replaced; a second answer is refused.
	if resp := r.call(control.OpInboxAnswer, control.InboxAnswerArgs{ID: first.ID, Text: "again"}, nil); resp.OK || resp.Code != control.CodeInvalidArgument {
		t.Errorf("a second answer: %+v", resp)
	}
	// The session works on: picked.
	r.push(s, control.EventPushArgs{Event: "PostToolUse", Tool: "Bash"})
	if got := r.entry(first.ID); got.Status != "picked" {
		t.Errorf("after the session worked on: %s", got.Status)
	}
	if got := r.entries(false); len(got) != 0 {
		t.Errorf("the default list holds a picked entry: %+v", got)
	}

	// A predefined answer of a permission entry presses a key and does not type the word.
	r.push(s, control.EventPushArgs{Event: "PermissionRequest", Tool: "Edit"})
	perm := r.openEntry()
	if resp := r.call(control.OpInboxAnswer, control.InboxAnswerArgs{ID: perm.ID, Text: "yes"}, nil); !resp.OK {
		t.Fatalf("yes: %+v", resp)
	}
	r.waitFor("the key on the screen", func() bool {
		for _, l := range strings.Split(r.screen(s), "\n") {
			if l == "1" {
				return true
			}
		}
		return false
	})
	if strings.Contains(r.screen(s), "yes") {
		t.Errorf("the word yes was typed:\n%s", r.screen(s))
	}
	// The same word for a question is text.
	r.push(s, control.EventPushArgs{Event: "PostToolUse"})
	r.push(s, control.EventPushArgs{Event: "PreToolUse", Tool: "AskUserQuestion"})
	q := r.openEntry()
	if q.Kind != "question" || q.Text != "AskUserQuestion" {
		t.Fatalf("question entry: %+v", q)
	}
	if resp := r.call(control.OpInboxAnswer, control.InboxAnswerArgs{ID: q.ID, Text: "yes"}, nil); !resp.OK {
		t.Fatalf("answer yes to a question: %+v", resp)
	}
	r.waitFor("the word on the screen", func() bool { return strings.Contains(r.screen(s), "yes") })

	// An entry answered in the session itself closes when the session works on.
	r.push(s, control.EventPushArgs{Event: "Stop"})
	r.push(s, control.EventPushArgs{Event: "PermissionRequest", Tool: "Bash"})
	direct := r.openEntry()
	r.push(s, control.EventPushArgs{Event: "PostToolUse"})
	if got := r.entry(direct.ID); got.Status != "closed" || got.Answer != "" {
		t.Errorf("an entry answered in the session: %+v", got)
	}

	// Refusals.
	for _, rf := range []struct {
		name string
		resp control.Response
		code string
	}{
		{"unknown entry", r.call(control.OpInboxAnswer, control.InboxAnswerArgs{ID: "zzzzzzzzzzz2", Text: "x"}, nil), control.CodeNotFound},
		{"bad id", r.call(control.OpInboxGet, control.InboxRefArgs{ID: "../x"}, nil), control.CodeInvalidArgument},
		{"empty answer", r.call(control.OpInboxAnswer, control.InboxAnswerArgs{ID: first.ID, Text: " \n\x1b"}, nil), control.CodeInvalidArgument},
		{"close a picked entry", r.call(control.OpInboxClose, control.InboxRefArgs{ID: first.ID}, nil), control.CodeInvalidArgument},
		{"unknown project", r.c.callWith(t, control.OpInboxList, control.InboxListArgs{Project: "nope"}), control.CodeNotFound},
		{"unknown field", r.c.callWith(t, control.OpInboxList, map[string]any{"x": 1}), control.CodeInvalidRequest},
	} {
		if rf.resp.OK || rf.resp.Code != rf.code {
			t.Errorf("%s: %+v, want code %s", rf.name, rf.resp, rf.code)
		}
	}

	// Stopping the session closes its open entries.
	r.push(s, control.EventPushArgs{Event: "PermissionRequest", Tool: "Bash"})
	open := r.entries(false)
	if len(open) != 1 {
		t.Fatalf("before the stop: %+v", open)
	}
	if resp := r.call(control.OpSessionStop, control.SessionRefArgs{ID: s.ID}, nil); !resp.OK {
		t.Fatalf("stop: %+v", resp)
	}
	if got := r.entry(open[0].ID); got.Status != "closed" {
		t.Errorf("an open entry after the stop: %s", got.Status)
	}
	if n := r.openCount(); n != 0 {
		t.Errorf("inbox_open after the stop: %d", n)
	}
	if resp := r.call(control.OpInboxClose, control.InboxRefArgs{ID: open[0].ID}, nil); resp.OK || resp.Code != control.CodeInvalidArgument {
		t.Errorf("closing a closed entry: %+v", resp)
	}
}

func TestInboxIdleAndReplacement(t *testing.T) {
	clock := testenv.NewFakeClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	r := newInboxRig(t, clock)
	s := r.newSession()
	r.push(s, control.EventPushArgs{Event: "Stop"})
	if got := r.entries(false); len(got) != 0 {
		t.Fatalf("an idle session made an entry at once: %+v", got)
	}
	// The sweep of the service runs every 2 s on the real clock and reads the injected one.
	clock.Advance(idleAfter + time.Second)
	r.waitFor("the idle entry", func() bool { return len(r.entries(false)) == 1 })
	idle := r.entries(false)[0]
	if idle.Kind != "idle" || idle.Status != "open" || idle.Session != s.ID {
		t.Fatalf("idle entry: %+v", idle)
	}
	// One entry for the idle phase: closed by hand, it does not come back, and an idle_prompt makes none.
	if resp := r.call(control.OpInboxClose, control.InboxRefArgs{ID: idle.ID}, nil); !resp.OK {
		t.Fatalf("close: %+v", resp)
	}
	clock.Advance(time.Hour)
	time.Sleep(2500 * time.Millisecond)
	r.push(s, control.EventPushArgs{Event: "Notification", Type: "idle_prompt"})
	if got := r.entries(false); len(got) != 0 {
		t.Errorf("the same idle phase made another entry: %+v", got)
	}
	// A new phase starts with a new prompt and ends with one entry; a permission request replaces it.
	r.push(s, control.EventPushArgs{Event: "UserPromptSubmit"})
	r.push(s, control.EventPushArgs{Event: "Stop"})
	r.push(s, control.EventPushArgs{Event: "Notification", Type: "idle_prompt", Text: "waiting for input"})
	idle2 := r.entries(false)
	if len(idle2) != 1 || idle2[0].Kind != "idle" || idle2[0].Text != "waiting for input" {
		t.Fatalf("idle_prompt entry: %+v", idle2)
	}
	r.push(s, control.EventPushArgs{Event: "PermissionRequest", Tool: "Bash"})
	got := r.entries(false)
	if len(got) != 1 || got[0].Kind != "permission" || r.entry(idle2[0].ID).Status != "closed" {
		t.Errorf("a permission request next to an idle entry: %+v, idle entry %s", got, r.entry(idle2[0].ID).Status)
	}
	if resp := r.call(control.OpSessionStop, control.SessionRefArgs{ID: s.ID}, nil); !resp.OK {
		t.Fatalf("stop: %+v", resp)
	}
}

func TestInboxErrorEntryAndGoneSession(t *testing.T) {
	clock := testenv.NewFakeClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	r := newInboxRig(t, clock)
	s := r.newSession()
	r.waitFor("the tool's first output", func() bool { return strings.Contains(r.screen(s), "fake claude ready") })
	r.push(s, control.EventPushArgs{Event: "Stop"})
	// The tool ends (end of input) without a SessionEnd.
	target := "=" + session.SessionName(r.p.ID) + ":" + session.WindowName(s.ID)
	if _, err := r.srv.Run("send-keys", "-t", target, "C-d"); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the error entry", func() bool { return len(r.entries(false)) == 1 })
	e := r.entries(false)[0]
	if e.Kind != "error" || e.Status != "open" || len(e.Lines) == 0 {
		t.Fatalf("error entry: %+v", e)
	}
	// The window has ended: the answer fails and the entry stays open.
	resp := r.call(control.OpInboxAnswer, control.InboxAnswerArgs{ID: e.ID, Text: "hello"}, nil)
	if resp.OK || resp.Code != control.CodeSessionGone {
		t.Fatalf("answer for an ended session: %+v", resp)
	}
	if got := r.entry(e.ID); got.Status != "open" || got.Answer != "" {
		t.Errorf("entry after the failed answer: %+v", got)
	}
	if resp := r.call(control.OpInboxClose, control.InboxRefArgs{ID: e.ID}, nil); !resp.OK {
		t.Errorf("close: %+v", resp)
	}
	r.call(control.OpSessionStop, control.SessionRefArgs{ID: s.ID}, nil)

	// An entry whose session has vanished from tmux: the same failure, no delivery to another window.
	s2 := r.newSession()
	r.push(s2, control.EventPushArgs{Event: "PermissionRequest", Tool: "Bash"})
	other := r.newSession()
	if _, err := r.srv.Run("kill-window", "-t", "="+session.SessionName(r.p.ID)+":"+session.WindowName(s2.ID)); err != nil {
		t.Fatal(err)
	}
	entry := r.entries(false)[0]
	if resp := r.call(control.OpInboxAnswer, control.InboxAnswerArgs{ID: entry.ID, Text: "hello"}, nil); resp.OK || resp.Code != control.CodeSessionGone {
		t.Errorf("answer for a vanished session: %+v", resp)
	}
	if strings.Contains(r.screen(other), "hello") {
		t.Error("the answer reached another session's window")
	}
	r.call(control.OpSessionStop, control.SessionRefArgs{ID: other.ID}, nil)
}
