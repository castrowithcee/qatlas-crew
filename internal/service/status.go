package service

import (
	"log/slog"
	"slices"
	"strings"
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

// The changes the service itself causes: tmux's report of an ended program, the ring of the bell of a Codex
// window, and the end of the time a failed model request waits for a follow-up event.
const (
	eventPaneDead = "pane-dead"
	eventBell     = "qcrew.bell"
)

// failedRequestAfter is how long a failed model request of Codex stands without a further event before the
// session counts as failed. The tool goes on to its prompt after a failure and says nothing more; a retry
// would send events within that time.
const failedRequestAfter = 10 * time.Second

// The names of Codex events that the mapping singles out. requestUserInputTool is the tool Codex asks a
// question with; its name is not yet proven against the real tool.
const (
	requestUserInputTool = "request_user_input"
	codexPostModel       = "PostModelRequest"
	codexTurnComplete    = "agent-turn-complete"
)

// transition is one change of the status of a session.
type transition struct {
	At       time.Time
	From, To string
	Event    string
}

// change is a move of a session to another status, with what caused it.
type change struct {
	ID, Project string
	From, To    string
	Event       string
	// Dead is set when the session's program had ended already, so that the event changed nothing.
	Dead bool
	// IdleNew is set for an idle_prompt event that opens its idle phase for the inbox, which offers each phase
	// once.
	IdleNew bool
}

// changed reports whether the status differs.
func (c change) changed() bool { return c.From != c.To }

type sessionState struct {
	project string
	tool    string
	// reportsEnd is set for a tool whose program ended without saying so was lost: an error, not an exit.
	reportsEnd bool
	// hooks is set for a Codex session from its first hook event on; before that, and without hooks, the
	// coarser signals of the session's flags are what the status follows.
	hooks bool
	// failedAt is when a failed model request was reported and nothing followed yet; zero otherwise.
	failedAt time.Time
	// agent is the id of the tool's own session last recorded in the window.
	agent string
	// startedHere is set for a session this service started, whose whole life it has watched.
	startedHere bool
	// seen is set once an event arrived since the service started.
	seen bool
	// dead is set once tmux reports the session's program as ended; the status no longer changes then.
	dead bool
	// idleNoted is set once the current idle phase has been offered to the inbox; any change of status
	// ends the phase.
	idleNoted bool
	status    string
	since     time.Time
	context   *int
	history   []transition
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
		st = &sessionState{project: x.Project, tool: x.Tool, reportsEnd: adapter.ReportsEnd(x.Tool), status: x.Status,
			since: x.Started}
		switch {
		case x.Status == statusExited:
			st.since = t.now()
		case x.Tool == adapter.Codex:
			// Codex says nothing before its first turn; waiting at its prompt is idle.
			st.status = statusIdle
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
	st.status, st.since, st.idleNoted = status, at, false
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
// tool with hooks ended without saying so. A session tmux no longer lists is forgotten. It returns the
// changes it made.
func (t *tracker) observe(list []session.Session) []change {
	t.mu.Lock()
	defer t.mu.Unlock()
	var changes []change
	present := make(map[string]bool, len(list))
	for _, x := range list {
		present[x.ID] = true
		st := t.get(x)
		if x.Status != statusExited || st.dead {
			continue
		}
		st.dead = true
		from := st.status
		switch {
		case st.status == statusExited || st.status == statusError:
		case st.reportsEnd && (st.startedHere || st.seen):
			// Its SessionEnd would have come first; the tool was lost without it.
			t.set(x.ID, st, statusError, eventPaneDead)
		default:
			t.set(x.ID, st, statusExited, eventPaneDead)
		}
		if st.status != from {
			changes = append(changes, change{ID: x.ID, Project: x.Project, From: from, To: st.status, Event: eventPaneDead})
		}
	}
	for id := range t.states {
		if !present[id] {
			delete(t.states, id)
		}
	}
	return changes
}

// idleDue returns the sessions that have been idle without a break for at least after and that were not yet
// offered to the inbox in this idle phase; each phase is returned once.
func (t *tracker) idleDue(after time.Duration) []change {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var due []change
	for id, st := range t.states {
		if st.status == statusIdle && !st.dead && !st.idleNoted && now.Sub(st.since) >= after {
			st.idleNoted = true
			due = append(due, change{ID: id, Project: st.project, From: statusIdle, To: statusIdle})
		}
	}
	slices.SortFunc(due, func(a, b change) int { return strings.Compare(a.ID, b.ID) })
	return due
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

// apply takes in an event of the tool of x and returns the move of the status it caused, whose From and To
// are equal if the status did not change.
func (t *tracker) apply(x session.Session, ev control.EventPushArgs) change {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.get(x)
	st.seen = true
	ch := change{ID: x.ID, Project: x.Project, From: st.status, To: st.status, Event: ev.Event, Dead: st.dead}
	if ev.Event == control.EventContext {
		if ev.ContextPercent != nil {
			c := *ev.ContextPercent
			st.context = &c
		}
		return ch
	}
	if x.Tool == adapter.Codex {
		return t.applyCodex(x, ev, st, ch)
	}
	if status, ok := claudeStatus(ev); ok && !st.dead {
		t.set(x.ID, st, status, ev.Event)
		ch.To = st.status
		if ev.Event == "Notification" && ev.Type == "idle_prompt" && !st.idleNoted {
			ch.IdleNew, st.idleNoted = true, true
		}
	}
	return ch
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

// applyCodex takes in an event of Codex. A notify event is the fallback and is ignored once hooks report; the
// first hook event ends the fallback for the session. Any hook event ends the wait for a follow-up of a
// failed model request.
func (t *tracker) applyCodex(x session.Session, ev control.EventPushArgs, st *sessionState, ch change) change {
	if ev.Source == control.EventSourceNotify {
		if st.hooks {
			return ch
		}
	} else {
		st.hooks, st.failedAt = true, time.Time{}
	}
	if st.dead {
		return ch
	}
	if ev.Event == codexPostModel && ev.Type == "failed" {
		st.failedAt = t.now()
	}
	if status, ok := codexStatus(ev); ok {
		t.set(x.ID, st, status, ev.Event)
		ch.To = st.status
	}
	return ch
}

// codexStatus is the status an event of Codex sets. Every other event leaves the status as it is; so does a
// PostModelRequest, whose failure counts only if no event follows (see failDue), and a SessionStart, which
// Codex sends at the first turn and not at its start. A question asked with request_user_input is waiting and
// not running. The error from an unreadable payload lasts until an event that sets a status follows.
func codexStatus(ev control.EventPushArgs) (string, bool) {
	switch ev.Event {
	case "UserPromptSubmit", "PreModelRequest", "PostToolUse":
		return statusRunning, true
	case "PreToolUse":
		if ev.Tool == requestUserInputTool {
			return statusWaiting, true
		}
		return statusRunning, true
	case "PermissionRequest":
		return statusWaiting, true
	case "Stop", "Interrupt", codexTurnComplete:
		return statusIdle, true
	case "SessionEnd":
		return statusExited, true
	case control.EventHookError:
		return statusError, true
	}
	return "", false
}

// failDue returns the changes of the Codex sessions whose last event was a failed model request that stood
// for at least after without a further event; they take the status error. Each failure is returned once.
func (t *tracker) failDue(after time.Duration) []change {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var due []change
	for id, st := range t.states {
		if st.failedAt.IsZero() || now.Sub(st.failedAt) < after {
			continue
		}
		st.failedAt = time.Time{}
		if st.dead {
			continue
		}
		from := st.status
		t.set(id, st, statusError, codexPostModel)
		if st.status != from {
			due = append(due, change{ID: id, Project: st.project, From: from, To: st.status, Event: codexPostModel})
		}
	}
	slices.SortFunc(due, func(a, b change) int { return strings.Compare(a.ID, b.ID) })
	return due
}

// bell takes in the ring of the bell of a Codex window: a prompt that waits for the person. Once hooks report
// for the session the ring says nothing new and changes nothing. The returned change is the move of the status.
func (t *tracker) bell(x session.Session) change {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.get(x)
	ch := change{ID: x.ID, Project: x.Project, From: st.status, To: st.status, Event: eventBell, Dead: st.dead}
	if st.hooks || st.dead || x.Tool != adapter.Codex {
		return ch
	}
	t.set(x.ID, st, statusWaiting, eventBell)
	ch.To = st.status
	return ch
}

// inputSent notes that the person's answer was typed into the window of a Codex session that reports no hook
// events: nothing else tells that the prompt is over. It returns the move of the status.
func (t *tracker) inputSent(x session.Session) change {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.get(x)
	ch := change{ID: x.ID, Project: x.Project, From: st.status, To: st.status, Event: "qcrew.input", Dead: st.dead}
	if st.hooks || st.dead || x.Tool != adapter.Codex || st.status != statusWaiting {
		return ch
	}
	t.set(x.ID, st, statusRunning, ch.Event)
	ch.To = st.status
	return ch
}

// signals tells where the status of x comes from: hooks or fallback for Codex, empty for the other tools.
func (t *tracker) signals(x session.Session) string {
	if x.Tool != adapter.Codex {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.get(x).hooks {
		return control.SignalsHooks
	}
	return control.SignalsFallback
}

// fallbackWatch reports whether a live Codex session depends on the fallback, whose bell is only seen when
// tmux is looked at, so that the service should look often.
func (t *tracker) fallbackWatch() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, st := range t.states {
		if st.tool == adapter.Codex && !st.hooks && !st.dead && st.status != statusExited {
			return true
		}
	}
	return false
}

// agentChanged reports whether id is a new id of the tool's own session for x, and notes it as recorded.
// If the record then fails, forgetAgent takes the note back so that the next event tries again.
func (t *tracker) agentChanged(x session.Session, id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.get(x)
	if st.agent == id {
		return false
	}
	st.agent = id
	return true
}

func (t *tracker) forgetAgent(x session.Session) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.get(x).agent = ""
}
