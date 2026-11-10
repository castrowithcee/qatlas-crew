package tui

import (
	"errors"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-crew/internal/control"
)

// setupLogLines is how many lines of results the Setup area keeps.
const setupLogLines = 200

// setupState is the Setup area: the programs qcrew needs, as the service reports them, and a log of what
// the installations did. The interface installs nothing itself: it hands the terminal to 'qcrew setup
// install <name>', which asks for every command.
type setupState struct {
	list *kittui.List
	log  *kittui.LogView
	// entries are the lines of the log, kept to fill a log that is built again after a change of language.
	entries []kittui.LogLine
	deps    []control.SetupDependency
	byID    map[kittui.ItemID]control.SetupDependency
	loaded  bool
	// failure is the error of the last check.
	failure string
	// checking is set while a check is under way; wanted while one is asked for but there is no channel yet;
	// recheck while the answer under way predates an installation that ended meanwhile.
	checking, wanted, recheck bool
	// installing is the program whose installation holds the terminal.
	installing string
	// pending is the result of an installation that waits for the check made after it.
	pending    *installResult
	executable func() (string, error)
	// execProcess hands the terminal to a program; tests replace it.
	execProcess func(*exec.Cmd, tea.ExecCallback) tea.Cmd
	tones       map[string]kittui.TextStyle
}

// installResult is how an installation ended.
type installResult struct {
	name string
	code int   // the exit code; -1 if the program did not start
	err  error // why it did not start or end normally
}

// setupMsg is what one check of the programs found.
type setupMsg struct {
	deps []control.SetupDependency
	err  error
}

// installDoneMsg reports that the terminal came back from an installation.
type installDoneMsg struct {
	name string
	err  error
}

func (m *Model) newSetup(executable func() (string, error)) (*setupState, error) {
	s := &setupState{byID: map[kittui.ItemID]control.SetupDependency{}, tones: map[string]kittui.TextStyle{},
		executable: executable, execProcess: tea.ExecProcess}
	if s.executable == nil {
		s.executable = os.Executable
	}
	var err error
	if s.list, s.log, err = m.newSetupWidgets(m.text); err != nil {
		return nil, err
	}
	return s, nil
}

func (m *Model) newSetupWidgets(text kittui.Translator) (*kittui.List, *kittui.LogView, error) {
	list, err := kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{
		{Title: textColProgram}, {Title: textColStatus, Tone: m.setupTone},
		{Title: textColVersion}, {Title: textColInstall, Flex: true}},
		Keys: listKeys(), Search: kittui.SearchByKey, Empty: textSetupEmpty}, m.keys, text)
	if err != nil {
		return nil, nil, err
	}
	log, err := kittui.NewLogView(kittui.LogViewSpec{Lines: setupLogLines, Wrap: true, Now: m.now,
		Keys: kittui.LogViewKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst,
			Last: actLast, Search: actSearch, Enter: actEnter, Back: actBack, Follow: kittui.ActionLogFollow,
			NextMatch: kittui.ActionLogNextMatch, PrevMatch: kittui.ActionLogPrevMatch, Wrap: kittui.ActionLogWrap,
			Timestamps: kittui.ActionLogTimestamps}}, m.keys, text)
	return list, log, err
}

func (m *Model) setupTone(cell string) kittui.TextStyle {
	if t, ok := m.setup.tones[cell]; ok {
		return t
	}
	return kittui.StyleText
}

// relocalizeSetup builds the widgets of the area again with the new texts, keeping cursor and log.
func (m *Model) relocalizeSetup() {
	s := m.setup
	list, log, err := m.newSetupWidgets(m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return
	}
	mem := remember(s.list)
	s.list, s.log = list, log
	log.Append(s.entries...)
	m.fillSetup()
	mem.restore(s.list)
}

// fillSetup sets the rows of the list from the programs read last.
func (m *Model) fillSetup() {
	s := m.setup
	clear(s.byID)
	clear(s.tones)
	items := make([]kittui.ListItem, len(s.deps))
	for i, d := range s.deps {
		id := kittui.ItemID(d.Name)
		s.byID[id] = d
		status, tone := m.text.Lookup(textSetupMissing), kittui.StyleWarn
		if d.Installed {
			status, tone = m.text.Lookup(textSetupInstalled), kittui.StyleActive
		}
		s.tones[status] = tone
		items[i] = kittui.ListItem{ID: id, Cells: []string{d.Name, status, d.Version, m.installCell(d)}}
	}
	if err := s.list.SetItems(items); err != nil {
		m.failure = m.text.Lookup(kittui.TextError, m.text.Lookup(textErrRows, err.Error()))
	}
}

