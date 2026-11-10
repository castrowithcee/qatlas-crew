package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// setMouse replaces the map with the targets of the frame just drawn. The map is off while text is typed and
// while the preference is off; a nil map acts on nothing.
func (m *Model) setMouse(targets []kittui.Target) {
	on := m.input() == kittui.InputNavigation
	if m.modal != nil {
		// The dialog decides, not the screen behind it.
		on = m.modal.dialog.Input() == kittui.InputNavigation
	}
	m.mouseMap = kittui.NewMouseMap(targets, m.mouseOn && on)
}

// mousePart is a widget of the workspace that takes clicks and the wheel itself, with its place in the
// workspace and its targets in workspace cells.
type mousePart struct {
	rect    kittui.Rect
	list    *kittui.List
	form    *kittui.Form
	targets []kittui.Target
}

// listView draws list at x and y of the workspace and registers it for the mouse. It is the one way a list is
// drawn, so no list is left without its targets.
func (m *Model) listView(list *kittui.List, width, height, x, y int) string {
	view, targets := list.View(m.styles, width, height)
	m.parts = append(m.parts, mousePart{rect: kittui.Rect{X: x, Y: y, Width: width, Height: height}, list: list,
		targets: kittui.OffsetTargets(targets, x, y)})
	return view
}

// partTargets are the targets of the registered parts as the screen shows them, and the parts themselves
// moved to their place on the screen.
func (m *Model) partTargets(room kittui.Rect) (targets []kittui.Target, parts []mousePart) {
	for _, p := range m.parts {
		targets = append(targets, kittui.OffsetTargets(p.targets, room.X, room.Y)...)
		p.rect.X += room.X
		p.rect.Y += room.Y
		parts = append(parts, p)
	}
	return targets, parts
}

// partAt is the widget drawn on the cell.
func (m *Model) partAt(x, y int) (mousePart, bool) {
	for _, p := range slices.Backward(m.mouseParts) {
		r := p.rect
		if (p.list != nil || p.form != nil) && x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height {
			return p, true
		}
	}
	return mousePart{}, false
}

// mouse acts on one mouse event. Every click is turned into the key press it stands for and takes the way of
// that key, so the mouse can never do more than a key does.
func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	res := m.mouseMap.Handle(msg)
	if !res.Handled {
		return nil
	}
	if m.modal != nil {
		return m.modalMouse(res)
	}
	if m.helpOpen {
		return m.helpMouse(res)
	}
	at := msg.Mouse()
	switch {
	case res.WheelSteps != 0:
		if p, ok := m.partAt(at.X, at.Y); ok {
			switch {
			case p.list != nil:
				p.list.Wheel(res.WheelSteps)
			case p.form != nil:
				p.form.Wheel(res.WheelSteps)
			}
		}
	case res.Item != "":
		if p, ok := m.partAt(at.X, at.Y); ok {
			switch {
			case p.list != nil:
				return m.rowClick(p.list, res)
			case p.form != nil:
				act, handled := p.form.Click(res)
				cmd, _ := m.formAct(act, handled)
				return cmd
			}
		}
	case res.Area != "":
		if i := slices.IndexFunc(m.areas(), func(a kittui.Area) bool { return a.ID == res.Area }); i >= 0 && i < len(areaKeys) {
			return m.pressName(areaKeys[i])
		}
	case res.Action != "":
		if key, ok := m.keyOf(res.Action); ok {
			return m.press(key)
		}
	case res.Zone != "":
		if slices.Contains(m.zones(), res.Zone) {
			m.setZone(res.Zone)
		}
	}
	return nil
}

// rowClick marks the clicked row and gives its pane the focus. A second click on the marked row opens it as
// enter does.
func (m *Model) rowClick(list *kittui.List, res kittui.MouseResult) tea.Cmd {
	open, _ := list.Click(res) // reads the focus as it was before this click
	switch {
	case list == m.projList && m.area != areaInbox:
		// With the inbox shown the click only chooses the project; the inbox stays beside it.
		m.area = areaProjects
	case list == m.sessList:
		m.area = areaSessions
	}
	m.setZone(kittui.ZoneRows)
	if open != "" && list == m.projList {
		return m.openProject()
	}
	return nil
}

// modalMouse is the mouse while a question is open: only the answers of its dialog take clicks.
func (m *Model) modalMouse(res kittui.MouseResult) tea.Cmd {
	if answer, _ := m.modal.dialog.Click(res); answer != "" {
		m.notice, m.result = "", ""
		return m.answer(answer)
	}
	return nil
}

// helpMouse is the mouse while the help is open: only it takes clicks and the wheel. A key of the list
// closes it, as the key does.
func (m *Model) helpMouse(res kittui.MouseResult) tea.Cmd {
	if res.WheelSteps != 0 {
		m.help.Wheel(res.WheelSteps)
		return nil
	}
	if act, _ := m.help.Click(res); act != "" {
		m.helpOpen = false
	}
	return nil
}

// answerTargets are the targets of ts that give an answer of the dialog; the rest of a frame behind a
// question takes no click.
func answerTargets(d *kittui.Dialog, ts []kittui.Target) []kittui.Target {
	var out []kittui.Target
	for _, h := range d.Hints() {
		for _, t := range ts {
			if t.Action == h.Action {
				out = append(out, t)
			}
		}
	}
	return out
}

func (m *Model) pressName(name string) tea.Cmd {
	if msg, ok := parseKey(name); ok {
		return m.press(msg)
	}
	return nil
}

// keyOf is a key press that triggers the action under the input focus of the screen.
func (m *Model) keyOf(id kittui.ActionID) (tea.KeyPressMsg, bool) {
	focus := m.input()
	for _, a := range m.keys.Actions() {
		if a.ID != id {
			continue
		}
		for _, name := range a.DefaultKeys {
			if msg, ok := parseKey(name); ok {
				if got, ok := m.keys.Match(msg, focus); ok && got == id {
					return msg, true
				}
			}
		}
	}
	return tea.KeyPressMsg{}, false
}

var namedKeys = map[string]rune{
	"esc": tea.KeyEscape, "enter": tea.KeyEnter, "tab": tea.KeyTab, "space": tea.KeySpace, "up": tea.KeyUp,
	"down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome, "end": tea.KeyEnd,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
}

// parseKey builds the key press a key table names ("esc", "ctrl+c", "f2", "P"); false where the name does
// not come back as itself.
func parseKey(name string) (tea.KeyPressMsg, bool) {
	var key tea.Key
	rest := name
	for {
		mod, tail, found := strings.Cut(rest, "+")
		if !found || tail == "" {
			break
		}
		switch mod {
		case "ctrl":
			key.Mod |= tea.ModCtrl
		case "shift":
			key.Mod |= tea.ModShift
		case "alt":
			key.Mod |= tea.ModAlt
		default:
			return tea.KeyPressMsg{}, false
		}
		rest = tail
	}
	var n int
	runes := []rune(rest)
	switch code, named := namedKeys[rest]; {
	case named:
		key.Code = code
		if code == tea.KeySpace {
			key.Text = " "
		}
	case len(runes) == 1:
		key.Code = runes[0]
		if key.Mod&^tea.ModShift == 0 {
			key.Text = rest
		}
	default:
		if _, err := fmt.Sscanf(rest, "f%d", &n); err != nil || n < 1 || n > 63 {
			return tea.KeyPressMsg{}, false
		}
		key.Code = tea.KeyF1 + rune(n-1)
	}
	msg := tea.KeyPressMsg(key)
	return msg, kittui.KeyName(msg) == name
}
