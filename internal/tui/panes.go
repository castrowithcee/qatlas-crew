package tui

import (
	"strings"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// pane is one of the three parts of the work screen.
type pane int

const (
	paneProjects pane = iota
	paneSessions
	paneInbox
)

// paneArea is the area a pane belongs to.
var paneArea = map[pane]kittui.AreaID{paneProjects: areaProjects, paneSessions: areaSessions, paneInbox: areaInbox}

// render draws the whole screen in the size of the terminal.
func (m *Model) render() string {
	m.mouseMap, m.mouseParts = nil, nil
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return m.styles.Paint(m.resizeView(), m.width, m.height)
	}
	m.syncFocus()
	spec := m.frameSpec()
	room := m.frame.Workspace(spec)
	spec.Workspace = m.workspace(room.Width, room.Height)
	view, targets := m.frame.Render(m.styles, spec)
	if m.modal != nil {
		// Nothing of the screen beneath takes a click while a question is open.
		targets = nil
		if over, ot, ok := m.modal.dialog.Overlay(m.styles, view, m.width, m.height, room); ok {
			view, targets = over, ot
		} else {
			spec.Footer, spec.Input, spec.Position, spec.Back = m.modal.dialog.Hints(), kittui.InputNavigation, "", nil
			room = m.frame.Workspace(spec)
			var wt, ft []kittui.Target
			spec.Workspace, wt = m.modal.dialog.View(m.styles, room.Width, room.Height)
			view, ft = m.frame.Render(m.styles, spec)
			targets = append(kittui.OffsetTargets(wt, room.X, room.Y), answerTargets(m.modal.dialog, ft)...)
		}
	}
	if m.helpOpen {
		if over, ot, ok := m.help.Overlay(m.styles, view, m.width, m.height, room); ok {
			view, targets = over, ot
		} else {
			spec.Footer, spec.Input, spec.Position, spec.Back = m.help.Hints(), kittui.InputNavigation, "", nil
			room = m.frame.Workspace(spec)
			var wt []kittui.Target
			spec.Workspace, wt = m.help.View(m.styles, room.Width, room.Height)
			view, _ = m.frame.Render(m.styles, spec)
			targets = kittui.OffsetTargets(wt, room.X, room.Y)
		}
	}
	if m.modal == nil && !m.helpOpen {
		// The parts lie on top of the frame; the map puts elements above zones whatever their order.
		pt, parts := m.partTargets(room)
		targets, m.mouseParts = append(targets, pt...), parts
	}
	m.setMouse(targets)
	return m.styles.Paint(view, m.width, m.height)
}

// workspace draws the content of the active area in width by height cells.
func (m *Model) workspace(width, height int) string {
	m.parts = nil
	if m.add != nil {
		return m.addView(width, height)
	}
	if m.area == areaSettings {
		view, targets := m.set.form.View(m.styles, width, height)
		m.parts = append(m.parts, mousePart{rect: kittui.Rect{Width: width, Height: height}, form: m.set.form, targets: targets})
		return view
	}
	if m.area == areaSetup {
		return m.setupView(width, height)
	}
	if !m.loaded {
		text := ""
		if m.failure == "" {
			text = m.text.Lookup(textProjectsLoading)
		}
		return fit(m.note(text, width), width, height)
	}
	active := map[kittui.AreaID]pane{areaProjects: paneProjects, areaSessions: paneSessions, areaInbox: paneInbox}[m.area]
	if view, listH, ok := m.sessionSplit(width, height); ok {
		return m.splitWorkspace(width, height, view, listH)
	}
	switch {
	case m.full:
		return m.drawPane(active, width, height, 0, 0)
	case width >= columnsWidth:
		left := min(max(width/3, 28), 44)
		sep := m.styles.Glyph(kittui.GlyphTabSeparator)
		right := width - left - 3
		content := paneSessions
		if m.area == areaInbox {
			content = paneInbox
		}
		l := strings.Split(m.drawPane(paneProjects, left, height, 0, 0), "\n")
		r := strings.Split(m.drawPane(content, right, height, left+3, 0), "\n")
		lines := make([]string, height)
		for i := range lines {
			lines[i] = l[i] + " " + sep + " " + r[i]
		}
		return strings.Join(lines, "\n")
	case height >= 9:
		top := min(max(height*2/5, 4), height-5)
		content := paneSessions
		if m.area == areaInbox {
			content = paneInbox
		}
		return m.drawPane(paneProjects, width, top, 0, 0) + "\n" + m.drawPane(content, width, height-top, 0, top)
	}
	return m.drawPane(active, width, height, 0, 0)
}

// drawPane draws a pane in width by height cells, its title in the first row, with its top left corner at x
// and y of the workspace, and registers what takes clicks.
func (m *Model) drawPane(p pane, width, height, x, y int) string {
	area := paneArea[p]
	focused := m.area == area && m.focus() == kittui.ZoneRows && !(p == paneSessions && m.viewSel)
	title, tone := m.paneTitle(p), kittui.StyleHint
	if focused {
		title, tone = m.styles.SelectionMark()+" "+title, kittui.StyleActive
	} else {
		title = "  " + title
	}
	// The title is a click target of its area, so the pane can be reached without a key.
	head := m.paneHead(p, title, tone, width, x, y)
	if height <= 1 {
		return head
	}
	var body string
	switch p {
	case paneProjects:
		body = m.listView(m.projList, width, height-1, x, y+1)
	case paneSessions:
		switch {
		case m.missing != "":
			body = m.note(m.missingText(), width)
		case m.shown == "":
			body = m.note(m.text.Lookup(textSessionsNoProj), width)
		default:
			body = m.listView(m.sessList, width, height-1, x, y+1)
		}
	case paneInbox:
		text := m.text.Lookup(textSessionsNoProj)
		if pr, ok := m.selectedProject(); ok {
			text = m.text.Lookup(textInboxSoon, pr.InboxOpen)
		}
		body = m.note(text, width)
	}
	return head + "\n" + fit(body, width, height-1)
}

// paneTitle names the pane and, for the panes that belong to a project, the project.
func (m *Model) paneTitle(p pane) string {
	t := m.text
	name := ""
	if pr, ok := m.selectedProject(); ok {
		name = pr.Name
	}
	switch {
	case p == paneProjects:
		return kittui.Escape(t.Lookup(textPaneProjects))
	case name == "":
		return kittui.Escape(t.Lookup(textAreaForPane(p)))
	case p == paneSessions:
		return kittui.Escape(t.Lookup(textPaneSessions, name))
	}
	return kittui.Escape(t.Lookup(textPaneInbox, name))
}

func textAreaForPane(p pane) kittui.TextID {
	if p == paneInbox {
		return textAreaInbox
	}
	return textAreaSessions
}

// note is a dimmed sentence wrapped into width cells and indented like the rows of a list.
func (m *Model) note(text string, width int) string {
	lines := kittui.Wrap(kittui.Escape(text), max(width-2, 1))
	for i, l := range lines {
		lines[i] = "  " + m.styles.Render(kittui.StyleHint, l)
	}
	return strings.Join(lines, "\n")
}

// fit cuts or pads view to exactly height rows of width cells.
func fit(view string, width, height int) string {
	lines := strings.Split(view, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	lines = lines[:max(height, 0)]
	for i, l := range lines {
		lines[i] = kittui.Pad(kittui.Clip(l, width), width)
	}
	return strings.Join(lines, "\n")
}