// installCell is the button of a program that can be installed here, or, for one that cannot, where its
// maker says how.
func (m *Model) installCell(d control.SetupDependency) string {
	switch {
	case d.Installed:
		return ""
	case d.Install != "":
		return m.text.Lookup(textSetupButton)
	}
	return m.text.Lookup(textSetupSee, d.Source)
}

// setupReload checks the programs again if the area is shown.
func (m *Model) setupReload() tea.Cmd {
	if m.area != areaSetup {
		return nil
	}
	return m.checkSetup()
}

// setupEntered checks the programs when the area is entered.
func (m *Model) setupEntered() tea.Cmd { return m.setupReload() }

// checkSetup asks the service which programs are installed. The check starts programs to read their version,
// so it runs when the area is shown, on reload and after an installation, not with the periodic read.
func (m *Model) checkSetup() tea.Cmd {
	s := m.setup
	if s.checking {
		if s.pending != nil {
			s.recheck = true
		}
		return nil
	}
	if m.conn == nil {
		// The next read opens the channel and asks again.
		s.wanted = true
		return nil
	}
	s.checking, s.wanted = true, false
	ctx, conn := m.ctx, m.conn
	return func() tea.Msg {
		deps, err := conn.SetupCheck(ctx)
		return setupMsg{deps: deps, err: err}
	}
}

// setupAfterRefresh asks for the check that waited for a channel.
func (m *Model) setupAfterRefresh() tea.Cmd {
	if m.setup.wanted && m.conn != nil {
		return m.checkSetup()
	}
	return nil
}

// applySetup takes a check. A failed one keeps the rows read before.
func (m *Model) applySetup(msg setupMsg) tea.Cmd {
	s := m.setup
	s.checking = false
	if msg.err != nil {
		s.failure = m.errorText(msg.err)
		m.dropConnection(msg.err)
		if s.pending != nil {
			m.logInstall(*s.pending, nil, msg.err)
			s.pending = nil
		}
		return nil
	}
	s.failure, s.deps, s.loaded = "", msg.deps, true
	m.fillSetup()
	if s.recheck {
		s.recheck = false
		return m.checkSetup()
	}
	if s.pending != nil {
		m.logInstall(*s.pending, msg.deps, nil)
		s.pending = nil
	}
	return nil
}

// beginInstall asks to install the program under the cursor. Nothing runs before the answer, and even then
// only the terminal is handed over: the commands have their own questions.
func (m *Model) beginInstall() tea.Cmd {
	if m.area != areaSetup {
		return nil
	}
	s, t := m.setup, m.text
	if s.installing != "" {
		m.warn(t.Lookup(textSetupBusy))
		return nil
	}
	id, _ := s.list.Selected()
	d, ok := s.byID[id]
	switch {
	case !ok:
		m.warn(t.Lookup(textSetupNothing))
		return nil
	case d.Installed:
		m.warn(t.Lookup(textSetupHave, d.Name))
		return nil
	case d.Install == "":
		m.warn(t.Lookup(textSetupNoWay, d.Name, d.Source))
		return nil
	}
	details := []kittui.DialogLine{{Prefix: "$ ", Text: d.Install}}
	if d.Sudo {
		details = append(details, kittui.DialogLine{Text: t.Lookup(textDetailSudo), Style: kittui.StyleWarn})
	}
	dialog, err := kittui.NewDialog(kittui.DialogSpec{Title: textTitleInstall, Question: t.Lookup(textAskInstall, d.Name),
		Consequence: t.Lookup(textWhatInstall, d.Name), Details: details,
		Answers: []kittui.Answer{{Action: actYes, Label: textAnswerInstall}, {Action: actNo, Label: textAnswerKeep}},
		Default: actNo, Keys: kittui.DialogKeys{Enter: actEnter}}, m.keys, t)
	if err != nil {
		m.failure = t.Lookup(kittui.TextError, err.Error())
		return nil
	}
	m.modal = &modal{dialog: dialog, yes: func() tea.Cmd { return m.runInstall(d.Name) }}
	return nil
}

// runInstall hands the terminal to 'qcrew setup install <name>' and takes it back when that ends.
func (m *Model) runInstall(name string) tea.Cmd {
	s := m.setup
	exe, err := s.executable()
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, m.text.Lookup(textErrExec, err.Error()))
		return nil
	}
	s.installing = name
	return s.execProcess(exec.Command(exe, "setup", "install", name),
		func(err error) tea.Msg { return installDoneMsg{name: name, err: err} })
}

