package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/inbox"
	"github.com/castrowithcee/qatlas-crew/internal/project"
	"github.com/castrowithcee/qatlas-crew/internal/session"
	"github.com/castrowithcee/qatlas-crew/internal/setup"
)

// ErrRunning means another service already listens on the channel.
var ErrRunning = localipc.ErrRunning

// The limits of the service's connections.
const (
	maxConns     = 32
	readTimeout  = 30 * time.Second
	writeTimeout = 10 * time.Second
	// watchEvery is how often the service checks that its program file is still the one it runs from.
	watchEvery = 2 * time.Second
)

// Endpoint is the channel of the service of the user whose home directory is home.
func Endpoint(home string) localipc.Endpoint {
	return localipc.Endpoint{
		RuntimeDirName: control.RuntimeDirName,
		SocketBase:     control.SocketBase,
		FallbackDir:    control.FallbackDir(home),
		ErrorName:      control.ErrorName,
		PipePrefix:     control.PipePrefix,
	}
}

// Options configure Run.
type Options struct {
	// Version is the program version the status operation reports.
	Version string
	// Home is the user's home directory; empty means os.UserHomeDir.
	Home string
	// Config is the path of the configuration file; empty means config.yaml in the qcrew data directory.
	Config string
	// Ready is called once the service listens, before it accepts the first connection.
	Ready func()
	// Program returns the path of the qcrew program that the hooks of a tool call; empty means the running
	// program. Tests of a program that is no qcrew binary replace it.
	Program func() (string, error)

	// Hooks for tests; the zero values are the production behaviour.
	programReplaced func() (string, bool, error)
	watchEvery      time.Duration
	logMax          int64
	// tmux replaces the tmux server that QCREW_TMUX_SOCKET selects.
	tmux *session.Server
	// now replaces the clock of the session status.
	now func() time.Time
	// setupEnv replaces where setup.check looks for the programs.
	setupEnv setup.Env
}

// handler answers one operation with its arguments, which may be empty. A *control.Error answers with its
// code; any other error answers with the runtime code and its text stays in the log.
type handler func(ctx context.Context, args json.RawMessage) (any, error)

type server struct {
	log      *slog.Logger
	started  time.Time
	version  string
	ops      map[string]handler
	projects *project.Registry
	sessions *session.Manager
	mirror   mirror
	status   *tracker
	inbox    *inbox.Store
	// lifecycle orders the starting of sessions against the removal of projects.
	lifecycle sync.Mutex
	cancel    context.CancelFunc
	replaced  func() (string, bool, error)
	every     time.Duration
	setupEnv  setup.Env

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	wg    sync.WaitGroup
}

// Run opens the channel and serves it until ctx ends, SIGINT or SIGTERM arrives, a client asks the service
// to stop, or the program file was replaced. It returns ErrRunning if another service already listens,
// without having touched that one. The socket is removed before Run returns.
func Run(ctx context.Context, opts Options) error {
	home := opts.Home
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return fmt.Errorf("cannot find the home directory: %w", err)
		}
	}
	configPath := opts.Config
	if configPath == "" {
		configPath = filepath.Join(control.DataDir(home), control.ConfigFile)
	}
	projects, err := project.NewRegistry(configPath)
	if err != nil {
		return fmt.Errorf("open the configuration %s: %w", configPath, err)
	}
	var tmuxServer session.Server
	if opts.tmux != nil {
		tmuxServer = *opts.tmux
	} else if tmuxServer, err = session.ServerFromEnv(os.Getenv); err != nil {
		return err
	}
	// No Harden: on Linux it hides the program file from the clients, whose check of the service then fails
	// closed. The service holds no secret yet; hardening comes with a challenge that replaces that check.
	listener, err := Endpoint(home).Listen()
	if err != nil {
		if errors.Is(err, localipc.ErrRunning) {
			return ErrRunning
		}
		return err
	}
	var closeOnce sync.Once
	closeListener := func() { closeOnce.Do(func() { _ = listener.Close() }) }
	defer closeListener()

	logMax := opts.logMax
	if logMax == 0 {
		logMax = maxLogSize
	}
	logw, err := openLog(control.LogDir(home), logMax)
	if err != nil {
		return err
	}
	defer logw.Close()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	now := opts.now
	if now == nil {
		now = time.Now
	}
	logger := slog.New(slog.NewTextHandler(logw, nil))
	clock := func() time.Time { return now().UTC().Truncate(time.Second) }
	s := &server{
		log: logger, started: time.Now().UTC(), version: opts.Version,
		cancel: cancel, replaced: opts.programReplaced, every: opts.watchEvery, setupEnv: opts.setupEnv, conns: map[net.Conn]struct{}{},
		projects: projects, status: newTracker(clock, logger), inbox: inbox.NewStore(clock),
		sessions: session.New(tmuxServer, session.WithDataDir(control.DataDir(home)), session.WithExecutable(opts.Program)),
	}
	if s.replaced == nil {
		s.replaced = localipc.ProgramReplaced
	}
	if s.every == 0 {
		s.every = watchEvery
	}
	s.ops = map[string]handler{
		control.OpPing: func(context.Context, json.RawMessage) (any, error) { return struct{}{}, nil },
		control.OpStop: func(context.Context, json.RawMessage) (any, error) { return struct{}{}, nil },
		control.OpStatus: func(context.Context, json.RawMessage) (any, error) {
			return control.StatusResult{Version: s.version, PID: os.Getpid(), StartedAt: s.started,
				Config: s.projects.Path()}, nil
		},
	}
	s.addProjectOps()
	s.addSessionOps()
	s.addInboxOps()
	s.addSetupOps()

	s.log.Info("service started", "pid", os.Getpid(), "version", s.version, "config", s.projects.Path())
	if opts.Ready != nil {
		opts.Ready()
	}
	go func() {
		<-ctx.Done()
		closeListener() // unblocks Accept
	}()
	go s.watch(ctx)
	go s.refreshLoop(ctx)

	err = s.accept(ctx, listener)
	cancel()
	s.shutdown()
	closeListener()
	if err != nil {
		s.log.Error("service failed", "err", err)
		return err
	}
	s.log.Info("service stopped")
	return nil
}

