package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-kit/background"

	"github.com/castrowithcee/qatlas-crew/internal/client"
	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

const hookSession = "abcdefghij23"

func TestClaudeEvent(t *testing.T) {
	cases := []struct {
		name, payload string
		want          control.EventPushArgs
	}{
		{"stop", `{"session_id":"x","hook_event_name":"Stop","last_assistant_message":"All done."}`,
			control.EventPushArgs{Event: "Stop", Text: "All done."}},
		{"notification", `{"hook_event_name":"Notification","notification_type":"permission_prompt","message":"Needs permission"}`,
			control.EventPushArgs{Event: "Notification", Type: "permission_prompt", Text: "Needs permission"}},
		{"message wins over last message", `{"hook_event_name":"Stop","message":"m","last_assistant_message":"l"}`,
			control.EventPushArgs{Event: "Stop", Text: "m"}},
		{"permission request", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls","nested":{"source":"x","message":"no"}}}`,
			control.EventPushArgs{Event: "PermissionRequest", Tool: "Bash"}},
		{"pre tool use", `{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{}}`,
			control.EventPushArgs{Event: "PreToolUse", Tool: "AskUserQuestion"}},
		{"session start keeps the source", `{"hook_event_name":"SessionStart","source":"resume","model":"m"}`,
			control.EventPushArgs{Event: "SessionStart", Start: "resume"}},
		{"source of another event is dropped", `{"hook_event_name":"Stop","source":"resume"}`,
			control.EventPushArgs{Event: "Stop"}},
		{"session end", `{"hook_event_name":"SessionEnd","reason":"other"}`, control.EventPushArgs{Event: "SessionEnd"}},
		{"non-string fields are ignored", `{"hook_event_name":"Notification","notification_type":7,"message":["x"]}`,
			control.EventPushArgs{Event: "Notification"}},
		{"control characters are masked", "{\"hook_event_name\":\"Stop\",\"last_assistant_message\":\"a\\u001b[31mred\\nnext\\tx\\u0000\\u0085\"}",
			control.EventPushArgs{Event: "Stop", Text: "a?[31mred next x??"}},
		{"invalid utf-8", "{\"hook_event_name\":\"Stop\",\"last_assistant_message\":\"a\xffb\"}",
			control.EventPushArgs{Event: "Stop", Text: "a\uFFFDb"}},
		{"truncated after the event", `{"hook_event_name":"PermissionRequest","tool_name":"Write","tool_input":{"content":"abc`,
			control.EventPushArgs{Event: "PermissionRequest", Tool: "Write"}},
		{"no object", `[1,2]`, control.EventPushArgs{Event: control.EventHookError}},
		{"empty", ``, control.EventPushArgs{Event: control.EventHookError}},
		{"garbage", `not json`, control.EventPushArgs{Event: control.EventHookError}},
		{"cut before the event", `{"session_id":"x","hook_ev`, control.EventPushArgs{Event: control.EventHookError}},
		{"no event name", `{"session_id":"x"}`, control.EventPushArgs{Event: control.EventHookError}},
		{"event name not a name", `{"hook_event_name":"Stop now"}`, control.EventPushArgs{Event: control.EventHookError}},
		{"event name of an own event", `{"hook_event_name":"qcrew.context"}`, control.EventPushArgs{Event: control.EventHookError}},
		{"event name too long", `{"hook_event_name":"` + strings.Repeat("a", 65) + `"}`, control.EventPushArgs{Event: control.EventHookError}},
		{"event name masked away", `{"hook_event_name":"Stop\u001b"}`, control.EventPushArgs{Event: control.EventHookError}},
	}
	for _, c := range cases {
		got := claudeEvent(hookSession, strings.NewReader(c.payload))
		c.want.Session, c.want.Source = hookSession, control.EventSourceHook
		if got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
		if err := got.Validate(); err != nil {
			t.Errorf("%s: the event does not validate: %v", c.name, err)
		}
	}
}

func TestClaudeEventLimits(t *testing.T) {
	long := strings.Repeat("ä", 5000)
	payload := fmt.Sprintf(`{"hook_event_name":"Notification","notification_type":%q,"tool_name":%q,"message":%q}`,
		strings.Repeat("t", 500), strings.Repeat("ä", 500), long)
	ev := claudeEvent(hookSession, strings.NewReader(payload))
	if n := utf8.RuneCountInString(ev.Text); n != control.MaxEventText || !strings.HasSuffix(ev.Text, "…") {
		t.Errorf("text: %d characters, ends %q", n, ev.Text[len(ev.Text)-5:])
	}
	if len(ev.Type) != control.MaxEventType || len(ev.Tool) > control.MaxEventTool || !utf8.ValidString(ev.Tool) || ev.Tool == "" {
		t.Errorf("type %d bytes, tool %d bytes valid=%v", len(ev.Type), len(ev.Tool), utf8.ValidString(ev.Tool))
	}
	if err := ev.Validate(); err != nil {
		t.Error(err)
	}
}

