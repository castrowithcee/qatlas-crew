package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/tui/term"
)

// The session view shows the session under the cursor of the session list as a live terminal. It shares the
// area of the sessions: the list stands above it, in the columns layout on the right and in the stacked one
// below the project list, and the full screen can show it alone.

// minViewHeight is the least height of the session view: its title, its status line and a few rows of screen.
const minViewHeight = 6

// stackedProjects is the height of the project list above the session list and the view when they stand
// below each other.
const stackedProjects = 4

// sessionTerm is the terminal of the session the view shows.
type sessionTerm struct {
	id, project string
	cols, rows  int
	// term is nil when no client runs: the session has ended, or the client could not be started.
	term   *term.Term
	ended  bool
	failed string
}

// sessionSplit places the session view in a workspace of width by height cells: its rectangle and the height
// of the session list above it. ok is false where the screen shows no view: no way to attach is given, the
// area is not the sessions, the full screen shows another pane, or the room is too small.
func (m *Model) sessionSplit(width, height int) (view kittui.Rect, listH int, ok bool) {
	if m.attach == nil || m.area != areaSessions || !m.loaded {
		return kittui.Rect{}, 0, false
	}
	switch {
	case m.full:
		if m.viewSel {
			return kittui.Rect{Width: width, Height: height}, 0, true
		}
	case width >= columnsWidth:
		left := min(max(width/3, 28), 44)
		listH = m.sessionListHeight(height)
		if height-listH >= minViewHeight {
			return kittui.Rect{X: left + 3, Y: listH, Width: width - left - 3, Height: height - listH}, listH, true
		}
	case height >= stackedProjects+4+minViewHeight:
		listH = m.sessionListHeight(height - stackedProjects)
		if h := height - stackedProjects - listH; h >= minViewHeight {
			return kittui.Rect{Y: stackedProjects + listH, Width: width, Height: h}, listH, true
		}
	}
	return kittui.Rect{}, 0, false
}

// sessionListHeight gives the list its rows and title line, but never more than a third of the room.
func (m *Model) sessionListHeight(room int) int {
	return min(max(len(m.sessByID)+2, 4), max(room/3, 4))
}

// splitWorkspace draws the work screen of the sessions area with the session view.
func (m *Model) splitWorkspace(width, height int, view kittui.Rect, listH int) string {
	switch {
	case m.full:
		return m.drawSessionView(view.Width, view.Height, 0, 0)
	case width >= columnsWidth:
		left := min(max(width/3, 28), 44)
		sep := m.styles.Glyph(kittui.GlyphTabSeparator)
		l := strings.Split(m.drawPane(paneProjects, left, height, 0, 0), "\n")
		r := strings.Split(m.drawPane(paneSessions, view.Width, listH, view.X, 0)+"\n"+
			m.drawSessionView(view.Width, view.Height, view.X, view.Y), "\n")
		lines := make([]string, height)
		for i := range lines {
			lines[i] = l[i] + " " + sep + " " + r[i]
		}
		return strings.Join(lines, "\n")
	}
	return m.drawPane(paneProjects, width, stackedProjects, 0, 0) + "\n" +
		m.drawPane(paneSessions, width, listH, 0, stackedProjects) + "\n" +
		m.drawSessionView(width, view.Height, 0, view.Y)
}

// shownSession is the session the view is to show.
func (m *Model) shownSession() (control.Session, bool) {
	id, ok := m.sessList.Selected()
	if !ok {
		return control.Session{}, false
	}
	s, ok := m.sessByID[id]
	return s, ok
}

