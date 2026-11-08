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

// codes maps the codes of the service to the shared error codes; a code of the service that is not listed
// is a runtime error of the service.
var codes = map[string]output.Code{
	control.CodeInvalidRequest:     output.CodeInvalidRequest,
	control.CodeUnsupportedVersion: output.CodeInvalidRequest,
	control.CodeUnknownOperation:   output.CodeNotFound,
	control.CodeRuntime:            output.CodeRuntime,
}

// Failure turns the code and message of a failed answer into a diagnostic. The answer is data of another
// process: its message is cut and stripped of control characters, and its code only selects from a fixed set.
func Failure(code, message string) *cli.Failure {
	mapped, ok := codes[code]
	if !ok {
		mapped = output.CodeRuntime
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
	if strings.TrimSpace(message) == "" {
		message = "the service reported an error"
	} else {
		message = "the service answered: " + message
	}
	return &cli.Failure{
		Code:     mapped,
		Message:  message,
		NextStep: "Run 'qcrew service status'; if the problem stays, run 'qcrew service stop' and start the service again.",
	}
}
