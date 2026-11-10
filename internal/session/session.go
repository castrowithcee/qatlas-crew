package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/adapter/claude"
	"github.com/castrowithcee/qatlas-crew/internal/project"
)

// Program is the name of the tmux program. It is the one place that spells it, so that other packages, such
// as setup, can name tmux without a literal that the architecture test forbids outside this package.
const Program = "tmux"

// ServerName is the name of the tmux server of qcrew. Its socket lies beside the default server's, which
// qcrew never addresses.
const ServerName = "qcrew"

// SocketEnv names the variable that moves qcrew to the tmux server at an absolute socket path instead of
// ServerName, for tests and for running a second, separate set of sessions.
const SocketEnv = "QCREW_TMUX_SOCKET"

// The states tmux knows of a session: its program runs or has ended. The service refines them.
const (
	StatusRunning = "running"
	StatusExited  = "exited"
)

// MaxTitle is the longest title in characters.
const MaxTitle = 64

// The bounds of a capture.
const (
	DefaultLines = 50
	MaxLines     = 2000
	// MaxCapture is the most bytes a capture returns; older lines are dropped first.
	MaxCapture = 48 << 10
)

// The kinds of error the operations return; errors.Is matches an *Error against its kind.
var (
	ErrInvalid     = errors.New("invalid session request")
	ErrNotFound    = errors.New("session not found")
	ErrNoTmux      = errors.New("tmux is missing")
	ErrToolMissing = errors.New("tool is missing")
)

// Error is a refusal with a message for the user.
type Error struct {
	Kind    error
	Message string
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Kind }

func refuse(kind error, format string, args ...any) error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// Server is the tmux server qcrew talks to: the named server ServerName, or the one at Socket.
type Server struct {
	Socket string
}

// ServerFromEnv returns the server SocketEnv names, or the named server when it is unset or empty.
func ServerFromEnv(getenv func(string) string) (Server, error) {
	socket := getenv(SocketEnv)
	if socket == "" {
		return Server{}, nil
	}
	if !filepath.IsAbs(socket) {
		return Server{}, fmt.Errorf("%s must be an absolute socket path, not %q", SocketEnv, socket)
	}
	return Server{Socket: filepath.Clean(socket)}, nil
}

// args selects the server. Every tmux call starts with them, so no call can reach the default server.
func (s Server) args() []string {
	if s.Socket != "" {
		return []string{"-S", s.Socket}
	}
	return []string{"-L", ServerName}
}

// SessionName is the tmux session of a project.
func SessionName(projectID string) string { return "p-" + projectID }

// WindowName is the tmux window of a session.
func WindowName(id string) string { return "s-" + id }

// AttachCommand is the command line that attaches a terminal to the window of a session.
func (s Server) AttachCommand(projectID, id string) []string {
	return append(append([]string{Program}, s.args()...), "attach", "-t", SessionName(projectID)+":"+WindowName(id))
}

// Session is one agent session: a tmux window with its options.
type Session struct {
	ID      string
	Project string
	Tool    string
	Title   string
	Started time.Time
	Status  string
	window  string
}

// The window options that carry a session. tmux holds them, so they outlive the service.
const (
	optSession = "@qcrew-session"
	optProject = "@qcrew-project"
	optTool    = "@qcrew-tool"
	optTitle   = "@qcrew-title"
	optStarted = "@qcrew-started"
	// optAgentSession holds the id a tool knows the session by, for tools that take one. A later resume needs
	// it, so it lives in tmux with the rest.
	optAgentSession = "@qcrew-agent-session"
)

// listFormat puts the title last because it is the only free text.
var listFormat = strings.Join([]string{"#{window_id}", "#{pane_dead}", "#{" + optSession + "}", "#{" + optProject + "}",
	"#{" + optTool + "}", "#{" + optStarted + "}", "#{" + optTitle + "}"}, "\t")

// placeholder holds a new window open while its options are set, so that even a tool that ends at once
// meets remain-on-exit and leaves an exited session behind.
var placeholder = []string{"sleep", "3600"}

// Manager starts, lists and stops the sessions on one tmux server. Its methods are safe for concurrent use;
// changes run one at a time.
type Manager struct {
	server   Server
	mu       sync.Mutex
	lookPath func(string) (string, error)
	getenv   func(string) string
	newID    func() (string, error)
	newUUID  func() (string, error)
	now      func() time.Time
	// dataDir is the data directory of qcrew, where adapters keep their files for a session; executable
	// returns the path of the qcrew program that hooks call.
	dataDir    string
	executable func() (string, error)
}

// Option configures a Manager.
type Option func(*Manager)

