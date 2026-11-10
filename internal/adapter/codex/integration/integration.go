// Package integration sets up the hooks of Codex in the user's Codex home: the entries in hooks.json that call
// qcrew, and their approval in config.toml. It reads the state of both files, previews a change and writes
// it; it changes only its own entries and keeps every other byte of both files.
//
// Keys and hashes of an approval follow codex-cli 0.167.0 (tag rust-v0.167.0 of github.com/openai/codex).
package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-crew/internal/adapter/codex"
)

// Status is how far the hooks of Codex are set up for qcrew.
type Status string

const (
	// Configured: every hook is in place and approved.
	Configured Status = "configured"
	// Missing: no hook of qcrew is in place.
	Missing Status = "missing"
	// Stale: a hook or its approval is missing, differs or no longer matches its position.
	Stale Status = "stale"
)

// The files in the Codex home.
const (
	HooksFile  = "hooks.json"
	ConfigFile = "config.toml"
)

// commandArgs follow the program in the command of a hook.
const commandArgs = " hook codex"

// Config names where the integration lives.
type Config struct {
	// CodexHome is the folder Codex reads its user configuration from, as Codex names it: CODEX_HOME with
	// symbolic links resolved, else ~/.codex as it is (codex-rs/utils/home-dir). It is part of every approval key.
	CodexHome string
	// DataDir is the qcrew data folder; backups go to backups/codex below it.
	DataDir string
	// Program is the qcrew program the hooks call.
	Program string
}

func (c Config) check() error {
	for _, p := range []struct{ name, path string }{
		{"Codex home", c.CodexHome}, {"qcrew data folder", c.DataDir}, {"qcrew program", c.Program},
	} {
		if !filepath.IsAbs(p.path) || filepath.Clean(p.path) != p.path {
			return fmt.Errorf("the path of the %s is not absolute and clean", p.name)
		}
	}
	return nil
}

func (c Config) hooksPath() string  { return filepath.Join(c.CodexHome, HooksFile) }
func (c Config) configPath() string { return filepath.Join(c.CodexHome, ConfigFile) }

// eventLabels are the event names Codex reads from hooks.json, with the label it uses in approval keys and
// hashes (codex-rs/hooks/src/lib.rs: HOOK_EVENT_NAMES, hook_event_key_label).
var eventLabels = map[string]string{
	"PreModelRequest": "pre_model_request", "PostModelRequest": "post_model_request",
	"PreToolUse": "pre_tool_use", "PermissionRequest": "permission_request", "PostToolUse": "post_tool_use",
	"PreCompact": "pre_compact", "PostCompact": "post_compact", "SessionStart": "session_start",
	"SessionEnd": "session_end", "UserPromptSubmit": "user_prompt_submit", "SubagentStart": "subagent_start",
	"SubagentStop": "subagent_stop", "Stop": "stop", "Interrupt": "interrupt",
}

// async tells whether the hook of an event runs in the background. Codex runs a SessionEnd hook synchronously
// whatever it says and warns about async there (codex-rs/hooks/src/engine/discovery.rs, append_matcher_groups).
func async(event string) bool { return event != "SessionEnd" }

// defaultTimeout is the timeout Codex gives a command hook without one; it is part of the hash
// (codex-rs/hooks/src/engine/discovery.rs, normalize_command_hook; SESSION_END_DEFAULT_TIMEOUT_SEC is 1).
func defaultTimeout(event string) int {
	if event == "SessionEnd" || event == "Interrupt" {
		return 1
	}
	return 600
}

// command is what the hooks run: the program, quoted for the shell, and the arguments.
func (c Config) command() string { return shellQuote(c.Program) + commandArgs }

var safeWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellQuote(word string) string {
	if safeWord.MatchString(word) {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// own tells whether a hook command is one of qcrew: it calls the Codex hook of this program or of any program
// named qcrew.
func (c Config) own(command string) bool {
	prog, ok := strings.CutSuffix(command, commandArgs)
	if !ok {
		return false
	}
	if inner, quoted := strings.CutPrefix(prog, "'"); quoted && strings.HasSuffix(inner, "'") {
		unq := strings.ReplaceAll(inner[:len(inner)-1], `'\''`, "'")
		if shellQuote(unq) == prog {
			prog = unq
		}
	}
	return prog == c.Program || filepath.Base(prog) == "qcrew"
}

// trustKey is the key of the approval of one handler (codex-rs/hooks/src/lib.rs, hook_key): the absolute path
// of hooks.json, the event label, the index of the matcher group and of the handler in it.
func trustKey(hooksPath, event string, group, handler int) string {
	return hooksPath + ":" + eventLabels[event] + ":" + strconv.Itoa(group) + ":" + strconv.Itoa(handler)
}

// trustHash is the hash Codex approves the qcrew handler of an event by. Codex hashes a normalized identity
// (codex-rs/hooks/src/engine/discovery.rs, hook_hash): the event label, the group's matcher (left out when
// absent, as for every event that ignores matchers; qcrew sets none) and the one handler with commandWindows
// dropped, the default timeout filled in, async as written and absent optional fields left out. It turns the
// identity into a TOML value and that into JSON with all object keys sorted, compact, and takes SHA-256 in
// lower-case hex with the prefix sha256: (codex-rs/config/src/fingerprint.rs, version_for_toml). Field names
// come from HookHandlerConfig in codex-rs/protocol/src/hooks.rs.
func (c Config) trustHash(event string) string {
	var b strings.Builder
	b.WriteString(`{"event_name":`)
	b.WriteString(serdeString(eventLabels[event]))
	b.WriteString(`,"hooks":[{"async":`)
	b.WriteString(strconv.FormatBool(async(event)))
	b.WriteString(`,"command":`)
	b.WriteString(serdeString(c.command()))
	b.WriteString(`,"timeout":`)
	b.WriteString(strconv.Itoa(defaultTimeout(event)))
	b.WriteString(`,"type":"command"}]}`)
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// serdeString writes s as serde_json does: only the quote, the backslash and control characters are escaped,
// with the short forms where JSON has them and lower-case hex otherwise.
func serdeString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if c < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// jsonText writes s as a JSON string without escaping HTML characters.
func jsonText(s string) string {
	var b strings.Builder
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

// groupText is the matcher group of qcrew for an event.
func (c Config) groupText(event string) string {
	handler := `{"type": "command", "command": ` + jsonText(c.command())
	if async(event) {
		handler += `, "async": true`
	}
	return `{"hooks": [` + handler + `}]}`
}

// hooksJSON is what qcrew reads from hooks.json.
type hooksJSON struct {
	root   *jvalue
	hooks  *jmember // nil when the file has no hooks
	events []*eventEntry
}

// eventEntry is the list of matcher groups of one event Codex knows.
type eventEntry struct {
	name   string
	member *jmember
	groups []*groupEntry
}

type groupEntry struct {
	value    *jvalue
	handlers int
	own      bool // every handler is one of qcrew; the group may be replaced or removed as a whole
	mixed    bool // a handler of qcrew in a group that also holds others; qcrew leaves it alone
	exact    bool // own and just the group qcrew writes
}

// readHooks reads hooks.json and refuses a file Codex would not load: it accepts only the keys description and
// hooks at the top (HooksFile in codex-rs/config/src/hook_config.rs denies unknown fields). Events Codex does
// not know are ignored, as Codex does.
func (c Config) readHooks(data []byte) (*hooksJSON, error) {
	root, err := parseJSON(data)
	if err != nil {
		return nil, err
	}
	if root.kind != jObject {
		return nil, errors.New("the file is not a JSON object")
	}
	h := &hooksJSON{root: root}
	for i := range root.members {
		m := &root.members[i]
		switch m.key {
		case "description":
			if m.value.kind != jString && !m.value.isNull() {
				return nil, errors.New("description is not a string")
			}
		case "hooks":
			if m.value.kind != jObject {
				return nil, errors.New("hooks is not an object")
			}
			h.hooks = m
		default:
			return nil, fmt.Errorf("unknown key %s at the top", jsonText(m.key))
		}
	}
	if h.hooks == nil {
		return h, nil
	}
	for i := range h.hooks.value.members {
		m := &h.hooks.value.members[i]
		if _, known := eventLabels[m.key]; !known {
			continue
		}
		if m.value.kind != jArray {
			return nil, fmt.Errorf("the hooks of %s are not a list", m.key)
		}
		e := &eventEntry{name: m.key, member: m}
		for gi, g := range m.value.elems {
			ge, err := c.readGroup(m.key, g)
			if err != nil {
				return nil, fmt.Errorf("group %d of %s: %w", gi, m.key, err)
			}
			e.groups = append(e.groups, ge)
		}
		h.events = append(h.events, e)
	}
	return h, nil
}

func (c Config) readGroup(event string, g *jvalue) (*groupEntry, error) {
	if g.kind != jObject {
		return nil, errors.New("not an object")
	}
	if m := g.member("matcher"); m != nil && m.kind != jString && !m.isNull() {
		return nil, errors.New("the matcher is not a string")
	}
	ge := &groupEntry{value: g}
	hooks := g.member("hooks")
	if hooks == nil {
		return ge, nil
	}
	if hooks.kind != jArray {
		return nil, errors.New("hooks is not a list")
	}
	own := 0
	for _, hd := range hooks.elems {
		if hd.kind != jObject {
			return nil, errors.New("a handler is not an object")
		}
		t := hd.member("type")
		if t == nil || t.kind != jString {
			return nil, errors.New("a handler has no type")
		}
		if t.str != "command" {
			continue
		}
		cmd := hd.member("command")
		if cmd == nil || cmd.kind != jString {
			return nil, errors.New("a command handler has no command")
		}
		if c.own(cmd.str) {
			own++
		}
	}
	ge.handlers = len(hooks.elems)
	onlyGroupKeys := !slices.ContainsFunc(g.members, func(m jmember) bool {
		return m.key != "matcher" && m.key != "hooks"
	})
	ge.own = own > 0 && own == ge.handlers && onlyGroupKeys
	ge.mixed = own > 0 && !ge.own
	ge.exact = ge.own && c.isWanted(event, g)
	return ge, nil
}

// isWanted tells whether a group is exactly the one qcrew writes for the event, key for key.
func (c Config) isWanted(event string, g *jvalue) bool {
	if len(g.members) != 1 || g.members[0].key != "hooks" || len(g.members[0].value.elems) != 1 {
		return false
	}
	h := g.members[0].value.elems[0]
	keys := 2
	if async(event) {
		keys = 3
		if a := h.member("async"); a == nil || a.raw != "true" {
			return false
		}
	}
	t, cmd := h.member("type"), h.member("command")
	return len(h.members) == keys && t != nil && t.kind == jString && t.str == "command" &&
		cmd != nil && cmd.kind == jString && cmd.str == c.command()
}

// position is a handler of qcrew in hooks.json.
type position struct {
	event          string
	group, handler int
	exact          bool
}

// ownPositions lists the handlers of the groups of qcrew. A handler of qcrew in a mixed group is not among
// them: its approval is left alone like the group.
func (h *hooksJSON) ownPositions() []position {
	var out []position
	if h == nil {
		return nil
	}
	for _, e := range h.events {
		for gi, g := range e.groups {
			if !g.own {
				continue
			}
			for hi := range g.handlers {
				out = append(out, position{event: e.name, group: gi, handler: hi, exact: g.exact})
			}
		}
	}
	return out
}

func (h *hooksJSON) event(name string) *eventEntry {
	if h == nil {
		return nil
	}
	for _, e := range h.events {
		if e.name == name {
			return e
		}
	}
	return nil
}

func ownGroups(e *eventEntry) []int {
	var out []int
	if e == nil {
		return nil
	}
	for i, g := range e.groups {
		if g.own {
			out = append(out, i)
		}
	}
	return out
}

// snapshot is the content of both files.
type snapshot struct {
	hooks, config             []byte
	hooksExists, configExists bool
}

// state is what qcrew reads from both files.
type state struct {
	cfg   Config
	snap  snapshot
	hooks *hooksJSON // nil when hooks.json is missing
	conf  *tomlDoc
}

func (c Config) read(snap snapshot) (*state, error) {
	s := &state{cfg: c, snap: snap}
	if snap.hooksExists {
		h, err := c.readHooks(snap.hooks)
		if err != nil {
			return nil, fmt.Errorf("%s cannot be read as Codex reads it: %w", HooksFile, err)
		}
		s.hooks = h
	}
	conf, err := parseTOML(snap.config)
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read as TOML: %w", ConfigFile, err)
	}
	s.conf = conf
	return s, nil
}

// Report is the result of an inspection.
type Report struct {
	Status Status
	// Problems say what is stale, one line each.
	Problems []string
}

func (s *state) report() (Report, error) {
	var problems []string
	found := false
	wanted := codex.HookEvents()
	if s.hooks != nil {
		for _, e := range s.hooks.events {
			for _, g := range e.groups {
				found = found || g.own || g.mixed
				if g.mixed {
					problems = append(problems, e.name+": a qcrew hook shares a group with other hooks")
				}
			}
			if len(ownGroups(e)) > 0 && !slices.Contains(wanted, e.name) {
				problems = append(problems, e.name+": qcrew no longer uses this hook")
			}
		}
	}
	if !found {
		return Report{Status: Missing}, nil
	}
	for _, ev := range wanted {
		e := s.hooks.event(ev)
		own := ownGroups(e)
		switch {
		case len(own) == 0:
			problems = append(problems, ev+": the hook is missing")
			continue
		case len(own) > 1:
			problems = append(problems, ev+": the hook is set up more than once")
			continue
		case !e.groups[own[0]].exact:
			problems = append(problems, ev+": the hook differs")
			continue
		}
		t, err := s.conf.stateTable(trustKey(s.cfg.hooksPath(), ev, own[0], 0))
		if err != nil {
			return Report{}, fmt.Errorf("%s: %w", ConfigFile, err)
		}
		if t < 0 {
			problems = append(problems, ev+": the hook is not approved")
			continue
		}
		switch hash, disabled := s.conf.approval(t); {
		case hash != s.cfg.trustHash(ev):
			problems = append(problems, ev+": the approval does not match the hook")
		case disabled:
			problems = append(problems, ev+": the hook is disabled")
		}
	}
	if len(problems) > 0 {
		return Report{Status: Stale, Problems: problems}, nil
	}
	return Report{Status: Configured}, nil
}

// Action is a change to plan.
type Action int

const (
	// Setup puts the hooks of qcrew in place and approves them.
	Setup Action = iota
	// Remove takes them out with their approvals.
	Remove
)

// plan computes the new content of both files.
func (s *state) plan(a Action) (snapshot, []string, error) {
	var newHooks []byte
	hooksExist := s.snap.hooksExists
	var warnings []string
	var err error
	switch {
	case a == Setup && !s.snap.hooksExists:
		newHooks, hooksExist = []byte(s.cfg.newHooksFile()), true
	case a == Setup:
		newHooks, warnings, err = s.setupHooks()
	case s.snap.hooksExists:
		newHooks, hooksExist, warnings, err = s.removeHooks()
	}
	if err != nil {
		return snapshot{}, nil, err
	}
	next := snapshot{hooks: newHooks, hooksExists: hooksExist}
	var after *hooksJSON
	if hooksExist {
		if after, err = s.cfg.readHooks(newHooks); err != nil {
			return snapshot{}, nil, fmt.Errorf("the new %s would be invalid: %w", HooksFile, err)
		}
	}
	want := map[string]string{}
	var order []string
	for _, p := range after.ownPositions() {
		if !p.exact {
			return snapshot{}, nil, fmt.Errorf("the new %s holds a qcrew hook qcrew did not write", HooksFile)
		}
		k := trustKey(s.cfg.hooksPath(), p.event, p.group, p.handler)
		want[k] = s.cfg.trustHash(p.event)
		order = append(order, k)
	}
	var drop []string
	for _, p := range s.hooks.ownPositions() {
		if k := trustKey(s.cfg.hooksPath(), p.event, p.group, p.handler); want[k] == "" {
			drop = append(drop, k)
		}
	}
	next.config, err = s.conf.planApprovals(order, want, drop)
	if err != nil {
		return snapshot{}, nil, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	next.configExists = s.snap.configExists || len(next.config) > 0
	// Codex reads a missing config.toml like an empty one; a file the removal leaves empty goes.
	if a == Remove && len(s.snap.config) > 0 && len(next.config) == 0 {
		next.configExists = false
	}
	return next, warnings, nil
}

// planApprovals sets the approval tables of want, in the given order, and removes those of drop. A table that
// approves the wanted hash already stays as it is; new tables go to the end of the file.
func (d *tomlDoc) planApprovals(order []string, want map[string]string, drop []string) ([]byte, error) {
	var edits []edit
	var added strings.Builder
	for _, k := range order {
		t, err := d.stateTable(k)
		if err != nil {
			return nil, err
		}
		text := "[hooks.state." + tomlQuote(k) + "]\ntrusted_hash = " + tomlQuote(want[k]) + "\n"
		switch {
		case t < 0:
			added.WriteString(text)
		default:
			if hash, disabled := d.approval(t); hash != want[k] || disabled {
				edits = append(edits, edit{d.tables[t].start, d.tables[t].end, text})
			}
		}
	}
	var removed []edit
	for _, k := range drop {
		t, err := d.stateTable(k)
		if err != nil {
			return nil, err
		}
		if t >= 0 {
			removed = append(removed, edit{start: d.tables[t].start, end: d.tables[t].end})
		}
	}
	removed = mergeRemovals(removed)
	// A last table without a line break was appended after a line break of qcrew's own; it goes with it, unless
	// a replaced table ends there.
	for i, r := range removed {
		replaced := slices.ContainsFunc(edits, func(e edit) bool { return e.end == r.start })
		if r.end == len(d.data) && d.data[r.end-1] != '\n' && r.start > 0 && d.data[r.start-1] == '\n' && !replaced {
			removed[i].start--
		}
	}
	out, err := applyEdits(d.data, append(edits, removed...))
	if err != nil {
		return nil, err
	}
	if added.Len() == 0 {
		return out, nil
	}
	text := added.String()
	if len(out) > 0 && out[len(out)-1] != '\n' {
		// The file's last line has no line break: qcrew adds one before its tables and leaves its own last
		// line without, so that removing the tables restores the file.
		text = "\n" + strings.TrimSuffix(text, "\n")
	}
	return append(out, text...), nil
}

func mergeRemovals(rs []edit) []edit {
	slices.SortFunc(rs, func(a, b edit) int { return a.start - b.start })
	var out []edit
	for _, r := range rs {
		if n := len(out); n > 0 && r.start <= out[n-1].end {
			out[n-1].end = max(out[n-1].end, r.end)
			continue
		}
		out = append(out, r)
	}
	return out
}

// edit replaces the bytes from start to end with text.
type edit struct {
	start, end int
	text       string
}

func applyEdits(data []byte, edits []edit) ([]byte, error) {
	slices.SortStableFunc(edits, func(a, b edit) int { return a.start - b.start })
	var out []byte
	at := 0
	for _, e := range edits {
		if e.start < at || e.end < e.start || e.end > len(data) {
			return nil, errors.New("overlapping changes")
		}
		out = append(out, data[at:e.start]...)
		out = append(out, e.text...)
		at = e.end
	}
	return append(out, data[at:]...), nil
}

// span is a child of a JSON container: an element, or a member from its key to the end of its value.
type span struct{ start, end int }

func elemSpans(v *jvalue) []span {
	var out []span
	for _, e := range v.elems {
		out = append(out, span{e.start, e.end})
	}
	return out
}

func memberSpans(v *jvalue) []span {
	var out []span
	for _, m := range v.members {
		out = append(out, span{m.start, m.value.end})
	}
	return out
}

// removeChildren deletes the children at the given indexes with the separators that came with them: a child
// appended after others takes the comma and space before it, the first child the space after the opening
// bracket. That is the reverse of appendChild, so removing what was appended restores the text.
func removeChildren(container *jvalue, children []span, remove []int) []edit {
	gone := map[int]bool{}
	for _, i := range remove {
		gone[i] = true
	}
	var edits []edit
	for a := 0; a < len(children); a++ {
		if !gone[a] {
			continue
		}
		b := a
		for b+1 < len(children) && gone[b+1] {
			b++
		}
		switch {
		case a > 0:
			edits = append(edits, edit{start: children[a-1].end, end: children[b].end})
		case b == len(children)-1:
			edits = append(edits, edit{start: container.start + 1, end: children[b].end})
		default:
			edits = append(edits, edit{start: children[a].start, end: children[b+1].start})
		}
		a = b
	}
	return edits
}

// appendChild inserts text as the last child of a container.
func appendChild(data []byte, container *jvalue, children []span, text string) edit {
	sep := childSep(data, container, children)
	if len(children) == 0 {
		return edit{container.start + 1, container.start + 1, sep + text}
	}
	end := children[len(children)-1].end
	return edit{end, end, "," + sep + text}
}

// childSep is the space before a new child: what stands before the last child if it starts a line, else one
// space; in an empty container a line break with one indentation step more than the container's line.
func childSep(data []byte, container *jvalue, children []span) string {
	if len(children) == 0 {
		return newline(data) + lineIndent(data, container.start) + "  "
	}
	i := children[len(children)-1].start
	j := i
	for j > 0 && strings.ContainsRune(" \t\r\n", rune(data[j-1])) {
		j--
	}
	ws := string(data[j:i])
	if k := strings.LastIndexByte(ws, '\n'); k >= 0 {
		if k > 0 && ws[k-1] == '\r' {
			k--
		}
		return ws[k:]
	}
	return " "
}

func newline(data []byte) string {
	if i := slices.Index(data, '\n'); i > 0 && data[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

func lineIndent(data []byte, pos int) string {
	start := pos
	for start > 0 && data[start-1] != '\n' {
		start--
	}
	end := start
	for end < pos && (data[end] == ' ' || data[end] == '\t') {
		end++
	}
	return string(data[start:end])
}

// eventMember is the member of an event with the qcrew group in a list, laid out below sep.
func (c Config) eventMember(event, sep string) string {
	inner, closing := sep+"  ", sep
	if !strings.Contains(sep, "\n") {
		inner, closing = "", ""
	}
	return jsonText(event) + ": [" + inner + c.groupText(event) + closing + "]"
}

// hooksObject is an object with the members of the given events, laid out below sep.
func (c Config) hooksObject(events []string, sep string) string {
	inner := sep + "  "
	if !strings.Contains(sep, "\n") {
		inner, sep = " ", " "
	}
	parts := make([]string, len(events))
	for i, ev := range events {
		parts[i] = c.eventMember(ev, inner)
	}
	return "{" + inner + strings.Join(parts, ","+inner) + sep + "}"
}

func (c Config) newHooksFile() string {
	return "{\n  \"hooks\": " + c.hooksObject(codex.HookEvents(), "\n  ") + "\n}\n"
}

// setupHooks plans the hooks of qcrew in an existing hooks.json. A group of qcrew that differs is replaced in
// place, a missing one is appended after the others, so the index and with it the approval of every other
// hook stays. Groups of qcrew beyond the first, or for an event qcrew no longer uses, are removed.
func (s *state) setupHooks() ([]byte, []string, error) {
	data, h, c := s.snap.hooks, s.hooks, s.cfg
	var edits, removals []edit
	var warnings []string
	var goneEvents []int
	wanted := codex.HookEvents()
	for _, e := range h.events {
		own := ownGroups(e)
		if slices.ContainsFunc(e.groups, func(g *groupEntry) bool { return g.mixed }) {
			warnings = append(warnings, e.name+": a qcrew hook shares a group with other hooks; remove it by hand")
		}
		arr := e.member.value
		switch {
		case slices.Contains(wanted, e.name) && len(own) == 0:
			edits = append(edits, appendChild(data, arr, elemSpans(arr), c.groupText(e.name)))
		case slices.Contains(wanted, e.name):
			if g := e.groups[own[0]]; !g.exact {
				edits = append(edits, edit{g.value.start, g.value.end, c.groupText(e.name)})
			}
			removals = append(removals, removeChildren(arr, elemSpans(arr), own[1:])...)
			warnings = append(warnings, shiftWarning(e, own[1:])...)
		case len(own) > 0 && len(own) == len(e.groups):
			goneEvents = append(goneEvents, memberIndex(h.hooks.value, e.member))
		case len(own) > 0:
			removals = append(removals, removeChildren(arr, elemSpans(arr), own)...)
			warnings = append(warnings, shiftWarning(e, own)...)
		}
	}
	if h.hooks != nil {
		removals = append(removals, removeChildren(h.hooks.value, memberSpans(h.hooks.value), goneEvents)...)
	}
	var missing []string
	for _, ev := range wanted {
		if h.event(ev) == nil {
			missing = append(missing, ev)
		}
	}
	if len(missing) > 0 {
		if h.hooks == nil {
			sep := childSep(data, h.root, memberSpans(h.root))
			edits = append(edits, appendChild(data, h.root, memberSpans(h.root), `"hooks": `+c.hooksObject(missing, sep)))
		} else {
			// New events follow the last event that stays; an insertion sorts before a removal at the same place.
			obj := h.hooks.value
			var children []span
			for i, sp := range memberSpans(obj) {
				if !slices.Contains(goneEvents, i) {
					children = append(children, sp)
				}
			}
			sep := childSep(data, obj, children)
			parts := make([]string, len(missing))
			for i, ev := range missing {
				parts[i] = c.eventMember(ev, sep)
			}
			edits = append(edits, appendChild(data, obj, children, strings.Join(parts, ","+sep)))
		}
	}
	out, err := applyEdits(data, append(edits, removals...))
	return out, warnings, err
}

// removeHooks plans hooks.json without the groups of qcrew. An event left without groups goes, and so does the
// hooks object left without events; a file left with nothing at all is deleted, which Codex treats like a
// missing file (load_hooks_json in codex-rs/hooks/src/engine/discovery.rs skips a file without hooks).
func (s *state) removeHooks() ([]byte, bool, []string, error) {
	data, h := s.snap.hooks, s.hooks
	if h.hooks == nil {
		return data, true, nil, nil
	}
	var edits []edit
	var warnings []string
	obj := h.hooks.value
	var goneEvents []int
	for _, e := range h.events {
		own := ownGroups(e)
		if slices.ContainsFunc(e.groups, func(g *groupEntry) bool { return g.mixed }) {
			warnings = append(warnings, e.name+": a qcrew hook shares a group with other hooks; remove it by hand")
		}
		switch {
		case len(own) == 0:
		case len(own) > 0 && len(own) == len(e.groups):
			goneEvents = append(goneEvents, memberIndex(obj, e.member))
		default:
			edits = append(edits, removeChildren(e.member.value, elemSpans(e.member.value), own)...)
			warnings = append(warnings, shiftWarning(e, own)...)
		}
	}
	if len(goneEvents) > 0 && len(goneEvents) == len(obj.members) {
		if len(h.root.members) == 1 {
			return nil, false, warnings, nil
		}
		edits = append(edits, removeChildren(h.root, memberSpans(h.root), []int{memberIndex(h.root, h.hooks)})...)
	} else {
		edits = append(edits, removeChildren(obj, memberSpans(obj), goneEvents)...)
	}
	out, err := applyEdits(data, edits)
	return out, true, warnings, err
}

func memberIndex(obj *jvalue, m *jmember) int {
	for i := range obj.members {
		if &obj.members[i] == m {
			return i
		}
	}
	return -1
}

// shiftWarning names the hooks of others that move up when groups of qcrew are removed before them: Codex keys
// an approval by position, so they need approving again. qcrew never approves them itself.
func shiftWarning(e *eventEntry, removed []int) []string {
	if len(removed) == 0 {
		return nil
	}
	moved := 0
	for i := removed[0] + 1; i < len(e.groups); i++ {
		if !slices.Contains(removed, i) {
			moved++
		}
	}
	if moved == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%s: %d other hook group(s) move up and must be approved again in Codex", e.name, moved)}
}

// CodexHome returns the folder Codex reads its user configuration from, as Codex resolves it
// (codex-rs/utils/home-dir, find_codex_home): CODEX_HOME, which must be a folder, with symbolic links resolved;
// else .codex in home, as it is.
func CodexHome(getenv func(string) string, home string) (string, error) {
	v := getenv("CODEX_HOME")
	if v == "" {
		if !filepath.IsAbs(home) {
			return "", errors.New("the home directory is not known")
		}
		return filepath.Join(filepath.Clean(home), ".codex"), nil
	}
	abs, err := filepath.Abs(v)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil {
		return "", fmt.Errorf("CODEX_HOME cannot be resolved: %w", err)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", errors.New("CODEX_HOME is not a folder")
	}
	return abs, nil
}
