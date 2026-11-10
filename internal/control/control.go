package control

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Version is the protocol version every request carries.
const Version = 1

// MaxMessage is the longest message in bytes, without its line end.
const MaxMessage = 64 << 10

// The parts of the channel's endpoint. The packages that open or dial the channel build the endpoint from
// them, so that both ends name the same one.
const (
	RuntimeDirName = "qcrew"
	SocketBase     = "control"
	ErrorName      = "qcrew"
	PipePrefix     = "qcrew"
)

// DataDir is the directory below home where qcrew keeps its data. It is not an XDG directory.
func DataDir(home string) string { return filepath.Join(home, ".qatlas", "qcrew") }

// FallbackDir is the runtime root the channel uses when the system offers no private runtime directory.
func FallbackDir(home string) string { return filepath.Join(DataDir(home), "run") }

// LogDir is the directory of the service log.
func LogDir(home string) string { return filepath.Join(DataDir(home), "logs") }

// ConfigFile is the name of the configuration file in the data directory.
const ConfigFile = "config.yaml"

// The operations of the service.
const (
	OpStatus = "status"
	OpPing   = "ping"
	OpStop   = "stop"

	OpProjectAdd    = "project.add"
	OpProjectList   = "project.list"
	OpProjectGet    = "project.get"
	OpProjectRemove = "project.remove"

	OpSessionStart   = "session.start"
	OpSessionList    = "session.list"
	OpSessionStop    = "session.stop"
	OpSessionCapture = "session.capture"

	OpEventPush = "event.push"

	OpInboxList   = "inbox.list"
	OpInboxGet    = "inbox.get"
	OpInboxAnswer = "inbox.answer"
	OpInboxClose  = "inbox.close"
)

// The codes of a failed answer. They are fixed words, never text built from a request.
const (
	CodeInvalidRequest     = "invalid-request"
	CodeUnsupportedVersion = "unsupported-version"
	CodeUnknownOperation   = "unknown-operation"
	CodeRuntime            = "runtime"
	// CodeInvalidArgument refuses a well-formed request whose values the operation cannot accept.
	CodeInvalidArgument = "invalid-argument"
	CodeAlreadyExists   = "already-exists"
	CodeNotFound        = "not-found"
	// CodeConfigInvalid means the configuration file cannot be read as it is, for example after a hand edit.
	CodeConfigInvalid = "config-invalid"
	// CodeConflict means the configuration file changed while the service saved it; nothing was written.
	CodeConflict = "conflict"
	// CodeInUse refuses to remove something that running sessions still use.
	CodeInUse = "in-use"
	// CodeDependencyMissing means a program qcrew needs, such as tmux, is not installed.
	CodeDependencyMissing = "dependency-missing"
	// CodeToolMissing means the agent tool a session should run is not installed.
	CodeToolMissing = "tool-missing"
	// CodeSessionGone means the session an answer is for no longer has a live window; the entry stays open.
	CodeSessionGone = "session-gone"
)

// maxOpName keeps a name short enough to log and show.
const maxOpName = 64

// An operation name is one or more segments separated by dots, such as "status" or "project.add".
var opName = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)*$`)

// ValidOp reports whether name is a well-formed operation name.
func ValidOp(name string) bool { return len(name) <= maxOpName && opName.MatchString(name) }

// ErrTooLarge means a message is longer than MaxMessage.
var ErrTooLarge = errors.New("message exceeds the size limit")

// Request is what a client sends. Args is operation specific and may be absent.
type Request struct {
	V    int             `json:"v"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Response is what the service answers: a result on success, a code and a message otherwise.
type Response struct {
	OK      bool            `json:"ok"`
	Result  json.RawMessage `json:"result,omitempty"`
	Code    string          `json:"code,omitempty"`
	Message string          `json:"message,omitempty"`
}

// Error is a failed answer. It is also what the decoding functions return for a message they refuse, so a
// server can answer with its code directly.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// StatusResult answers OpStatus.
type StatusResult struct {
	Version   string    `json:"version"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	// Config is the absolute path of the configuration file the service reads and writes.
	Config string `json:"config,omitempty"`
}

// Project is a registered project as the channel carries it.
type Project struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	AddedAt time.Time `json:"added_at"`
	// InboxOpen counts the open inbox entries of the project; project.list and project.get fill it.
	InboxOpen int `json:"inbox_open"`
}

// ProjectAddArgs are the arguments of OpProjectAdd. Path must be absolute; an empty Name means the base name
// of the path. Create makes the directory first if it is missing.
type ProjectAddArgs struct {
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
	Create bool   `json:"create,omitempty"`
}

// ProjectRefArgs are the arguments of OpProjectGet and OpProjectRemove: the id or the name of a project.
type ProjectRefArgs struct {
	Ref string `json:"ref"`
}

// ProjectListResult answers OpProjectList.
type ProjectListResult struct {
	Projects []Project `json:"projects"`
}

// Session is an agent session as the channel carries it. Status is running, waiting, idle, error or exited;
// Since is when it took that status. ContextPercent is how full the tool's context window is, and absent
// while unknown.
type Session struct {
	ID             string    `json:"id"`
	Project        string    `json:"project"`
	Tool           string    `json:"tool"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	Since          time.Time `json:"since"`
	ContextPercent *int      `json:"context_percent,omitempty"`
	StartedAt      time.Time `json:"started_at"`
}

