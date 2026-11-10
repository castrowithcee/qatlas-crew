package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-crew/internal/helptopics"
)

func TestAClickOnATabSwitchesTheAreaLikeADigit(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	m.render()
	x, y := cellOf(t, m, "Inbox")
	clickAt(m, x, y)
	if m.area != areaInbox {
		t.Fatalf("area = %s", m.area)
	}
	x, y = cellOf(t, m, "Settings")
	clickAt(m, x, y)
	if m.area != areaSettings {
		t.Fatalf("area = %s", m.area)
	}
	x, y = cellOf(t, m, "Projects")
	clickAt(m, x, y)
	if m.area != areaProjects {
		t.Fatalf("area = %s", m.area)
	}
}

func TestAClickOnTheHelpKeyOpensTheHelp(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	m.render()
	x, y := cellOf(t, m, "[?]")
	clickAt(m, x, y)
	if !m.helpOpen {
		t.Fatal("the header key did not open the help")
	}
	contains(t, m, "qcrew")
	send(m, "esc")
	if m.helpOpen {
		t.Fatal("esc did not close the help")
	}
}

func TestAClickOnAProjectChoosesIt(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	m.render()
	x, y := cellOf(t, m, "beta")
	clickAt(m, x, y)
	contains(t, m, "Sessions of beta", "build watcher")
	if m.area != areaProjects {
		t.Fatalf("a first click opened the project: area %s", m.area)
	}
	x, y = cellOf(t, m, "beta")
	clickAt(m, x, y)
	if m.area != areaSessions {
		t.Fatalf("a second click did not open it: area %s", m.area)
	}
}

func TestAClickOnASessionFocusesTheSessions(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	m.render()
	x, y := cellOf(t, m, "write docs")
	clickAt(m, x, y)
	if m.area != areaSessions {
		t.Fatalf("area %s", m.area)
	}
	if id, _ := m.sessList.Selected(); id != "s2" {
		t.Fatalf("selected %q", id)
	}
}

func TestAClickOnAPaneTitleEntersItsArea(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	m.render()
	x, y := cellOf(t, m, "Sessions of alpha")
	clickAt(m, x, y)
	if m.area != areaSessions {
		t.Fatalf("area %s", m.area)
	}
}

func TestTheFooterKeysActLikeTheKeys(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	m.render()
	x, y := cellOf(t, m, "f full screen")
	clickAt(m, x, y)
	if !m.full {
		t.Fatal("the footer key did not switch to the full screen")
	}
	x, y = cellOf(t, m, "f columns")
	clickAt(m, x, y)
	if m.full {
		t.Fatal("the footer key did not switch back")
	}
	send(m, "enter")
	x, y = cellOf(t, m, "t stop session")
	clickAt(m, x, y)
	if m.modal == nil {
		t.Fatal("the footer key did not open the question")
	}
	if got := b.stops(); len(got) != 0 {
		t.Fatalf("calls = %v", got)
	}
	x, y = cellOf(t, m, "y stop")
	clickAt(m, x, y)
	if got := b.stops(); len(got) != 1 {
		t.Fatalf("the answer did not stop: %v", got)
	}
}

func TestTheWheelMovesTheMarkedRow(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	m.render()
	x, y := cellOf(t, m, "beta")
	_, cmd := m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	drain(m, cmd)
	contains(t, m, "Sessions of beta")
}

func TestNothingBehindAQuestionTakesAClick(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "enter", "t")
	m.render()
	x, y := cellOf(t, m, "Inbox")
	clickAt(m, x, y)
	if m.area != areaSessions || m.modal == nil {
		t.Fatalf("a click behind the question acted: area %s", m.area)
	}
	if len(b.stops()) != 0 {
		t.Fatal("the service was called")
	}
}

func TestTheMouseMapIsOffWhereTheMouseMustNotAct(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	m.render()
	x, y := cellOf(t, m, "Inbox")
	m.mouseOn = false
	m.render()
	clickAt(m, x, y)
	if m.area != areaProjects {
		t.Fatal("a click acted with the mouse switched off")
	}
	m.mouseOn = true
	send(m, "/") // typing a filter
	m.render()
	clickAt(m, x, y)
	if m.area != areaProjects {
		t.Fatal("a click acted while a filter is typed")
	}
	send(m, "esc")
	clickAt(m, x, y)
	if m.area != areaInbox {
		t.Fatal("the click does not act again")
	}
}

func TestViewAsksForTheMouseOnlyWhenOn(t *testing.T) {
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("the mouse is not requested")
	}
	m.mouseOn = false
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("the mouse is requested while it is off")
	}
}

