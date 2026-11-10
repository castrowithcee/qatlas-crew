package service

import (
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/session"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

var t0 = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

func newTestTracker() (*tracker, *testenv.FakeClock) {
	clock := testenv.NewFakeClock(t0.Add(time.Minute))
	return newTracker(clock.Now, slog.New(slog.NewTextHandler(io.Discard, nil))), clock
}

func claudeSession(status string) session.Session {
	return session.Session{ID: "aaaaaaaaaaa2", Project: "p", Tool: adapter.Claude, Title: "claude", Status: status, Started: t0}
}

func event(name string, mod ...func(*control.EventPushArgs)) control.EventPushArgs {
	ev := control.EventPushArgs{Session: "aaaaaaaaaaa2", Source: control.EventSourceHook, Event: name}
	for _, m := range mod {
		m(&ev)
	}
	return ev
}

func typed(v string) func(*control.EventPushArgs) {
	return func(a *control.EventPushArgs) { a.Type = v }
}
func tooled(v string) func(*control.EventPushArgs) {
	return func(a *control.EventPushArgs) { a.Tool = v }
}
func started(v string) func(*control.EventPushArgs) {
	return func(a *control.EventPushArgs) { a.Start = v }
}

func TestClaudeStatusTable(t *testing.T) {
	cases := []struct {
		ev   control.EventPushArgs
		want string // empty: no change
	}{
		{event("UserPromptSubmit"), statusRunning},
		{event("PostToolUse"), statusRunning},
		{event("PostToolUseFailure"), statusRunning},
		{event("PermissionRequest"), statusWaiting},
		{event("Notification", typed("permission_prompt")), statusWaiting},
		{event("Notification", typed("elicitation_dialog")), statusWaiting},
		{event("Notification", typed("elicitation_url_dialog")), statusWaiting},
		{event("Notification", typed("idle_prompt")), statusIdle},
		{event("Notification", typed("auth_success")), ""},
		{event("Notification", typed("agent_needs_input")), ""},
		{event("Notification"), ""},
		{event("PreToolUse", tooled("AskUserQuestion")), statusWaiting},
		{event("PreToolUse", tooled("Bash")), ""},
		{event("PreToolUse"), ""},
		{event("Stop"), statusIdle},
		{event("SessionStart", started("startup")), statusIdle},
		{event("SessionStart", started("resume")), statusIdle},
		{event("SessionStart", started("clear")), statusIdle},
		{event("SessionStart", started("compact")), ""},
		{event("SessionStart", started("fork")), ""},
		{event("SessionStart"), ""},
		{event("SessionEnd"), statusExited},
		{event(control.EventHookError), statusError},
		{event("SubagentStop"), ""},
		{event("PreCompact"), ""},
		{event("Unknown"), ""},
		{event(control.EventContext), ""},
	}
	for _, c := range cases {
		got, ok := claudeStatus(c.ev)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%+v: %q, %v; want %q", c.ev, got, ok, c.want)
		}
	}
}

func TestEventsMoveTheStatus(t *testing.T) {
	tr, clock := newTestTracker()
	x := claudeSession(statusRunning)
	tr.started(x)
	steps := []struct {
		ev   control.EventPushArgs
		want string
	}{
		{event("SessionStart", started("startup")), statusIdle},
		{event("UserPromptSubmit"), statusRunning},
		{event("PermissionRequest"), statusWaiting},
		{event("PostToolUse"), statusRunning},
		{event("PreToolUse", tooled("AskUserQuestion")), statusWaiting},
		{event("PostToolUse"), statusRunning},
		{event("Stop"), statusIdle},
	}
	var wantHistory []transition
	prev := statusRunning
	for _, s := range steps {
		clock.Advance(10 * time.Second)
		tr.apply(x, s.ev)
		wantHistory = append(wantHistory, transition{At: clock.Now(), From: prev, To: s.want, Event: s.ev.Event})
		prev = s.want
		status, since, _ := tr.view(x)
		if status != s.want || !since.Equal(clock.Now()) {
			t.Fatalf("after %s: %s since %v; want %s since %v", s.ev.Event, status, since, s.want, clock.Now())
		}
	}
	if got := tr.history(x.ID); !slices.Equal(got, wantHistory) {
		t.Errorf("history:\n got %+v\nwant %+v", got, wantHistory)
	}

	// An event that changes nothing keeps the time of the status and adds no change.
	since := clock.Now()
	clock.Advance(time.Minute)
	for _, ev := range []control.EventPushArgs{event("Stop"), event("SessionStart", started("compact")),
		event("Notification", typed("auth_success")), event("SubagentStop")} {
		tr.apply(x, ev)
	}
	if status, at, _ := tr.view(x); status != statusIdle || !at.Equal(since) || len(tr.history(x.ID)) != len(steps) {
		t.Errorf("no-change events: %s since %v, %d changes", status, at, len(tr.history(x.ID)))
	}
}

