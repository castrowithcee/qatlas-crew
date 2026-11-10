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
	"github.com/castrowithcee/qatlas-crew/internal/session"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// fakeClaude puts a program named claude on the PATH that waits for a line on its input and then ends. The
// service and tmux take the PATH from this process.
func fakeClaude(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nread line\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
}

func listed(t *testing.T, c *conn) map[string]control.Session {
	t.Helper()
	var list control.SessionListResult
	if resp := c.callWith(t, control.OpSessionList, control.SessionListArgs{}); json.Unmarshal(resp.Result, &list) != nil || !resp.OK {
		t.Fatalf("list: %+v", resp)
	}
	out := map[string]control.Session{}
	for _, x := range list.Sessions {
		out[x.ID] = x
	}
	return out
}

func TestEventsDriveTheStatus(t *testing.T) {
	srv := testenv.Tmux(t)
	fakeClaude(t)
	t.Setenv("SHELL", "/bin/sh")
	clock := testenv.NewFakeClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	r := start(t, Options{tmux: &session.Server{Socket: srv.Socket()}, now: clock.Now,
		Program: func() (string, error) { return "/opt/qcrew/bin/qcrew", nil }})
	c := r.dial(t)
	dir := filepath.Join(r.home, "work", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var p control.Project
	if resp := c.callWith(t, control.OpProjectAdd, control.ProjectAddArgs{Path: dir}); json.Unmarshal(resp.Result, &p) != nil {
		t.Fatalf("project add: %+v", resp)
	}
	t.Cleanup(func() { _, _ = srv.Run("kill-session", "-t", "="+session.SessionName(p.ID)) })
	startSession := func(tool string) control.Session {
		t.Helper()
		var s control.Session
		resp := c.callWith(t, control.OpSessionStart, control.SessionStartArgs{Project: "app", Tool: tool})
		if err := json.Unmarshal(resp.Result, &s); err != nil || !resp.OK || s.Status != "running" || s.ContextPercent != nil {
			t.Fatalf("start %s: %+v, %v", tool, resp, err)
		}
		return s
	}
	push := func(s control.Session, ev control.EventPushArgs) control.Response {
		ev.Session, ev.Source = s.ID, control.EventSourceHook
		return c.callWith(t, control.OpEventPush, ev)
	}

	s := startSession("claude")
	settings := filepath.Join(control.DataDir(r.home), "sessions", s.ID, "claude-settings.json")
	data, err := os.ReadFile(settings)
	if err != nil || !strings.Contains(string(data), "/opt/qcrew/bin/qcrew hook claude --session "+s.ID) {
		t.Fatalf("settings file: %v\n%s", err, data)
	}
	if got := listed(t, c)[s.ID]; got != s || !got.Since.Equal(s.StartedAt) {
		t.Fatalf("a session without events: %+v, started as %+v", got, s)
	}

	steps := []struct {
		ev   control.EventPushArgs
		want string
	}{
		{control.EventPushArgs{Event: "PermissionRequest", Tool: "Bash"}, "waiting"},
		{control.EventPushArgs{Event: "PostToolUse", Tool: "Bash"}, "running"},
		{control.EventPushArgs{Event: "Notification", Type: "idle_prompt", Text: "waiting"}, "idle"},
		{control.EventPushArgs{Event: "Stop", Text: "Done"}, "idle"},
	}
	var changed time.Time
	for _, step := range steps {
		clock.Advance(time.Minute)
		if resp := push(s, step.ev); !resp.OK {
			t.Fatalf("%s: %+v", step.ev.Event, resp)
		}
		got := listed(t, c)[s.ID]
		if step.ev.Event != "Stop" {
			changed = clock.Now()
		}
		if got.Status != step.want || !got.Since.Equal(changed) {
			t.Fatalf("after %s: %s since %v, want %s since %v", step.ev.Event, got.Status, got.Since, step.want, changed)
		}
	}
	clock.Advance(time.Minute)
	ctxPercent := 61
	if resp := push(s, control.EventPushArgs{Event: control.EventContext, ContextPercent: &ctxPercent}); !resp.OK {
		t.Fatalf("context: %+v", resp)
	}
	if got := listed(t, c)[s.ID]; got.ContextPercent == nil || *got.ContextPercent != 61 || got.Status != "idle" || !got.Since.Equal(changed) {
		t.Fatalf("after the context report: %+v", got)
	}

	// Refusals: an unknown session, values out of form, and a tool that reports no events.
	shell := startSession("shell")
	refusals := []struct {
		name string
		resp control.Response
		code string
	}{
		{"unknown session", push(control.Session{ID: "zzzzzzzzzzz2"}, control.EventPushArgs{Event: "Stop"}), control.CodeNotFound},
		{"bad id", push(control.Session{ID: "../x"}, control.EventPushArgs{Event: "Stop"}), control.CodeInvalidArgument},
		{"control character", push(s, control.EventPushArgs{Event: "Stop", Text: "a\x1b"}), control.CodeInvalidArgument},
		{"long text", push(s, control.EventPushArgs{Event: "Stop", Text: strings.Repeat("a", 2000)}), control.CodeInvalidArgument},
		{"bad event", push(s, control.EventPushArgs{Event: "Stop it"}), control.CodeInvalidArgument},
		{"shell session", push(shell, control.EventPushArgs{Event: "Stop"}), control.CodeInvalidArgument},
		{"unknown field", c.callWith(t, control.OpEventPush, map[string]any{"session": s.ID, "source": "hook", "event": "Stop", "x": 1}), control.CodeInvalidRequest},
		{"wrong source", c.callWith(t, control.OpEventPush, map[string]any{"session": s.ID, "source": "screen", "event": "Stop"}), control.CodeInvalidArgument},
	}
	for _, rf := range refusals {
		if rf.resp.OK || rf.resp.Code != rf.code || strings.Contains(rf.resp.Message, "\x1b") {
			t.Errorf("%s: %+v, want code %s", rf.name, rf.resp, rf.code)
		}
	}
	if got := listed(t, c)[s.ID]; got.Status != "idle" {
		t.Errorf("a refused event changed the status to %s", got.Status)
	}
	if got := listed(t, c)[shell.ID]; got.Status != "running" || got.ContextPercent != nil {
		t.Errorf("the shell session: %+v", got)
	}

	// The tool ends without a SessionEnd: error, from the time it was seen.
	clock.Advance(time.Minute)
	if _, err := srv.Run("send-keys", "-t", "="+session.SessionName(p.ID)+":"+session.WindowName(s.ID), "Enter"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var got control.Session
	for got = listed(t, c)[s.ID]; got.Status != "error" && time.Now().Before(deadline); got = listed(t, c)[s.ID] {
		time.Sleep(50 * time.Millisecond)
	}
	if got.Status != "error" || !got.Since.Equal(clock.Now()) {
		t.Fatalf("after the tool ended: %+v, want error since %v", got, clock.Now())
	}

	// Stopping removes the files of the session.
	if resp := c.callWith(t, control.OpSessionStop, control.SessionRefArgs{ID: s.ID}); !resp.OK {
		t.Fatalf("stop: %+v", resp)
	}
	if _, err := os.Stat(filepath.Dir(settings)); !os.IsNotExist(err) {
		t.Errorf("the session folder is still there: %v", err)
	}
	if resp := push(s, control.EventPushArgs{Event: "Stop"}); resp.OK || resp.Code != control.CodeNotFound {
		t.Errorf("an event for a stopped session: %+v", resp)
	}
	if resp := c.callWith(t, control.OpSessionStop, control.SessionRefArgs{ID: shell.ID}); !resp.OK {
		t.Fatalf("stop shell: %+v", resp)
	}
}