// probe is what a click on the cell would report, without acting on it.
func probe(m *Model, x, y int) kittui.MouseResult {
	return m.mouseMap.Handle(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// cellsOfText are the cells of every occurrence of needle on the drawn screen, by rune.
func cellsOfText(m *Model, needle string) (cells [][2]int) {
	for y, line := range strings.Split(m.render(), "\n") {
		for from := 0; ; {
			i := strings.Index(line[from:], needle)
			if i < 0 {
				break
			}
			cells = append(cells, [2]int{len([]rune(line[:from+i])), y})
			from += i + len(needle)
		}
	}
	return cells
}

// wantTarget fails unless some occurrence of text on the screen, at row minY or below, takes a click that
// reports what accept approves of; it returns false where the text is not on the screen.
func wantTarget(t *testing.T, m *Model, what, text string, minY int, accept func(kittui.MouseResult) bool) bool {
	t.Helper()
	found := false
	for _, c := range cellsOfText(m, text) {
		if c[1] < minY {
			continue
		}
		found = true
		if res := probe(m, c[0], c[1]); res.Handled && accept(res) {
			return true
		}
	}
	if found {
		t.Errorf("%s %q is on the screen but takes no click:\n%s", what, text, m.render())
	}
	return found
}

func isAction(id kittui.ActionID) func(kittui.MouseResult) bool {
	return func(r kittui.MouseResult) bool { return r.Action == id }
}

// hintTargets checks every key hint that is drawn: a click on its first key reports its action.
func hintTargets(t *testing.T, m *Model, hints []kittui.Hint, minY int) (drawn int) {
	t.Helper()
	for _, g := range m.keys.Hints(hints, m.input(), m.text) {
		if len(g.Keys) == 0 || g.Kind == kittui.HintNavigation {
			continue // the arrow keys that move the cursor are no buttons
		}
		if wantTarget(t, m, "hint", g.Text(), minY, isAction(g.Keys[0].Action)) {
			drawn++
		}
	}
	return drawn
}

// Every operating element that is drawn has a click target: the tabs, the keys of header and footer, the
// rows, the titles of the panes, the fields and buttons of the settings, the answers of a question and the
// tabs of the help; at the narrow and the wide size, in both languages.
func TestEveryVisibleElementHasAClickTarget(t *testing.T) {
	states := []struct {
		name string
		keys []string
	}{
		{"projects", nil}, {"sessions", []string{"enter"}}, {"full-projects", []string{"f"}},
		{"full-sessions", []string{"enter", "f"}}, {"inbox", []string{"2"}}, {"settings", []string{"4"}},
		{"stop", []string{"enter", "t"}}, {"help", []string{"?"}},
	}
	sizes := map[string][2]int{"narrow": {60, 30}, "wide": {120, 40}}
	for _, s := range states {
		for lang, l := range map[string]i18n.Lang{"en": i18n.English, "de": i18n.German} {
			for size, wh := range sizes {
				t.Run(s.name+"."+lang+"."+size, func(t *testing.T) {
					b := demoBackend()
					m := newTestModel(t, l, b, wh[0], wh[1])
					send(m, s.keys...)
					screen := m.render()
					switch {
					case m.helpOpen:
						n := 0
						for _, topic := range helptopics.All() {
							if wantTarget(t, m, "help topic", topic.Name, 0, func(kittui.MouseResult) bool { return true }) {
								n++
							}
						}
						if n == 0 {
							t.Errorf("no help tab is drawn:\n%s", screen)
						}
					case m.modal != nil:
						if n := hintTargets(t, m, m.modal.dialog.Hints(), 0); n < 2 {
							t.Errorf("%d answers drawn, want 2:\n%s", n, screen)
						}
					default:
						spec := m.frameSpec()
						for _, a := range m.areas() {
							wantTarget(t, m, "tab", m.text.Lookup(a.Title), 1, func(r kittui.MouseResult) bool { return r.Area == a.ID })
						}
						if !wantTarget(t, m, "header key", "[?]", 0, isAction(actHelp)) {
							t.Errorf("no help key in the header:\n%s", screen)
						}
						if spec.Back != nil && !wantTarget(t, m, "back button", "[esc]", 0, isAction(actBack)) {
							t.Errorf("no back button although the way back is offered:\n%s", screen)
						}
						if n := hintTargets(t, m, spec.Footer, wh[1]-3); n < 2 {
							t.Errorf("%d footer keys drawn, want at least 2:\n%s", n, screen)
						}
						item := func(r kittui.MouseResult) bool { return r.Item != "" }
						rows := func(list *kittui.List, texts ...string) {
							for _, text := range texts {
								if !wantTarget(t, m, "row", text, 3, item) {
									t.Errorf("row %q is not drawn:\n%s", text, screen)
								}
							}
						}
						if strings.Contains(screen, strings.ToUpper(m.text.Lookup(textColProject))) {
							rows(m.projList, "alpha", "beta")
						}
						if strings.Contains(screen, strings.ToUpper(m.text.Lookup(textColTool))) {
							rows(m.sessList, "refactor parser", "write docs")
						}
						if m.area == areaSettings {
							for _, label := range []kittui.TextID{kittui.TextSettingTheme, kittui.TextSettingStart,
								kittui.TextSettingLanguage, kittui.TextSettingIcons, kittui.TextSettingMouse} {
								if !wantTarget(t, m, "settings field", m.text.Lookup(label), 3, item) {
									t.Errorf("field %q is not drawn:\n%s", label, screen)
								}
							}
							for _, btn := range []kittui.TextID{kittui.TextKeySave, kittui.TextKeyDiscard} {
								if !wantTarget(t, m, "settings button", m.text.Lookup(btn), 3, func(kittui.MouseResult) bool { return true }) {
									t.Errorf("button %q is not drawn:\n%s", btn, screen)
								}
							}
						}
						if m.area == areaProjects || m.area == areaSessions {
							wantTarget(t, m, "pane title", m.paneTitle(paneProjects), 3, func(r kittui.MouseResult) bool { return r.Area == areaProjects })
						}
					}
				})
			}
		}
	}
}
