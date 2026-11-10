package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/inbox"
	"github.com/castrowithcee/qatlas-crew/internal/session"
)

// idleAfter is how long a session must stay idle without a break before the inbox gets an idle entry.
const idleAfter = 60 * time.Second

// waitSignal is the kind and text of the inbox entry an event of Claude Code or Codex asks for, if it asks
// for one. The text is the event's own, else the name of the tool, else a fixed phrase.
func waitSignal(tool string, ev control.EventPushArgs) (inbox.Kind, string, bool) {
	pick := func(fallback string, texts ...string) string {
		for _, t := range texts {
			if t != "" {
				return t
			}
		}
		return fallback
	}
	if tool == adapter.Codex {
		switch {
		case ev.Event == "PermissionRequest":
			return inbox.KindPermission, pick("a permission is requested", ev.Text, ev.Tool), true
		case ev.Event == "PreToolUse" && ev.Tool == requestUserInputTool:
			return inbox.KindQuestion, pick("a question is asked", ev.Text, ev.Tool), true
		}
		return "", "", false
	}
	switch ev.Event {
	case "PermissionRequest":
		return inbox.KindPermission, pick("a permission is requested", ev.Text, ev.Tool), true
	case "PreToolUse":
		if ev.Tool == "AskUserQuestion" {
			return inbox.KindQuestion, pick("a question is asked", ev.Text, ev.Tool), true
		}
	case "Notification":
		switch ev.Type {
		case "permission_prompt":
			return inbox.KindPermission, pick("a permission is requested", ev.Text, ev.Tool), true
		case "elicitation_dialog", "elicitation_url_dialog":
			return inbox.KindQuestion, pick("a question is asked", ev.Text, ev.Tool), true
		case "idle_prompt":
			return inbox.KindIdle, pick("the session waits for input", ev.Text), true
		}
	}
	return "", "", false
}

// noteEvent lets the inbox follow an event of a session's tool, after the tracker took it in.
func (s *server) noteEvent(ctx context.Context, x session.Session, ev control.EventPushArgs, ch change) {
	if ch.Dead {
		return
	}
	if ch.changed() && (ch.To == statusRunning || (x.Tool == adapter.Codex && ch.From == statusWaiting)) {
		// A Codex session that leaves waiting for any status has left the prompt, answered in the session itself.
		s.inbox.Resumed(x.ID)
	}
	if ch.changed() && ch.To == statusError {
		s.openEntry(ctx, x.Project, x.ID, inbox.KindError, "a hook of the tool sent something unreadable")
	}
	if kind, text, ok := waitSignal(x.Tool, ev); ok && (kind != inbox.KindIdle || ch.IdleNew) {
		s.openEntry(ctx, x.Project, x.ID, kind, text)
	}
}

// noteChanges lets the inbox follow the changes that tmux's report of an ended program caused. An exit by
// SessionEnd is no change here: it is the tool ending in the session on purpose, and makes no entry. An
// exit that no SessionEnd announced, and a tool lost without one, do.
func (s *server) noteChanges(ctx context.Context, changes []change) {
	for _, ch := range changes {
		switch ch.To {
		case statusError:
			s.openEntry(ctx, ch.Project, ch.ID, inbox.KindError, "the program of the session ended without reporting its end")
		case statusExited:
			s.openEntry(ctx, ch.Project, ch.ID, inbox.KindError, "the program of the session ended")
		}
	}
}

// sweepFailures makes an error entry for each Codex session whose failed model request stood for
// failedRequestAfter without a further event.
func (s *server) sweepFailures(ctx context.Context) {
	for _, ch := range s.status.failDue(failedRequestAfter) {
		s.openEntry(ctx, ch.Project, ch.ID, inbox.KindError, "a model request of the tool failed")
	}
}

// sweepIdle makes an idle entry for each session that has been idle for idleAfter.
func (s *server) sweepIdle(ctx context.Context) {
	for _, ch := range s.status.idleDue(idleAfter) {
		s.openEntry(ctx, ch.Project, ch.ID, inbox.KindIdle, "the session has been idle for over a minute")
	}
}

// openEntry makes an inbox entry for a session unless the rules of the store forbid one. The screen lines
// come from tmux, outside any lock; a failed capture leaves them empty.
func (s *server) openEntry(ctx context.Context, project, id string, kind inbox.Kind, text string) {
	if !s.inbox.Wants(id, kind) {
		return
	}
	var lines []string
	if out, err := s.sessions.Capture(ctx, id, inbox.MaxLines); err == nil && out != "" {
		lines = strings.Split(out, "\n")
	}
	if text == "" {
		// No text of its own: the last line that shows something, else a fixed phrase.
		text = "the tool waits for you"
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.TrimSpace(lines[i]) != "" {
				text = lines[i]
				break
			}
		}
	}
	if e, ok := s.inbox.Open(project, id, kind, text, lines); ok {
		s.log.Info("inbox entry opened", "entry", e.ID, "project", project, "session", id, "kind", string(kind))
	}
}

