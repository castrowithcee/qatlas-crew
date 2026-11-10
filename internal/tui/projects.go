package tui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/output"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-crew/internal/client"
	"github.com/castrowithcee/qatlas-crew/internal/control"
)

// The fields of the dialog that adds a project, below the folder picker.
const (
	addModeField kittui.FieldID = "mode"
	addNewField  kittui.FieldID = "newfolder"
	addNameField kittui.FieldID = "name"

	addExisting kittui.ChoiceID = "existing"
	addNew      kittui.ChoiceID = "new"
)

// The tools a session runs, each with the action that starts it, in the order of their buttons.
var toolActions = []struct {
	tool string
	act  kittui.ActionID
}{{"claude", actClaude}, {"codex", actCodex}, {"shell", actShell}}

// addDialog adds a project: the folder picker, then whether the folder itself or a new folder in it becomes
// the project, and its name. It fills the workspace; nothing behind it takes a key or a click.
type addDialog struct {
	picker *folderPicker
	form   *kittui.Form
	// onForm says that the fields below the picker have the focus.
	onForm bool
	// autoName is the name the dialog filled in itself; a name the person typed stays as typed.
	autoName string
	running  bool
}

// addMsg is the answer to adding a project; removeMsg to removing one, startMsg to starting a session.
type (
	addMsg struct {
		project control.Project
		err     error
	}
	removeMsg struct {
		project control.Project
		err     error
	}
	startMsg struct {
		tool    string
		session control.Session
		err     error
	}
)

// addForm builds the fields below the picker with values; the field of the new folder stands only where a new
// folder is made.
func (m *Model) addForm(values map[kittui.FieldID]kittui.FormValue) (*kittui.Form, error) {
	mode := values[addModeField].Choice
	if mode == "" {
		mode = addExisting
	}
	fields := []kittui.FormField{{ID: addModeField, Kind: kittui.FieldChoice, Label: textAddUse, Hint: textHintUse,
		Choices: []kittui.Choice{{ID: addExisting, Label: textAddExisting}, {ID: addNew, Label: textAddNew}},
		Value:   kittui.FormValue{Choice: mode}}}
	if mode == addNew {
		fields = append(fields, kittui.FormField{ID: addNewField, Kind: kittui.FieldText, Label: textAddNewName,
			Hint: textHintNewName, Value: kittui.FormValue{Text: values[addNewField].Text}})
	}
	fields = append(fields, kittui.FormField{ID: addNameField, Kind: kittui.FieldText, Label: textAddName,
		Hint: textHintName, Value: kittui.FormValue{Text: values[addNameField].Text}})
	return kittui.NewForm(kittui.FormSpec{Keys: formKeys(), Fields: fields,
		Buttons: []kittui.FormButton{{Action: actSave, Label: textAnswerAdd, Primary: true},
			{Action: actBack, Label: textAnswerKeep}}}, m.keys, m.text)
}

// beginAdd opens the dialog that adds a project, in the home folder.
func (m *Model) beginAdd() tea.Cmd {
	if m.area == areaSettings {
		return nil
	}
	start := "/"
	if m.home != "" {
		start = "~/"
	}
	picker, err := newFolderPicker(m.keys, m.text, m.folders, m.home, start)
	var form *kittui.Form
	if err == nil {
		form, err = m.addForm(nil)
	}
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return nil
	}
	m.add = &addDialog{picker: picker, form: form}
	m.fillName()
	return nil
}

// fillName puts the name of the chosen folder into the name field, unless the person typed a name.
func (m *Model) fillName() {
	d := m.add
	v := d.form.Values()
	if v[addNameField].Text != d.autoName {
		return
	}
	name := ""
	if folder, ok := d.picker.Folder(); ok {
		name = filepath.Base(folder)
		if name == "/" {
			name = ""
		}
	}
	if v[addModeField].Choice == addNew {
		name = strings.TrimSpace(v[addNewField].Text)
	}
	if name != d.autoName && d.form.SetValue(addNameField, kittui.FormValue{Text: name}) == nil {
		d.autoName = name
	}
}

// addInput is the input focus of the dialog.
func (d *addDialog) input() kittui.InputFocus {
	if d.onForm {
		return d.form.Input()
	}
	return d.picker.Input()
}

