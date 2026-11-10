package inbox

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestStore() (*Store, *clock) {
	c := &clock{t: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)}
	return NewStore(c.now), c
}

// The replacement rules: what happens to a session's open entry when a new one of the given kind arises.
func TestOpenRules(t *testing.T) {
	cases := []struct {
		name    string
		first   Kind
		then    Kind
		created bool
		oldWas  Status
	}{
		{"waiting replaces idle", KindIdle, KindPermission, true, StatusClosed},
		{"question replaces idle", KindIdle, KindQuestion, true, StatusClosed},
		{"idle next to idle", KindIdle, KindIdle, false, StatusOpen},
		{"idle next to permission", KindPermission, KindIdle, false, StatusOpen},
		{"permission next to permission", KindPermission, KindPermission, false, StatusOpen},
		{"question next to permission", KindPermission, KindQuestion, false, StatusOpen},
		{"permission next to question", KindQuestion, KindPermission, false, StatusOpen},
		{"error replaces permission", KindPermission, KindError, true, StatusClosed},
		{"error replaces idle", KindIdle, KindError, true, StatusClosed},
		{"error next to error", KindError, KindError, false, StatusOpen},
		{"permission next to error", KindError, KindPermission, false, StatusOpen},
		{"stale replaces question", KindQuestion, KindStale, true, StatusClosed},
	}
	for _, c := range cases {
		s, _ := newTestStore()
		first, ok := s.Open("p", "sess", c.first, "a", nil)
		if !ok {
			t.Fatalf("%s: the first entry was not made", c.name)
		}
		if got := s.Wants("sess", c.then); got != c.created {
			t.Errorf("%s: Wants = %v, want %v", c.name, got, c.created)
		}
		_, ok = s.Open("p", "sess", c.then, "b", nil)
		old, _ := s.Get(first.ID)
		open := 0
		for _, e := range s.List("", true) {
			if e.Status == StatusOpen {
				open++
			}
		}
		if ok != c.created || old.Status != c.oldWas || open != 1 {
			t.Errorf("%s: created %v, old entry %s, %d open; want %v, %s, 1", c.name, ok, old.Status, open, c.created, c.oldWas)
		}
	}
}

func TestOpenKeepsAnsweredAndSeparatesSessions(t *testing.T) {
	s, _ := newTestStore()
	a, _ := s.Open("p", "one", KindPermission, "x", nil)
	if _, err := s.Answer(a.ID, "yes"); err != nil {
		t.Fatal(err)
	}
	// An answered entry is never replaced; the new wait state gets an entry of its own.
	if _, ok := s.Open("p", "one", KindPermission, "y", nil); !ok {
		t.Error("a new permission next to an answered entry was refused")
	}
	if got, _ := s.Get(a.ID); got.Status != StatusAnswered {
		t.Errorf("the answered entry is now %s", got.Status)
	}
	if _, ok := s.Open("p", "two", KindPermission, "z", nil); !ok {
		t.Error("another session was refused")
	}
	if n := s.OpenCount("p"); n != 2 {
		t.Errorf("OpenCount = %d, want 2", n)
	}
	if n := s.OpenCount("other"); n != 0 {
		t.Errorf("OpenCount of another project = %d", n)
	}
}

func TestOpenCleansAndBounds(t *testing.T) {
	s, _ := newTestStore()
	var lines []string
	for i := range 30 {
		lines = append(lines, "line "+string(rune('a'+i%26)))
	}
	lines[29] = "esc\x1b[31m red\x07" + strings.Repeat("x", 500)
	e, _ := s.Open("p", "s", KindPermission, "tool\x1b]0;x\x07 ‮ name\nnext", lines)
	if len(e.Lines) != MaxLines || e.Lines[0] != lines[10] {
		t.Errorf("lines: %d, first %q", len(e.Lines), e.Lines[0])
	}
	last := e.Lines[MaxLines-1]
	if strings.ContainsAny(last, "\x1b\x07") || len([]rune(last)) != MaxLineRunes || !strings.HasSuffix(last, "…") {
		t.Errorf("last line not cleaned and bounded: %q", last)
	}
	if e.Text != "tool]0;x  name next" {
		t.Errorf("text = %q", e.Text)
	}
	e.Lines[0] = "changed"
	if got, _ := s.Get(e.ID); got.Lines[0] == "changed" {
		t.Error("an entry returned by the store shares its lines with the store")
	}
}

