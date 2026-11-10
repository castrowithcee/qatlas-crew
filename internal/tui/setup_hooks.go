package tui

import (
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-crew/internal/control"
)

// previewDetailLines is how many changed lines of a preview the dialog shows; the command line shows all of
// them before it writes.
const previewDetailLines = 8

// hooksPreviewMsg is the change of the Codex hooks the service previewed.
type hooksPreviewMsg struct {
	remove bool
	res    control.SetupIntegrationResult
	err    error
}

// hooksDoneMsg reports that the terminal came back from a change of the Codex hooks.
type hooksDoneMsg struct{ err error }

// hooksAction tells what the hooks key does for a program: set up or renew its hooks, or remove them; ok is
// false for a program without hooks or whose state is unknown.
func hooksAction(d control.SetupDependency) (remove, ok bool) {
	switch d.Integration {
	case control.IntegrationMissing, control.IntegrationStale:
		return false, true
	case control.IntegrationConfigured:
		return true, true
	}
	return false, false
}

// integrationWord is the state of the hooks of a program in words.
func (m *Model) integrationWord(state string) string {
	switch state {
	case control.IntegrationNotNeeded:
		return m.text.Lookup(textIntegNotNeeded)
	case control.IntegrationConfigured:
		return m.text.Lookup(textIntegConfigured)
	case control.IntegrationMissing:
		return m.text.Lookup(textIntegMissing)
	case control.IntegrationStale:
		return m.text.Lookup(textIntegStale)
	case "":
		return ""
	}
	return m.text.Lookup(textIntegUnknown)
}

// integrationCell is the state of the hooks of a program with the button that changes them.
func (m *Model) integrationCell(d control.SetupDependency) string {
	word := m.integrationWord(d.Integration)
	if remove, ok := hooksAction(d); ok {
		button := textIntegConfigure
		if remove {
			button = textIntegRemove
		}
		return word + " " + m.text.Lookup(button)
	}
	return word
}

// openSetupRow is enter or a second click on a row: it installs a program that can be installed, and changes
// the hooks of one that is there.
func (m *Model) openSetupRow() tea.Cmd {
	id, _ := m.setup.list.Selected()
	if d, ok := m.setup.byID[id]; ok && (d.Installed || d.Install == "") && d.Integration != "" &&
		d.Integration != control.IntegrationNotNeeded {
		return m.beginHooks()
	}
	return m.beginInstall()
}

// beginHooks asks the service for the change to the Codex hooks of the program under the cursor; the answer
// opens the question. Nothing is written by the interface.
func (m *Model) beginHooks() tea.Cmd {
	if m.area != areaSetup {
		return nil
	}
	s, t := m.setup, m.text
	if s.installing != "" || s.hooking || s.previewing {
		m.warn(t.Lookup(textHooksBusy))
		return nil
	}
	id, _ := s.list.Selected()
	d, ok := s.byID[id]
	if ok && d.Integration == control.IntegrationUnknown {
		m.warn(t.Lookup(textHooksUnknown, strings.Join(d.IntegrationProblems, "; ")))
		return nil
	}
	remove, can := hooksAction(d)
	if !ok || !can {
		m.warn(t.Lookup(textHooksNothing))
		return nil
	}
	s.previewing = true
	ctx, conn := m.ctx, m.conn
	return func() tea.Msg {
		if conn == nil {
			return hooksPreviewMsg{remove: remove, err: errNoConnection}
		}
		res, err := conn.SetupIntegration(ctx, remove)
		return hooksPreviewMsg{remove: remove, res: res, err: err}
	}
}