// addKey reads a key while the dialog is open. Save adds from anywhere, back closes the dialog unless a list of
// values or a filter takes it, and ctrl+c quits.
func (m *Model) addKey(msg tea.KeyPressMsg) tea.Cmd {
	d := m.add
	if id, ok := m.keys.Match(msg, kittui.InputNavigation); ok && id == actForceQuit {
		return tea.Quit
	}
	if d.running {
		return nil
	}
	is := func(id kittui.ActionID) bool { return m.keys.Is(msg, id, d.input()) }
	if !d.onForm {
		switch leave, handled := d.picker.Update(msg); {
		case leave > 0:
			d.onForm = true
			d.form.Focus(addModeField)
		case leave < 0:
			d.onForm = true
			d.form.Focus(addNameField)
		case !handled && is(actSave):
			return m.submitAdd()
		case !handled && is(actBack):
			m.add = nil
		}
		if m.add != nil {
			m.fillName()
		}
		return nil
	}
	if btn, ok := d.form.FocusedButton(); ok && btn == actBack && is(actNext) {
		d.onForm = false
		d.picker.Focus(false)
		return nil
	}
	if d.form.Focused() == addModeField && !onButton(d.form) && is(actPrev) {
		d.onForm = false
		d.picker.Focus(true)
		return nil
	}
	mode := d.form.Values()[addModeField].Choice
	act, handled := d.form.Update(msg)
	return m.addFormAct(act, handled, is(actUp), mode)
}

func onButton(f *kittui.Form) bool {
	_, ok := f.FocusedButton()
	return ok
}

// addFormAct runs what the fields below the picker reported for a key or a click. A change of the mode builds
// them again, so the field of the new folder comes and goes.
func (m *Model) addFormAct(act kittui.ActionID, handled, up bool, mode kittui.ChoiceID) tea.Cmd {
	d := m.add
	switch {
	case act == actSave && handled:
		return m.submitAdd()
	case act == actBack:
		m.add = nil
		return nil
	case !handled && up:
		d.onForm = false
		d.picker.Focus(true)
	}
	if v := d.form.Values(); v[addModeField].Choice != mode {
		focus := d.form.Focused()
		if form, err := m.addForm(v); err == nil {
			d.form = form
			d.form.Focus(focus)
		}
	}
	m.fillName()
	return nil
}

// submitAdd checks what can be checked here and asks the service to add the project. Every value stays as typed
// when something is wrong.
func (m *Model) submitAdd() tea.Cmd {
	d, t := m.add, m.text
	v := d.form.Values()
	problem := func(field kittui.FieldID, cause, next string) tea.Cmd {
		d.form.SetProblems([]kittui.FormProblem{{Field: field, Cause: cause, NextStep: next}})
		// A problem of the path keeps the focus in the path; one of a field goes to that field.
		d.onForm = field != ""
		return nil
	}
	typed := strings.TrimSpace(d.picker.Text())
	folder, ok := d.picker.Folder()
	switch {
	case typed == "":
		return problem("", t.Lookup(textAddNoPath), t.Lookup(textPickTypeFolder))
	case !ok:
		return problem("", t.Lookup(textAddNotAbs, typed), t.Lookup(textPickTypeFolder))
	}
	args := control.ProjectAddArgs{Path: folder, Name: strings.TrimSpace(v[addNameField].Text)}
	if v[addModeField].Choice == addNew {
		name := strings.TrimSpace(v[addNewField].Text)
		switch {
		case name == "":
			return problem(addNewField, t.Lookup(textAddNoNew), t.Lookup(textAddNextNew))
		case strings.ContainsRune(name, '/') || name == "." || name == "..":
			return problem(addNewField, t.Lookup(textAddBadNew), t.Lookup(textAddNextNew))
		}
		if info, err := m.folders.Stat(folder); err != nil || !info.IsDir() {
			return problem("", t.Lookup(textAddNoPlace, typed), t.Lookup(textAddNextPlace))
		}
		args.Path = filepath.Join(folder, name)
		if _, err := m.folders.Stat(args.Path); err == nil {
			return problem(addNewField, t.Lookup(textAddExists, name), t.Lookup(textAddNextHere))
		}
		args.Create = true
	}
	d.running = true
	ctx, conn := m.ctx, m.conn
	return func() tea.Msg {
		if conn == nil {
			return addMsg{err: errNoConnection}
		}
		p, err := conn.AddProject(ctx, args)
		return addMsg{project: p, err: err}
	}
}