func wireEntry(e inbox.Entry) control.InboxEntry {
	out := control.InboxEntry{ID: e.ID, Project: e.Project, Session: e.Session, Kind: string(e.Kind), Text: e.Text,
		Lines: e.Lines, CreatedAt: e.CreatedAt, Status: string(e.Status), Answer: e.Answer}
	if out.Lines == nil {
		out.Lines = []string{}
	}
	if !e.AnsweredAt.IsZero() {
		at := e.AnsweredAt
		out.AnsweredAt = &at
	}
	return out
}

// inboxError gives a refusal of the store its code.
func inboxError(err error) error {
	switch {
	case errors.Is(err, inbox.ErrNotFound):
		return &control.Error{Code: control.CodeNotFound, Message: "no inbox entry has that id"}
	case errors.Is(err, inbox.ErrState):
		return &control.Error{Code: control.CodeInvalidArgument, Message: "the entry is not open"}
	case errors.Is(err, inbox.ErrAnswerEmpty), errors.Is(err, inbox.ErrAnswerLong):
		return &control.Error{Code: control.CodeInvalidArgument, Message: err.Error()}
	}
	return err
}

func decodeInboxRef(args json.RawMessage) (string, error) {
	var a control.InboxRefArgs
	if err := decodeArgs(args, &a); err != nil {
		return "", err
	}
	if !inbox.ValidID(a.ID) {
		return "", &control.Error{Code: control.CodeInvalidArgument, Message: "the inbox entry id is missing or not valid"}
	}
	return a.ID, nil
}

func (s *server) addInboxOps() {
	s.ops[control.OpInboxList] = func(_ context.Context, args json.RawMessage) (any, error) {
		var a control.InboxListArgs
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
		out := control.InboxListResult{Entries: []control.InboxEntry{}}
		for _, e := range s.inbox.List(projectID, a.All) {
			out.Entries = append(out.Entries, wireEntry(e))
		}
		return out, nil
	}
	s.ops[control.OpInboxGet] = func(_ context.Context, args json.RawMessage) (any, error) {
		id, err := decodeInboxRef(args)
		if err != nil {
			return nil, err
		}
		e, err := s.inbox.Get(id)
		if err != nil {
			return nil, inboxError(err)
		}
		return wireEntry(e), nil
	}
	s.ops[control.OpInboxClose] = func(_ context.Context, args json.RawMessage) (any, error) {
		id, err := decodeInboxRef(args)
		if err != nil {
			return nil, err
		}
		e, err := s.inbox.Close(id)
		if err != nil {
			if errors.Is(err, inbox.ErrState) {
				return nil, &control.Error{Code: control.CodeInvalidArgument, Message: "only an open or delivered entry can be closed"}
			}
			return nil, inboxError(err)
		}
		return wireEntry(e), nil
	}
	s.ops[control.OpInboxAnswer] = s.answer
}

// answer delivers the person's answer to the window of the entry's session. The entry is claimed first, so
// that two answers cannot both be sent; a failed delivery gives it back as open.
func (s *server) answer(ctx context.Context, args json.RawMessage) (any, error) {
	var a control.InboxAnswerArgs
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if !inbox.ValidID(a.ID) {
		return nil, &control.Error{Code: control.CodeInvalidArgument, Message: "the inbox entry id is missing or not valid"}
	}
	text, err := inbox.CleanAnswer(a.Text)
	if err != nil {
		return nil, inboxError(err)
	}
	e, err := s.inbox.Get(a.ID)
	if err != nil {
		return nil, inboxError(err)
	}
	if e.Status != inbox.StatusOpen {
		return nil, inboxError(inbox.ErrState)
	}
	// The entry, its session and the window are bound: the session must exist, belong to the entry's project
	// and still run, and the text goes to that session's window only.
	x, ok := s.findSession(ctx, e.Session)
	if !ok || x.Project != e.Project || x.Status == session.StatusExited {
		return nil, &control.Error{Code: control.CodeSessionGone, Message: "the session of the entry has no live window"}
	}
	if _, err := s.inbox.Answer(e.ID, text); err != nil {
		return nil, inboxError(err)
	}
	// The words yes, always and no choose an option only for Claude Code, whose keys are known; Codex gets the
	// text as typed.
	if key, preset := inbox.PresetKey(e.Kind, text); preset && x.Tool == adapter.Claude {
		err = s.sessions.SendKey(ctx, x.ID, key)
	} else {
		err = s.sessions.SendText(ctx, x.ID, text)
	}
	if err != nil {
		s.inbox.Revert(e.ID)
		var refusal *session.Error
		if errors.As(err, &refusal) && errors.Is(refusal.Kind, session.ErrNotFound) {
			return nil, &control.Error{Code: control.CodeSessionGone, Message: "the session of the entry has no live window"}
		}
		return nil, sessionError(err)
	}
	s.inbox.Delivered(e.ID)
	s.log.Info("inbox entry answered", "entry", e.ID, "session", x.ID)
	done, err := s.inbox.Get(e.ID)
	if err != nil {
		return nil, inboxError(err)
	}
	// Without hooks nothing tells that the prompt is over; the person's answer does.
	if ch := s.status.inputSent(x); ch.changed() {
		s.inbox.Resumed(x.ID)
	}
	return wireEntry(done), nil
}