func TestContextPercent(t *testing.T) {
	tr, clock := newTestTracker()
	x := claudeSession(statusRunning)
	if _, _, c := tr.view(x); c != nil {
		t.Fatalf("context before any report: %d", *c)
	}
	clock.Advance(time.Second)
	for _, p := range []int{12, 0} {
		tr.apply(x, event(control.EventContext, func(a *control.EventPushArgs) { a.ContextPercent = &p }))
		status, since, c := tr.view(x)
		if c == nil || *c != p || status != statusRunning || !since.Equal(t0) {
			t.Errorf("context %d: %v, %s since %v", p, c, status, since)
		}
	}
	// A report without a value keeps the last one, and the view hands out a copy.
	tr.apply(x, event(control.EventContext))
	_, _, c := tr.view(x)
	*c = 99
	if _, _, again := tr.view(x); again == nil || *again != 0 {
		t.Errorf("context after a report without a value: %v", again)
	}
	if len(tr.history(x.ID)) != 0 {
		t.Error("a context report counts as a status change")
	}
}

func TestHookErrorLastsUntilAStatusEvent(t *testing.T) {
	tr, clock := newTestTracker()
	x := claudeSession(statusRunning)
	tr.apply(x, event(control.EventHookError))
	for _, ev := range []control.EventPushArgs{event("SubagentStop"), event("Notification", typed("auth_success")),
		event("SessionStart", started("compact")), event(control.EventContext)} {
		clock.Advance(time.Second)
		tr.apply(x, ev)
		if status, _, _ := tr.view(x); status != statusError {
			t.Fatalf("%s ended the error: %s", ev.Event, status)
		}
	}
	tr.apply(x, event("UserPromptSubmit"))
	if status, _, _ := tr.view(x); status != statusRunning {
		t.Errorf("a valid event left the status at %s", status)
	}
}

func TestSessionEndAndClear(t *testing.T) {
	tr, clock := newTestTracker()
	x := claudeSession(statusRunning)
	tr.started(x)
	clock.Advance(time.Second)
	tr.apply(x, event("SessionEnd"))
	// The tool ends a session on /clear and starts the next one: the new one is idle again.
	clock.Advance(time.Second)
	tr.apply(x, event("SessionStart", started("clear")))
	if status, _, _ := tr.view(x); status != statusIdle {
		t.Errorf("after clear: %s", status)
	}
	clock.Advance(time.Second)
	tr.apply(x, event("SessionEnd"))
	endedAt := clock.Now()
	clock.Advance(time.Second)
	x.Status = statusExited
	tr.observe([]session.Session{x})
	if status, since, _ := tr.view(x); status != statusExited || !since.Equal(endedAt) {
		t.Errorf("exit after SessionEnd: %s since %v, want exited since %v", status, since, endedAt)
	}
}

