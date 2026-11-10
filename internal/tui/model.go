package tui

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/helptopics"
)

// The areas of the tab line, in its order. Projects, Inbox and Sessions share the work screen: the project
// list stands on the left, and the content on the right is the one the active area names.
const (
	areaProjects kittui.AreaID = "projects"
	areaInbox    kittui.AreaID = "inbox"
	areaSessions kittui.AreaID = "sessions"
	areaSettings kittui.AreaID = "settings"
	// areaSetup is the last area, so the keys of the others stay as they were.
	areaSetup kittui.AreaID = "setup"
)

func (m *Model) areas() []kittui.Area {
	list := []kittui.Area{
		{ID: areaProjects, Title: textAreaProjects},
		{ID: areaInbox, Title: textAreaInbox},
		{ID: areaSessions, Title: textAreaSessions},
		{ID: areaSettings, Title: kittui.TextSettingsTitle},
		{ID: areaSetup, Title: textAreaSetup},
	}
	for i := range list {
		list[i].Icon = areaIcon(m.icons, list[i].ID)
	}
	return list
}

// areaIDs are the IDs of the areas a start can name.
func areaIDs() []kittui.AreaID {
	return []kittui.AreaID{areaProjects, areaInbox, areaSessions, areaSettings, areaSetup}
}

// The icon sets of the icons preference. An icon stands beside the name, never in its place.
var iconSets = map[string]map[kittui.AreaID]string{
	uiprefs.IconsUnicode: {areaProjects: "▤", areaInbox: "✉", areaSessions: "▶", areaSettings: "⚙", areaSetup: "⚒"},
	uiprefs.IconsNerdFont: {areaProjects: "", areaInbox: "", areaSessions: "",
		areaSettings: "", areaSetup: "\uf019"},
}

func areaIcon(set string, id kittui.AreaID) string { return iconSets[set][id] }

// refreshInterval is how often the interface reads the service again on its own; the reload key reads at once.
const refreshInterval = 2 * time.Second

// The smallest terminal the frame is drawn in, and the width from which the panes stand side by side.
const (
	minWidth     = 40
	minHeight    = 10
	columnsWidth = 100
)

// productName stands at the left of the header.
const productName = "qcrew"

// config is what a Model is built from.
type config struct {
	ctx     context.Context
	connect Connect
	// conn is a channel that is open already; nil opens one with connect on the first read.
	conn     Connection
	lang     i18n.Lang
	theme    string
	profile  kittui.ColorProfile
	charset  kittui.CharsetMode
	start    kittui.AreaID
	now      func() time.Time // clock of the SINCE column; nil takes time.Now
	warnings []string
	mouse    bool
	// prefs are the whole preferences as loaded; savePrefs writes them, nil when they cannot be saved.
	prefs     uiprefs.Prefs
	savePrefs func(uiprefs.Prefs) error
	// executable names the qcrew program that runs an installation; nil takes os.Executable.
	executable func() (string, error)
}

// tickMsg starts a periodic refresh.
type tickMsg struct{}