// WithDataDir sets the data directory of qcrew, below which adapters keep their files.
func WithDataDir(dir string) Option { return func(m *Manager) { m.dataDir = dir } }

// WithExecutable sets how the manager finds the qcrew program that the hooks of a tool call; the default is
// the running program.
func WithExecutable(f func() (string, error)) Option {
	return func(m *Manager) {
		if f != nil {
			m.executable = f
		}
	}
}

// New returns the manager of the sessions on server.
func New(server Server, opts ...Option) *Manager {
	m := &Manager{server: server, lookPath: exec.LookPath, getenv: os.Getenv, newID: project.NewID, newUUID: newUUID,
		now: time.Now, executable: os.Executable}
	for _, o := range opts {
		o(m)
	}
	return m
}

// newUUID returns a random version 4 UUID.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("create a tool session id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// adapterFor returns the adapter of a tool.
func (m *Manager) adapterFor(tool string) (adapter.Adapter, error) {
	if tool == adapter.Claude {
		return claude.Adapter{Getenv: m.getenv}, nil
	}
	return adapter.Plain(tool, m.getenv)
}

// Server is the tmux server of the manager.
func (m *Manager) Server() Server { return m.server }

// errNoServer means no tmux server listens on the socket.
var errNoServer = errors.New("no tmux server")

// tmux runs one tmux command, or several separated by ";", against the manager's server and returns stdout.
func (m *Manager) tmux(ctx context.Context, args ...string) (string, error) {
	bin, err := m.lookPath(Program)
	if err != nil {
		return "", refuse(ErrNoTmux, "tmux is not installed or not in PATH")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append(m.server.args(), args...)...)
	cmd.Env = tmuxEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.HasPrefix(msg, "no server running") || strings.HasPrefix(msg, "error connecting to") {
			return "", errNoServer
		}
		return "", fmt.Errorf("tmux %s: %w: %s", args[0], err, msg)
	}
	return stdout.String(), nil
}

// tmuxEnv drops what would make tmux see the call as nested in the caller's tmux, and gives a detached
// client the terminal type it insists on.
func tmuxEnv(env []string) []string {
	out := slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		return strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=")
	})
	if !slices.ContainsFunc(out, func(kv string) bool { return strings.HasPrefix(kv, "TERM=") && kv != "TERM=" }) {
		out = append(out, "TERM=xterm-256color")
	}
	return out
}

