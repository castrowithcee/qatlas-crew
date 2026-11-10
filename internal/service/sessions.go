package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/adapter/codex"
	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/inbox"
	"github.com/castrowithcee/qatlas-crew/internal/session"
)

// refreshEvery is how often the service mirrors the sessions from tmux, and fastEvery how often it does while
// a Codex session depends on the fallback, whose only sign of a prompt is the bell that tmux records. The
// person sees a waiting session within fastEvery of the bell plus the time the inbox needs, well inside two
// seconds.
const (
	refreshEvery = 2 * time.Second
	fastEvery    = 500 * time.Millisecond
)

// mirror is the service's copy of the sessions in tmux, which stays the truth about them.
type mirror struct {
	mu      sync.Mutex
	list    []session.Session
	lastErr string
}

// refreshSessions reads the sessions from tmux into the mirror and returns them.
func (s *server) refreshSessions(ctx context.Context) ([]session.Session, error) {
	list, err := s.sessions.List(ctx)
	s.mirror.mu.Lock()
	if err != nil {
		// Logged once per distinct error, so that a lasting fault does not fill the log every few seconds.
		if msg := err.Error(); msg != s.mirror.lastErr {
			s.mirror.lastErr = msg
			s.log.Warn("cannot read the sessions from tmux", "err", err)
		}
		s.mirror.mu.Unlock()
		return nil, err
	}
	s.mirror.list, s.mirror.lastErr = list, ""
	changes := s.status.observe(list)
	s.mirror.mu.Unlock()
	// The inbox looks at tmux for the screen lines, which must not happen under the mirror's lock.
	s.noteChanges(ctx, changes)
	s.takeBells(ctx, list)
	s.scanScreens(ctx, list)
	return list, nil
}

// screenLines is how many screen lines a look at a Codex session reads.
const screenLines = 30

// scanScreens looks at the screen of the Codex sessions that need it: it lets a starting session become idle
// and an error line show as an error, with an inbox entry that carries the line.
func (s *server) scanScreens(ctx context.Context, list []session.Session) {
	for _, x := range list {
		if x.Tool != adapter.Codex || x.Status == session.StatusExited || !s.status.screenWatch(x) {
			continue
		}
		out, err := s.sessions.Capture(ctx, x.ID, screenLines)
		if err != nil {
			continue
		}
		ready, errLine := codex.ReadScreen(strings.Split(out, "\n"))
		if ch := s.status.screen(x, ready, errLine); ch.changed() && ch.To == statusError && !ch.Dead {
			s.openEntry(ctx, x.Project, x.ID, inbox.KindError, errLine)
		}
	}
}

// takeBells lets the sessions whose window rang move to waiting and the inbox follow, and takes the ring in so
// that the next one counts anew. A ring after hooks report is taken in and changes nothing.
func (s *server) takeBells(ctx context.Context, list []session.Session) {
	for _, x := range list {
		if !x.Bell || x.Tool != adapter.Codex {
			continue
		}
		ch := s.status.bell(x)
		if err := s.sessions.ClearBell(ctx, x.ID); err != nil {
			s.log.Warn("cannot clear the bell of a session", "id", x.ID, "err", err)
		}
		if ch.changed() && ch.To == statusWaiting && !ch.Dead {
			// The bell says a prompt waits, not which kind; the text is the last line of the screen.
			s.openEntry(ctx, x.Project, x.ID, inbox.KindPermission, "")
		}
	}
}

func (s *server) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(fastEvery)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Since(last) >= refreshEvery || s.status.fallbackWatch() {
				last = time.Now()
				_, _ = s.refreshSessions(ctx)
			}
			s.sweepIdle(ctx)
			s.sweepFailures(ctx)
		}
	}
}