// applyAdd closes the dialog and marks the new project, or shows in the dialog why it failed.
func (m *Model) applyAdd(msg addMsg) tea.Cmd {
	d := m.add
	if msg.err != nil {
		m.dropConnection(msg.err)
		if d == nil {
			m.failure = m.errorText(msg.err)
			return nil
		}
		d.running = false
		cause, next := m.addFailure(msg.err)
		d.form.SetProblems([]kittui.FormProblem{{Cause: cause, NextStep: next}})
		return nil
	}
	m.add = nil
	if !slices.ContainsFunc(m.projects, func(p control.Project) bool { return p.ID == msg.project.ID }) {
		m.projects = append(m.projects, msg.project)
	}
	m.fillProjects()
	m.projList.SetFilter("")
	m.projList.Select(kittui.ItemID(msg.project.ID))
	m.result = m.text.Lookup(textResultAdd, msg.project.Name)
	return m.reload()
}

// addFailure is the cause and the next step of a project the service did not add.
func (m *Model) addFailure(err error) (cause, next string) {
	t := m.text
	var failure *cli.Failure
	switch {
	case errors.As(err, &failure):
		next = failure.NextStep
		switch failure.Code {
		case client.CodeAlreadyExists:
			next = t.Lookup(textAddNextTaken)
		case output.CodeNotFound:
			next = t.Lookup(textAddNextMissing)
		case output.CodeInvalidRequest:
			next = t.Lookup(textAddNextInvalid)
		}
		return failure.Message, next
	case errors.Is(err, errNoConnection):
		return t.Lookup(textErrNoConn), t.Lookup(textAddNextRetry)
	}
	return t.Lookup(textErrChannel, err.Error()), t.Lookup(textAddNextRetry)
}

// addPosition is the position in the folders while they have the focus.
func (m *Model) addPosition() string {
	if m.add.onForm || !m.add.picker.onList {
		return ""
	}
	return m.add.picker.list.Position()
}

// addHints are the keys of the dialog for the footer.
func (m *Model) addHints() []kittui.Hint {
	d := m.add
	add := kittui.Hint{Action: actSave, Label: textAnswerAdd, Kind: kittui.HintAlways}
	cancel := kittui.Hint{Action: actBack, Label: textAnswerKeep, Priority: 1}
	if !d.onForm {
		return append(d.picker.Hints(), add, cancel)
	}
	var hints []kittui.Hint
	for _, h := range d.form.Hints() {
		if h.Action != actSave {
			hints = append(hints, h)
		}
	}
	if !d.form.Open() {
		hints = append(hints, add, cancel)
	}
	return hints
}

// addView draws the dialog in width by height cells of the workspace and registers its parts for the mouse.
func (m *Model) addView(width, height int) string {
	d, t := m.add, m.text
	title := m.styles.Render(kittui.StyleTitle, kittui.Clip(kittui.Escape(t.Lookup(textTitleAdd)), width))
	lead := m.styles.Render(kittui.StyleHint, kittui.Clip(kittui.Escape(t.Lookup(textAddLead)), width))
	top := 2
	formH := min(max(height-top-pickerRoom, 3), 14)
	pickH := max(height-top-formH, 1)
	pick, ft, lt, listY := d.picker.View(m.styles, t, width, pickH)
	form, fts := d.form.View(m.styles, width, formH)
	m.parts = append(m.parts,
		mousePart{rect: kittui.Rect{Y: top, Width: width, Height: listY}, form: d.picker.form, targets: kittui.OffsetTargets(ft, 0, top)},
		mousePart{rect: kittui.Rect{Y: top + listY, Width: width, Height: pickH - listY}, list: d.picker.list, targets: kittui.OffsetTargets(lt, 0, top)},
		mousePart{rect: kittui.Rect{Y: top + pickH, Width: width, Height: formH}, form: d.form, targets: kittui.OffsetTargets(fts, 0, top+pickH)})
	return fit(kittui.Pad(title, width)+"\n"+kittui.Pad(lead, width)+"\n"+pick+"\n"+form, width, height)
}

// addMouse acts on a click or the wheel while the dialog is open: only its parts and the keys of its footer
// take them.
func (m *Model) addMouse(msg tea.MouseMsg, res kittui.MouseResult) tea.Cmd {
	d := m.add
	if d.running {
		return nil
	}
	at := msg.Mouse()
	p, inPart := m.partAt(at.X, at.Y)
	switch {
	case res.WheelSteps != 0 && inPart && p.list != nil:
		p.list.Wheel(res.WheelSteps)
	case res.WheelSteps != 0 && inPart && p.form == d.form:
		d.form.Wheel(res.WheelSteps)
	case res.Item != "" && inPart && p.list == d.picker.list:
		d.onForm = false
		d.picker.Click(res, true)
		m.fillName()
	case res.Item != "" && inPart && p.form == d.picker.form:
		d.onForm = false
		d.picker.Click(res, false)
	case res.Item != "" && inPart && p.form == d.form:
		d.onForm = true
		mode := d.form.Values()[addModeField].Choice
		act, handled := d.form.Click(res)
		return m.addFormAct(act, handled, false, mode)
	case res.Action != "" && slices.ContainsFunc(m.addHints(), func(h kittui.Hint) bool { return h.Action == res.Action }):
		if key, ok := m.keyOf(res.Action); ok {
			return m.press(key)
		}
	}
	return nil
}

