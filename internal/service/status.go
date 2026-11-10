package service

import (
	"log/slog"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/session"
)

// The statuses of a session. A tool without hooks has only running and exited, as tmux shows them.
const (
	statusRunning = session.StatusRunning
	statusExited  = session.StatusExited
	statusWaiting = "waiting"
	statusIdle    = "idle"
	statusError   = "error"
)

// maxHistory is how many status changes the service keeps for a session.
const maxHistory = 64

// eventPaneDead names the change that tmux's report of an ended program causes.
const eventPaneDead = "pane-dead"

// transition is one change of the status of a session.
type transition struct {
	At       time.Time
	From, To string
	Event    string
}

type sessionState struct {
	hooked bool
	// startedHere is set for a session this service started, whose whole life it has watched.
	startedHere bool
	// seen is set once an event arrived since the service started.
	seen bool
	// dead is set once tmux reports the session's program as ended; the status no longer changes then.
	dead    bool
	status  string
	since   time.Time
	context *int
	history []transition
}

// tracker derives the status of the sessions from the events of their tools and from tmux. It lives in
// memory; what it knows starts anew with the service.
type tracker struct {
	mu     sync.Mutex
	now    func() time.Time
	log    *slog.Logger
	states map[string]*sessionState
}

func newTracker(now func() time.Time, log *slog.Logger) *tracker {
	return &tracker{now: now, log: log, states: map[string]*sessionState{}}
}

// get returns the state of x, which starts at what tmux shows: the time of the start for a running session,
// the time of the observation for an ended one.
func (t *tracker) get(x session.Session) *sessionState {
	st, ok := t.states[x.ID]
	if !ok {
		st = &sessionState{hooked: adapter.HasHooks(x.Tool), status: x.Status, since: x.Started}
		if x.Status == statusExited {
			st.since = t.now()
		}
		t.states[x.ID] = st
	}
	return st
}

// set changes the status and records it; an unchanged status is no change.
func (t *tracker) set(id string, st *sessionState, status, event string) {
	if status == st.status {
		return
	}
	at := t.now()
	st.history = append(st.history, transition{At: at, From: st.status, To: status, Event: event})
	if len(st.history) > maxHistory {
		st.history = st.history[len(st.history)-maxHistory:]
	}
	t.log.Info("session status changed", "id", id, "from", st.status, "to", status, "event", event)
	st.status, st.since = status, at
}

// started notes a session that this service has just started.
func (t *tracker) started(x session.Session) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.get(x).startedHere = true
}

// forget drops what is known of a session that is gone.
func (t *tracker) forget(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.states, id)
}

// observe takes in the sessions tmux lists: a program that has ended makes the session exited, or error if a
// tool with hooks ended without saying so. A session tmux no longer lists is forgotten.
func (t *tracker) observe(list []session.Session) {
	t.mu.Lock()
	defer t.mu.Unlock()
	present := make(map[string]bool, len(list))
	for _, x := range list {
		present[x.ID] = true
		st := t.get(x)
		if x.Status != statusExited || st.dead {
			continue
		}
		st.dead = true
		switch {
		case st.status == statusExited || st.status == statusError:
		case st.hooked && (st.startedHere || st.seen):
			// Its SessionEnd would have come first; the tool was lost without it.
			t.set(x.ID, st, statusError, eventPaneDead)
		default:
			t.set(x.ID, st, statusExited, eventPaneDead)
		}
	}
	for id := range t.states {
		if !present[id] {
			delete(t.states, id)
		}
	}
}

// view returns the status of x, since when it holds, and the fill of the context window if known.
func (t *tracker) view(x session.Session) (string, time.Time, *int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.get(x)
	var context *int
	if st.context != nil {
		c := *st.context
		context = &c
	}
	return st.status, st.since, context
}

// history returns the changes of the status of a session, oldest first.
func (t *tracker) history(id string) []transition {
	t.mu.Lock()
	defer t.mu.Unlock()
	if st := t.states[id]; st != nil {
		return append([]transition(nil), st.history...)
	}
	return nil
}

// apply takes in an event of the tool of x.
func (t *tracker) apply(x session.Session, ev control.EventPushArgs) {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.get(x)
	st.seen = true
	if ev.Event == control.EventContext {
		if ev.ContextPercent != nil {
			c := *ev.ContextPercent
			st.context = &c
		}
		return
	}
	if status, ok := claudeStatus(ev); ok && !st.dead {
		t.set(x.ID, st, status, ev.Event)
	}
}

// claudeStatus is the status an event of Claude Code sets. Every other event, every other notification type
// and a SessionStart after compaction leave the status as it is. The error from an unreadable payload lasts
// until an event that sets a status follows.
func claudeStatus(ev control.EventPushArgs) (string, bool) {
	switch ev.Event {
	case "UserPromptSubmit", "PostToolUse", "PostToolUseFailure":
		return statusRunning, true
	case "PermissionRequest":
		return statusWaiting, true
	case "PreToolUse":
		if ev.Tool == "AskUserQuestion" {
			return statusWaiting, true
		}
	case "Notification":
		switch ev.Type {
		case "permission_prompt", "elicitation_dialog", "elicitation_url_dialog":
			return statusWaiting, true
		case "idle_prompt":
			return statusIdle, true
		}
	case "Stop":
		return statusIdle, true
	case "SessionStart":
		switch ev.Start {
		case "startup", "resume", "clear":
			return statusIdle, true
		}
	case "SessionEnd":
		return statusExited, true
	case control.EventHookError:
		return statusError, true
	}
	return "", false
}
