package service

import (
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/inbox"
	"github.com/castrowithcee/qatlas-crew/internal/session"
)

func texted(v string) func(*control.EventPushArgs) {
	return func(a *control.EventPushArgs) { a.Text = v }
}

func TestWaitSignalTable(t *testing.T) {
	cases := []struct {
		ev   control.EventPushArgs
		kind inbox.Kind // empty: no entry
		text string
	}{
		{event("PermissionRequest", tooled("Bash")), inbox.KindPermission, "Bash"},
		{event("PermissionRequest", tooled("Bash"), texted("may I?")), inbox.KindPermission, "may I?"},
		{event("PermissionRequest"), inbox.KindPermission, "a permission is requested"},
		{event("Notification", typed("permission_prompt"), texted("needs permission")), inbox.KindPermission, "needs permission"},
		{event("PreToolUse", tooled("AskUserQuestion")), inbox.KindQuestion, "AskUserQuestion"},
		{event("Notification", typed("elicitation_dialog"), texted("which one?")), inbox.KindQuestion, "which one?"},
		{event("Notification", typed("elicitation_url_dialog")), inbox.KindQuestion, "a question is asked"},
		{event("Notification", typed("idle_prompt"), texted("waiting")), inbox.KindIdle, "waiting"},
		{event("Notification", typed("idle_prompt")), inbox.KindIdle, "the session waits for input"},
		{event("PreToolUse", tooled("Bash")), "", ""},
		{event("Notification", typed("auth_success")), "", ""},
		{event("Stop"), "", ""},
		{event("PostToolUse"), "", ""},
		{event(control.EventHookError), "", ""},
	}
	for _, c := range cases {
		kind, text, ok := waitSignal(c.ev)
		if kind != c.kind || ok != (c.kind != "") || text != c.text {
			t.Errorf("%+v: %q %q %v; want %q %q", c.ev, kind, text, ok, c.kind, c.text)
		}
	}
}

func TestApplyReportsTheChange(t *testing.T) {
	tr, clock := newTestTracker()
	x := claudeSession(statusRunning)
	clock.Advance(time.Second)
	ch := tr.apply(x, event("PermissionRequest"))
	if !ch.changed() || ch.From != statusRunning || ch.To != statusWaiting || ch.Project != "p" || ch.Dead {
		t.Errorf("first request: %+v", ch)
	}
	// A second wait event keeps the status; it is no change, but still an event for the inbox.
	if ch = tr.apply(x, event("Notification", typed("permission_prompt"))); ch.changed() || ch.To != statusWaiting {
		t.Errorf("second request: %+v", ch)
	}
	if ch = tr.apply(x, event("PostToolUse")); !ch.changed() || ch.To != statusRunning {
		t.Errorf("resume: %+v", ch)
	}
	x.Status = statusExited
	tr.observe([]session.Session{x})
	if ch = tr.apply(x, event("UserPromptSubmit")); !ch.Dead || ch.changed() {
		t.Errorf("event for an ended program: %+v", ch)
	}
}

func TestObserveReportsEndedPrograms(t *testing.T) {
	cases := []struct {
		name string
		tool string
		end  bool // the tool said SessionEnd first
		to   string
	}{
		{"lost claude", adapter.Claude, false, statusError},
		{"claude ended in the session", adapter.Claude, true, ""},
		{"shell ended", adapter.Shell, false, statusExited},
	}
	for _, c := range cases {
		tr, _ := newTestTracker()
		x := claudeSession(statusRunning)
		x.Tool = c.tool
		tr.started(x)
		tr.observe([]session.Session{x})
		if c.end {
			tr.apply(x, event("SessionEnd"))
		}
		x.Status = statusExited
		changes := tr.observe([]session.Session{x})
		switch {
		case c.to == "" && len(changes) != 0:
			t.Errorf("%s: %+v", c.name, changes)
		case c.to != "" && (len(changes) != 1 || changes[0].To != c.to || changes[0].Event != eventPaneDead || changes[0].Project != "p"):
			t.Errorf("%s: %+v, want a change to %s", c.name, changes, c.to)
		}
		if again := tr.observe([]session.Session{x}); len(again) != 0 {
			t.Errorf("%s: a second look reported %+v", c.name, again)
		}
	}
	// An ended session first seen after a restart of the service is no event.
	tr, _ := newTestTracker()
	if changes := tr.observe([]session.Session{claudeSession(statusExited)}); len(changes) != 0 {
		t.Errorf("first seen ended: %+v", changes)
	}
}

// The idle threshold runs on the injected clock, and a phase yields one offer.
func TestIdleDue(t *testing.T) {
	tr, clock := newTestTracker()
	x := claudeSession(statusRunning)
	clock.Advance(time.Second)
	tr.apply(x, event("Stop"))
	if due := tr.idleDue(idleAfter); len(due) != 0 {
		t.Fatalf("due at once: %+v", due)
	}
	clock.Advance(idleAfter - time.Second)
	if due := tr.idleDue(idleAfter); len(due) != 0 {
		t.Fatalf("due before the threshold: %+v", due)
	}
	clock.Advance(time.Second)
	due := tr.idleDue(idleAfter)
	if len(due) != 1 || due[0].ID != x.ID || due[0].Project != "p" {
		t.Fatalf("due at the threshold: %+v", due)
	}
	clock.Advance(time.Hour)
	if due := tr.idleDue(idleAfter); len(due) != 0 {
		t.Errorf("the same phase offered twice: %+v", due)
	}
	// A new phase after a break.
	tr.apply(x, event("UserPromptSubmit"))
	tr.apply(x, event("Stop"))
	clock.Advance(idleAfter)
	if due := tr.idleDue(idleAfter); len(due) != 1 {
		t.Errorf("a new idle phase: %+v", due)
	}
	// A running session is never due, and an idle_prompt counts as the offer of its phase, once.
	tr.apply(x, event("UserPromptSubmit"))
	clock.Advance(time.Hour)
	if due := tr.idleDue(idleAfter); len(due) != 0 {
		t.Errorf("a running session: %+v", due)
	}
	tr.apply(x, event("Stop"))
	if ch := tr.apply(x, event("Notification", typed("idle_prompt"))); !ch.IdleNew {
		t.Error("the first idle_prompt of a phase is not new")
	}
	if ch := tr.apply(x, event("Notification", typed("idle_prompt"))); ch.IdleNew {
		t.Error("a second idle_prompt of the phase is new")
	}
	clock.Advance(time.Hour)
	if due := tr.idleDue(idleAfter); len(due) != 0 {
		t.Errorf("after an idle_prompt: %+v", due)
	}
	// An ended program is never due.
	tr.apply(x, event("UserPromptSubmit"))
	tr.apply(x, event("Stop"))
	x.Status = statusExited
	tr.observe([]session.Session{x})
	clock.Advance(time.Hour)
	if due := tr.idleDue(idleAfter); len(due) != 0 {
		t.Errorf("an ended program: %+v", due)
	}
}
