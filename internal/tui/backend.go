package tui

import (
	"context"
	"errors"
	"io"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/cli"

	"github.com/castrowithcee/qatlas-crew/internal/client"
	"github.com/castrowithcee/qatlas-crew/internal/control"
)

// Backend is what the interface asks of the service; *client.Client satisfies it.
type Backend interface {
	Projects(ctx context.Context) ([]control.Project, error)
	Sessions(ctx context.Context, project string) ([]control.Session, error)
	StopSession(ctx context.Context, id string) (control.Session, error)
	AddProject(ctx context.Context, args control.ProjectAddArgs) (control.Project, error)
	RemoveProject(ctx context.Context, ref string) (control.Project, error)
	StartSession(ctx context.Context, args control.SessionStartArgs) (control.Session, error)
}

// Connection is an open channel to the service.
type Connection interface {
	Backend
	io.Closer
}

// Connect opens a channel to the service, starting the service where the caller allows it.
type Connect func(ctx context.Context) (Connection, error)

// serial lets one call at a time use the channel: a refresh and a stop may be under way together, and the
// channel carries one request and answer at a time.
type serial struct {
	mu sync.Mutex
	c  Connection
}

func (s *serial) Projects(ctx context.Context) ([]control.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c.Projects(ctx)
}

func (s *serial) Sessions(ctx context.Context, project string) ([]control.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c.Sessions(ctx, project)
}

func (s *serial) StopSession(ctx context.Context, id string) (control.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c.StopSession(ctx, id)
}

func (s *serial) AddProject(ctx context.Context, args control.ProjectAddArgs) (control.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c.AddProject(ctx, args)
}

func (s *serial) RemoveProject(ctx context.Context, ref string) (control.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c.RemoveProject(ctx, ref)
}

func (s *serial) StartSession(ctx context.Context, args control.SessionStartArgs) (control.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c.StartSession(ctx, args)
}

func (s *serial) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.c.Close()
}

// refreshMsg is what one read of the service found. Projects and sessions are separate calls: a host without
// the session runtime still lists its projects.
type refreshMsg struct {
	conn     Connection
	projects []control.Project
	sessions []control.Session
	err      error // connecting or reading the projects failed
	sessErr  error // reading the sessions failed
}

// fetch reads the projects and the sessions of every project; the interface filters them by project, so
// choosing another project needs no call.
func fetch(ctx context.Context, conn Connection, connect Connect) tea.Msg {
	msg := refreshMsg{conn: conn}
	if conn == nil {
		if connect == nil {
			msg.err = errNoConnection
			return msg
		}
		c, err := connect(ctx)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.conn = &serial{c: c}
		conn = msg.conn
	}
	if msg.projects, msg.err = conn.Projects(ctx); msg.err != nil {
		return msg
	}
	msg.sessions, msg.sessErr = conn.Sessions(ctx, "")
	return msg
}

var errNoConnection = errors.New("no connection")

// stopMsg is the answer to a stop.
type stopMsg struct{ err error }

// answered reports whether err is an answer of the service, as opposed to a broken channel or a failure to
// reach it; after the latter the connection is dropped and opened again.
func answered(err error) bool {
	var failure *cli.Failure
	return errors.As(err, &failure)
}

// missingProgram reports whether err says that a program qcrew needs, such as tmux, is not installed, and
// returns the service's own sentence.
func missingProgram(err error) (string, bool) {
	var failure *cli.Failure
	if errors.As(err, &failure) && (failure.Code == client.CodeDependencyMissing || failure.Code == client.CodeToolMissing) {
		return failure.Message, true
	}
	return "", false
}