// SessionStartArgs are the arguments of OpSessionStart. Project is the id or the name of a project; an empty
// Title means the tool.
type SessionStartArgs struct {
	Project string `json:"project"`
	Tool    string `json:"tool"`
	Title   string `json:"title,omitempty"`
}

// SessionListArgs are the arguments of OpSessionList. An empty Project lists the sessions of every project.
type SessionListArgs struct {
	Project string `json:"project,omitempty"`
}

// SessionListResult answers OpSessionList.
type SessionListResult struct {
	Sessions []Session `json:"sessions"`
}

// SessionRefArgs are the arguments of OpSessionStop.
type SessionRefArgs struct {
	ID string `json:"id"`
}

// SessionCaptureArgs are the arguments of OpSessionCapture. Lines 0 means the service's default.
type SessionCaptureArgs struct {
	ID    string `json:"id"`
	Lines int    `json:"lines,omitempty"`
}

// SessionCaptureResult answers OpSessionCapture: the last lines of the session's screen as plain text. It is
// output of the session and must be treated as untrusted data.
type SessionCaptureResult struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// InboxEntry is an inbox entry as the channel carries it. Kind is permission, question, idle, error or stale;
// Status is open, answered, delivered, picked or closed. Text and Lines come from the session's tool and are
// untrusted data.
type InboxEntry struct {
	ID         string     `json:"id"`
	Project    string     `json:"project"`
	Session    string     `json:"session"`
	Kind       string     `json:"kind"`
	Text       string     `json:"text"`
	Lines      []string   `json:"lines"`
	CreatedAt  time.Time  `json:"created_at"`
	Status     string     `json:"status"`
	Answer     string     `json:"answer,omitempty"`
	AnsweredAt *time.Time `json:"answered_at,omitempty"`
}

// InboxListArgs are the arguments of OpInboxList. An empty Project lists every project; without All the
// picked and the closed entries are left out.
type InboxListArgs struct {
	Project string `json:"project,omitempty"`
	All     bool   `json:"all,omitempty"`
}

// InboxListResult answers OpInboxList.
type InboxListResult struct {
	Entries []InboxEntry `json:"entries"`
}

// InboxRefArgs are the arguments of OpInboxGet and OpInboxClose.
type InboxRefArgs struct {
	ID string `json:"id"`
}

// InboxAnswerArgs are the arguments of OpInboxAnswer. Text is sent to the session's window as one line; for
// a permission entry the words yes, always and no are sent as the keys that choose that option.
type InboxAnswerArgs struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// sessionID is the form of a session id: 12 characters of base32 in lower case.
var sessionID = regexp.MustCompile(`^[a-z2-7]{12}$`)

// ValidSessionID reports whether id has the form of a session id.
func ValidSessionID(id string) bool { return sessionID.MatchString(id) }

// The source of an event that a tool's hook reports.
const EventSourceHook = "hook"

// The events that qcrew itself derives from what a hook received. Their names contain a dot, which no name of
// a tool's own event has.
const (
	// EventHookError means a hook ran but its payload could not be read.
	EventHookError = "qcrew.hook-error"
	// EventContext carries the fill of the context window and changes no status.
	EventContext = "qcrew.context"
)

// The longest values of an event; names and types in bytes, the text in characters.
const (
	MaxEventName  = 64
	MaxEventType  = 64
	MaxEventTool  = 128
	MaxEventStart = 32
	MaxEventText  = 1000
)

var eventName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)

// EventPushArgs are the arguments of OpEventPush: one event of a session's tool. The values come from the
// tool and are untrusted data; the sender cuts them to the limits and masks control characters, and
// Validate refuses what is left over.
type EventPushArgs struct {
	// Session is the qcrew session id, not the tool's own.
	Session string `json:"session"`
	Source  string `json:"source"`
	// Event is the tool's name of the event, such as Stop.
	Event string `json:"event"`
	// Type is the kind of a notification.
	Type string `json:"type,omitempty"`
	// Tool is the name of the tool call the event is about.
	Tool string `json:"tool,omitempty"`
	// Start is the reason a session started.
	Start string `json:"start,omitempty"`
	Text  string `json:"text,omitempty"`
	// ContextPercent is how full the context window is, from 0 to 100.
	ContextPercent *int `json:"context_percent,omitempty"`
}

