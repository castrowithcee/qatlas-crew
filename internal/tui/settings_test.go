package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

func newSettingsModel(t *testing.T) (*Model, *testPrefs) {
	t.Helper()
	m := newTestModel(t, i18n.English, demoBackend(), wideWidth, wideHeight)
	p := &testPrefs{}
	m.savePrefs = p.save
	send(m, "4")
	return m, p
}

func TestSettingsListTheFiveChoices(t *testing.T) {
	m, _ := newSettingsModel(t)
	contains(t, m, "theme", "start view", "language", "icons", "mouse", "English", "none", "on")
}

func TestSavingTheLanguageSwitchesAtOnce(t *testing.T) {
	m, p := newSettingsModel(t)
	send(m, "down", "down", "right", "f2")
	if len(p.saved) != 1 || p.saved[0].Language != uiprefs.LanguageGerman {
		t.Fatalf("saved %+v", p.saved)
	}
	contains(t, m, "Auf diesem Gerät gespeichert.", "Sprache", "Einstellungen")
	send(m, "1")
	contains(t, m, "Projekte", "SEIT", "läuft")
	if id, _ := m.projList.Selected(); id != "p-alpha" {
		t.Fatalf("the cursor moved to %q", id)
	}
}

func TestSavingKeepsWhatTheFormDoesNotHold(t *testing.T) {
	m, p := newSettingsModel(t)
	m.prefs.Keys = map[kittui.ActionID]string{actHelp: "f1"}
	send(m, "right", "f2") // theme
	if len(p.saved) != 1 || p.saved[0].Theme == uiprefs.Default(mustSchema(t)).Theme {
		t.Fatalf("saved %+v", p.saved)
	}
	if p.saved[0].Keys[actHelp] != "f1" {
		t.Fatalf("the key bindings were lost: %+v", p.saved[0].Keys)
	}
	if m.theme != p.saved[0].Theme {
		t.Fatalf("the theme %q is not in force", m.theme)
	}
}

func mustSchema(t *testing.T) uiprefs.Schema {
	t.Helper()
	s, err := PrefsSchema()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSavingTheStartAreaSaysItAppliesLater(t *testing.T) {
	m, p := newSettingsModel(t)
	send(m, "down", "right", "f2")
	if len(p.saved) != 1 || p.saved[0].Start != string(areaInbox) {
		t.Fatalf("saved %+v", p.saved)
	}
	contains(t, m, "applies at the next start")
	if m.area != areaSettings {
		t.Fatalf("area %s", m.area)
	}
}

func TestIconsAndMouseApplyAtOnce(t *testing.T) {
	m, p := newSettingsModel(t)
	send(m, "down", "down", "down", "left", "down", "right", "f2")
	if len(p.saved) != 1 || p.saved[0].Icons != uiprefs.IconsNerdFont || p.saved[0].MouseEnabled {
		t.Fatalf("saved %+v", p.saved)
	}
	if m.icons != uiprefs.IconsNerdFont || m.mouseOn || m.View().MouseMode != tea.MouseModeNone {
		t.Fatalf("icons %q mouse %v", m.icons, m.mouseOn)
	}
	if m.areas()[0].Icon == "" {
		t.Fatal("the area has no icon")
	}
}

func TestSaveFailureStaysInTheForm(t *testing.T) {
	m, p := newSettingsModel(t)
	p.err = errors.New("disk full")
	send(m, "right", "f2")
	contains(t, m, "could not be written: disk full")
	if m.area != areaSettings || m.theme != "terminal" || m.prefs.Theme != "terminal" {
		t.Fatalf("area %s theme %q", m.area, m.theme)
	}
}

func TestNoPlaceForPreferencesIsSaid(t *testing.T) {
	m, _ := newSettingsModel(t)
	m.savePrefs = nil
	send(m, "right", "f2")
	contains(t, m, "no place for the preferences")
}

func TestLeavingSettingsAsksOnlyAfterAChange(t *testing.T) {
	m, p := newSettingsModel(t)
	send(m, "esc")
	if m.area != areaProjects || m.modal != nil {
		t.Fatalf("area %s modal %v", m.area, m.modal)
	}
	send(m, "4", "right", "esc")
	if m.modal == nil || m.area != areaSettings {
		t.Fatal("no question about the change")
	}
	contains(t, m, "Discard the changes")
	send(m, "n")
	if m.area != areaSettings {
		t.Fatal("cancel left the area")
	}
	send(m, "esc", "y")
	if m.area != areaProjects || len(p.saved) != 0 {
		t.Fatalf("area %s saved %v", m.area, p.saved)
	}
	send(m, "4")
	if m.set.form.Changed() {
		t.Fatal("the discarded change is still in the form")
	}
}

func TestSettingsKeysQuitAndHelp(t *testing.T) {
	m, _ := newSettingsModel(t)
	send(m, "?")
	if !m.helpOpen {
		t.Fatal("? opens no help")
	}
	send(m, "esc")
	if !isQuit(press(m, "q")) {
		t.Fatal("q does not quit in the settings")
	}
}

func TestSettingsClicksChooseAndSave(t *testing.T) {
	m, p := newSettingsModel(t)
	x, y := cellOf(t, m, "save")
	clickAt(m, x, y)
	if len(p.saved) != 1 {
		t.Fatalf("a click on save wrote %d times", len(p.saved))
	}
}
