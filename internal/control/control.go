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

// The operations of the service.
const (
	OpStatus = "status"
	OpPing   = "ping"
	OpStop   = "stop"
)

// The codes of a failed answer. They are fixed words, never text built from a request.
const (
	CodeInvalidRequest     = "invalid-request"
	CodeUnsupportedVersion = "unsupported-version"
	CodeUnknownOperation   = "unknown-operation"
	CodeRuntime            = "runtime"
)

// maxOpName keeps a name short enough to log and show.
const maxOpName = 64

var opName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

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
