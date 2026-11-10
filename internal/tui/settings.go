package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

// settingsState is the Settings area: one form with the device-local preferences.
type settingsState struct {
	form *kittui.Form
}

func (m *Model) newSettings() (*settingsState, error) {
	form, err := m.generalForm()
	if err != nil {
		return nil, err
	}
	return &settingsState{form: form}, nil
}

func choice(id string, label kittui.TextID) kittui.Choice {
	return kittui.Choice{ID: kittui.ChoiceID(id), Label: label}
}

// generalForm is the form of the preferences. It holds finite choices only, so nothing is typed.
func (m *Model) generalForm() (*kittui.Form, error) {
	p := m.prefs
	if !slices.Contains(m.themes.Names(), p.Theme) {
		p.Theme = m.theme
	}
	if !slices.Contains(m.themes.Names(), p.Theme) {
		p.Theme = kittui.ThemeTerminal
	}
	if !slices.Contains(areaIDs(), kittui.AreaID(p.Start)) {
		p.Start = string(areaProjects)
	}
	if p.Icons == "" {
		p.Icons = uiprefs.IconsUnicode
	}
	if p.Language == "" {
		p.Language = uiprefs.LanguageEnglish
	}
	var themes []kittui.Choice
	for _, name := range m.themes.Names() {
		// A theme name is a proper name; the catalog has no text for it, so the name stands as it is.
		themes = append(themes, choice(name, kittui.TextID(name)))
	}
	var starts []kittui.Choice
	for _, a := range m.areas() {
		starts = append(starts, choice(string(a.ID), a.Title))
	}
	mouse := "on"
	if !p.MouseEnabled {
		mouse = "off"
	}
	pick := func(id string, label, hint kittui.TextID, choices []kittui.Choice, value string) kittui.FormField {
		return kittui.FormField{ID: kittui.FieldID(id), Kind: kittui.FieldChoice, Label: label, Hint: hint,
			Choices: choices, Value: kittui.FormValue{Choice: kittui.ChoiceID(value)}}
	}
	return kittui.NewForm(kittui.FormSpec{Keys: formKeys(),
		Buttons: []kittui.FormButton{{Action: actSave, Label: kittui.TextKeySave, Primary: true},
			{Action: actBack, Label: kittui.TextKeyDiscard}},
		Fields: []kittui.FormField{
			pick("theme", kittui.TextSettingTheme, textHintTheme, themes, p.Theme),
			pick("start", kittui.TextSettingStart, textHintStart, starts, p.Start),
			pick("language", kittui.TextSettingLanguage, textHintLanguage, []kittui.Choice{
				choice(uiprefs.LanguageEnglish, textLangEnglish), choice(uiprefs.LanguageGerman, textLangGerman),
				choice(uiprefs.LanguageSystem, textLangSystem)}, p.Language),
			pick("icons", kittui.TextSettingIcons, textHintIcons, []kittui.Choice{
				choice(uiprefs.IconsUnicode, textIconsUnicode), choice(uiprefs.IconsNerdFont, textIconsNerd),
				choice(uiprefs.IconsNone, kittui.TextValueNone)}, p.Icons),
			pick("mouse", kittui.TextSettingMouse, textHintMouse, []kittui.Choice{
				choice("on", kittui.TextValueOn), choice("off", kittui.TextValueOff)}, mouse),
		}}, m.keys, m.text)
}

// resetForm shows the saved preferences in the form again.
func (m *Model) resetForm() {
	if form, err := m.generalForm(); err == nil {
		m.set.form = form
	} else {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
	}
}

// formAct runs what a key or a click of the form reported; done is false where the form did not take it.
func (m *Model) formAct(act kittui.ActionID, handled bool) (cmd tea.Cmd, done bool) {
	switch {
	case act == actSave && handled:
		return m.submit(), true
	case act == actBack:
		return m.discard(), true
	}
	return nil, handled
}

// discard leaves the Settings area. Changes that are not saved are asked about first.
func (m *Model) discard() tea.Cmd {
	if !m.set.form.Changed() {
		m.leaveSettings()
		return nil
	}
	t := m.text
	m.ask(textTitleDiscard, t.Lookup(textAskDiscard), t.Lookup(textWhatDiscard), nil, kittui.TextKeyDiscard,
		func() tea.Cmd {
			m.leaveSettings()
			return nil
		})
	return nil
}

func (m *Model) leaveSettings() {
	m.resetForm()
	m.enter(areaProjects)
}

// submit writes the chosen values with the rest of the preferences unchanged. Nothing of the running state
// changes unless the write succeeded.
func (m *Model) submit() tea.Cmd {
	t, form := m.text, m.set.form
	problem := func(cause, next string) {
		form.SetProblems([]kittui.FormProblem{{Cause: cause, NextStep: next}})
	}
	if m.savePrefs == nil {
		problem(t.Lookup(textCannotSave), t.Lookup(textFormNoPlace))
		return nil
	}
	v := form.Values()
	next := m.prefs
	next.Theme, next.Start = string(v["theme"].Choice), string(v["start"].Choice)
	next.Language, next.Icons = string(v["language"].Choice), string(v["icons"].Choice)
	next.MouseEnabled = v["mouse"].Choice == "on"
	if err := m.savePrefs(next); err != nil {
		problem(t.Lookup(textSaveFailed, err.Error()), t.Lookup(textFormRetry))
		return nil
	}
	return m.applyPrefs(next)
}

// applyPrefs makes saved preferences effective at once, except the start area, which only the next start reads.
func (m *Model) applyPrefs(next uiprefs.Prefs) tea.Cmd {
	var cmds []tea.Cmd
	startChanged := next.Start != m.prefs.Start
	langChanged := uiprefs.Language(next.Language) != m.lang
	m.prefs = next
	m.icons = next.Icons
	m.mouseOn = next.MouseEnabled
	if next.Theme != m.theme {
		m.theme = next.Theme
		m.restyle()
		if m.themes.AsksTerminalColors(m.theme, m.profile) {
			cmds = append(cmds, tea.RequestForegroundColor, tea.RequestBackgroundColor)
		}
	}
	if langChanged {
		m.relocalize(uiprefs.Language(next.Language))
	}
	m.resetForm()
	m.result = m.text.Lookup(kittui.TextSettingsSaved)
	if startChanged {
		m.result = m.text.Lookup(textSavedStart)
	}
	return tea.Batch(cmds...)
}

// listMemory is the state of a list that outlives a rebuild.
type listMemory struct {
	selected kittui.ItemID
	query    string
}

func remember(l *kittui.List) listMemory {
	sel, _ := l.Selected()
	return listMemory{selected: sel, query: l.Query()}
}

// restore puts the state back once the rows are set again; what no longer exists is dropped.
func (mem listMemory) restore(l *kittui.List) {
	l.SetFilter(mem.query)
	if mem.selected != "" {
		l.Select(mem.selected)
	}
}

// relocalize switches the interface to lang. Both lists are built again with the new texts and filled from
// what was read before, keeping cursor and filter.
func (m *Model) relocalize(lang i18n.Lang) {
	text, err := kittui.NewTranslator(lang, catalog(), m.keys)
	var projects, sessions *kittui.List
	if err == nil {
		projects, sessions, err = m.newLists(text)
	}
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return
	}
	pm, sm := remember(m.projList), remember(m.sessList)
	m.lang, m.text, m.frame = lang, text, kittui.NewFrame(m.keys, text)
	m.projList, m.sessList = projects, sessions
	m.fillProjects()
	pm.restore(m.projList)
	m.fillSessions(false)
	sm.restore(m.sessList)
	m.relocalizeSetup()
}
