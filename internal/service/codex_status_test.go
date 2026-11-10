package service

import (
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/inbox"
	"github.com/castrowithcee/qatlas-crew/internal/session"
)

func codexSession(status string) session.Session {
	return session.Session{ID: "aaaaaaaaaaa3", Project: "p", Tool: adapter.Codex, Title: "codex", Status: status, Started: t0}
}

func notified(name string) control.EventPushArgs {
	ev := event(name)
	ev.Source = control.EventSourceNotify
	return ev
}

func TestCodexStatusTable(t *testing.T) {
	cases := []struct {
		ev   control.EventPushArgs
		want string // empty: no change
	}{
		{event("UserPromptSubmit"), statusRunning},
		{event("PreModelRequest"), statusRunning},
		{event("PreToolUse", tooled("shell")), statusRunning},
		{event("PreToolUse"), statusRunning},
		{event("PostToolUse", tooled("shell")), statusRunning},
		{event("PermissionRequest", tooled("shell")), statusWaiting},
		{event("PreToolUse", tooled(requestUserInputTool)), statusWaiting},
		{event("Stop"), statusIdle},
		{event("Interrupt"), statusIdle},
		{event("PostModelRequest", typed("failed")), ""},
		{event("PostModelRequest", typed("completed")), ""},
		{event("PostModelRequest", typed("cancelled")), ""},
		{event("SessionStart", started("startup")), ""},
		{event("SessionStart", started("resume")), ""},
		{event("SessionEnd"), statusExited},
		{event(control.EventHookError), statusError},
		{event("PreCompact"), ""},
		{event("SubagentStop"), ""},
		{event("Notification", typed("idle_prompt")), ""},
		{event(control.EventContext), ""},
		{notified(codexTurnComplete), statusIdle},
	}
	for _, c := range cases {
		got, ok := codexStatus(c.ev)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%+v: %q, %v; want %q", c.ev, got, ok, c.want)
		}
	}
}

// A PreToolUse with the question tool is waiting and not running, in the tracker as in the inbox.
func TestCodexQuestionIsNotRunning(t *testing.T) {
	tr, _ := newTestTracker()
	x := codexSession(statusRunning)
	tr.apply(x, event("UserPromptSubmit"))
	ch := tr.apply(x, event("PreToolUse", tooled(requestUserInputTool)))
	if ch.To != statusWaiting {
		t.Errorf("the question tool: %+v", ch)
	}
	if kind, _, ok := waitSignal(adapter.Codex, event("PreToolUse", tooled(requestUserInputTool))); !ok || kind != inbox.KindQuestion {
		t.Errorf("inbox kind %q, %v", kind, ok)
	}
	if _, _, ok := waitSignal(adapter.Codex, event("PreToolUse", tooled("AskUserQuestion"))); ok {
		t.Error("the tool of Claude Code asks a question of Codex")
	}
	if kind, text, ok := waitSignal(adapter.Codex, event("PermissionRequest", texted("ls"))); !ok || kind != inbox.KindPermission || text != "ls" {
		t.Errorf("permission: %q %q %v", kind, text, ok)
	}
	for _, ev := range []control.EventPushArgs{event("Notification", typed("idle_prompt")), event("Stop"),
		event("PreToolUse", tooled("shell")), notified(codexTurnComplete)} {
		if _, _, ok := waitSignal(adapter.Codex, ev); ok {
			t.Errorf("%+v asks for an entry", ev)
		}
	}
}

func TestCodexStartsIdle(t *testing.T) {
	tr, _ := newTestTracker()
	x := codexSession(statusRunning)
	status, since, _ := tr.view(x)
	if status != statusIdle || !since.Equal(t0) {
		t.Errorf("a started Codex session: %s since %v", status, since)
	}
	if got := tr.signals(x); got != control.SignalsFallback {
		t.Errorf("signals before any hook event: %q", got)
	}
	if got := tr.signals(claudeSession(statusRunning)); got != "" {
		t.Errorf("signals of Claude: %q", got)
	}
	gone := codexSession(statusExited)
	gone.ID = "bbbbbbbbbbb2"
	if status, _, _ := tr.view(gone); status != statusExited {
		t.Errorf("a Codex session that ended: %s", status)
	}
}