// beginRemove asks to remove the chosen project from qcrew. Nothing reaches the service before the answer.
func (m *Model) beginRemove() tea.Cmd {
	if m.area != areaProjects && m.area != areaSessions {
		return nil
	}
	t := m.text
	if m.removing {
		m.warn(t.Lookup(textRemoveBusy))
		return nil
	}
	p, ok := m.selectedProject()
	if !ok {
		m.warn(t.Lookup(textRemoveNothing))
		return nil
	}
	var details []kittui.DialogLine
	if p.Path != "" {
		details = []kittui.DialogLine{{Text: t.Lookup(textDetailPath, p.Path), Style: kittui.StyleHint}}
	}
	dialog, err := kittui.NewDialog(kittui.DialogSpec{Title: textTitleRemove, Question: t.Lookup(textAskRemove, p.Name),
		Consequence: t.Lookup(textWhatRemove), Details: details,
		Answers: []kittui.Answer{{Action: actYes, Label: textAnswerRemove}, {Action: actNo, Label: textAnswerKeep}},
		Default: actNo, Danger: actYes, Keys: kittui.DialogKeys{Enter: actEnter}}, m.keys, t)
	if err != nil {
		m.failure = t.Lookup(kittui.TextError, err.Error())
		return nil
	}
	m.modal = &modal{dialog: dialog, yes: func() tea.Cmd { return m.remove(p) }}
	return nil
}

// remove asks the service to remove the project.
func (m *Model) remove(p control.Project) tea.Cmd {
	m.removing = true
	ctx, conn := m.ctx, m.conn
	return func() tea.Msg {
		if conn == nil {
			return removeMsg{project: p, err: errNoConnection}
		}
		_, err := conn.RemoveProject(ctx, p.ID)
		return removeMsg{project: p, err: err}
	}
}

// applyRemove shows the outcome of a removal; a project with running sessions names the way to stop them.
func (m *Model) applyRemove(msg removeMsg) tea.Cmd {
	m.removing = false
	t := m.text
	var failure *cli.Failure
	switch {
	case errors.As(msg.err, &failure) && failure.Code == client.CodeInUse:
		m.failure = t.Lookup(kittui.TextError, t.Lookup(textErrService, failure.Message, t.Lookup(textRemoveInUse)))
		return nil
	case msg.err != nil:
		m.failure = m.errorText(msg.err)
		m.dropConnection(msg.err)
		return nil
	}
	m.projects = slices.DeleteFunc(m.projects, func(p control.Project) bool { return p.ID == msg.project.ID })
	m.fillProjects()
	m.result = t.Lookup(textResultRemove, msg.project.Name)
	return m.reload()
}

// startTool starts a session of tool in the chosen project. A tool the service found missing before is not
// asked for again until the person reads the service again; its button names the next step.
func (m *Model) startTool(tool string) tea.Cmd {
	if m.area != areaProjects && m.area != areaSessions {
		return nil
	}
	t := m.text
	p, ok := m.selectedProject()
	switch {
	case !ok:
		m.warn(t.Lookup(textSessionsNoProj))
		return nil
	case m.toolMissing[tool]:
		m.warn(t.Lookup(textMissingTool, tool))
		return nil
	case m.starting:
		m.warn(t.Lookup(textStartBusy))
		return nil
	}
	m.starting = true
	ctx, conn := m.ctx, m.conn
	return func() tea.Msg {
		if conn == nil {
			return startMsg{tool: tool, err: errNoConnection}
		}
		s, err := conn.StartSession(ctx, control.SessionStartArgs{Project: p.ID, Tool: tool})
		return startMsg{tool: tool, session: s, err: err}
	}
}