// Model is the state of the interface. The periodic refresh never changes anything in the service; a stop
// reaches it only after the person confirmed, and a click only presses the key it stands for.
type Model struct {
	ctx     context.Context
	connect Connect
	conn    Connection
	keys    *kittui.Keymap
	text    kittui.Translator
	lang    i18n.Lang
	frame   *kittui.Frame
	// prefs are the preferences as saved last; a save changes the chosen values and keeps the rest.
	prefs     uiprefs.Prefs
	savePrefs func(uiprefs.Prefs) error
	icons     string
	now       func() time.Time

	themes         *kittui.ThemeSet
	theme          string
	profile        kittui.ColorProfile
	charset        kittui.CharsetMode
	terminal       kittui.TerminalColors
	haveFG, haveBG bool
	styles         kittui.Styles
	width, height  int
	area           kittui.AreaID
	// full shows the pane of the active area alone.
	full bool
	// zone is the focus zone the person chose; focus falls back to the content zone when the screen has no such zone.
	zone kittui.FocusZone

	projList, sessList *kittui.List
	statusTones        map[string]kittui.TextStyle
	projects           []control.Project
	projByID           map[kittui.ItemID]control.Project
	sessions           []control.Session
	sessByID           map[kittui.ItemID]control.Session
	// shown is the project the session list holds the sessions of.
	shown           kittui.ItemID
	loaded, loading bool
	// failure is the error of the last read; sessFailure the one of reading the sessions; missing the sentence
	// of the service when a program qcrew needs is not installed.
	failure, sessFailure, missing string
	// notice is a warning and result a success; both stay until the next key.
	notice, result string

	set      *settingsState
	setup    *setupState
	modal    *modal
	stopping bool
	// after starts a timer; tests replace it.
	after    func(time.Duration, func(time.Time) tea.Msg) tea.Cmd
	help     *kittui.Help
	helpOpen bool
	// mouseOn is the mouse preference; it may change while the interface runs. mouseMap holds the click targets
	// of the frame drawn last.
	mouseOn  bool
	mouseMap *kittui.MouseMap
	// parts are the widgets the last workspace drawing registered, in workspace cells; mouseParts are those of
	// the frame on the screen, in screen cells.
	parts, mouseParts []mousePart
}

func newModel(c config) (*Model, error) {
	keys, err := kittui.NewKeymap(keyTable(), c.charset)
	if err != nil {
		return nil, err
	}
	text, err := kittui.NewTranslator(c.lang, catalog(), keys)
	if err != nil {
		return nil, err
	}
	themes, err := kittui.NewThemeSet(nil)
	if err != nil {
		return nil, err
	}
	m := &Model{ctx: c.ctx, connect: c.connect, conn: c.conn, keys: keys, text: text, lang: c.lang,
		frame: kittui.NewFrame(keys, text), prefs: c.prefs, savePrefs: c.savePrefs, icons: c.prefs.Icons,
		themes: themes, theme: c.theme, profile: c.profile, charset: c.charset, area: areaProjects,
		statusTones: map[string]kittui.TextStyle{}, projByID: map[kittui.ItemID]control.Project{},
		sessByID: map[kittui.ItemID]control.Session{}, after: tea.Tick, mouseOn: c.mouse}
	if m.ctx == nil {
		m.ctx = context.Background()
	}
	if m.now = c.now; m.now == nil {
		m.now = time.Now
	}
	if slices.Contains(areaIDs(), c.start) {
		m.area = c.start
	}
	if m.projList, m.sessList, err = m.newLists(text); err != nil {
		return nil, err
	}
	if m.set, err = m.newSettings(); err != nil {
		return nil, err
	}
	if m.setup, err = m.newSetup(c.executable); err != nil {
		return nil, err
	}
	if !m.restyle() {
		m.notice = text.Lookup(kittui.TextWarning, text.Lookup(kittui.TextThemeUnknown, c.theme, kittui.ThemeTerminal))
	}
	if len(c.warnings) > 0 {
		m.notice = text.Lookup(kittui.TextWarning, c.warnings[0])
	}
	return m, nil
}

// newLists builds both lists with the translator, so a change of language builds them again.
func (m *Model) newLists(text kittui.Translator) (projects, sessions *kittui.List, err error) {
	projects, err = kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{
		{Title: textColProject}, {Title: textColInbox, Right: true, Tone: zeroDimmed}},
		Keys: listKeys(), Search: kittui.SearchByKey, Empty: textProjectsEmpty}, m.keys, text)
	if err != nil {
		return nil, nil, err
	}
	sessions, err = kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{
		{Title: textColTool}, {Title: textColTitle, Flex: true},
		{Title: textColStatus, Tone: m.statusTone}, {Title: textColSince, Right: true, Dimmed: true}},
		Keys: listKeys(), Search: kittui.SearchByKey, Empty: textSessionsEmpty}, m.keys, text)
	return projects, sessions, err
}