// A failed model request is an error if nothing follows within ten seconds; any event ends the wait.
func TestCodexFailedRequestBecomesAnError(t *testing.T) {
	tr, clock := newTestTracker()
	x := codexSession(statusRunning)
	tr.apply(x, event("UserPromptSubmit"))
	tr.apply(x, event("PostModelRequest", typed("failed")))
	if got := tr.failDue(failedRequestAfter); got != nil {
		t.Fatalf("an error at once: %+v", got)
	}
	clock.Advance(failedRequestAfter - time.Second)
	if got := tr.failDue(failedRequestAfter); got != nil {
		t.Fatalf("an error after %v: %+v", failedRequestAfter-time.Second, got)
	}
	if status, _, _ := tr.view(x); status != statusRunning {
		t.Fatalf("status before the time is up: %s", status)
	}
	clock.Advance(time.Second)
	got := tr.failDue(failedRequestAfter)
	if len(got) != 1 || got[0].To != statusError || got[0].From != statusRunning || got[0].ID != x.ID {
		t.Fatalf("after ten seconds: %+v", got)
	}
	if status, since, _ := tr.view(x); status != statusError || !since.Equal(clock.Now()) {
		t.Errorf("status after the time is up: %s since %v", status, since)
	}
	if again := tr.failDue(failedRequestAfter); again != nil {
		t.Errorf("a failure reported twice: %+v", again)
	}
	// The next event that sets a status ends the error.
	tr.apply(x, event("UserPromptSubmit"))
	if status, _, _ := tr.view(x); status != statusRunning {
		t.Errorf("after a new prompt: %s", status)
	}

	// A follow-up event within the time cancels the error, even one that sets no status.
	for _, follow := range []control.EventPushArgs{event("PreModelRequest"), event("PreCompact"), event("PostModelRequest", typed("completed"))} {
		tr.apply(x, event("PostModelRequest", typed("failed")))
		clock.Advance(5 * time.Second)
		tr.apply(x, follow)
		clock.Advance(time.Minute)
		if got := tr.failDue(failedRequestAfter); got != nil {
			t.Errorf("%s did not cancel the error: %+v", follow.Event, got)
		}
		if status, _, _ := tr.view(x); status == statusError {
			t.Errorf("%s did not cancel the error", follow.Event)
		}
	}
	// A failed request on a dead program changes nothing.
	tr.apply(x, event("PostModelRequest", typed("failed")))
	x.Status = statusExited
	tr.observe([]session.Session{x})
	clock.Advance(time.Minute)
	if got := tr.failDue(failedRequestAfter); got != nil {
		t.Errorf("an exited session failed: %+v", got)
	}
	if status, _, _ := tr.view(x); status != statusExited {
		t.Errorf("status of the exited session: %s", status)
	}
}

func TestCodexExitIsTmuxNotAnError(t *testing.T) {
	for _, name := range []string{"started here", "with events"} {
		tr, _ := newTestTracker()
		x := codexSession(statusRunning)
		if name == "started here" {
			tr.started(x)
		} else {
			tr.apply(x, event("Stop"))
		}
		x.Status = statusExited
		changes := tr.observe([]session.Session{x})
		if status, _, _ := tr.view(x); status != statusExited || len(changes) != 1 || changes[0].To != statusExited {
			t.Errorf("%s: %s, %+v", name, status, changes)
		}
	}
	// SessionEnd first, then the program: still exited, no second change.
	tr, _ := newTestTracker()
	x := codexSession(statusRunning)
	tr.apply(x, event("SessionEnd"))
	x.Status = statusExited
	if changes := tr.observe([]session.Session{x}); len(changes) != 0 {
		t.Errorf("changes after SessionEnd: %+v", changes)
	}
}

// Once a hook event arrived the fallback signals are ignored, so that nothing is counted twice.
func TestCodexFallbackAndHooks(t *testing.T) {
	tr, clock := newTestTracker()
	x := codexSession(statusRunning)

	// Fallback: the bell is waiting, the notify event ends the turn.
	tr.view(x) // the tracker learns of the session
	if !tr.fallbackWatch() {
		t.Error("a fallback session is not watched")
	}
	clock.Advance(time.Second)
	if ch := tr.bell(x); !ch.changed() || ch.To != statusWaiting || ch.From != statusIdle {
		t.Errorf("bell: %+v", ch)
	}
	if ch := tr.bell(x); ch.changed() {
		t.Errorf("a second bell changed the status: %+v", ch)
	}
	if ch := tr.inputSent(x); !ch.changed() || ch.To != statusRunning {
		t.Errorf("input: %+v", ch)
	}
	if ch := tr.inputSent(x); ch.changed() {
		t.Errorf("input while running: %+v", ch)
	}
	if ch := tr.apply(x, notified(codexTurnComplete)); !ch.changed() || ch.To != statusIdle {
		t.Errorf("notify: %+v", ch)
	}
	if got := tr.signals(x); got != control.SignalsFallback {
		t.Errorf("signals %q", got)
	}

	// The first hook event: hooks from now on.
	tr.apply(x, event("UserPromptSubmit"))
	if got := tr.signals(x); got != control.SignalsHooks {
		t.Errorf("signals after a hook event: %q", got)
	}
	if tr.fallbackWatch() {
		t.Error("a session with hooks is watched for the bell")
	}
	if ch := tr.bell(x); ch.changed() {
		t.Errorf("a bell changed the status of a session with hooks: %+v", ch)
	}
	tr.apply(x, event("PermissionRequest"))
	if ch := tr.apply(x, notified(codexTurnComplete)); ch.changed() {
		t.Errorf("a notify event changed the status of a session with hooks: %+v", ch)
	}
	if ch := tr.inputSent(x); ch.changed() {
		t.Errorf("input changed the status of a session with hooks: %+v", ch)
	}
	if status, _, _ := tr.view(x); status != statusWaiting {
		t.Errorf("status %s", status)
	}
	// A dead program's bell changes nothing.
	dead := codexSession(statusExited)
	dead.ID = "ccccccccccc2"
	tr.observe([]session.Session{dead})
	if ch := tr.bell(dead); ch.changed() {
		t.Errorf("bell of an exited session: %+v", ch)
	}
}

func TestAgentSessionIsBoundOncePerId(t *testing.T) {
	tr, _ := newTestTracker()
	x := codexSession(statusRunning)
	if !tr.agentChanged(x, "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9") {
		t.Error("a first id is not new")
	}
	if tr.agentChanged(x, "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9") {
		t.Error("the same id is new again")
	}
	if !tr.agentChanged(x, "1a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9") {
		t.Error("another id after /new is not new")
	}
	tr.forgetAgent(x)
	if !tr.agentChanged(x, "1a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9") {
		t.Error("a failed record is not retried")
	}
}