// runningSessions counts the running sessions of a project.
func (s *server) runningSessions(ctx context.Context, projectID string) (int, error) {
	list, err := s.refreshSessions(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, x := range list {
		if x.Project == projectID && x.Status == session.StatusRunning {
			n++
		}
	}
	return n, nil
}

func (s *server) addSessionOps() {
	s.ops[control.OpEventPush] = func(ctx context.Context, args json.RawMessage) (any, error) {
		var a control.EventPushArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		if err := a.Validate(); err != nil {
			return nil, &control.Error{Code: control.CodeInvalidArgument, Message: err.Error()}
		}
		x, ok := s.findSession(ctx, a.Session)
		if !ok {
			return nil, &control.Error{Code: control.CodeNotFound, Message: "no session has the id " + a.Session}
		}
		if !adapter.HasHooks(x.Tool) || (a.Source == control.EventSourceNotify && x.Tool != adapter.Codex) {
			return nil, &control.Error{Code: control.CodeInvalidArgument, Message: "the tool of the session reports no events"}
		}
		ch := s.status.apply(x, a)
		s.noteEvent(ctx, x, a, ch)
		s.bindAgentSession(ctx, x, a.AgentSession)
		return struct{}{}, nil
	}
	s.ops[control.OpSessionStart] = func(ctx context.Context, args json.RawMessage) (any, error) {
		var a control.SessionStartArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		if a.Project == "" || a.Tool == "" {
			return nil, &control.Error{Code: control.CodeInvalidArgument, Message: "the project and the tool are required"}
		}
		// A project cannot be removed while a session for it starts.
		s.lifecycle.Lock()
		defer s.lifecycle.Unlock()
		p, err := s.projects.Get(a.Project)
		if err != nil {
			return nil, projectError(err)
		}
		started, err := s.sessions.Start(ctx, session.StartRequest{ProjectID: p.ID, Dir: p.Path, Tool: a.Tool, Title: a.Title})
		if err != nil {
			return nil, sessionError(err)
		}
		s.log.Info("session started", "id", started.ID, "project", p.ID, "tool", started.Tool)
		s.status.started(started)
		_, _ = s.refreshSessions(ctx)
		return s.wire(started), nil
	}
	s.ops[control.OpSessionList] = func(ctx context.Context, args json.RawMessage) (any, error) {
		var a control.SessionListArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		projectID := ""
		if a.Project != "" {
			p, err := s.projects.Get(a.Project)
			if err != nil {
				return nil, projectError(err)
			}
			projectID = p.ID
		}
		list, err := s.refreshSessions(ctx)
		if err != nil {
			return nil, sessionError(err)
		}
		out := control.SessionListResult{Sessions: []control.Session{}}
		for _, x := range list {
			if projectID == "" || x.Project == projectID {
				out.Sessions = append(out.Sessions, s.wire(x))
			}
		}
		return out, nil
	}
	s.ops[control.OpSessionStop] = func(ctx context.Context, args json.RawMessage) (any, error) {
		var a control.SessionRefArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		if a.ID == "" {
			return nil, &control.Error{Code: control.CodeInvalidArgument, Message: "the session id is missing"}
		}
		stopped, err := s.sessions.Stop(ctx, a.ID)
		if err != nil {
			return nil, sessionError(err)
		}
		s.log.Info("session stopped", "id", stopped.ID, "project", stopped.Project)
		out := s.wire(stopped)
		s.status.forget(stopped.ID)
		s.inbox.CloseSession(stopped.ID)
		_, _ = s.refreshSessions(ctx)
		return out, nil
	}
	s.ops[control.OpSessionCapture] = func(ctx context.Context, args json.RawMessage) (any, error) {
		var a control.SessionCaptureArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		if a.ID == "" {
			return nil, &control.Error{Code: control.CodeInvalidArgument, Message: "the session id is missing"}
		}
		text, err := s.sessions.Capture(ctx, a.ID, a.Lines)
		if err != nil {
			return nil, sessionError(err)
		}
		return control.SessionCaptureResult{ID: a.ID, Text: text}, nil
	}
}

// wire returns a session as the channel carries it, with the status the service derived.
func (s *server) wire(x session.Session) control.Session {
	status, since, context := s.status.view(x)
	return control.Session{ID: x.ID, Project: x.Project, Tool: x.Tool, Title: x.Title, Status: status, Since: since,
		ContextPercent: context, StartedAt: x.Started, Signals: s.status.signals(x)}
}

// bindAgentSession records the id a Codex session names itself with in the window of the session, where a
// later resume finds it. An id that is already recorded is left alone.
func (s *server) bindAgentSession(ctx context.Context, x session.Session, id string) {
	if id == "" || x.Tool != adapter.Codex || !s.status.agentChanged(x, id) {
		return
	}
	if err := s.sessions.SetAgentSession(ctx, x.ID, id); err != nil {
		s.status.forgetAgent(x)
		s.log.Warn("cannot record the tool session id", "id", x.ID, "err", err)
	}
}

// findSession returns the session with the given id: from the mirror, or, if it is not there yet, from a
// fresh look at tmux.
func (s *server) findSession(ctx context.Context, id string) (session.Session, bool) {
	s.mirror.mu.Lock()
	list := s.mirror.list
	s.mirror.mu.Unlock()
	for range 2 {
		for _, x := range list {
			if x.ID == id {
				return x, true
			}
		}
		var err error
		if list, err = s.refreshSessions(ctx); err != nil {
			return session.Session{}, false
		}
	}
	return session.Session{}, false
}

// sessionError gives a refusal of the session manager its code. Anything else is a runtime error whose text
// stays in the log.
func sessionError(err error) error {
	var refusal *session.Error
	if errors.As(err, &refusal) {
		code := ""
		switch refusal.Kind {
		case session.ErrInvalid:
			code = control.CodeInvalidArgument
		case session.ErrNotFound:
			code = control.CodeNotFound
		case session.ErrNoTmux:
			code = control.CodeDependencyMissing
		case session.ErrToolMissing:
			code = control.CodeToolMissing
		}
		if code != "" {
			return &control.Error{Code: code, Message: refusal.Message}
		}
	}
	return err
}

// inUse refuses to remove a project that has running sessions.
func inUse(name string, running int) error {
	noun := "sessions"
	if running == 1 {
		noun = "session"
	}
	return &control.Error{Code: control.CodeInUse,
		Message: fmt.Sprintf("the project %s has %d running %s; stop them first", name, running, noun)}
}
