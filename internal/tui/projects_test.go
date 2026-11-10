package tui

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/output"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-crew/internal/client"
	"github.com/castrowithcee/qatlas-crew/internal/control"
)

func (f *fakeBackend) AddProject(_ context.Context, args control.ProjectAddArgs) (control.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, args)
	if f.addErr != nil {
		return control.Project{}, f.addErr
	}
	name := args.Name
	if name == "" {
		name = filepath.Base(args.Path)
	}
	p := control.Project{ID: "p-" + name, Name: name, Path: args.Path}
	f.projects = append(f.projects, p)
	return p, nil
}

func (f *fakeBackend) RemoveProject(_ context.Context, ref string) (control.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, ref)
	if f.removeErr != nil {
		return control.Project{}, f.removeErr
	}
	f.projects = slices.DeleteFunc(f.projects, func(p control.Project) bool { return p.ID == ref })
	return control.Project{ID: ref}, nil
}

func (f *fakeBackend) StartSession(_ context.Context, args control.SessionStartArgs) (control.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, args)
	if f.startErr != nil {
		return control.Session{}, f.startErr
	}
	s := control.Session{ID: "s-new-" + args.Tool, Project: args.Project, Tool: args.Tool, Title: args.Tool,
		Status: "running", Since: epoch}
	f.sessions = append(f.sessions, s)
	return s, nil
}

func (f *fakeBackend) calls() (added []control.ProjectAddArgs, removed []string, started []control.SessionStartArgs) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.added), slices.Clone(f.removed), slices.Clone(f.started)
}

// newHomeModel is a model whose home folder is a new temporary directory with a few folders.
func newHomeModel(t *testing.T, lang i18n.Lang, b *fakeBackend, width, height int) (*Model, string) {
	t.Helper()
	home := folderTree(t, "work/app", "work/lib", "notes")
	m := newTestModel(t, lang, b, width, height)
	m.home = home
	return m, home
}

// typeIn types text into the focused field.
func typeIn(m *Model, text string) {
	for _, r := range text {
		_, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		drain(m, cmd)
	}
	m.render()
}

func TestAddingAnExistingFolder(t *testing.T) {
	b := demoBackend()
	m, home := newHomeModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "n")
	if m.add == nil {
		t.Fatal("n opened no dialog")
	}
	contains(t, m, "Add project", "Folders in ~/", "work/", "notes/")
	typeIn(m, "work/app")
	contains(t, m, "app")
	if got := m.add.form.Values()[addNameField].Text; got != "app" {
		t.Fatalf("the name is %q, want the folder name", got)
	}
	send(m, "f2")
	added, _, _ := b.calls()
	if want := (control.ProjectAddArgs{Path: filepath.Join(home, "work", "app"), Name: "app"}); len(added) != 1 || added[0] != want {
		t.Fatalf("added %+v, want %+v", added, want)
	}
	if m.add != nil {
		t.Fatal("the dialog stays open after the project was added")
	}
	if p, ok := m.selectedProject(); !ok || p.ID != "p-app" {
		t.Fatalf("the new project is not chosen: %+v", p)
	}
	contains(t, m, "The project 'app' is added.")
}

func TestAddingANewFolderHere(t *testing.T) {
	b := demoBackend()
	m, home := newHomeModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "n")
	typeIn(m, "work/")
	send(m, "enter") // on to the fields below the picker
	if !m.add.onForm || m.add.form.Focused() != addModeField {
		t.Fatalf("enter did not reach the mode: on form %v, field %s", m.add.onForm, m.add.form.Focused())
	}
	send(m, "right")
	contains(t, m, "a new folder in it", "new folder")
	send(m, "down")
	typeIn(m, "fresh")
	send(m, "down")
	typeIn(m, "-x")
	send(m, "f2")
	added, _, _ := b.calls()
	want := control.ProjectAddArgs{Path: filepath.Join(home, "work", "fresh"), Name: "fresh-x", Create: true}
	if len(added) != 1 || added[0] != want {
		t.Fatalf("added %+v, want %+v", added, want)
	}
}