// A payload beyond the limit is read to the limit and the rest is left alone.
func TestClaudeEventOversizedPayload(t *testing.T) {
	head := `{"session_id":"x","hook_event_name":"PermissionRequest","tool_name":"Write","tool_input":{"content":"`
	payload := &countingReader{r: strings.NewReader(head + strings.Repeat("a", 3*maxHookPayload) + `"}}`)}
	ev := claudeEvent(hookSession, payload)
	if ev.Event != "PermissionRequest" || ev.Tool != "Write" {
		t.Errorf("event: %+v", ev)
	}
	if payload.n > maxHookPayload+64<<10 { // the decoder may buffer a little beyond what it used
		t.Errorf("read %d bytes of a payload of %d, limit %d", payload.n, 3*maxHookPayload, maxHookPayload)
	}
	// With the event name beyond the limit, nothing is known.
	late := `{"filler":"` + strings.Repeat("a", maxHookPayload) + `","hook_event_name":"Stop"}`
	if ev := claudeEvent(hookSession, strings.NewReader(late)); ev.Event != control.EventHookError {
		t.Errorf("event name beyond the limit: %+v", ev)
	}
}

type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestClaudeContextEvent(t *testing.T) {
	pct := func(v int) *int { return &v }
	cases := []struct {
		payload string
		want    *int
	}{
		{`{"context_window":{"used_percentage":42.4}}`, pct(42)},
		{`{"context_window":{"used_percentage":42.5}}`, pct(43)},
		{`{"context_window":{"used_percentage":0}}`, pct(0)},
		{`{"context_window":{"used_percentage":250}}`, pct(100)},
		{`{"context_window":{"used_percentage":-3}}`, pct(0)},
		{`{"model":{"id":"x"},"context_window":{"used_percentage":null,"current_usage":null}}`, nil},
		{`{"context_window":{}}`, nil},
		{`{}`, nil},
		{`{"context_window":{"used_percentage":"high"}}`, nil},
		{`nope`, nil},
		{``, nil},
	}
	for _, c := range cases {
		ev, ok := claudeContextEvent(hookSession, strings.NewReader(c.payload))
		if (c.want != nil) != ok || (ok && (*ev.ContextPercent != *c.want || ev.Event != control.EventContext || ev.Validate() != nil)) {
			t.Errorf("%s: %+v, %v; want %v", c.payload, ev, ok, c.want)
		}
	}
}

func TestHookArgs(t *testing.T) {
	cases := []struct {
		args []string
		kind string
		ok   bool
	}{
		{[]string{"claude", "--session", hookSession}, "claude", true},
		{[]string{"claude-status", "--session=" + hookSession}, "claude-status", true},
		{[]string{"claude", "--session", "x"}, "", false},
		{[]string{"claude", "--session", "../../" + hookSession}, "", false},
		{[]string{"codex", "--session", hookSession}, "", false},
		{[]string{"claude"}, "", false},
		{[]string{"claude", "--session"}, "", false},
		{[]string{"claude", "--session", hookSession, "extra"}, "", false},
		{nil, "", false},
	}
	for _, c := range cases {
		if kind, session, ok := hookArgs(c.args); ok != c.ok || (ok && (kind != c.kind || session != hookSession)) {
			t.Errorf("%q: %q %q %v", c.args, kind, session, ok)
		}
	}
}

func TestMask(t *testing.T) {
	if got := mask("a\x1b]0;x\x07b\r\n\tc\x7f"); got != "a?]0;x?b   c?" {
		t.Errorf("mask = %q", got)
	}
	if got := clipBytes("ääää", 5); got != "ää" {
		t.Errorf("clipBytes = %q", got)
	}
	if got := clipRunes("abcdef", 4); got != "abc…" {
		t.Errorf("clipRunes = %q", got)
	}
	if got := clipRunes("abcd", 4); got != "abcd" {
		t.Errorf("clipRunes at the limit = %q", got)
	}
}