// List returns the sessions on the server in the order they started. Without tmux or without a running
// server there are none.
func (m *Manager) List(ctx context.Context) ([]Session, error) {
	out, err := m.tmux(ctx, "list-windows", "-a", "-F", listFormat)
	if errors.Is(err, errNoServer) || errors.Is(err, ErrNoTmux) {
		return []Session{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseList(out), nil
}

func parseList(out string) []Session {
	list := []Session{}
	for line := range strings.Lines(out) {
		f := strings.SplitN(strings.TrimSuffix(line, "\n"), "\t", 7)
		if len(f) != 7 || f[2] == "" {
			continue // a window qcrew did not start
		}
		s := Session{window: f[0], ID: f[2], Project: f[3], Tool: f[4], Title: f[6], Status: StatusRunning}
		if f[1] == "1" {
			s.Status = StatusExited
		}
		s.Started, _ = time.Parse(time.RFC3339, f[5])
		list = append(list, s)
	}
	slices.SortStableFunc(list, func(a, b Session) int { return a.Started.Compare(b.Started) })
	return list
}

// find returns the session with the given id.
func (m *Manager) find(ctx context.Context, id string) (Session, error) {
	list, err := m.List(ctx)
	if err != nil {
		return Session{}, err
	}
	for _, s := range list {
		if s.ID == id {
			return s, nil
		}
	}
	return Session{}, refuse(ErrNotFound, "no session has the id %q", id)
}

// StartRequest describes a session to start.
type StartRequest struct {
	// ProjectID names the tmux session the window joins.
	ProjectID string
	// Dir is the working directory, the project's path.
	Dir string
	// Tool is one of adapter.Tools.
	Tool string
	// Title defaults to the tool.
	Title string
}

// ValidTitle checks a title: at most MaxTitle characters of valid UTF-8 without control characters.
func ValidTitle(title string) error {
	switch {
	case !utf8.ValidString(title):
		return refuse(ErrInvalid, "the title is not valid UTF-8")
	case utf8.RuneCountInString(title) > MaxTitle:
		return refuse(ErrInvalid, "the title is longer than %d characters", MaxTitle)
	case strings.ContainsFunc(title, unicode.IsControl):
		return refuse(ErrInvalid, "the title contains a control character")
	}
	return nil
}

// Start opens a window for a new session in the tmux session of its project, creating that session and the
// server as needed, and starts the tool there.
func (m *Manager) Start(ctx context.Context, req StartRequest) (Session, error) {
	ad, err := m.adapterFor(req.Tool)
	if err != nil {
		return Session{}, refuse(ErrInvalid, "the tool %q is not one of %s", req.Tool, strings.Join(adapter.Tools, ", "))
	}
	// The program to look for does not depend on the session, so the check can come before the session exists.
	command := ad.Command(adapter.Session{})
	title := req.Title
	if title == "" {
		title = req.Tool
	}
	if err := ValidTitle(title); err != nil {
		return Session{}, err
	}
	if _, err := m.lookPath(Program); err != nil {
		return Session{}, refuse(ErrNoTmux, "tmux is not installed or not in PATH")
	}
	if _, err := m.lookPath(command[0]); err != nil {
		return Session{}, refuse(ErrToolMissing, "the tool %s is not installed or not in PATH: %s", req.Tool, command[0])
	}
	if info, err := os.Stat(req.Dir); err != nil || !info.IsDir() {
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return Session{}, refuse(ErrInvalid, "the project directory %s does not exist", req.Dir)
		}
		return Session{}, refuse(ErrInvalid, "cannot read the project directory %s", req.Dir)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	id, err := m.freeID(ctx)
	if err != nil {
		return Session{}, err
	}
	s := Session{ID: id, Project: req.ProjectID, Tool: req.Tool, Title: title, Status: StatusRunning,
		Started: m.now().UTC().Truncate(time.Second)}
	as := adapter.Session{ID: id, Dir: req.Dir, DataDir: m.dataDir}
	as.Program, _ = m.executable() // only a tool that calls back needs it, and its adapter refuses without
	if adapter.HasHooks(req.Tool) {
		if as.AgentSession, err = m.newUUID(); err != nil {
			return Session{}, err
		}
	}

	created := []string{"-n", WindowName(id), "-c", req.Dir, "-P", "-F", "#{window_id}\t#{pane_id}", "--"}
	created = append(created, placeholder...)
	var out string
	if _, err = m.tmux(ctx, "has-session", "-t", "="+SessionName(req.ProjectID)); err == nil {
		out, err = m.tmux(ctx, append([]string{"new-window", "-d", "-t", "=" + SessionName(req.ProjectID) + ":"}, created...)...)
	} else if errors.Is(err, errNoServer) || strings.Contains(err.Error(), "can't find session") {
		// A user configuration with destroy-unattached on would end the never attached session at once; the
		// option goes in the same call, before the server gets to check. The target takes no "=": a pane
		// target refuses it, and the session that has just been created matches its name exactly.
		args := append([]string{"new-session", "-d", "-s", SessionName(req.ProjectID)}, created...)
		args = append(args, ";", "set-option", "-t", SessionName(req.ProjectID), "destroy-unattached", "off")
		out, err = m.tmux(ctx, args...)
	}
	if err != nil {
		return Session{}, fmt.Errorf("create the window: %w", err)
	}
	window, pane, ok := strings.Cut(strings.TrimSpace(out), "\t")
	if !ok || window == "" || pane == "" {
		return Session{}, fmt.Errorf("create the window: unexpected answer %q", out)
	}
	s.window = window

	set := func(name, value string) []string { return []string{"set-option", "-w", "-t", window, name, value, ";"} }
	var setup []string
	options := [][2]string{
		{optSession, s.ID}, {optProject, s.Project}, {optTool, s.Tool}, {optTitle, s.Title},
		{optStarted, s.Started.Format(time.RFC3339)},
		{"remain-on-exit", "on"}, {"allow-rename", "off"}, {"automatic-rename", "off"},
		{"pane-border-status", "top"}, {"pane-border-format", " #{" + optTitle + "} "},
	}
	if as.AgentSession != "" {
		options = append(options, [2]string{optAgentSession, as.AgentSession})
	}
	for _, opt := range options {
		setup = append(setup, set(opt[0], opt[1])...)
	}
	setup = append(setup, "select-pane", "-t", pane, "-T", s.Title)
	if _, err := m.tmux(ctx, setup...); err != nil {
		_, _ = m.tmux(ctx, "kill-window", "-t", window)
		return Session{}, fmt.Errorf("set the window options: %w", err)
	}
	if err := ad.Prepare(as); err != nil {
		_, _ = m.tmux(ctx, "kill-window", "-t", window)
		return Session{}, fmt.Errorf("prepare the tool: %w", err)
	}
	command = ad.Command(as)
	if _, err := m.tmux(ctx, append([]string{"respawn-pane", "-k", "-t", pane, "-c", req.Dir, "--"}, command...)...); err != nil {
		_, _ = m.tmux(ctx, "kill-window", "-t", window)
		ad.Cleanup(as)
		return Session{}, fmt.Errorf("start the tool: %w", err)
	}
	return s, nil
}

// freeID returns a new id that no session on the server has.
func (m *Manager) freeID(ctx context.Context) (string, error) {
	list, err := m.List(ctx)
	if err != nil {
		return "", err
	}
	for range 8 {
		id, err := m.newID()
		if err != nil {
			return "", err
		}
		if !slices.ContainsFunc(list, func(s Session) bool { return s.ID == id }) {
			return id, nil
		}
	}
	return "", errors.New("cannot find a free session id")
}

// Stop ends a session and closes its window, whether the tool still runs or not. tmux closes the project's
// session with its last window, and the server with its last session.
func (m *Manager) Stop(ctx context.Context, id string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.find(ctx, id)
	if err != nil {
		return Session{}, err
	}
	if _, err := m.tmux(ctx, "kill-window", "-t", s.window); err != nil {
		return Session{}, fmt.Errorf("close the window: %w", err)
	}
	if ad, err := m.adapterFor(s.Tool); err == nil {
		ad.Cleanup(adapter.Session{ID: s.ID, DataDir: m.dataDir})
	}
	return s, nil
}

// Capture returns the last lines of a session's screen and scrollback as plain text (see Plain), at most
// MaxCapture bytes. lines 0 means DefaultLines.
func (m *Manager) Capture(ctx context.Context, id string, lines int) (string, error) {
	switch {
	case lines == 0:
		lines = DefaultLines
	case lines < 0 || lines > MaxLines:
		return "", refuse(ErrInvalid, "lines must be between 1 and %d", MaxLines)
	}
	s, err := m.find(ctx, id)
	if err != nil {
		return "", err
	}
	out, err := m.tmux(ctx, "capture-pane", "-p", "-e", "-t", s.window, "-S", fmt.Sprint(-lines))
	if err != nil {
		return "", fmt.Errorf("capture the window: %w", err)
	}
	return lastLines(Plain(strings.TrimSuffix(out, "\n")), lines, MaxCapture), nil
}

// lastLines drops trailing blank lines and returns at most n of the remaining lines in at most max bytes.
func lastLines(text string, n, max int) string {
	rows := strings.Split(text, "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	if len(rows) > n {
		rows = rows[len(rows)-n:]
	}
	size := 0
	for i := len(rows) - 1; i >= 0; i-- {
		size += len(rows[i]) + 1
		if size > max {
			rows = rows[i+1:]
			break
		}
	}
	return strings.Join(rows, "\n")
}

// keyName is the form of a key SendKey sends: the name of a plain key such as 1 or Escape.
var keyName = regexp.MustCompile(`^[A-Za-z0-9]{1,16}$`)

// live returns the session with the given id if its window still runs the tool; an ended one cannot take input.
func (m *Manager) live(ctx context.Context, id string) (Session, error) {
	s, err := m.find(ctx, id)
	if err != nil {
		return Session{}, err
	}
	if s.Status == StatusExited {
		return Session{}, refuse(ErrNotFound, "the window of session %q has ended", id)
	}
	return s, nil
}

// SendText types text into the window of a session, as characters and not as key names, and presses Enter.
// The text must be one line of plain characters. Only the window of that session is addressed.
func (m *Manager) SendText(ctx context.Context, id, text string) error {
	if text == "" || !utf8.ValidString(text) || strings.ContainsFunc(text, unicode.IsControl) {
		return refuse(ErrInvalid, "the text is empty or not one line of plain characters")
	}
	// tmux reads a trailing ";" of the last argument as the end of a command; "\;" is a literal one.
	if strings.HasSuffix(text, ";") {
		text = strings.TrimSuffix(text, ";") + `\;`
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.live(ctx, id)
	if err != nil {
		return err
	}
	if _, err := m.tmux(ctx, "send-keys", "-t", s.window, "-l", "--", text); err != nil {
		return fmt.Errorf("send the text: %w", err)
	}
	if _, err := m.tmux(ctx, "send-keys", "-t", s.window, "Enter"); err != nil {
		return fmt.Errorf("send Enter: %w", err)
	}
	return nil
}

// SendKey presses one key, given by its tmux name such as 1 or Escape, in the window of a session.
func (m *Manager) SendKey(ctx context.Context, id, key string) error {
	if !keyName.MatchString(key) {
		return refuse(ErrInvalid, "the key name is not valid")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.live(ctx, id)
	if err != nil {
		return err
	}
	if _, err := m.tmux(ctx, "send-keys", "-t", s.window, key); err != nil {
		return fmt.Errorf("send the key: %w", err)
	}
	return nil
}
