package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-crew/internal/control"
)

// CallTimeout bounds a call whose context has no deadline.
const CallTimeout = 30 * time.Second

// ErrNotRunning means nothing listens on the service channel.
var ErrNotRunning = errors.New("the qcrew service is not running")

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

// Client is one connection to the service. It is not safe for concurrent calls.
type Client struct {
	conn net.Conn
	br   *bufio.Reader
}

// Dial connects to the service of the current user and lets the transport check who listens. A refusal is a
// *localipc.PeerError; nothing listening is ErrNotRunning.
func Dial(ctx context.Context, opts localipc.DialOptions) (*Client, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot find the home directory: %w", err)
	}
	conn, err := Endpoint(home).DialWith(ctx, opts)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOTDIR) {
			return nil, fmt.Errorf("%w: %w", ErrNotRunning, err)
		}
		return nil, err
	}
	return &Client{conn: conn, br: bufio.NewReaderSize(conn, 4096)}, nil
}

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Call sends one operation and returns its result, which is nil if the service sent none. args may be nil.
// A failed answer is a *cli.Failure, see Failure.
func (c *Client) Call(ctx context.Context, op string, args any) (json.RawMessage, error) {
	req, err := control.EncodeRequest(op, args)
	if err != nil {
		return nil, err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(CallTimeout)
	}
	_ = c.conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	// A service that refuses this peer closes without reading, so a failed write can still be followed by
	// the end of the connection; the read tells more.
	_, writeErr := c.conn.Write(req)
	line, err := control.ReadLine(c.br)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if writeErr != nil {
			return nil, fmt.Errorf("send the request: %w", writeErr)
		}
		return nil, fmt.Errorf("read the answer: %w", err)
	}
	resp, err := control.DecodeResponse(line)
	if err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, Failure(resp.Code, resp.Message)
	}
	return resp.Result, nil
}

// Status asks for the state of the service.
func (c *Client) Status(ctx context.Context) (control.StatusResult, error) {
	var st control.StatusResult
	raw, err := c.Call(ctx, control.OpStatus, nil)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return control.StatusResult{}, errors.New("the status answer is malformed")
	}
	return st, nil
}

// Ping checks that the service answers.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Call(ctx, control.OpPing, nil)
	return err
}

// Stop asks the service to end. It answers before it ends.
func (c *Client) Stop(ctx context.Context) error {
	_, err := c.Call(ctx, control.OpStop, nil)
	return err
}

// SetupCheck asks the service which of the programs qcrew needs are installed. It installs nothing.
func (c *Client) SetupCheck(ctx context.Context) ([]control.SetupDependency, error) {
	var res control.SetupCheckResult
	if err := c.callInto(ctx, control.OpSetupCheck, nil, &res); err != nil {
		return nil, err
	}
	return res.Dependencies, nil
}

// Projects returns the registered projects.
func (c *Client) Projects(ctx context.Context) ([]control.Project, error) {
	var res control.ProjectListResult
	if err := c.callInto(ctx, control.OpProjectList, nil, &res); err != nil {
		return nil, err
	}
	return res.Projects, nil
}

// Project returns the project whose id or name is ref.
func (c *Client) Project(ctx context.Context, ref string) (control.Project, error) {
	var p control.Project
	err := c.callInto(ctx, control.OpProjectGet, control.ProjectRefArgs{Ref: ref}, &p)
	return p, err
}

// AddProject registers a directory as a project.
func (c *Client) AddProject(ctx context.Context, args control.ProjectAddArgs) (control.Project, error) {
	var p control.Project
	err := c.callInto(ctx, control.OpProjectAdd, args, &p)
	return p, err
}

// RemoveProject unregisters the project whose id or name is ref and returns it.
func (c *Client) RemoveProject(ctx context.Context, ref string) (control.Project, error) {
	var p control.Project
	err := c.callInto(ctx, control.OpProjectRemove, control.ProjectRefArgs{Ref: ref}, &p)
	return p, err
}

// StartSession starts an agent session in a project.
func (c *Client) StartSession(ctx context.Context, args control.SessionStartArgs) (control.Session, error) {
	var x control.Session
	err := c.callInto(ctx, control.OpSessionStart, args, &x)
	return x, err
}

// Sessions returns the sessions of the project whose id or name is project, or of every project if it is
// empty.
func (c *Client) Sessions(ctx context.Context, project string) ([]control.Session, error) {
	var res control.SessionListResult
	if err := c.callInto(ctx, control.OpSessionList, control.SessionListArgs{Project: project}, &res); err != nil {
		return nil, err
	}
	return res.Sessions, nil
}

// PushEvent reports an event of a session's tool to the service.
func (c *Client) PushEvent(ctx context.Context, args control.EventPushArgs) error {
	_, err := c.Call(ctx, control.OpEventPush, args)
	return err
}

// StopSession ends a session and closes its window.
func (c *Client) StopSession(ctx context.Context, id string) (control.Session, error) {
	var x control.Session
	err := c.callInto(ctx, control.OpSessionStop, control.SessionRefArgs{ID: id}, &x)
	return x, err
}

// CaptureSession returns the last lines of a session's screen.
func (c *Client) CaptureSession(ctx context.Context, id string, lines int) (control.SessionCaptureResult, error) {
	var res control.SessionCaptureResult
	err := c.callInto(ctx, control.OpSessionCapture, control.SessionCaptureArgs{ID: id, Lines: lines}, &res)
	return res, err
}

