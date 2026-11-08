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

func TestMain(m *testing.M) { os.Exit(testenv.TmuxMain(m)) }

func TestSessionOperations(t *testing.T) {
	srv := testenv.Tmux(t)
	t.Setenv("SHELL", "/bin/sh")
	r := start(t, Options{tmux: &session.Server{Socket: srv.Socket()}})
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

	resp := c.callWith(t, control.OpSessionStart, control.SessionStartArgs{Project: "app", Tool: "shell", Title: "build"})
	var s control.Session
	if err := json.Unmarshal(resp.Result, &s); err != nil || !resp.OK || s.Project != p.ID || s.Title != "build" || s.Status != "running" {
		t.Fatalf("start: %+v, %v", resp, err)
	}
	out, err := srv.Run("list-windows", "-a", "-F", "#{session_name} #{window_name} #{@qcrew-project}")
	if err != nil || !strings.Contains(out, session.SessionName(p.ID)+" "+session.WindowName(s.ID)+" "+p.ID+"\n") {
		t.Fatalf("tmux windows: %q, %v", out, err)
	}

	resp = c.callWith(t, control.OpSessionList, control.SessionListArgs{Project: p.ID})
	var list control.SessionListResult
	if err := json.Unmarshal(resp.Result, &list); err != nil || len(list.Sessions) != 1 || list.Sessions[0] != s {
		t.Fatalf("list: %+v, %v", resp, err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		resp = c.callWith(t, control.OpSessionCapture, control.SessionCaptureArgs{ID: s.ID, Lines: 5})
		var capture control.SessionCaptureResult
		if err := json.Unmarshal(resp.Result, &capture); err == nil && capture.ID == s.ID && strings.TrimSpace(capture.Text) != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("capture shows no prompt: %+v", resp)
		}
		time.Sleep(50 * time.Millisecond)
	}

	refusals := []struct {
		op   string
		args any
		code string
	}{
		{control.OpSessionStart, control.SessionStartArgs{Project: "app"}, control.CodeInvalidArgument},
		{control.OpSessionStart, control.SessionStartArgs{Project: "app", Tool: "vim"}, control.CodeInvalidArgument},
		{control.OpSessionStart, control.SessionStartArgs{Project: "nope", Tool: "shell"}, control.CodeNotFound},
		{control.OpSessionList, control.SessionListArgs{Project: "nope"}, control.CodeNotFound},
		{control.OpSessionStop, control.SessionRefArgs{}, control.CodeInvalidArgument},
		{control.OpSessionStop, control.SessionRefArgs{ID: "nope"}, control.CodeNotFound},
		{control.OpSessionCapture, control.SessionCaptureArgs{ID: s.ID, Lines: -1}, control.CodeInvalidArgument},
		{control.OpProjectRemove, control.ProjectRefArgs{Ref: "app"}, control.CodeInUse},
	}
	for _, tc := range refusals {
		if resp := c.callWith(t, tc.op, tc.args); resp.OK || resp.Code != tc.code || resp.Message == "" {
			t.Errorf("%s %v: %+v, want code %s", tc.op, tc.args, resp, tc.code)
		}
	}

	resp = c.callWith(t, control.OpSessionStop, control.SessionRefArgs{ID: s.ID})
	var stopped control.Session
	if err := json.Unmarshal(resp.Result, &stopped); err != nil || stopped.ID != s.ID {
		t.Fatalf("stop: %+v, %v", resp, err)
	}
	if _, err := srv.Run("has-session", "-t", "="+session.SessionName(p.ID)); err == nil {
		t.Fatal("the empty project session is still there")
	}
	if resp := c.callWith(t, control.OpProjectRemove, control.ProjectRefArgs{Ref: "app"}); !resp.OK {
		t.Fatalf("remove after stop: %+v", resp)
	}
}