// drawSessionView draws the title, the screen of the session and the status line in width by height cells
// with its top left corner at x and y of the workspace, and registers the whole pane for the mouse.
func (m *Model) drawSessionView(width, height, x, y int) string {
	s, have := m.shownSession()
	title := m.text.Lookup(textViewTitle)
	if have {
		name := s.Title
		if name == "" {
			name = s.ID
		}
		title = m.text.Lookup(textViewTitleOf, name)
	}
	title, tone := kittui.Escape(title), kittui.StyleHint
	if m.viewSel {
		title, tone = m.styles.SelectionMark()+" "+title, kittui.StyleActive
	} else {
		title = "  " + title
	}
	rect := kittui.Rect{X: x, Y: y, Width: width, Height: height}
	m.parts = append(m.parts, mousePart{rect: rect, view: true,
		targets: []kittui.Target{{Rect: rect, Zone: kittui.ZoneRows}}})
	head := m.styles.Render(tone, kittui.Pad(kittui.Clip(title, width), width))
	if height <= 2 {
		return head
	}
	rows := height - 2
	var body string
	switch v := m.view; {
	case !have || v == nil:
		body = m.note(m.text.Lookup(textViewNone), width)
	case v.failed != "":
		body = m.note(m.text.Lookup(textViewFailed, v.failed), width)
	case v.ended || v.term == nil:
		body = m.note(m.text.Lookup(textViewEnded), width)
	default:
		// The output of the session is foreign content: only the terminal part draws it, from cells and styles.
		body = v.term.View(width, rows)
	}
	return head + "\n" + fit(body, width, rows) + "\n" + m.viewStatusLine(s, have, width)
}

// viewStatusLine is the line under the screen: tool, status and, where the tool reports it, the context
// fill.
func (m *Model) viewStatusLine(s control.Session, have bool, width int) string {
	if !have {
		return kittui.Pad("", width)
	}
	status, _ := m.statusText(s.Status)
	line := m.text.Lookup(textViewStatus, kittui.Escape(s.Tool), kittui.Escape(status))
	if s.ContextPercent != nil {
		line += "  " + m.text.Lookup(textViewContext, *s.ContextPercent)
	}
	return m.styles.Render(kittui.StyleHint, kittui.Pad(kittui.Clip("  "+line, width), width))
}

// selectView gives the session view the selection; the right arrow lands here, and a click or enter on the
// view where no client runs.
func (m *Model) selectView() {
	if m.attach != nil && m.area == areaSessions {
		m.viewSel = true
		m.setZone(kittui.ZoneRows)
	}
}

// viewWanted is the session the view is to be attached to and the size of its screen; false where nothing is
// to be attached.
func (m *Model) viewWanted() (s control.Session, cols, rows int, ok bool) {
	if m.width < minWidth || m.height < minHeight {
		return control.Session{}, 0, 0, false
	}
	room := m.frame.Workspace(m.frameSpec())
	view, _, shown := m.sessionSplit(room.Width, room.Height)
	if !shown {
		return control.Session{}, 0, 0, false
	}
	s, have := m.shownSession()
	if !have {
		return control.Session{}, 0, 0, false
	}
	return s, max(view.Width, 1), max(view.Height-2, 1), true
}

// syncTerm attaches the terminal to the session the view shows, replaces it when another session is chosen,
// closes it when the view is gone and gives it the size of the screen it is drawn in. While a question or the
// help covers the screen nothing changes: they draw in another room.
func (m *Model) syncTerm() tea.Cmd {
	if m.modal != nil || m.helpOpen {
		return nil
	}
	s, cols, rows, ok := m.viewWanted()
	if v := m.view; v != nil && (!ok || v.id != s.ID || v.project != s.Project) {
		m.closeTerm()
	}
	if !ok {
		return nil
	}
	if v := m.view; v != nil {
		if v.term != nil && (v.cols != cols || v.rows != rows) {
			v.cols, v.rows = cols, rows
			if err := v.term.Resize(cols, rows); err != nil {
				v.failed = err.Error()
			}
		}
		return nil
	}
	v := &sessionTerm{id: s.ID, project: s.Project, cols: cols, rows: rows}
	m.view = v
	if s.Status == "exited" {
		v.ended = true
		return nil
	}
	t, err := term.Start(m.attach(s.Project, s.ID), cols, rows)
	if err != nil {
		v.failed = err.Error()
		return nil
	}
	v.term = t
	return t.Next()
}