// With no service listening, a hook ends with exit code 0 and says nothing, and it starts no service.
func TestHookWithoutServiceIsSilent(t *testing.T) {
	testenv.Setup(t)
	testenv.ShortRuntime(t)
	old := serviceCommand
	serviceCommand = func() (c background.Command) { t.Error("the hook started the service"); return c }
	t.Cleanup(func() { serviceCommand = old })
	oldIn := hookInput
	hookInput = func() io.Reader { return strings.NewReader(`{"hook_event_name":"Stop"}`) }
	t.Cleanup(func() { hookInput = oldIn })
	for _, args := range [][]string{
		{"hook", "claude", "--session", hookSession},
		{"hook", "claude-status", "--session", hookSession},
		{"hook", "claude"},
		{"hook", "claude", "--session", "bad"},
		{"hook", "nonsense", "--bogus"},
		{"hook"},
		{"hook", "--help"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Run(args, &stdout, &stderr); code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Errorf("%q: code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}

// A service that does not answer holds the hook up for the timeout at most.
func TestHookTimeout(t *testing.T) {
	old := hookTimeout
	hookTimeout = 200 * time.Millisecond
	t.Cleanup(func() { hookTimeout = old })
	access := &serviceAccess{dial: func(ctx context.Context) (*client.Client, error) {
		<-ctx.Done()
		return nil, errors.New("timed out")
	}}
	start := time.Now()
	runHook(context.Background(), access, []string{"claude", "--session", hookSession}, strings.NewReader(`{"hook_event_name":"Stop"}`))
	if d := time.Since(start); d > hookTimeout+time.Second {
		t.Errorf("the hook took %v", d)
	}
	// A reader that never ends does not hold it up either.
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	start = time.Now()
	runHook(context.Background(), access, []string{"claude", "--session", hookSession}, pr)
	if d := time.Since(start); d > hookTimeout+time.Second {
		t.Errorf("the hook with a stalled input took %v", d)
	}
}

func TestCodexEvent(t *testing.T) {
	const agent = "0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9"
	cases := []struct {
		name, source, payload string
		want                  control.EventPushArgs
	}{
		{"prompt", "hook", `{"session_id":"` + agent + `","cwd":"/work","hook_event_name":"UserPromptSubmit","model":"m","prompt":"go"}`,
			control.EventPushArgs{Event: "UserPromptSubmit", AgentSession: agent}},
		{"permission with a command", "hook", `{"session_id":"` + agent + `","hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls -l\n","description":"x"}}`,
			control.EventPushArgs{Event: "PermissionRequest", Tool: "Bash", Text: "ls -l ", AgentSession: agent}},
		{"permission with a patch", "hook", `{"hook_event_name":"PermissionRequest","tool_name":"apply_patch","tool_input":{"patch":"x"}}`,
			control.EventPushArgs{Event: "PermissionRequest", Tool: "apply_patch"}},
		{"question tool", "hook", `{"hook_event_name":"PreToolUse","tool_name":"request_user_input","tool_input":{}}`,
			control.EventPushArgs{Event: "PreToolUse", Tool: "request_user_input"}},
		{"failed model request", "hook", `{"hook_event_name":"PostModelRequest","status":"failed","error":"rate limited","token_usage":{"n":1}}`,
			control.EventPushArgs{Event: "PostModelRequest", Type: "failed", Text: "rate limited"}},
		{"completed model request", "hook", `{"hook_event_name":"PostModelRequest","status":"completed","error":null}`,
			control.EventPushArgs{Event: "PostModelRequest", Type: "completed"}},
		{"stop", "hook", `{"session_id":"` + agent + `","hook_event_name":"Stop","stop_hook_active":false,"last_assistant_message":"Done."}`,
			control.EventPushArgs{Event: "Stop", Text: "Done.", AgentSession: agent}},
		{"session end", "hook", `{"hook_event_name":"SessionEnd","reason":"other"}`, control.EventPushArgs{Event: "SessionEnd"}},
		{"an id that is no uuid is dropped", "hook", `{"session_id":"s-1","hook_event_name":"Stop"}`, control.EventPushArgs{Event: "Stop"}},
		{"nested fields do not count", "hook", `{"hook_event_name":"Stop","tool_input":{"command":"no","status":"failed"}}`,
			control.EventPushArgs{Event: "Stop"}},
		{"the command is the text of a permission request only", "hook", `{"hook_event_name":"PreToolUse","tool_name":"shell","tool_input":{"command":"ls"}}`,
			control.EventPushArgs{Event: "PreToolUse", Tool: "shell"}},
		{"control characters are masked", "hook", "{\"hook_event_name\":\"PermissionRequest\",\"tool_input\":{\"command\":\"a\\u001b[31mb\"}}",
			control.EventPushArgs{Event: "PermissionRequest", Text: "a?[31mb"}},
		{"truncated after the event", "hook", `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ab`,
			control.EventPushArgs{Event: "PermissionRequest", Tool: "Bash"}},
		{"garbage", "hook", `not json`, control.EventPushArgs{Event: control.EventHookError}},
		{"empty", "hook", ``, control.EventPushArgs{Event: control.EventHookError}},
		{"no event name", "hook", `{"session_id":"` + agent + `"}`, control.EventPushArgs{Event: control.EventHookError}},
		{"event name of an own event", "hook", `{"hook_event_name":"qcrew.context"}`, control.EventPushArgs{Event: control.EventHookError}},
		{"notify", "notify", `{"type":"agent-turn-complete","thread-id":"` + agent + `","turn-id":"t","cwd":"/work","client":"codex-tui","input-messages":["go"],"last-assistant-message":"Done."}`,
			control.EventPushArgs{Event: "agent-turn-complete", Text: "Done.", AgentSession: agent}},
		{"notify without a type", "notify", `{"thread-id":"` + agent + `"}`, control.EventPushArgs{Event: control.EventHookError}},
		{"notify with a hook name", "notify", `{"hook_event_name":"Stop"}`, control.EventPushArgs{Event: control.EventHookError}},
		{"notify unreadable", "notify", `{"type":`, control.EventPushArgs{Event: control.EventHookError}},
	}
	for _, c := range cases {
		source := control.EventSourceHook
		if c.source == "notify" {
			source = control.EventSourceNotify
		}
		got := codexEvent(hookSession, source, strings.NewReader(c.payload))
		c.want.Session, c.want.Source = hookSession, source
		if got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
		if err := got.Validate(); err != nil {
			t.Errorf("%s: the event does not validate: %v", c.name, err)
		}
	}
}

// The payload of a hook is read up to the limit; the fields before the cut still count.
func TestCodexEventLimit(t *testing.T) {
	payload := `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"` +
		strings.Repeat("x", 2*maxHookPayload) + `"}}`
	ev := codexEvent(hookSession, control.EventSourceHook, strings.NewReader(payload))
	if ev.Event != "PermissionRequest" || ev.Tool != "Bash" || ev.Text != "" {
		t.Errorf("event %+v", ev)
	}
}

// The environment of the tool names the session; the hook says nothing and reaches nothing without it.
func TestCodexHookNeedsTheSessionFromTheEnvironment(t *testing.T) {
	dials := 0
	access := &serviceAccess{dial: func(ctx context.Context) (*client.Client, error) {
		dials++
		return nil, errors.New("no service")
	}}
	oldEnv := hookEnv
	t.Cleanup(func() { hookEnv = oldEnv })
	reads := 0
	in := readerFunc(func(p []byte) (int, error) { reads++; return 0, io.EOF })
	for _, session := range []string{"", "bad", "../abcdefghij23", hookSession + "x", "ABCDEFGHIJ23"} {
		hookEnv = func(k string) string {
			if k == "QCREW_SESSION" {
				return session
			}
			return "unrelated"
		}
		for _, args := range [][]string{{"codex"}, {"codex", "--notify", `{"type":"agent-turn-complete"}`}} {
			runHook(context.Background(), access, args, in)
		}
	}
	if dials != 0 || reads != 0 {
		t.Errorf("with no valid session the hook dialed %d times and read the input %d times", dials, reads)
	}
	// With a valid session it reports, from stdin or from the argument.
	hookEnv = func(string) string { return hookSession }
	runHook(context.Background(), access, []string{"codex"}, strings.NewReader(`{"hook_event_name":"Stop"}`))
	runHook(context.Background(), access, []string{"codex", "--notify", `{"type":"agent-turn-complete"}`}, in)
	if dials != 2 {
		t.Errorf("with a valid session the hook dialed %d times, want 2", dials)
	}
	// Anything else in the arguments ends it before the channel.
	for _, args := range [][]string{{"codex", "--session", hookSession}, {"codex", "extra"}, {"codex", "--bogus", "{}"}} {
		runHook(context.Background(), access, args, in)
	}
	if dials != 2 {
		t.Errorf("a malformed call dialed: %d", dials)
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// The hidden command ends with exit code 0 and no output for every form of the Codex hook.
func TestCodexHookCommandIsSilent(t *testing.T) {
	testenv.Setup(t)
	testenv.ShortRuntime(t)
	old := serviceCommand
	serviceCommand = func() (c background.Command) { t.Error("the hook started the service"); return c }
	t.Cleanup(func() { serviceCommand = old })
	oldIn, oldEnv := hookInput, hookEnv
	hookInput = func() io.Reader { return strings.NewReader(`{"hook_event_name":"Stop"}`) }
	t.Cleanup(func() { hookInput, hookEnv = oldIn, oldEnv })
	for _, session := range []string{"", hookSession} {
		hookEnv = func(string) string { return session }
		for _, args := range [][]string{
			{"hook", "codex"}, {"hook", "codex", "--notify", `{"type":"agent-turn-complete"}`}, {"hook", "codex", "--notify"},
			{"hook", "codex", "--bogus"},
		} {
			var stdout, stderr bytes.Buffer
			if code := Run(args, &stdout, &stderr); code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("%q with session %q: code=%d stdout=%q stderr=%q", args, session, code, stdout.String(), stderr.String())
			}
		}
	}
}