// InboxEntries returns the inbox entries of the project whose id or name is project, or of every project if
// it is empty. Without all the picked and the closed ones are left out.
func (c *Client) InboxEntries(ctx context.Context, project string, all bool) ([]control.InboxEntry, error) {
	var res control.InboxListResult
	if err := c.callInto(ctx, control.OpInboxList, control.InboxListArgs{Project: project, All: all}, &res); err != nil {
		return nil, err
	}
	return res.Entries, nil
}

// InboxEntry returns the inbox entry with the given id.
func (c *Client) InboxEntry(ctx context.Context, id string) (control.InboxEntry, error) {
	var e control.InboxEntry
	err := c.callInto(ctx, control.OpInboxGet, control.InboxRefArgs{ID: id}, &e)
	return e, err
}

// AnswerInbox sends an answer to the session of an open inbox entry and returns the entry.
func (c *Client) AnswerInbox(ctx context.Context, id, text string) (control.InboxEntry, error) {
	var e control.InboxEntry
	err := c.callInto(ctx, control.OpInboxAnswer, control.InboxAnswerArgs{ID: id, Text: text}, &e)
	return e, err
}

// CloseInbox closes an inbox entry without an answer and returns it.
func (c *Client) CloseInbox(ctx context.Context, id string) (control.InboxEntry, error) {
	var e control.InboxEntry
	err := c.callInto(ctx, control.OpInboxClose, control.InboxRefArgs{ID: id}, &e)
	return e, err
}

func (c *Client) callInto(ctx context.Context, op string, args, result any) error {
	raw, err := c.Call(ctx, op, args)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("the answer to %s is malformed", op)
	}
	return nil
}

// CodeAlreadyExists is the shared error code of a request for something that exists already.
const CodeAlreadyExists output.Code = "already-exists"

// CodeConflict is the shared error code of a change that lost against another change made meanwhile.
const CodeConflict output.Code = "conflict"

// The shared error codes of a removal that running sessions block, and of a missing program.
const (
	CodeInUse             output.Code = "in-use"
	CodeDependencyMissing output.Code = "dependency-missing"
	CodeToolMissing       output.Code = "tool-missing"
	// CodeSessionGone is the shared error code of an answer whose session has no live window.
	CodeSessionGone output.Code = "session-gone"
)

// failureKind is how a code of the service appears on the command line. A refused request of the user is a
// usage error with exit code 2, and its message is the service's own; a fault keeps the prefix that marks
// the message as an answer of another process, unless plain says the message describes the host, not the
// service.
type failureKind struct {
	code    output.Code
	refusal bool
	plain   bool
	next    string
}

const statusNextStep = "Run 'qcrew service status'; if the problem stays, run 'qcrew service stop' and start the service again."

// codes maps the codes of the service to the shared error codes; a code of the service that is not listed
// is a runtime error of the service.
var codes = map[string]failureKind{
	control.CodeInvalidRequest:     {code: output.CodeInvalidRequest, next: statusNextStep},
	control.CodeUnsupportedVersion: {code: output.CodeInvalidRequest, next: statusNextStep},
	control.CodeUnknownOperation:   {code: output.CodeNotFound, next: statusNextStep},
	control.CodeRuntime:            {code: output.CodeRuntime, next: statusNextStep},
	control.CodeInvalidArgument:    {code: output.CodeInvalidRequest, refusal: true, next: "Correct the request and repeat it."},
	control.CodeAlreadyExists:      {code: CodeAlreadyExists, refusal: true, next: "Choose another value and repeat the request."},
	control.CodeNotFound:           {code: output.CodeNotFound, refusal: true, next: "Check the name and repeat the request."},
	control.CodeConfigInvalid: {code: output.CodeConfigInvalid,
		next: "Correct the configuration file by hand, or restore a copy from the backups directory beside it."},
	control.CodeConflict: {code: CodeConflict, next: "Repeat the request."},
	control.CodeInUse:    {code: CodeInUse, refusal: true, next: "Stop what still uses it and repeat the request."},
	control.CodeDependencyMissing: {code: CodeDependencyMissing, plain: true,
		next: "Run 'qcrew setup' to install what qcrew needs."},
	control.CodeToolMissing: {code: CodeToolMissing, plain: true, next: "Run 'qcrew setup' to install the tool."},
	control.CodeSessionGone: {code: CodeSessionGone, refusal: true,
		next: "Run 'qcrew session list' to see the sessions, and close the entry with 'qcrew inbox close <id>'."},
}

// Failure turns the code and message of a failed answer into a diagnostic. The answer is data of another
// process: its message is cut and stripped of control characters, and its code only selects from a fixed set.
func Failure(code, message string) *cli.Failure {
	kind, ok := codes[code]
	if !ok {
		kind = failureKind{code: output.CodeRuntime, next: statusNextStep}
	}
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, message)
	if len(message) > 200 {
		message = strings.ToValidUTF8(message[:200], "")
	}
	switch {
	case strings.TrimSpace(message) == "":
		message = "the service reported an error"
	case !kind.refusal && !kind.plain:
		message = "the service answered: " + message
	}
	return &cli.Failure{Code: kind.code, Message: message, NextStep: kind.next, Usage: kind.refusal}
}
