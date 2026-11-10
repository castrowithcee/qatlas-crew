package inbox

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Kind says why an entry exists. Stale is only carried as a kind; nothing creates it yet.
type Kind string

// The kinds of entry.
const (
	KindPermission Kind = "permission"
	KindQuestion   Kind = "question"
	KindIdle       Kind = "idle"
	KindError      Kind = "error"
	KindStale      Kind = "stale"
)

// Status is where an entry stands. open: waits for the person. answered: an answer is taken and being sent.
// delivered: the answer reached the session's window. picked: the session worked on after it. closed: given up
// without an answer, or answered in the session itself.
type Status string

// The statuses of an entry.
const (
	StatusOpen      Status = "open"
	StatusAnswered  Status = "answered"
	StatusDelivered Status = "delivered"
	StatusPicked    Status = "picked"
	StatusClosed    Status = "closed"
)

// The bounds of an entry and of an answer.
const (
	// MaxLines is how many screen lines an entry keeps.
	MaxLines = 20
	// MaxLineRunes and MaxTextRunes bound a screen line and the text of an entry, in characters.
	MaxLineRunes = 400
	MaxTextRunes = 1000
	// MaxAnswerRunes is the longest answer in characters; a longer one is refused, not cut.
	MaxAnswerRunes = 1000
	// KeepFinished is how many picked or closed entries a project keeps; older ones are dropped.
	KeepFinished = 50
)

// Entry is one item that waits for a person.
type Entry struct {
	ID      string
	Project string
	Session string
	Kind    Kind
	// Text is plain text from the session's tool; Lines are the last screen lines when the entry arose. Both
	// are untrusted data, cleaned of control characters.
	Text       string
	Lines      []string
	CreatedAt  time.Time
	Status     Status
	Answer     string
	AnsweredAt time.Time
}

// Refusals of the store; errors.Is matches them.
var (
	ErrNotFound    = errors.New("no such inbox entry")
	ErrState       = errors.New("the entry is not in a state that allows this")
	ErrAnswerEmpty = errors.New("the answer is empty")
	ErrAnswerLong  = errors.New("the answer is too long")
)

var idPattern = regexp.MustCompile(`^[a-z2-7]{12}$`)

// ValidID reports whether id has the form of an entry id: 12 characters of base32 in lower case.
func ValidID(id string) bool { return idPattern.MatchString(id) }

var idEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return strings.ToLower(idEncoding.EncodeToString(b[:]))[:12], nil
}

// Store holds the entries in memory, in the order they arose. Its methods are safe for concurrent use. The
// rules: a session has at most one open entry; a new waiting state replaces an open idle entry and never an
// answered one; a further waiting state next to an open permission or question entry makes no second entry.
type Store struct {
	mu      sync.Mutex
	now     func() time.Time
	newID   func() (string, error)
	entries []*Entry
}

// NewStore returns an empty store that reads the time from now.
func NewStore(now func() time.Time) *Store { return &Store{now: now, newID: newID} }

// replaces reports whether a new entry of kind next takes the place of an open entry of kind old. A dead tool
// (error, stale) replaces every open entry that waits for the tool; a waiting state replaces only idle.
func replaces(old, next Kind) bool {
	switch next {
	case KindError, KindStale:
		return old == KindIdle || old == KindPermission || old == KindQuestion
	}
	return old == KindIdle && next != KindIdle
}

func (s *Store) openOf(session string) *Entry {
	for _, e := range s.entries {
		if e.Session == session && e.Status == StatusOpen {
			return e
		}
	}
	return nil
}

// Wants reports whether Open would make an entry for the session now, so that the caller can skip the
// work of gathering its content.
func (s *Store) Wants(session string, kind Kind) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.openOf(session)
	return cur == nil || replaces(cur.Kind, kind)
}

// Open makes an open entry for the session unless the rules forbid it, and returns it. text and lines are
// cleaned and bounded here; lines keeps the last MaxLines.
func (s *Store) Open(project, session string, kind Kind, text string, lines []string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur := s.openOf(session); cur != nil {
		if !replaces(cur.Kind, kind) {
			return Entry{}, false
		}
		cur.Status = StatusClosed
	}
	id, err := s.newID()
	for err == nil && s.byID(id) != nil {
		id, err = s.newID()
	}
	if err != nil {
		return Entry{}, false
	}
	if len(lines) > MaxLines {
		lines = lines[len(lines)-MaxLines:]
	}
	clean := make([]string, len(lines))
	for i, l := range lines {
		clean[i] = Clean(l, MaxLineRunes)
	}
	e := &Entry{ID: id, Project: project, Session: session, Kind: kind, Text: Clean(text, MaxTextRunes), Lines: clean,
		CreatedAt: s.now(), Status: StatusOpen}
	s.entries = append(s.entries, e)
	s.trim(project)
	return *copyEntry(e), true
}