// applyHooksPreview opens the question with the change, or says that there is none.
func (m *Model) applyHooksPreview(msg hooksPreviewMsg) tea.Cmd {
	s, t := m.setup, m.text
	s.previewing = false
	if msg.err != nil {
		m.failure = m.errorText(msg.err)
		m.dropConnection(msg.err)
		return nil
	}
	if msg.res.Empty {
		m.warn(t.Lookup(textHooksNoChange))
		return m.checkSetup()
	}
	question, what, answer := textAskHooks, textWhatHooks, textAnswerConfigure
	if msg.remove {
		question, what, answer = textAskHooksRemove, textWhatHooksRemove, textAnswerRemove
	}
	dialog, err := kittui.NewDialog(kittui.DialogSpec{Title: textTitleHooks, Question: t.Lookup(question),
		Consequence: t.Lookup(what), Details: m.previewDetails(msg.res),
		Answers: []kittui.Answer{{Action: actYes, Label: answer}, {Action: actNo, Label: textAnswerKeep}},
		Default: actNo, Keys: kittui.DialogKeys{Enter: actEnter}}, m.keys, t)
	if err != nil {
		m.failure = t.Lookup(kittui.TextError, err.Error())
		return nil
	}
	remove := msg.remove
	m.modal = &modal{dialog: dialog, yes: func() tea.Cmd { return m.runHooks(remove) }}
	return nil
}

// previewDetails are the lines of a preview the dialog shows: each changed file, the first added and removed
// lines, how many more there are, and the warnings.
func (m *Model) previewDetails(res control.SetupIntegrationResult) []kittui.DialogLine {
	var out []kittui.DialogLine
	shown, more := 0, 0
	from := ""
	for _, l := range strings.Split(res.Preview, "\n") {
		switch {
		case strings.HasPrefix(l, "--- "):
			from = strings.TrimPrefix(l, "--- ")
		case strings.HasPrefix(l, "+++ "):
			name := strings.TrimPrefix(l, "+++ ")
			if name == "/dev/null" {
				name = from
			}
			out = append(out, kittui.DialogLine{Text: name, Style: kittui.StyleHint})
		case strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-"):
			if shown < previewDetailLines {
				out = append(out, kittui.DialogLine{Text: l})
				shown++
			} else {
				more++
			}
		}
	}
	if more > 0 {
		out = append(out, kittui.DialogLine{Text: m.text.Lookup(textDetailMore, more), Style: kittui.StyleHint})
	}
	for _, w := range res.Warnings {
		out = append(out, kittui.DialogLine{Text: w, Style: kittui.StyleWarn})
	}
	return out
}

// runHooks hands the terminal to 'qcrew setup codex-hooks', which shows the change again and asks before it
// writes, and takes it back when that ends.
func (m *Model) runHooks(remove bool) tea.Cmd {
	s := m.setup
	exe, err := s.executable()
	if err != nil {
		m.failure = m.text.Lookup(kittui.TextError, m.text.Lookup(textErrExec, err.Error()))
		return nil
	}
	args := []string{"setup", "codex-hooks"}
	if remove {
		args = append(args, "--remove")
	}
	s.hooking = true
	return s.execProcess(exec.Command(exe, args...), func(err error) tea.Msg { return hooksDoneMsg{err: err} })
}

// hooksDone notes how the change ended and checks again.
func (m *Model) hooksDone(msg hooksDoneMsg) tea.Cmd {
	s := m.setup
	s.hooking = false
	res := exitResult("codex", msg.err)
	s.pendingHooks = &res
	return m.checkSetup()
}

// logHooks adds the result of a change of the Codex hooks to the log: how the command ended, the state of
// the hooks now, and the next step.
func (m *Model) logHooks(res installResult, deps []control.SetupDependency, checkErr error) {
	t := m.text
	var lines []string
	if res.code < 0 {
		lines = append(lines, t.Lookup(textLogHooksNoStart, res.err.Error()))
	} else {
		lines = append(lines, t.Lookup(textLogHooksExit, res.code))
	}
	switch {
	case checkErr != nil:
		lines = append(lines, t.Lookup(textLogHooksNoCheck, checkErr.Error()), t.Lookup(textNextCheck))
	default:
		for _, d := range deps {
			if d.Name == res.name {
				lines = append(lines, t.Lookup(textLogHooksState, m.integrationWord(d.Integration)))
			}
		}
		if res.code != 0 {
			lines = append(lines, t.Lookup(textNextHooksFailed))
		}
	}
	m.appendLog(lines)
}