func TestEndedProgramWithoutSessionEnd(t *testing.T) {
	cases := []struct {
		name  string
		tool  string
		setup func(tr *tracker, x session.Session)
		want  string
	}{
		{"claude started here", adapter.Claude, func(tr *tracker, x session.Session) { tr.started(x) }, statusError},
		{"claude with events", adapter.Claude, func(tr *tracker, x session.Session) { tr.apply(x, event("Stop")) }, statusError},
		{"claude after a hook error", adapter.Claude, func(tr *tracker, x session.Session) { tr.apply(x, event(control.EventHookError)) }, statusError},
		{"claude, nothing seen", adapter.Claude, func(*tracker, session.Session) {}, statusExited},
		{"shell", adapter.Shell, func(tr *tracker, x session.Session) { tr.started(x) }, statusExited},
		{"codex", adapter.Codex, func(*tracker, session.Session) {}, statusExited},
	}
	for _, c := range cases {
		tr, clock := newTestTracker()
		x := claudeSession(statusRunning)
		x.Tool = c.tool
		tr.observe([]session.Session{x})
		c.setup(tr, x)
		clock.Advance(30 * time.Second)
		x.Status = statusExited
		tr.observe([]session.Session{x})
		status, since, _ := tr.view(x)
		if status != c.want {
			t.Errorf("%s: %s, want %s", c.name, status, c.want)
		}
		if c.want == statusExited && c.tool != adapter.Claude || c.name == "claude, nothing seen" {
			if !since.Equal(clock.Now()) {
				t.Errorf("%s: since %v, want the time of the observation %v", c.name, since, clock.Now())
			}
		}
		// A dead program stays dead whatever late events say, and a second look changes nothing.
		clock.Advance(time.Second)
		tr.apply(x, event("UserPromptSubmit"))
		tr.observe([]session.Session{x})
		if again, at, _ := tr.view(x); again != status || !at.Equal(since) {
			t.Errorf("%s: a later event or look changed %s since %v to %s since %v", c.name, status, since, again, at)
		}
	}
}

func TestStatusWithoutEvents(t *testing.T) {
	tr, clock := newTestTracker()
	running := claudeSession(statusRunning)
	shell := claudeSession(statusRunning)
	shell.ID, shell.Tool = "bbbbbbbbbbb3", adapter.Shell
	// A session found after a restart of the service shows what tmux shows, since its start.
	tr.observe([]session.Session{running, shell})
	clock.Advance(time.Hour)
	for _, x := range []session.Session{running, shell} {
		if status, since, c := tr.view(x); status != statusRunning || !since.Equal(t0) || c != nil {
			t.Errorf("%s: %s since %v, context %v", x.Tool, status, since, c)
		}
	}
	// An ended one is seen for the first time at the time of the observation.
	gone := claudeSession(statusExited)
	gone.ID = "ccccccccccc4"
	tr.observe([]session.Session{running, shell, gone})
	if status, since, _ := tr.view(gone); status != statusExited || !since.Equal(clock.Now()) {
		t.Errorf("first seen ended: %s since %v", status, since)
	}
}

func TestVanishedSessionIsForgotten(t *testing.T) {
	tr, _ := newTestTracker()
	x := claudeSession(statusRunning)
	tr.apply(x, event("Stop"))
	tr.observe([]session.Session{x})
	tr.observe(nil)
	if h := tr.history(x.ID); h != nil {
		t.Errorf("history of a gone session: %+v", h)
	}
	if status, _, _ := tr.view(x); status != statusRunning {
		t.Errorf("a session of the same id starts anew, got %s", status)
	}
	tr.apply(x, event("Stop"))
	tr.forget(x.ID)
	if h := tr.history(x.ID); h != nil {
		t.Errorf("history after forget: %+v", h)
	}
}

func TestHistoryIsBounded(t *testing.T) {
	tr, clock := newTestTracker()
	x := claudeSession(statusRunning)
	for i := range 3 * maxHistory {
		clock.Advance(time.Second)
		name := "Stop"
		if i%2 == 1 {
			name = "UserPromptSubmit"
		}
		tr.apply(x, event(name))
	}
	h := tr.history(x.ID)
	if len(h) != maxHistory {
		t.Fatalf("history holds %d changes, want %d", len(h), maxHistory)
	}
	if want := fmt.Sprint(clock.Now()); fmt.Sprint(h[len(h)-1].At) != want {
		t.Errorf("the newest change is not kept")
	}
}