// zeroDimmed dims a counter of zero, so the projects that wait for something stand out.
func zeroDimmed(cell string) kittui.TextStyle {
	if cell == "0" {
		return kittui.StyleHint
	}
	return kittui.StyleText
}

func (m *Model) statusTone(cell string) kittui.TextStyle {
	if t, ok := m.statusTones[cell]; ok {
		return t
	}
	return kittui.StyleText
}

// restyle resolves the styles of the theme under the terminal; false when the theme is unknown.
func (m *Model) restyle() bool {
	var terminal *kittui.TerminalColors
	if m.haveFG && m.haveBG {
		terminal = &m.terminal
	}
	styles, known := m.themes.Resolve(m.theme, m.profile, m.charset, terminal)
	m.styles = styles
	return known
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.reload(), m.tick(), m.setupReload()}
	if m.themes.AsksTerminalColors(m.theme, m.profile) {
		cmds = append(cmds, tea.RequestForegroundColor, tea.RequestBackgroundColor)
	}
	return tea.Batch(cmds...)
}

func (m *Model) tick() tea.Cmd {
	return m.after(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// reload reads the service again unless a read is under way.
func (m *Model) reload() tea.Cmd {
	if m.loading {
		return nil
	}
	m.loading = true
	ctx, conn, connect := m.ctx, m.conn, m.connect
	return func() tea.Msg { return fetch(ctx, conn, connect) }
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.syncSessions()
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return tea.Batch(m.reload(), m.tick())
	case refreshMsg:
		m.apply(msg)
		return m.setupAfterRefresh()
	case setupMsg:
		return m.applySetup(msg)
	case installDoneMsg:
		return m.installDone(msg)
	case stopMsg:
		return m.applyStop(msg)
	case tea.ForegroundColorMsg:
		m.terminal.Foreground, m.haveFG = kittui.ColorOf(msg.Color), true
		m.restyle()
	case tea.BackgroundColorMsg:
		m.terminal.Background, m.haveBG = kittui.ColorOf(msg.Color), true
		m.restyle()
	case kittui.InputBatch:
		var cmds []tea.Cmd
		for _, sub := range msg {
			cmds = append(cmds, m.update(sub))
		}
		return tea.Batch(cmds...)
	case tea.KeyPressMsg:
		return m.press(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	}
	return nil
}

// apply takes a read. A failed one keeps the rows read before.
func (m *Model) apply(msg refreshMsg) {
	m.loading = false
	if msg.conn != nil {
		m.conn = msg.conn
	}
	if msg.err != nil {
		m.failure = m.errorText(msg.err)
		m.dropConnection(msg.err)
		return
	}
	m.failure, m.sessFailure, m.missing = "", "", ""
	m.projects, m.loaded = msg.projects, true
	if msg.sessErr != nil {
		m.dropConnection(msg.sessErr)
		if text, ok := missingProgram(msg.sessErr); ok {
			m.missing = text
		} else {
			m.sessFailure = m.errorText(msg.sessErr)
		}
	} else {
		m.sessions = msg.sessions
	}
	m.fillProjects()
	m.fillSessions(false)
}

// dropConnection forgets a channel that broke, so the next read opens a new one; an answer of the service
// leaves it open.
func (m *Model) dropConnection(err error) {
	if m.conn == nil || answered(err) || errors.Is(err, errNoConnection) {
		return
	}
	_ = m.conn.Close()
	m.conn = nil
}

// errorText says in the message line why the service could not answer.
func (m *Model) errorText(err error) string {
	t := m.text
	var failure *cli.Failure
	var text string
	switch {
	case errors.Is(err, errNoConnection):
		text = t.Lookup(textErrNoConn)
	case errors.As(err, &failure):
		text = failure.Message
		if failure.NextStep != "" {
			text = t.Lookup(textErrService, failure.Message, failure.NextStep)
		}
	default:
		text = t.Lookup(textErrChannel, err.Error())
	}
	return t.Lookup(kittui.TextError, text)
}

// fillProjects sets the rows of the project list from the projects read last.
func (m *Model) fillProjects() {
	clear(m.projByID)
	items := make([]kittui.ListItem, len(m.projects))
	for i, p := range m.projects {
		id := kittui.ItemID(p.ID)
		m.projByID[id] = p
		items[i] = kittui.ListItem{ID: id, Cells: []string{p.Name, strconv.Itoa(p.InboxOpen)}}
	}
	if err := m.projList.SetItems(items); err != nil {
		m.failure = m.text.Lookup(kittui.TextError, m.text.Lookup(textErrRows, err.Error()))
	}
}

// selectedProject is the project under the cursor of the project list.
func (m *Model) selectedProject() (control.Project, bool) {
	id, ok := m.projList.Selected()
	if !ok {
		return control.Project{}, false
	}
	p, ok := m.projByID[id]
	return p, ok
}

// syncSessions shows the sessions of the chosen project: whatever moves the cursor of the project list
// leads here, so the session list always follows it.
func (m *Model) syncSessions() {
	id, _ := m.projList.Selected()
	if id != m.shown {
		m.fillSessions(true)
	}
}

// fillSessions sets the rows of the session list to the sessions of the chosen project. A change of project
// starts the list at its first row; a refresh keeps the cursor on its session.
func (m *Model) fillSessions(newProject bool) {
	id, _ := m.projList.Selected()
	m.shown = id
	clear(m.sessByID)
	clear(m.statusTones)
	var items []kittui.ListItem
	now := m.now()
	for _, s := range m.sessions {
		if kittui.ItemID(s.Project) != id || id == "" {
			continue
		}
		sid := kittui.ItemID(s.ID)
		m.sessByID[sid] = s
		status, tone := m.statusText(s.Status)
		m.statusTones[status] = tone
		items = append(items, kittui.ListItem{ID: sid,
			Cells: []string{s.Tool, s.Title, status, since(now, s.Since)}})
	}
	if newProject {
		m.sessList.Reset()
	}
	if err := m.sessList.SetItems(items); err != nil {
		m.failure = m.text.Lookup(kittui.TextError, m.text.Lookup(textErrRows, err.Error()))
	}
}

var statusTexts = map[string]struct {
	text kittui.TextID
	tone kittui.TextStyle
}{
	"running": {textStatusRunning, kittui.StyleActive}, "waiting": {textStatusWaiting, kittui.StyleWarn},
	"idle": {textStatusIdle, kittui.StyleHint}, "error": {textStatusError, kittui.StyleFail},
	"exited": {textStatusExited, kittui.StyleHint},
}

// statusText is the word of a status; one the interface does not know stands as the service sent it.
func (m *Model) statusText(status string) (string, kittui.TextStyle) {
	if s, ok := statusTexts[status]; ok {
		return m.text.Lookup(s.text), s.tone
	}
	return status, kittui.StyleText
}

// since is the time from then to now as the largest whole unit.
func since(now, then time.Time) string {
	if then.IsZero() {
		return "-"
	}
	d := max(now.Sub(then), 0)
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

// input is the input focus of the screen.
func (m *Model) input() kittui.InputFocus {
	if m.helpOpen || m.modal != nil {
		return kittui.InputNavigation
	}
	switch m.area {
	case areaSettings:
		return m.set.form.Input()
	case areaProjects:
		return m.projList.Input()
	case areaSessions:
		return m.sessList.Input()
	case areaSetup:
		return m.setup.list.Input()
	}
	return kittui.InputNavigation
}

// focusedList is the list the keys of the active area go to; nil where the area has none.
func (m *Model) focusedList() *kittui.List {
	switch m.area {
	case areaProjects:
		return m.projList
	case areaSessions:
		return m.sessList
	case areaSetup:
		return m.setup.list
	}
	return nil
}

// press runs one key press; the keyboard and the mouse both come here.
func (m *Model) press(msg tea.KeyPressMsg) tea.Cmd {
	m.notice, m.result = "", ""
	if m.modal != nil {
		return m.modalKey(msg)
	}
	if m.helpOpen {
		if act, handled := m.help.Update(msg); handled {
			if act != "" {
				m.helpOpen = false
			}
			return nil
		}
		if id, ok := m.keys.Match(msg, kittui.InputNavigation); ok && (id == actQuit || id == actForceQuit) {
			return tea.Quit
		}
		return nil
	}
	if m.area == areaSettings {
		act, handled := m.set.form.Update(msg)
		if cmd, done := m.formAct(act, handled); done {
			return cmd
		}
	}
	focus := m.input()
	if focus == kittui.InputText {
		if l := m.focusedList(); l != nil {
			if _, handled := l.Update(msg); handled {
				return nil
			}
		}
	}
	id, ok := m.keys.Match(msg, focus)
	if ok {
		if cmd, handled := m.navKey(id, msg, focus); handled {
			return cmd
		}
		switch id {
		case actQuit, actForceQuit:
			return tea.Quit
		case actHelp:
			m.openHelp()
			return nil
		case actReload:
			return tea.Batch(m.reload(), m.setupReload())
		case actInstall:
			return m.beginInstall()
		case kittui.ActionLogFollow, kittui.ActionLogNextMatch, kittui.ActionLogPrevMatch, kittui.ActionLogWrap,
			kittui.ActionLogTimestamps:
			if m.area == areaSetup {
				m.setup.log.Act(id)
			}
			return nil
		case actFull:
			m.toggleFull()
			return nil
		case actStop:
			return m.beginStop()
		case actRight:
			if m.area == areaProjects && m.focus() == kittui.ZoneRows {
				return m.openProject()
			}
		case actLeft:
			if m.area == areaSessions || m.area == areaInbox {
				return m.enter(areaProjects)
			}
		case actBack:
			if l := m.focusedList(); l != nil {
				if _, handled := l.Update(msg); handled {
					return nil
				}
			}
			m.back()
			return nil
		}
	}
	if l := m.focusedList(); l != nil && m.focus() != kittui.ZoneTabs {
		if open, _ := l.Update(msg); open != "" {
			switch m.area {
			case areaProjects:
				return m.openProject()
			case areaSetup:
				return m.beginInstall()
			}
		}
	}
	return nil
}

// toggleFull shows the pane of the active area alone, or the columns again. The Settings area is the whole
// screen already and has nothing to toggle.
func (m *Model) toggleFull() {
	if m.area != areaSettings && m.area != areaSetup {
		m.full = !m.full
	}
}

// back goes one level up: out of the full screen, then to the project list.
func (m *Model) back() {
	switch {
	case m.full:
		m.full = false
	case m.area != areaProjects:
		m.enter(areaProjects)
	}
}

// openProject shows the sessions of the project under the cursor, with the focus on them.
func (m *Model) openProject() tea.Cmd {
	if _, ok := m.selectedProject(); !ok {
		return nil
	}
	return m.enter(areaSessions)
}

// enter shows an area.
func (m *Model) enter(area kittui.AreaID) tea.Cmd {
	m.area = area
	m.setZone(kittui.ZoneRows)
	if area == areaSettings {
		m.setZone(kittui.ZoneForm)
	}
	return m.setupEntered()
}

// zones are the focus zones of the screen in their fixed order: the tab line and the content. The settings
// form takes tab for its own fields, so it is the only zone there.
func (m *Model) zones() []kittui.FocusZone {
	if m.area == areaSettings {
		return []kittui.FocusZone{kittui.ZoneForm}
	}
	return []kittui.FocusZone{kittui.ZoneTabs, kittui.ZoneRows}
}

// focus is the zone that has the focus: the chosen one, or the content when the screen has no such zone.
func (m *Model) focus() kittui.FocusZone {
	zones := m.zones()
	if slices.Contains(zones, m.zone) {
		return m.zone
	}
	return zones[len(zones)-1]
}

func (m *Model) setZone(z kittui.FocusZone) { m.zone = z }

// navKey handles the keys that switch the area or the focus zone, for every screen alike.
func (m *Model) navKey(id kittui.ActionID, msg tea.KeyPressMsg, focus kittui.InputFocus) (tea.Cmd, bool) {
	if id == actArea {
		if i := slices.Index(areaKeys, kittui.KeyName(msg)); i >= 0 {
			return m.enter(m.areas()[i].ID), true
		}
		return nil, true
	}
	if focus != kittui.InputNavigation || m.area == areaSettings {
		return nil, false
	}
	zones, cur := m.zones(), m.focus()
	tabs := cur == kittui.ZoneTabs
	switch {
	case id == actNext:
		m.setZone(kittui.CycleZone(zones, cur, 1))
	case id == actPrev:
		m.setZone(kittui.CycleZone(zones, cur, -1))
	case tabs && (id == actDown || id == actEnter):
		m.setZone(kittui.CycleZone(zones, cur, 1))
	case tabs && (id == actLeft || id == actRight):
		step := 1
		if id == actLeft {
			step = -1
		}
		cmd := m.enter(kittui.StepArea(m.areas(), m.area, step))
		m.setZone(kittui.ZoneTabs) // the arrows keep the focus on the tab line so they can go on
		return cmd, true
	default:
		return nil, false
	}
	return nil, true
}

func (m *Model) openHelp() {
	help, err := kittui.NewHelp(kittui.HelpSpec{Topics: helptopics.All(), Screen: m.footer(),
		Global: []kittui.ActionID{actHelp, actArea, actReload, actBack, actQuit}, Keys: helpKeys()}, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return
	}
	m.help, m.helpOpen = help, true
}

// syncFocus tells the lists whether their rows have the focus, which decides what a click on the marked row
// does.
func (m *Model) syncFocus() {
	rows := m.focus() == kittui.ZoneRows
	m.projList.SetFocused(rows && m.area == areaProjects)
	m.sessList.SetFocused(rows && m.area == areaSessions)
	m.setup.list.SetFocused(rows && m.area == areaSetup)
}

// marks are the status marks of the header.
func (m *Model) marks() []kittui.StatusMark {
	if m.stopping {
		return []kittui.StatusMark{{Kind: kittui.StatusBusy, Text: m.text.Lookup(textKeyStop)}}
	}
	return nil
}

func (m *Model) message() *kittui.Message {
	switch {
	case m.failure != "":
		return &kittui.Message{Kind: kittui.MessageError, Text: m.failure}
	case m.sessFailure != "":
		return &kittui.Message{Kind: kittui.MessageError, Text: m.sessFailure}
	case m.area == areaSetup && m.setup.failure != "":
		return &kittui.Message{Kind: kittui.MessageError, Text: m.setup.failure}
	case m.missing != "":
		return &kittui.Message{Kind: kittui.MessageWarning, Text: m.missingText()}
	case m.notice != "":
		return &kittui.Message{Kind: kittui.MessageWarning, Text: m.notice}
	case m.result != "":
		return &kittui.Message{Kind: kittui.MessageResult, Text: m.result}
	}
	return nil
}

// missingText names what is missing and the next step.
func (m *Model) missingText() string {
	return m.text.Lookup(kittui.TextWarning, m.text.Lookup(textMissingProgram, m.missing))
}

// footer is the key line of the screen.
func (m *Model) footer() []kittui.Hint {
	if m.modal != nil {
		// The keys of the screen behind a question do nothing; the question shows its own.
		return nil
	}
	nav := []kittui.Hint{
		{Action: actUp, Label: kittui.TextKeyChoose, Kind: kittui.HintNavigation},
		{Action: actDown, Label: kittui.TextKeyChoose, Kind: kittui.HintNavigation},
	}
	common := []kittui.Hint{
		{Action: actReload, Label: kittui.TextKeyReload, Priority: 2},
		{Action: actArea, Label: textKeyArea, Priority: 3},
		{Action: actQuit, Label: kittui.TextKeyQuit, Priority: 1},
	}
	if m.area == areaSettings {
		return append(m.set.form.Hints(), kittui.Hint{Action: actArea, Label: textKeyArea, Priority: 3},
			kittui.Hint{Action: actBack, Label: kittui.TextKeyBack, Priority: 1})
	}
	full := kittui.Hint{Action: actFull, Label: textKeyFull, Priority: 1}
	if m.full {
		full.Label = textKeyColumns
	}
	if m.focus() == kittui.ZoneTabs {
		common = append([]kittui.Hint{{Action: actLeft, Label: textKeyPrevArea, Priority: 3},
			{Action: actRight, Label: textKeyNextArea, Priority: 3},
			{Action: actNext, Label: textKeyZone, Priority: 3}}, common...)
	}
	if m.area != areaSetup {
		common = append([]kittui.Hint{full}, common...)
	}
	switch m.area {
	case areaSetup:
		return m.setupFooter(common, nav)
	case areaProjects:
		return m.listFooter(m.projList, len(m.projects), textKeySessions, common, nav)
	case areaSessions:
		if len(m.sessByID) > 0 {
			common = append([]kittui.Hint{{Action: actStop, Label: textKeyStop, Priority: 1}}, common...)
		}
		return m.listFooter(m.sessList, len(m.sessByID), "", common, nav)
	}
	return common
}

// listFooter is the key line of a screen that is a list; enter, where it has a label, opens the marked row.
func (m *Model) listFooter(list *kittui.List, rows int, enter kittui.TextID, common, nav []kittui.Hint) []kittui.Hint {
	if rows == 0 {
		return common
	}
	if list.Searching() {
		return []kittui.Hint{
			{Action: actEnter, Label: textFilterKeep},
			{Action: actBack, Label: textFilterClear},
		}
	}
	var hints []kittui.Hint
	if enter != "" {
		hints = append(hints, kittui.Hint{Action: actEnter, Label: enter})
	}
	hints = append(hints, kittui.Hint{Action: actSearch, Label: kittui.TextKeySearch, Priority: 1})
	if list.Query() != "" {
		hints = append(hints, kittui.Hint{Action: actBack, Label: textFilterClear})
	}
	return append(append(hints, common...), nav...)
}

func (m *Model) frameSpec() kittui.FrameSpec {
	spec := kittui.FrameSpec{
		Width: m.width, Height: m.height, Product: productName, Status: m.marks(),
		HeaderKeys: []kittui.Hint{{Action: actHelp, Label: kittui.TextKeyHelp}},
		Areas:      m.areas(), ActiveArea: m.area, Focus: m.focus(), Input: m.input(),
		Message: m.message(), Footer: m.footer(),
	}
	if l := m.focusedList(); l != nil {
		spec.Position = l.Position()
	}
	if m.area != areaProjects || m.full {
		spec.Back = &kittui.Hint{Action: actBack, Label: kittui.TextKeyBack}
	}
	return spec
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	if m.mouseOn {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// resizeView is what a terminal below the minimum size shows.
func (m *Model) resizeView() string {
	t := m.text
	lines := []string{
		m.styles.Render(kittui.StyleTitle, t.Lookup(kittui.TextResizeTitle)),
		t.Lookup(kittui.TextResizeNeed, minWidth, minHeight),
		t.Lookup(kittui.TextResizeHave, m.width, m.height),
	}
	for i, l := range lines {
		lines[i] = kittui.Clip(l, m.width)
	}
	return strings.Join(lines[:min(len(lines), m.height)], "\n")
}