func TestAnInvalidPathStaysInTheDialog(t *testing.T) {
	b := demoBackend()
	m, _ := newHomeModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "n", "backspace", "backspace")
	typeIn(m, "rel/dir")
	send(m, "f2")
	if added, _, _ := b.calls(); len(added) != 0 {
		t.Fatalf("an invalid path reached the service: %+v", added)
	}
	if m.add == nil || m.add.picker.Text() != "rel/dir" {
		t.Fatal("the dialog or the input is gone")
	}
	contains(t, m, "The path rel/dir is not absolute.", "Type a path that starts with / or ~.")
}

func TestANewFolderThatExistsIsRefusedHere(t *testing.T) {
	b := demoBackend()
	m, _ := newHomeModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "n")
	typeIn(m, "work/")
	send(m, "enter", "right", "down")
	typeIn(m, "app")
	send(m, "f2")
	if added, _, _ := b.calls(); len(added) != 0 {
		t.Fatalf("the service was asked: %+v", added)
	}
	contains(t, m, "app exists already.", "Open it and use this folder")
}

func TestARefusalOfTheServiceStaysInTheDialog(t *testing.T) {
	b := demoBackend()
	b.addErr = &cli.Failure{Code: client.CodeAlreadyExists, Message: "a project named app\x1b[31m exists already"}
	m, _ := newHomeModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "n")
	typeIn(m, "work/app")
	send(m, "f2")
	if m.add == nil || m.add.running {
		t.Fatal("the dialog closed or still waits after a refusal")
	}
	if m.add.picker.Text() != "~/work/app" || m.add.form.Values()[addNameField].Text != "app" {
		t.Fatal("the input is lost")
	}
	contains(t, m, "exists already", "Choose another folder or another name.")
	lacks(t, m, "\x1b[31m")
	b.addErr = &cli.Failure{Code: output.CodeNotFound, Message: "no such folder"}
	send(m, "f2")
	contains(t, m, "no such folder", "Choose a folder that exists")
}

func TestEscClosesTheDialogWithoutACall(t *testing.T) {
	b := demoBackend()
	m, _ := newHomeModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "n")
	typeIn(m, "work")
	send(m, "q") // typed, not quit
	if m.add == nil || m.add.picker.Text() != "~/workq" {
		t.Fatal("q left the dialog")
	}
	send(m, "esc")
	if m.add != nil {
		t.Fatal("esc did not close the dialog")
	}
	if added, _, _ := b.calls(); len(added) != 0 {
		t.Fatalf("calls %+v", added)
	}
}

func TestAClickOnAFolderOpensIt(t *testing.T) {
	b := demoBackend()
	m, _ := newHomeModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "n")
	x, y := cellOf(t, m, "work/")
	clickAt(m, x, y)
	if !m.add.picker.onList {
		t.Fatal("the click did not focus the folders")
	}
	clickAt(m, x, y)
	if got := m.add.picker.Text(); got != "~/work/" {
		t.Fatalf("a second click opened %q", got)
	}
	x, y = cellOf(t, m, "[ esc cancel ]")
	clickAt(m, x, y)
	if m.add != nil {
		t.Fatal("the cancel button did not close the dialog")
	}
}

func TestTheAddButtonOpensTheDialog(t *testing.T) {
	m, _ := newHomeModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	x, y := cellOf(t, m, "[ + project ]")
	clickAt(m, x, y)
	if m.add == nil {
		t.Fatal("the button opened no dialog")
	}
	x, y = cellOf(t, m, "[ F2 add ]")
	clickAt(m, x, y)
	if added, _, _ := m.conn.(*fakeBackend).calls(); len(added) != 1 {
		t.Fatalf("the add button did not add: %+v", added)
	}
}