func (s *Store) byID(id string) *Entry {
	for _, e := range s.entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

func copyEntry(e *Entry) *Entry {
	c := *e
	c.Lines = append([]string(nil), e.Lines...)
	return &c
}

// trim drops the oldest finished entries of a project beyond KeepFinished.
func (s *Store) trim(project string) {
	finished := 0
	for _, e := range s.entries {
		if e.Project == project && (e.Status == StatusPicked || e.Status == StatusClosed) {
			finished++
		}
	}
	if finished <= KeepFinished {
		return
	}
	drop := finished - KeepFinished
	kept := s.entries[:0]
	for _, e := range s.entries {
		if drop > 0 && e.Project == project && (e.Status == StatusPicked || e.Status == StatusClosed) {
			drop--
			continue
		}
		kept = append(kept, e)
	}
	clear(s.entries[len(kept):])
	s.entries = kept
}

// Get returns the entry with the given id.
func (s *Store) Get(id string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.byID(id); e != nil {
		return *copyEntry(e), nil
	}
	return Entry{}, ErrNotFound
}

// List returns the entries of a project, or of every project if it is empty, in the order they arose. Without
// all it leaves out the picked and the closed ones.
func (s *Store) List(project string, all bool) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Entry{}
	for _, e := range s.entries {
		if project != "" && e.Project != project {
			continue
		}
		if !all && (e.Status == StatusPicked || e.Status == StatusClosed) {
			continue
		}
		out = append(out, *copyEntry(e))
	}
	return out
}

// OpenCount counts the open entries of a project.
func (s *Store) OpenCount(project string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.entries {
		if e.Project == project && e.Status == StatusOpen {
			n++
		}
	}
	return n
}

// Answer takes the answer for an open entry: it becomes answered. The caller sends the answer, then calls
// Delivered, or Revert if the sending failed.
func (s *Store) Answer(id, text string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.byID(id)
	switch {
	case e == nil:
		return Entry{}, ErrNotFound
	case e.Status != StatusOpen:
		return Entry{}, ErrState
	}
	e.Status, e.Answer, e.AnsweredAt = StatusAnswered, text, s.now()
	return *copyEntry(e), nil
}

// Delivered notes that the answer reached the window. An entry the session has picked up meanwhile stays
// picked.
func (s *Store) Delivered(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.byID(id); e != nil && e.Status == StatusAnswered {
		e.Status = StatusDelivered
	}
}

// Revert takes an answer back after the sending failed: the entry is open again, unless the session has
// another open entry by now, in which case it is closed.
func (s *Store) Revert(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.byID(id)
	if e == nil || e.Status != StatusAnswered {
		return
	}
	e.Answer, e.AnsweredAt = "", time.Time{}
	if s.openOf(e.Session) != nil {
		e.Status = StatusClosed
		return
	}
	e.Status = StatusOpen
}

// Close closes an open or delivered entry without an answer.
func (s *Store) Close(id string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.byID(id)
	switch {
	case e == nil:
		return Entry{}, ErrNotFound
	case e.Status != StatusOpen && e.Status != StatusDelivered:
		return Entry{}, ErrState
	}
	e.Status = StatusClosed
	return *copyEntry(e), nil
}

// Resumed notes that the session works again: its answered or delivered entries are picked, and its open
// permission, question and idle entries were answered in the session itself, so they close. An open error
// entry stays.
func (s *Store) Resumed(session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.Session != session {
			continue
		}
		switch {
		case e.Status == StatusAnswered || e.Status == StatusDelivered:
			e.Status = StatusPicked
		case e.Status == StatusOpen && (e.Kind == KindPermission || e.Kind == KindQuestion || e.Kind == KindIdle):
			e.Status = StatusClosed
		}
	}
}

// CloseSession closes the open entries of a session that was stopped.
func (s *Store) CloseSession(session string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.Session == session && e.Status == StatusOpen {
			e.Status = StatusClosed
		}
	}
}

// DropProject forgets every entry of a removed project.
func (s *Store) DropProject(project string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.entries[:0]
	for _, e := range s.entries {
		if e.Project != project {
			kept = append(kept, e)
		}
	}
	clear(s.entries[len(kept):])
	s.entries = kept
}

// bidi reports the characters that reorder displayed text.
func bidi(r rune) bool { return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) }

// Clean makes foreign text plain and short: invalid UTF-8 and control characters go, line breaks and tabs
// become spaces, and at most max characters stay, the last of them an ellipsis if the text was longer.
func Clean(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.ToValidUTF8(s, "") {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			r = ' '
		case unicode.IsControl(r) || bidi(r):
			continue
		}
		if n == max {
			out := []rune(b.String())
			return string(out[:max-1]) + "…"
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// CleanAnswer makes the text of an answer one plain line: line breaks become spaces (a line break would send
// the text early), other control characters are removed, white space at the ends goes. An empty result and
// a result beyond MaxAnswerRunes are refused.
func CleanAnswer(text string) (string, error) {
	text = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(text)
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || bidi(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(text, ""))
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return "", ErrAnswerEmpty
	case utf8.RuneCountInString(text) > MaxAnswerRunes:
		return "", ErrAnswerLong
	}
	return text, nil
}

// presetKeys maps the predefined answers to a permission prompt to the keys of tmux. ASSUMPTION until the
// test against the real tool proves it: the prompt of Claude Code offers 1 yes, 2 yes and do not ask again,
// and Escape declines.
var presetKeys = map[string]string{"yes": "1", "always": "2", "no": "Escape"}

// PresetKey returns the key a predefined answer stands for. Only a permission entry has predefined answers;
// for every other kind the word is typed as text.
func PresetKey(kind Kind, answer string) (string, bool) {
	if kind != KindPermission {
		return "", false
	}
	key, ok := presetKeys[answer]
	return key, ok
}
