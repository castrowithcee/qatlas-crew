package cli

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-crew/internal/adapter/codex"
	"github.com/castrowithcee/qatlas-crew/internal/control"
)

// hookTimeout bounds the whole hook call, so that a hook never holds up the tool that runs it. A variable for
// the tests.
var hookTimeout = 2 * time.Second

const (
	// maxHookPayload is the most bytes of a hook payload read; the rest is dropped.
	maxHookPayload = 256 << 10
	// The hook kinds of Claude Code: its hooks, and its status line.
	hookClaude       = "claude"
	hookClaudeStatus = "claude-status"
	// hookCodex is the hook kind of Codex; its notify program is the same command with the flag hookNotify.
	hookCodex  = "codex"
	hookNotify = "--notify"
)

// hookEnv reads the environment a hook runs in. Tests replace it.
var hookEnv = os.Getenv

// hookInput is where a hook reads its payload. Tests replace it.
var hookInput = func() io.Reader { return os.Stdin }

// newHookCommand is the hidden command that a tool's hooks call. It is no command for people: it ends with
// exit code 0 and prints nothing whatever happens, because the tool runs it inline and may read its output as
// an instruction. It does not start the service; without one the event is lost.
func newHookCommand(access *serviceAccess) *cobra.Command {
	return &cobra.Command{
		Use:    "hook claude|claude-status --session <id> | hook codex [--notify]",
		Short:  "Report an event of an agent tool to the service (called by the tool's hooks)",
		Hidden: true,
		// The arguments are read by hand: a flag error must not print anything.
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			runHook(c.Context(), access, args, hookInput())
			return nil
		},
	}
}

// hookArgs reads "<kind> --session <id>" or "<kind> --session=<id>".
func hookArgs(args []string) (kind, session string, ok bool) {
	if len(args) == 3 && args[1] == "--session" {
		kind, session = args[0], args[2]
	} else if len(args) == 2 && strings.HasPrefix(args[1], "--session=") {
		kind, session = args[0], strings.TrimPrefix(args[1], "--session=")
	}
	ok = (kind == hookClaude || kind == hookClaudeStatus) && control.ValidSessionID(session)
	return
}

// runHook reads the payload from in and reports the event to the service, within hookTimeout. It returns
// nothing: a failure at any step ends the hook silently.
func runHook(ctx context.Context, access *serviceAccess, args []string, in io.Reader) {
	if len(args) > 0 && args[0] == hookCodex {
		runCodexHook(ctx, access, args, in)
		return
	}
	kind, session, ok := hookArgs(args)
	if !ok {
		return
	}
	send(ctx, access, func() (control.EventPushArgs, bool) {
		if kind == hookClaude {
			return claudeEvent(session, in), true
		}
		return claudeContextEvent(session, in)
	})
}

// runCodexHook reports an event of Codex: a hook, which reads its payload from in, or with the notify flag
// the notify program, which gets the payload as its last argument. The session comes from the environment of
// the tool; without a valid one the hook ends at once, without reading or touching the channel.
func runCodexHook(ctx context.Context, access *serviceAccess, args []string, in io.Reader) {
	notify := len(args) >= 2 && args[1] == hookNotify
	if len(args) != 1 && !notify {
		return
	}
	session := hookEnv(codex.SessionEnv)
	if !control.ValidSessionID(session) {
		return
	}
	send(ctx, access, func() (control.EventPushArgs, bool) {
		if notify {
			payload := ""
			if len(args) > 2 {
				payload = args[len(args)-1]
			}
			return codexEvent(session, control.EventSourceNotify, strings.NewReader(clipBytes(payload, maxHookPayload))), true
		}
		return codexEvent(session, control.EventSourceHook, in), true
	})
}

// send builds an event and reports it to the service, within hookTimeout; build runs in its own goroutine,
// because reading the payload may block. Nothing is returned or printed.
func send(ctx context.Context, access *serviceAccess, build func() (control.EventPushArgs, bool)) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		ev, ok := build()
		if !ok {
			return
		}
		cl, err := access.dial(ctx)
		if err != nil {
			return
		}
		defer cl.Close()
		_ = cl.PushEvent(ctx, ev)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// claudeFields are the fields of a Claude Code hook payload that qcrew uses.
type claudeFields struct {
	event, notificationType, tool, message, lastMessage, source string
}

var claudeEventName = regexp.MustCompile(`^[A-Za-z]{1,64}$`)

// scanPayload reads the top-level fields of a JSON object from the start of a hook payload, at most
// maxHookPayload bytes of it, and calls handle with each key and its raw value. A payload cut off by the
// limit, or damaged later on, still yields the fields that came before the cut, because the event is known
// from the first of them: a permission request with a huge tool input is still a permission request. It
// reports whether the payload started as an object.
func scanPayload(in io.Reader, handle func(key string, raw json.RawMessage)) bool {
	dec := json.NewDecoder(io.LimitReader(in, maxHookPayload))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return false
	}
	for dec.More() {
		key, err := dec.Token()
		name, isString := key.(string)
		if err != nil || !isString {
			break
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		handle(name, raw)
	}
	return true
}