// installDone notes how the installation ended and checks the programs again.
func (m *Model) installDone(msg installDoneMsg) tea.Cmd {
	s := m.setup
	s.installing = ""
	res := installResult{name: msg.name, err: msg.err}
	var coded interface{ ExitCode() int }
	switch {
	case msg.err == nil:
	case errors.As(msg.err, &coded):
		res.code = coded.ExitCode()
	default:
		res.code = -1
	}
	s.pending = &res
	return m.checkSetup()
}

// logInstall adds the result of an installation to the log: how the command ended, whether the program is
// found now, and the next step.
func (m *Model) logInstall(res installResult, deps []control.SetupDependency, checkErr error) {
	t, s := m.text, m.setup
	var lines []string
	if res.code < 0 {
		lines = append(lines, t.Lookup(textLogNoStart, res.name, res.err.Error()))
	} else {
		lines = append(lines, t.Lookup(textLogExit, res.name, res.code))
	}
	found, version := false, ""
	for _, d := range deps {
		if d.Name == res.name {
			found, version = d.Installed, d.Version
		}
	}
	switch {
	case checkErr != nil:
		lines = append(lines, t.Lookup(textLogNoCheck, res.name, checkErr.Error()), t.Lookup(textNextCheck))
	case res.code != 0:
		lines = append(lines, t.Lookup(textNextFailed))
	case found:
		if version != "" {
			lines = append(lines, t.Lookup(textLogFound, res.name, version))
		} else {
			lines = append(lines, t.Lookup(textLogFoundPlain, res.name))
		}
		lines = append(lines, t.Lookup(textNextReady, res.name))
	default:
		lines = append(lines, t.Lookup(textLogNotFound, res.name), t.Lookup(textNextPath))
	}
	at := m.now()
	var added []kittui.LogLine
	for _, l := range lines {
		added = append(added, kittui.LogLine{Text: l, Time: at})
	}
	s.entries = append(s.entries, added...)
	if len(s.entries) > setupLogLines {
		s.entries = s.entries[len(s.entries)-setupLogLines:]
	}
	s.log.Append(added...)
}

// setupFooter is the key line of the Setup area.
func (m *Model) setupFooter(common, nav []kittui.Hint) []kittui.Hint {
	s := m.setup
	if id, ok := s.list.Selected(); ok {
		if d := s.byID[id]; !d.Installed && d.Install != "" {
			common = append([]kittui.Hint{{Action: actInstall, Label: textKeyInstall, Priority: 1}}, common...)
		}
	}
	return m.listFooter(s.list, len(s.deps), "", common, nav)
}

// setupHeadHeight is the rows the list of programs takes: its title, its header and up to three programs.
const setupHeadHeight = 5

// setupView draws the area: the programs on top and, below them, the log of the installations.
func (m *Model) setupView(width, height int) string {
	s := m.setup
	if !s.loaded {
		text := m.text.Lookup(textSetupLoading)
		if s.failure != "" {
			text = ""
		}
		return fit(m.note(text, width), width, height)
	}
	top := min(height, max(setupHeadHeight, len(s.deps)+2))
	if height-top < 3 {
		top = height
	}
	out := m.setupHead(textPaneSetup, m.area == areaSetup && m.focus() == kittui.ZoneRows, width, 0) + "\n" +
		fit(m.listView(s.list, width, top-1, 0, 1), width, top-1)
	if top >= height {
		return fit(out, width, height)
	}
	logHeight := height - top
	view, targets := s.log.View(m.styles, width, logHeight-1)
	m.parts = append(m.parts, mousePart{rect: kittui.Rect{Y: top + 1, Width: width, Height: logHeight - 1}, log: s.log,
		targets: kittui.OffsetTargets(targets, 0, top+1)})
	return out + "\n" + m.setupHead(textPaneSetupLog, false, width, top) + "\n" + fit(view, width, logHeight-1)
}

// setupHead draws the title row of a part of the area at row y; a click on it enters the area.
func (m *Model) setupHead(id kittui.TextID, focused bool, width, y int) string {
	title, tone := "  "+kittui.Escape(m.text.Lookup(id)), kittui.StyleHint
	if focused {
		title, tone = m.styles.SelectionMark()+" "+kittui.Escape(m.text.Lookup(id)), kittui.StyleActive
	}
	rect := kittui.Rect{Y: y, Width: width, Height: 1}
	m.parts = append(m.parts, mousePart{rect: rect, targets: []kittui.Target{{Rect: rect, Area: areaSetup}}})
	return m.styles.Render(tone, kittui.Pad(kittui.Clip(title, width), width))
}