func TestStateMachine(t *testing.T) {
	s, c := newTestStore()
	e, _ := s.Open("p", "s", KindPermission, "t", nil)
	if e.Status != StatusOpen || !e.CreatedAt.Equal(c.t) || e.Answer != "" || !e.AnsweredAt.IsZero() {
		t.Fatalf("new entry: %+v", e)
	}
	c.t = c.t.Add(time.Minute)
	a, err := s.Answer(e.ID, "yes")
	if err != nil || a.Status != StatusAnswered || a.Answer != "yes" || !a.AnsweredAt.Equal(c.t) {
		t.Fatalf("answer: %+v, %v", a, err)
	}
	if _, err := s.Answer(e.ID, "again"); !errors.Is(err, ErrState) {
		t.Errorf("a second answer: %v", err)
	}
	if _, err := s.Close(e.ID); !errors.Is(err, ErrState) {
		t.Errorf("closing an answered entry: %v", err)
	}
	s.Revert(e.ID)
	if got, _ := s.Get(e.ID); got.Status != StatusOpen || got.Answer != "" || !got.AnsweredAt.IsZero() {
		t.Errorf("after a revert: %+v", got)
	}
	if _, err := s.Answer(e.ID, "yes"); err != nil {
		t.Fatal(err)
	}
	s.Delivered(e.ID)
	if got, _ := s.Get(e.ID); got.Status != StatusDelivered {
		t.Errorf("delivered: %s", got.Status)
	}
	s.Resumed("s")
	if got, _ := s.Get(e.ID); got.Status != StatusPicked {
		t.Errorf("picked: %s", got.Status)
	}
	if _, err := s.Close(e.ID); !errors.Is(err, ErrState) {
		t.Errorf("closing a picked entry: %v", err)
	}
	if _, err := s.Close("zzzzzzzzzzz2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("closing an unknown entry: %v", err)
	}
	if _, err := s.Get("zzzzzzzzzzz2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("an unknown entry: %v", err)
	}
}

// A hook may report that the session works on before the delivery is noted.
func TestPickedBeforeDelivered(t *testing.T) {
	s, _ := newTestStore()
	e, _ := s.Open("p", "s", KindPermission, "t", nil)
	_, _ = s.Answer(e.ID, "1")
	s.Resumed("s")
	s.Delivered(e.ID)
	if got, _ := s.Get(e.ID); got.Status != StatusPicked {
		t.Errorf("status %s, want picked", got.Status)
	}
}

func TestRevertWithAnotherOpenEntryCloses(t *testing.T) {
	s, _ := newTestStore()
	e, _ := s.Open("p", "s", KindPermission, "t", nil)
	_, _ = s.Answer(e.ID, "yes")
	if _, ok := s.Open("p", "s", KindPermission, "u", nil); !ok {
		t.Fatal("the second entry was not made")
	}
	s.Revert(e.ID)
	if got, _ := s.Get(e.ID); got.Status != StatusClosed {
		t.Errorf("status %s, want closed", got.Status)
	}
}

func TestResumedAndCloseSession(t *testing.T) {
	s, _ := newTestStore()
	perm, _ := s.Open("p", "s", KindPermission, "t", nil)
	other, _ := s.Open("p", "o", KindIdle, "t", nil)
	s.Resumed("s")
	if got, _ := s.Get(perm.ID); got.Status != StatusClosed {
		t.Errorf("an open permission after resume: %s", got.Status)
	}
	if got, _ := s.Get(other.ID); got.Status != StatusOpen {
		t.Errorf("another session's entry after resume: %s", got.Status)
	}
	errEntry, _ := s.Open("p", "s", KindError, "t", nil)
	s.Resumed("s")
	if got, _ := s.Get(errEntry.ID); got.Status != StatusOpen {
		t.Errorf("an error entry after resume: %s", got.Status)
	}
	s.CloseSession("s")
	if got, _ := s.Get(errEntry.ID); got.Status != StatusClosed {
		t.Errorf("an open entry after the session was stopped: %s", got.Status)
	}
	if got, _ := s.Get(other.ID); got.Status != StatusOpen {
		t.Errorf("another session's entry after the stop: %s", got.Status)
	}
}