// stringFields returns a handler for scanPayload that stores the string values of the named keys; a value
// of another type is ignored.
func stringFields(targets map[string]*string) func(string, json.RawMessage) {
	return func(name string, raw json.RawMessage) {
		if dst := targets[name]; dst != nil {
			var v string
			if json.Unmarshal(raw, &v) == nil {
				*dst = v
			}
		}
	}
}

// readClaudePayload reads the fields of a Claude Code hook payload that qcrew uses. ok is false if no valid
// event name was read.
func readClaudePayload(in io.Reader) (f claudeFields, ok bool) {
	scanPayload(in, stringFields(map[string]*string{"hook_event_name": &f.event, "notification_type": &f.notificationType,
		"tool_name": &f.tool, "message": &f.message, "last_assistant_message": &f.lastMessage, "source": &f.source}))
	return f, claudeEventName.MatchString(f.event)
}

// claudeEvent builds the event of a hook call. A payload without a readable event becomes the hook error.
func claudeEvent(session string, in io.Reader) control.EventPushArgs {
	f, ok := readClaudePayload(in)
	ev := control.EventPushArgs{Session: session, Source: control.EventSourceHook}
	if !ok {
		ev.Event = control.EventHookError
		return ev
	}
	ev.Event = f.event
	ev.Type = clipBytes(mask(f.notificationType), control.MaxEventType)
	ev.Tool = clipBytes(mask(f.tool), control.MaxEventTool)
	if f.event == "SessionStart" {
		ev.Start = clipBytes(mask(f.source), control.MaxEventStart)
	}
	text := f.message
	if text == "" {
		text = f.lastMessage
	}
	ev.Text = clipRunes(mask(text), control.MaxEventText)
	return ev
}

// claudeContextEvent builds the event of a status line call: the fill of the context window, or nothing
// while the tool does not know it yet.
func claudeContextEvent(session string, in io.Reader) (control.EventPushArgs, bool) {
	var p struct {
		ContextWindow struct {
			Used *float64 `json:"used_percentage"`
		} `json:"context_window"`
	}
	if json.NewDecoder(io.LimitReader(in, maxHookPayload)).Decode(&p) != nil || p.ContextWindow.Used == nil ||
		math.IsNaN(*p.ContextWindow.Used) {
		return control.EventPushArgs{}, false
	}
	percent := int(math.Round(math.Max(0, math.Min(100, *p.ContextWindow.Used))))
	return control.EventPushArgs{Session: session, Source: control.EventSourceHook, Event: control.EventContext,
		ContextPercent: &percent}, true
}

// mask makes foreign text plain: invalid UTF-8 becomes U+FFFD, line breaks and tabs become spaces, and any
// other control character becomes a question mark, so that nothing in it can steer a terminal.
func mask(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return '?'
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
}

// clipBytes cuts s to at most n bytes at a character boundary.
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// clipRunes cuts s to at most n characters, the last of them an ellipsis if s was longer.
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// codexFields are the fields of a Codex hook payload, or of a notify payload, that qcrew uses.
type codexFields struct {
	event, session, tool, status, message, command string
}

// notifyEventName is the form of the type of a notify payload, such as agent-turn-complete.
var notifyEventName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,63}$`)

// readCodexPayload reads the fields it needs from a hook payload (hook_event_name, session_id, tool_name,
// status, tool_input.command, error, last_assistant_message) or, with notify, from a notify payload (type,
// thread-id, last-assistant-message). ok is false if no valid event name was read.
func readCodexPayload(in io.Reader, notify bool) (f codexFields, ok bool) {
	var errText, last, input string
	targets := map[string]*string{"hook_event_name": &f.event, "session_id": &f.session, "tool_name": &f.tool,
		"status": &f.status, "error": &errText, "last_assistant_message": &last}
	if notify {
		targets = map[string]*string{"type": &f.event, "thread-id": &f.session, "last-assistant-message": &last}
	}
	byKey := stringFields(targets)
	scanPayload(in, func(name string, raw json.RawMessage) {
		if name == "tool_input" && !notify {
			var ti struct{ Command string }
			if json.Unmarshal(raw, &ti) == nil {
				input = ti.Command
			}
			return
		}
		byKey(name, raw)
	})
	f.command = input
	f.message = last
	if f.message == "" {
		f.message = errText
	}
	name := claudeEventName
	if notify {
		name = notifyEventName
	}
	return f, name.MatchString(f.event)
}

// codexEvent builds the event of a hook or notify call. A payload without a readable event becomes the hook
// error. The status of a PostModelRequest goes in Type; the command a permission request asks for is its text.
func codexEvent(session, source string, in io.Reader) control.EventPushArgs {
	f, ok := readCodexPayload(in, source == control.EventSourceNotify)
	ev := control.EventPushArgs{Session: session, Source: source}
	if !ok {
		ev.Event = control.EventHookError
		return ev
	}
	ev.Event = f.event
	ev.Type = clipBytes(mask(f.status), control.MaxEventType)
	ev.Tool = clipBytes(mask(f.tool), control.MaxEventTool)
	if control.ValidAgentSessionID(f.session) {
		ev.AgentSession = f.session
	}
	text := f.message
	if f.event == "PermissionRequest" && f.command != "" {
		text = f.command
	}
	ev.Text = clipRunes(mask(text), control.MaxEventText)
	return ev
}