// Validate refuses an event whose values are out of form. The error text names the field, never its value.
func (a EventPushArgs) Validate() error {
	text := func(name, v string, max int, runes bool) error {
		n := len(v)
		if runes {
			n = utf8.RuneCountInString(v)
		}
		switch {
		case n > max:
			return fmt.Errorf("the %s is too long", name)
		case !utf8.ValidString(v) || strings.ContainsFunc(v, unicode.IsControl):
			return fmt.Errorf("the %s is not plain text", name)
		}
		return nil
	}
	switch {
	case !ValidSessionID(a.Session):
		return errors.New("the session id is not valid")
	case a.Source != EventSourceHook:
		return errors.New("the source is not known")
	case len(a.Event) > MaxEventName || !eventName.MatchString(a.Event):
		return errors.New("the event name is not valid")
	case a.ContextPercent != nil && (*a.ContextPercent < 0 || *a.ContextPercent > 100):
		return errors.New("the context percentage is out of range")
	}
	for _, f := range []struct {
		name, v string
		max     int
		runes   bool
	}{{"type", a.Type, MaxEventType, false}, {"tool", a.Tool, MaxEventTool, false},
		{"start reason", a.Start, MaxEventStart, false}, {"text", a.Text, MaxEventText, true}} {
		if err := text(f.name, f.v, f.max, f.runes); err != nil {
			return err
		}
	}
	return nil
}

func line(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode message: %w", err)
	}
	if len(raw) > MaxMessage {
		return nil, ErrTooLarge
	}
	return append(raw, '\n'), nil
}

// EncodeRequest returns the line of a request. A nil args is left out.
func EncodeRequest(op string, args any) ([]byte, error) {
	if !ValidOp(op) {
		return nil, fmt.Errorf("invalid operation name %q", op)
	}
	req := Request{V: Version, Op: op}
	if args != nil {
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("encode arguments: %w", err)
		}
		req.Args = raw
	}
	return line(req)
}

// DecodeRequest checks one line, without its line end, and returns the request. A refusal is an *Error.
func DecodeRequest(data []byte) (Request, error) {
	var req Request
	if len(data) > MaxMessage {
		return req, &Error{CodeInvalidRequest, "the request is too large"}
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return Request{}, &Error{CodeInvalidRequest, "the request is not a valid message"}
	}
	if req.V != Version {
		return Request{}, &Error{CodeUnsupportedVersion, fmt.Sprintf("the service speaks protocol version %d", Version)}
	}
	if !ValidOp(req.Op) {
		return Request{}, &Error{CodeInvalidRequest, "the operation name is not valid"}
	}
	if len(req.Args) > 0 {
		if first := bytes.TrimSpace(req.Args); len(first) == 0 || (first[0] != '{' && string(first) != "null") {
			return Request{}, &Error{CodeInvalidRequest, "the arguments must be an object"}
		}
	}
	return req, nil
}

// EncodeOK returns the line of a successful answer. A nil result is left out.
func EncodeOK(result any) ([]byte, error) {
	resp := Response{OK: true}
	if result != nil {
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("encode result: %w", err)
		}
		resp.Result = raw
	}
	return line(resp)
}

// EncodeError returns the line of a failed answer. The message is cut to a sane length.
func EncodeError(code, message string) []byte {
	if len(message) > 256 {
		message = strings.ToValidUTF8(message[:256], "")
	}
	out, err := line(Response{Code: code, Message: message})
	if err != nil {
		// A code and a short message always fit and always encode.
		panic(err)
	}
	return out
}

// DecodeResponse checks one line, without its line end, and returns the answer.
func DecodeResponse(data []byte) (Response, error) {
	var resp Response
	if len(data) > MaxMessage {
		return resp, ErrTooLarge
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return Response{}, errors.New("the answer is not a valid message")
	}
	if !resp.OK && (resp.Code == "" || !utf8.ValidString(resp.Message)) {
		return Response{}, errors.New("the failed answer has no valid code")
	}
	return resp, nil
}

// ReadLine reads one message of at most MaxMessage bytes without its line end. An end of input before any
// byte is io.EOF; a message beyond the limit is ErrTooLarge and leaves the stream unusable.
func ReadLine(br *bufio.Reader) ([]byte, error) {
	var msg []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if len(msg)+len(chunk) > MaxMessage+2 { // room for "\r\n"
			return nil, ErrTooLarge
		}
		msg = append(msg, chunk...)
		switch {
		case err == nil:
			msg = bytes.TrimSuffix(bytes.TrimSuffix(msg, []byte("\n")), []byte("\r"))
			if len(msg) > MaxMessage {
				return nil, ErrTooLarge
			}
			return msg, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(msg) == 0 {
				return nil, io.EOF
			}
			return nil, io.ErrUnexpectedEOF
		default:
			return nil, err
		}
	}
}