func TestListAndRetention(t *testing.T) {
	s, c := newTestStore()
	var first Entry
	for i := range KeepFinished + 5 {
		c.t = c.t.Add(time.Second)
		e, ok := s.Open("p", "s"+string(rune('a'+i%20)), KindPermission, "t", nil)
		if !ok {
			// the same session may still hold an open entry from 20 steps earlier
			continue
		}
		if i == 0 {
			first = e
		}
		_, _ = s.Close(e.ID)
	}
	keep, _ := s.Open("p", "live", KindPermission, "t", nil)
	all := s.List("p", true)
	finished := 0
	for _, e := range all {
		if e.Status == StatusClosed {
			finished++
		}
	}
	if finished != KeepFinished {
		t.Errorf("%d finished entries kept, want %d", finished, KeepFinished)
	}
	if _, err := s.Get(first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("the oldest finished entry is still there: %v", err)
	}
	if got := s.List("p", false); len(got) != 1 || got[0].ID != keep.ID {
		t.Errorf("the default list: %+v", got)
	}
	if got := s.List("other", true); len(got) != 0 {
		t.Errorf("another project's list: %+v", got)
	}
	s.DropProject("p")
	if got := s.List("", true); len(got) != 0 {
		t.Errorf("after DropProject: %+v", got)
	}
}

func TestCleanAnswer(t *testing.T) {
	cases := []struct {
		in, want string
		err      error
	}{
		{"yes", "yes", nil},
		{"  two words  ", "two words", nil},
		{"a\nb\r\nc\rd", "a b c d", nil},
		{"tab\there", "tabhere", nil},
		{"esc\x1b[31mred\x00\x7f\u0085", "esc[31mred", nil},
		{"bidi‮evil", "bidievil", nil},
		{"bad \xff utf8", "bad  utf8", nil},
		{"naïve ✓", "naïve ✓", nil},
		{"", "", ErrAnswerEmpty},
		{" \n\t ", "", ErrAnswerEmpty},
		{"\x1b\x07", "", ErrAnswerEmpty},
		{strings.Repeat("ä", MaxAnswerRunes), strings.Repeat("ä", MaxAnswerRunes), nil},
		{strings.Repeat("ä", MaxAnswerRunes+1), "", ErrAnswerLong},
	}
	for _, c := range cases {
		got, err := CleanAnswer(c.in)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("CleanAnswer(%q) = %q, %v; want %q, %v", c.in, got, err, c.want, c.err)
		}
	}
}

func TestPresetKey(t *testing.T) {
	cases := []struct {
		kind   Kind
		answer string
		key    string
		ok     bool
	}{
		{KindPermission, "yes", "1", true},
		{KindPermission, "always", "2", true},
		{KindPermission, "no", "Escape", true},
		{KindPermission, "Yes", "", false},
		{KindPermission, "maybe", "", false},
		{KindQuestion, "yes", "", false},
		{KindIdle, "no", "", false},
		{KindError, "always", "", false},
	}
	for _, c := range cases {
		key, ok := PresetKey(c.kind, c.answer)
		if key != c.key || ok != c.ok {
			t.Errorf("PresetKey(%s, %q) = %q, %v; want %q, %v", c.kind, c.answer, key, ok, c.key, c.ok)
		}
	}
}

func TestValidID(t *testing.T) {
	id, err := newID()
	if err != nil || !ValidID(id) {
		t.Fatalf("new id %q: %v", id, err)
	}
	for _, bad := range []string{"", "abc", "ABCDEFGHIJK2", "abcdefghijk1", "abcdefghijk22", "../../etc/x"} {
		if ValidID(bad) {
			t.Errorf("%q is accepted as an id", bad)
		}
	}
}
