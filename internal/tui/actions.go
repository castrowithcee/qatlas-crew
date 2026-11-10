package tui

import (
	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// modal is the one question that takes every key.
type modal struct {
	dialog *kittui.Dialog
	// yes runs when the person confirms; nothing else reaches the service.
	yes func() tea.Cmd
}

// ask opens a confirmation. The destroying answer is never the default, so enter and every stray key cancel.
func (m *Model) ask(title kittui.TextID, question, consequence string, details []kittui.DialogLine,
	confirm kittui.TextID, yes func() tea.Cmd) {
	dialog, err := kittui.NewDialog(kittui.DialogSpec{Title: title, Question: question, Consequence: consequence,
		Details: details, Irreversible: true,
		Answers: []kittui.Answer{{Action: actYes, Label: confirm}, {Action: actNo, Label: textAnswerKeep}},
		Default: actNo, Danger: actYes, Keys: kittui.DialogKeys{Enter: actEnter}}, m.keys, m.text)
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, err.Error())
		return
	}
	m.modal = &modal{dialog: dialog, yes: yes}
}

func (m *Model) warn(text string) {
	m.notice = m.text.Lookup(kittui.TextWarning, text)
}

func (m *Model) modalKey(msg tea.KeyPressMsg) tea.Cmd {
	if answer, handled := m.modal.dialog.Update(msg); handled {
		return m.answer(answer)
	}
	if id, ok := m.keys.Match(msg, kittui.InputNavigation); ok && id == actForceQuit {
		return tea.Quit
	}
	return nil
}

// answer closes the question and runs what a yes stands for.
func (m *Model) answer(answer kittui.ActionID) tea.Cmd {
	md := m.modal
	m.modal = nil
	if answer == actYes && md != nil && md.yes != nil {
		return md.yes()
	}
	return nil
}

// beginStop asks to stop the session under the cursor. Nothing reaches the service before the answer.
func (m *Model) beginStop() tea.Cmd {
	if m.area != areaSessions {
		return nil
	}
	if m.stopping {
		m.warn(m.text.Lookup(textStopBusy))
		return nil
	}
	id, _ := m.sessList.Selected()
	s, ok := m.sessByID[id]
	if !ok {
		m.warn(m.text.Lookup(textStopNothing))
		return nil
	}
	name := s.Title
	if name == "" {
		name = s.Tool
	}
	t := m.text
	m.ask(textTitleStop, t.Lookup(textAskStop, name), t.Lookup(textWhatStop),
		[]kittui.DialogLine{{Text: t.Lookup(textDetailTool, s.Tool), Style: kittui.StyleHint}},
		textAnswerStop, func() tea.Cmd { return m.stop(s.ID) })
	return nil
}

// stop asks the service to stop the session.
func (m *Model) stop(id string) tea.Cmd {
	m.stopping = true
	ctx, conn := m.ctx, m.conn
	return func() tea.Msg {
		if conn == nil {
			return stopMsg{err: errNoConnection}
		}
		_, err := conn.StopSession(ctx, id)
		return stopMsg{err: err}
	}
}

// applyStop shows the outcome of a stop and reads the sessions again.
func (m *Model) applyStop(msg stopMsg) tea.Cmd {
	m.stopping = false
	if msg.err != nil {
		m.failure = m.errorText(msg.err)
		m.dropConnection(msg.err)
		return nil
	}
	m.result = m.text.Lookup(textResultStop)
	return m.reload()
}