// applyStart marks the new session in the session list, or says why it did not start.
func (m *Model) applyStart(msg startMsg) tea.Cmd {
	m.starting = false
	t := m.text
	var failure *cli.Failure
	if errors.As(msg.err, &failure) && failure.Code == client.CodeToolMissing {
		m.toolMissing[msg.tool] = true
	}
	if text, ok := missingProgram(msg.err); ok {
		m.notice = t.Lookup(kittui.TextWarning, t.Lookup(textMissingProgram, text))
		return nil
	}
	if msg.err != nil {
		m.failure = m.errorText(msg.err)
		m.dropConnection(msg.err)
		return nil
	}
	s := msg.session
	if !slices.ContainsFunc(m.sessions, func(o control.Session) bool { return o.ID == s.ID }) {
		m.sessions = append(m.sessions, s)
	}
	m.fillSessions(false)
	if kittui.ItemID(s.Project) == m.shown {
		m.area = areaSessions
		m.setZone(kittui.ZoneRows)
		m.sessList.SetFilter("")
		m.sessList.Select(kittui.ItemID(s.ID))
	}
	name := s.Title
	if name == "" {
		name = s.Tool
	}
	m.result = t.Lookup(textResultStart, name)
	return m.reload()
}

// paneButtons are the buttons in the title row of a pane: adding and removing a project over the project list,
// one per tool over the session list. A tool found missing names the next step on its button.
func (m *Model) paneButtons(p pane) (labels []string, acts []kittui.ActionID) {
	t := m.text
	switch p {
	case paneProjects:
		labels = []string{t.Lookup(textBtnAdd)}
		acts = []kittui.ActionID{actAdd}
		if _, ok := m.selectedProject(); ok {
			labels, acts = append(labels, t.Lookup(textBtnRemove)), append(acts, actRemove)
		}
	case paneSessions:
		if _, ok := m.selectedProject(); !ok || m.missing != "" {
			return nil, nil
		}
		for _, ta := range toolActions {
			label := ta.tool
			if m.toolMissing[ta.tool] {
				label = t.Lookup(textBtnToolMissing, ta.tool)
			}
			labels, acts = append(labels, label), append(acts, ta.act)
		}
	}
	return labels, acts
}

// paneHead draws the title row of a pane in width cells at x and y of the workspace: the title on the left, the
// buttons of the pane on the right where they fit beside a part of the title, and registers its targets. The
// buttons are drawn after the title target, so a click on them wins.
func (m *Model) paneHead(p pane, title string, tone kittui.TextStyle, width, x, y int) string {
	targets := []kittui.Target{{Rect: kittui.Rect{X: x, Y: y, Width: width, Height: 1}, Area: paneArea[p]}}
	labels, acts := m.paneButtons(p)
	var buttons []string
	room := 0
	for _, l := range labels {
		b := m.styles.Button(kittui.ButtonNormal, l, false)
		buttons = append(buttons, b)
		room += ansi.StringWidth(b) + 1
	}
	if len(buttons) == 0 || width < room+12 {
		m.parts = append(m.parts, mousePart{rect: kittui.Rect{X: x, Y: y, Width: width, Height: 1}, targets: targets})
		return m.styles.Render(tone, kittui.Pad(kittui.Clip(title, width), width))
	}
	left := width - room
	head := m.styles.Render(tone, kittui.Pad(kittui.Clip(title, left), left))
	bx := x + left
	for i, b := range buttons {
		w := ansi.StringWidth(b)
		targets = append(targets, kittui.Target{Rect: kittui.Rect{X: bx, Y: y, Width: w, Height: 1}, Action: acts[i]})
		head += b + " "
		bx += w + 1
	}
	m.parts = append(m.parts, mousePart{rect: kittui.Rect{X: x, Y: y, Width: width, Height: 1}, targets: targets})
	return head
}

// projectFooter are the keys of adding a project, over the project list, and of starting sessions, over the
// session list, for the footer of area. Removing has its button and the help; the footer keeps its room for the
// moves.
func (m *Model) projectFooter(area kittui.AreaID) []kittui.Hint {
	if area == areaProjects {
		return []kittui.Hint{{Action: actAdd, Label: textKeyAdd, Priority: 2}}
	}
	var hints []kittui.Hint
	if _, ok := m.selectedProject(); ok && m.missing == "" {
		for _, ta := range toolActions {
			hints = append(hints, kittui.Hint{Action: ta.act, Label: textKeyStart, Priority: 1})
		}
	}
	return hints
}

// toolOf is the tool an action starts.
func toolOf(id kittui.ActionID) (string, bool) {
	for _, ta := range toolActions {
		if ta.act == id {
			return ta.tool, true
		}
	}
	return "", false
}