// viewRunning tells that a client runs in the view and can take input.
func (m *Model) viewRunning() bool { return m.view != nil && m.view.term != nil }

// viewFocused tells that the session view has the focus: every key, paste and click goes to the session.
func (m *Model) viewFocused() bool { return m.viewFocus && m.viewRunning() }

// focusView gives the session view the focus, where a client runs; otherwise it only selects the view.
func (m *Model) focusView() {
	m.selectView()
	m.viewFocus = m.viewSel && m.viewRunning()
	m.viewDown = false
}

// settleFocus ends a focus that has nothing to stand on: the client ended, the view is no longer selected or
// not shown, or a window covers it.
func (m *Model) settleFocus() {
	if m.viewFocus && !(m.viewSel && m.viewRunning() && m.area == areaSessions && m.modal == nil && !m.helpOpen) {
		m.viewFocus = false
	}
}

// focusedInput takes every input message while the view has the focus, before any other part of the
// interface sees it. Only the exit key stays here; the rest goes to the session. Messages that are no input
// are not taken.
func (m *Model) focusedInput(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.keys.Is(msg, actViewExit, kittui.InputNavigation) {
			m.viewFocus = false
		} else {
			m.view.term.SendKey(msg)
		}
	case tea.PasteMsg:
		m.view.term.Paste(msg.Content)
	case tea.MouseMsg:
		m.focusedMouse(msg)
	case tea.KeyReleaseMsg, tea.PasteStartMsg, tea.PasteEndMsg:
		// The paste markers are the program's own to ask for: Paste adds them where it wants them.
	default:
		return false
	}
	return true
}

// focusedMouse passes a click, its release and a wheel step inside the screen of the view to the session, at
// cells relative to its top left corner. Motion and everything outside the screen is dropped. A release goes
// on only after a press the session got, so the click that gave the focus does not end in the session.
func (m *Model) focusedMouse(msg tea.MouseMsg) {
	_, release := msg.(tea.MouseReleaseMsg)
	switch msg.(type) {
	case tea.MouseClickMsg, tea.MouseReleaseMsg, tea.MouseWheelMsg:
	default:
		return
	}
	at := msg.Mouse()
	for _, p := range m.mouseParts {
		if !p.view {
			continue
		}
		// The screen lies between the title and the status line.
		x, y := at.X-p.rect.X, at.Y-p.rect.Y-1
		inside := x >= 0 && y >= 0 && x < min(p.rect.Width, m.view.cols) && y < min(p.rect.Height-2, m.view.rows)
		down := m.viewDown
		if release {
			m.viewDown = false
		}
		if !inside || release && !down {
			return
		}
		if _, click := msg.(tea.MouseClickMsg); click {
			m.viewDown = true
		}
		m.view.term.SendMouse(msg, x, y)
		return
	}
}

// closeTerm ends the client of the view. The session itself keeps running.
func (m *Model) closeTerm() {
	m.viewFocus = false
	if m.view != nil && m.view.term != nil {
		_ = m.view.term.Close()
	}
	m.view = nil
}

// termMsg handles the messages of the terminal. A message of a terminal that was replaced is dropped, which
// also ends its chain of waits.
func (m *Model) termMsg(msg tea.Msg) tea.Cmd {
	v := m.view
	switch msg := msg.(type) {
	case term.FrameMsg:
		if v != nil && v.term != nil && msg.ID == v.term.ID() {
			return v.term.Next()
		}
	case term.ExitedMsg:
		if v != nil && v.term != nil && msg.ID == v.term.ID() {
			_ = v.term.Close()
			v.term, v.ended = nil, true
		}
	}
	return nil
}
