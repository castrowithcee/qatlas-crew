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
)

// hookInput is where a hook reads its payload. Tests replace it.
var hookInput = func() io.Reader { return os.Stdin }

// newHookCommand is the hidden command that a tool's hooks call. It is no command for people: it ends with
// exit code 0 and prints nothing whatever happens, because the tool runs it inline and may read its output as
// an instruction. It does not start the service; without one the event is lost.
func newHookCommand(access *serviceAccess) *cobra.Command {
	return &cobra.Command{
		Use:    "hook claude|claude-status --session <id>",
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
	kind, session, ok := hookArgs(args)
	if !ok {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }()
		var ev control.EventPushArgs
		var ok bool
		if kind == hookClaude {
			ev, ok = claudeEvent(session, in), true
		} else {
			ev, ok = claudeContextEvent(session, in)
		}
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

// readClaudePayload reads the top-level string fields it needs from the start of a hook payload, at most
// maxHookPayload bytes of it. A payload cut off by the limit, or damaged later on, still yields the fields
// that came before the cut, because the event is known from the first of them: a permission request with a
// huge tool input is still a permission request. ok is false if no valid event name was read.
func readClaudePayload(in io.Reader) (f claudeFields, ok bool) {
	dec := json.NewDecoder(io.LimitReader(in, maxHookPayload))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return f, false
	}
	targets := map[string]*string{"hook_event_name": &f.event, "notification_type": &f.notificationType,
		"tool_name": &f.tool, "message": &f.message, "last_assistant_message": &f.lastMessage, "source": &f.source}
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
		if dst := targets[name]; dst != nil {
			var v string
			if json.Unmarshal(raw, &v) == nil {
				*dst = v
			}
		}
	}
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