func TestRemovingAProjectAsksFirst(t *testing.T) {
	b := demoBackend()
	b.projects[0].Path = "/srv/alpha"
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "d")
	if m.modal == nil {
		t.Fatal("d opened no question")
	}
	contains(t, m, "Remove the project 'alpha' from qcrew?", "path: /srv/alpha", "stay as they are")
	send(m, "enter")
	send(m, "d", "n")
	if _, removed, _ := b.calls(); len(removed) != 0 {
		t.Fatalf("removed before a yes: %v", removed)
	}
	x, y := cellOf(t, m, "[ remove ]")
	clickAt(m, x, y)
	x, y = cellOf(t, m, "y remove")
	clickAt(m, x, y)
	if _, removed, _ := b.calls(); !slices.Equal(removed, []string{"p-alpha"}) {
		t.Fatalf("removed %v", removed)
	}
	contains(t, m, "The project 'alpha' is removed")
	lacks(t, m, "refactor parser")
}

func TestRemovingAProjectInUseNamesTheNextStep(t *testing.T) {
	b := demoBackend()
	b.removeErr = &cli.Failure{Code: client.CodeInUse, Message: "the project has running sessions"}
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "d", "y")
	if !strings.Contains(m.failure, "the project has running sessions") ||
		!strings.Contains(m.failure, "stop its sessions with e in the Sessions area") {
		t.Fatalf("failure = %q", m.failure)
	}
	if _, ok := m.projByID["p-alpha"]; !ok {
		t.Fatal("the project left the list")
	}
}

func TestAToolKeyStartsAndChoosesTheSession(t *testing.T) {
	for key, tool := range map[string]string{"c": "claude", "x": "codex", "s": "shell"} {
		b := demoBackend()
		m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
		send(m, key)
		_, _, started := b.calls()
		if want := (control.SessionStartArgs{Project: "p-alpha", Tool: tool}); len(started) != 1 || started[0] != want {
			t.Fatalf("%s started %+v, want %+v", key, started, want)
		}
		if id, _ := m.sessList.Selected(); m.area != areaSessions || id != kittui.ItemID("s-new-"+tool) {
			t.Fatalf("%s: area %s, selected %q", key, m.area, id)
		}
	}
}

func TestAToolButtonStartsTheSession(t *testing.T) {
	b := demoBackend()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	x, y := cellOf(t, m, "[ codex ]")
	clickAt(m, x, y)
	if _, _, started := b.calls(); len(started) != 1 || started[0].Tool != "codex" {
		t.Fatalf("started %+v", started)
	}
	if id, _ := m.sessList.Selected(); id != "s-new-codex" {
		t.Fatalf("selected %q", id)
	}
}

// startModel is a model that ran its start, which checks the programs once.
func startModel(t *testing.T, b *fakeBackend) *Model {
	t.Helper()
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	drain(m, m.Init())
	m.render()
	return m
}

func TestTheCheckAtStartDisablesTheMissingTools(t *testing.T) {
	b := demoBackend() // claude and codex are missing, tmux is there
	m := startModel(t, b)
	if b.setupReads != 1 {
		t.Fatalf("checks at start = %d, want 1", b.setupReads)
	}
	contains(t, m, "[ claude: setup ]", "[ codex: setup ]", "[ shell ]")
	send(m, "c")
	x, y := cellOf(t, m, "[ codex: setup ]")
	clickAt(m, x, y)
	if _, _, started := b.calls(); len(started) != 0 {
		t.Fatalf("a missing tool reached the service: %+v", started)
	}
	contains(t, m, "codex is not installed. Next step: install it in the Setup area or with 'qcrew setup'.")
	send(m, "s")
	if _, _, started := b.calls(); len(started) != 1 || started[0].Tool != "shell" {
		t.Fatalf("the shell did not start: %+v", started)
	}
	// The periodic read does not check again; reading again with r does.
	m.tick()
	timersOf[m].fire(t, m)
	if b.setupReads != 1 {
		t.Fatalf("the periodic read checked the programs: %d", b.setupReads)
	}
	b.markInstalled("claude")
	b.markInstalled("codex")
	send(m, "r")
	if b.setupReads != 2 {
		t.Fatalf("reload did not check again: %d", b.setupReads)
	}
	lacks(t, m, ": setup ]")
	send(m, "1", "c")
	if _, _, started := b.calls(); len(started) != 2 || started[1].Tool != "claude" {
		t.Fatalf("an installed tool did not start: %+v", started)
	}
}