func (s *server) accept(ctx context.Context, l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			var peer *localipc.PeerError
			if errors.As(err, &peer) {
				s.log.Warn("refused a peer", "peer_pid", peer.PID)
				continue
			}
			return fmt.Errorf("accept: %w", err)
		}
		if !s.track(conn) {
			_ = conn.Close()
			s.log.Warn("too many connections; closed one")
			continue
		}
		go func() {
			defer s.untrack(conn)
			s.serveConn(ctx, conn)
		}()
	}
}

func (s *server) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.conns) >= maxConns {
		return false
	}
	s.conns[conn] = struct{}{}
	s.wg.Add(1)
	return true
}

func (s *server) untrack(conn net.Conn) {
	_ = conn.Close()
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
	s.wg.Done()
}

// shutdown ends the connections still open and waits for their handlers.
func (s *server) shutdown() {
	s.mu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// watch ends the service once its program file was replaced or removed: every client refuses a service of
// another program file, so it could no longer be reached or stopped.
func (s *server) watch(ctx context.Context) {
	ticker := time.NewTicker(s.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if _, replaced, err := s.replaced(); err == nil && replaced {
			s.log.Info("the program file was replaced; stopping")
			s.cancel()
			return
		}
	}
}

func (s *server) serveConn(ctx context.Context, conn net.Conn) {
	br := bufio.NewReaderSize(conn, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		msg, err := control.ReadLine(br)
		if err != nil {
			if errors.Is(err, control.ErrTooLarge) {
				s.write(conn, control.EncodeError(control.CodeInvalidRequest, "the request is too large"))
			}
			return
		}
		answer, stop := s.dispatch(ctx, msg)
		if !s.write(conn, answer) {
			return
		}
		if stop {
			s.log.Info("stop requested")
			s.cancel()
			return
		}
	}
}

func (s *server) write(conn net.Conn, data []byte) bool {
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, err := conn.Write(data)
	return err == nil
}

// dispatch answers one message. The second result is true when the service ends after the answer.
func (s *server) dispatch(ctx context.Context, msg []byte) (answer []byte, stop bool) {
	req, err := control.DecodeRequest(msg)
	if err != nil {
		var ce *control.Error
		if errors.As(err, &ce) {
			return control.EncodeError(ce.Code, ce.Message), false
		}
		return control.EncodeError(control.CodeInvalidRequest, "the request is not valid"), false
	}
	h, ok := s.ops[req.Op]
	if !ok {
		return control.EncodeError(control.CodeUnknownOperation, "the operation is not known"), false
	}
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("operation panicked", "op", req.Op, "panic", fmt.Sprint(r))
			answer, stop = control.EncodeError(control.CodeRuntime, "the operation failed"), false
		}
	}()
	result, err := h(ctx, req.Args)
	if err != nil {
		var ce *control.Error
		if errors.As(err, &ce) {
			return control.EncodeError(ce.Code, ce.Message), false
		}
		s.log.Error("operation failed", "op", req.Op, "err", err)
		return control.EncodeError(control.CodeRuntime, "the operation failed"), false
	}
	answer, err = control.EncodeOK(result)
	if err != nil {
		s.log.Error("cannot encode the result", "op", req.Op, "err", err)
		return control.EncodeError(control.CodeRuntime, "the operation failed"), false
	}
	return answer, req.Op == control.OpStop
}
