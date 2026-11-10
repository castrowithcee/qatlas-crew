package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"

	"github.com/castrowithcee/qatlas-crew/internal/control"
)

var epoch = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

// fakeBackend is the service as the interface sees it.
type fakeBackend struct {
	mu       sync.Mutex
	projects []control.Project
	sessions []control.Session
	projErr  error
	sessErr  error
	stopErr  error
	stopped  []string
	reads    int
	closed   bool
}

func demoBackend() *fakeBackend {
	return &fakeBackend{
		projects: []control.Project{
			{ID: "p-alpha", Name: "alpha", InboxOpen: 3},
			{ID: "p-beta", Name: "beta", InboxOpen: 0},
		},
		sessions: []control.Session{
			{ID: "s1", Project: "p-alpha", Tool: "claude", Title: "refactor parser", Status: "running", Since: epoch.Add(-90 * time.Second)},
			{ID: "s2", Project: "p-alpha", Tool: "codex", Title: "write docs", Status: "waiting", Since: epoch.Add(-3 * time.Hour)},
			{ID: "s3", Project: "p-beta", Tool: "shell", Title: "build watcher", Status: "idle", Since: epoch.Add(-5 * time.Second)},
		},
	}
}

func (f *fakeBackend) Projects(context.Context) ([]control.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	return append([]control.Project(nil), f.projects...), f.projErr
}

func (f *fakeBackend) Sessions(_ context.Context, project string) ([]control.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]control.Session(nil), f.sessions...), f.sessErr
}

func (f *fakeBackend) StopSession(_ context.Context, id string) (control.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, id)
	return control.Session{ID: id}, f.stopErr
}

func (f *fakeBackend) Close() error {
	f.closed = true
	return nil
}

func (f *fakeBackend) stops() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.stopped...)
}

// timers records the timers a model asks for instead of starting them, so a test fires them itself.
type timers struct {
	waits []time.Duration
	fns   []func(time.Time) tea.Msg
}

func (r *timers) start(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	r.waits, r.fns = append(r.waits, d), append(r.fns, fn)
	return nil
}

// fire takes the oldest timer and delivers its message.
func (r *timers) fire(t *testing.T, m *Model) {
	t.Helper()
	if len(r.fns) == 0 {
		t.Fatal("no timer is running")
	}
	fn := r.fns[0]
	r.waits, r.fns = r.waits[1:], r.fns[1:]
	drain(m, func() tea.Msg { return fn(time.Time{}) })
}

var timersOf = map[*Model]*timers{}

type testPrefs struct {
	saved []uiprefs.Prefs
	err   error
}

func (p *testPrefs) save(next uiprefs.Prefs) error {
	if p.err != nil {
		return p.err
	}
	p.saved = append(p.saved, next)
	return nil
}

func newTestModel(t *testing.T, lang i18n.Lang, b *fakeBackend, width, height int) *Model {
	t.Helper()
	m, err := newModel(config{ctx: context.Background(), conn: b, lang: lang, theme: kittui.ThemeTerminal,
		profile: kittui.ProfileNone, charset: kittui.CharsetASCII, now: func() time.Time { return epoch },
		connect: func(context.Context) (Connection, error) { return b, nil }, mouse: true,
		prefs: uiprefs.Prefs{Theme: kittui.ThemeTerminal, Start: string(areaProjects), Icons: uiprefs.IconsNone,
			Language: uiprefs.LanguageEnglish, MouseEnabled: true}, savePrefs: (&testPrefs{}).save})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	timersOf[m] = &timers{}
	m.after = timersOf[m].start
	load(t, m)
	return m
}

// drain runs a command and every command its messages lead to, to the end.
func drain(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			drain(m, c)
		}
	default:
		_, next := m.Update(msg)
		drain(m, next)
	}
}

// load runs one read synchronously.
func load(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.reload()
	if cmd == nil {
		t.Fatal("no read started")
	}
	m.Update(cmd())
}

// send presses keys and runs every command they lead to.
func send(m *Model, keys ...string) {
	for _, k := range keys {
		msg, ok := parseKey(k)
		if !ok {
			panic("unknown key " + k)
		}
		_, cmd := m.Update(msg)
		drain(m, cmd)
		m.render()
	}
}

// press presses one key and returns its command without running it.
func press(m *Model, key string) tea.Cmd {
	msg, _ := parseKey(key)
	_, cmd := m.Update(msg)
	return cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func contains(t *testing.T, m *Model, want ...string) {
	t.Helper()
	screen := m.render()
	for _, w := range want {
		if !strings.Contains(screen, w) {
			t.Fatalf("the screen lacks %q:\n%s", w, screen)
		}
	}
}

func lacks(t *testing.T, m *Model, unwanted ...string) {
	t.Helper()
	screen := m.render()
	for _, w := range unwanted {
		if strings.Contains(screen, w) {
			t.Fatalf("the screen holds %q:\n%s", w, screen)
		}
	}
}

// cellOf is the first cell of needle on the drawn screen.
func cellOf(t *testing.T, m *Model, needle string) (x, y int) {
	t.Helper()
	for y, line := range strings.Split(m.render(), "\n") {
		if i := strings.Index(line, needle); i >= 0 {
			return len([]rune(line[:i])), y
		}
	}
	t.Fatalf("%q is not on the screen:\n%s", needle, m.render())
	return 0, 0
}

func clickAt(m *Model, x, y int) {
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drain(m, cmd)
	m.render()
}