func TestToolsStayActiveWithoutACheck(t *testing.T) {
	b := demoBackend()
	b.setupErr = &cli.Failure{Code: "unknown-operation", Message: "unknown operation"}
	m := startModel(t, b)
	contains(t, m, "[ claude ]", "[ codex ]")
	send(m, "c")
	if _, _, started := b.calls(); len(started) != 1 {
		t.Fatalf("a failed check disabled the tool: %+v", started)
	}
}

func TestAFailedStartMarksTheToolMissing(t *testing.T) {
	b := demoBackend()
	for _, name := range []string{"claude", "codex"} {
		b.markInstalled(name)
	}
	b.startErr = &cli.Failure{Code: client.CodeToolMissing, Message: "the tool codex is not installed or not in PATH: codex"}
	m := startModel(t, b)
	send(m, "x")
	contains(t, m, "not installed", "qcrew setup", "[ codex: setup ]")
	send(m, "x")
	if _, _, started := b.calls(); len(started) != 1 {
		t.Fatalf("a missing tool was asked for again: %+v", started)
	}
	contains(t, m, "codex is not installed. Next step: install it in the Setup area")
	b.startErr = nil
	send(m, "r", "x")
	if _, _, started := b.calls(); len(started) != 2 {
		t.Fatalf("a new check did not make the tool active again: %+v", started)
	}
	lacks(t, m, "codex: setup")
}

func TestStartingNeedsAProject(t *testing.T) {
	b := &fakeBackend{}
	m := newTestModel(t, i18n.English, b, wideWidth, wideHeight)
	send(m, "c")
	if _, _, started := b.calls(); len(started) != 0 {
		t.Fatalf("started %+v", started)
	}
	contains(t, m, "Choose a project first.", "Add one with n.")
}

// The new buttons, the fields of the add dialog and its folders take clicks like every other element.
func TestTheProjectElementsHaveClickTargets(t *testing.T) {
	for lang, l := range map[string]i18n.Lang{"en": i18n.English, "de": i18n.German} {
		for size, wh := range map[string][2]int{"narrow": {60, 30}, "wide": {120, 40}} {
			t.Run(lang+"."+size, func(t *testing.T) {
				m, _ := newHomeModel(t, l, demoBackend(), wh[0], wh[1])
				tx := m.text
				for _, b := range []struct {
					label string
					act   kittui.ActionID
				}{{tx.Lookup(textBtnAdd), actAdd}, {tx.Lookup(textBtnRemove), actRemove}, {"claude", actClaude},
					{"codex", actCodex}, {"shell", actShell}} {
					if !wantTarget(t, m, "button", "[ "+b.label+" ]", 3, isAction(b.act)) {
						t.Errorf("button %q is not drawn:\n%s", b.label, m.render())
					}
				}
				send(m, "n")
				screen := m.render()
				for _, label := range []kittui.TextID{textPickFolder, textAddUse, textAddName} {
					if !wantTarget(t, m, "field", tx.Lookup(label), 3, func(r kittui.MouseResult) bool { return r.Item != "" }) {
						t.Errorf("field %q is not drawn:\n%s", label, screen)
					}
				}
				for _, row := range []string{"work/", "notes/", ".."} {
					if !wantTarget(t, m, "folder", row, 3, func(r kittui.MouseResult) bool { return r.Item != "" }) {
						t.Errorf("folder %q is not drawn:\n%s", row, screen)
					}
				}
				if n := hintTargets(t, m, m.footer(), wh[1]-3); n < 2 {
					t.Errorf("%d footer keys drawn, want at least 2:\n%s", n, screen)
				}
			})
		}
	}
}

func TestForeignFolderNamesAreMasked(t *testing.T) {
	m, home := newHomeModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	if err := os.Mkdir(filepath.Join(home, "evil\x1b[31mred"), 0o755); err != nil {
		t.Skip("the file system takes no control character in a name:", err)
	}
	send(m, "n")
	contains(t, m, `evil\x1b[31mred/`)
	lacks(t, m, "\x1b")
	send(m, "tab", "tab") // into the list; the name cannot be typed, so it is not opened
	if strings.ContainsRune(m.add.picker.Text(), '\x1b') {
		t.Fatalf("a control character reached the path: %q", m.add.picker.Text())
	}
}
