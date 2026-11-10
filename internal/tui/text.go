package tui

import (
	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The texts of the interface. Command names and the terms project, session and inbox stay as they are in
// the command line.
const (
	textAreaProjects kittui.TextID = "area.projects"
	textAreaInbox    kittui.TextID = "area.inbox"
	textAreaSessions kittui.TextID = "area.sessions"

	textKeyArea     kittui.TextID = "key.area"
	textKeyZone     kittui.TextID = "key.zone"
	textKeyPrevArea kittui.TextID = "key.prevarea"
	textKeyNextArea kittui.TextID = "key.nextarea"
	textKeyFull     kittui.TextID = "key.full"
	textKeyColumns  kittui.TextID = "key.columns"
	textKeyStop     kittui.TextID = "key.stop"
	textKeySessions kittui.TextID = "key.sessions"
	textFilterKeep  kittui.TextID = "filter.keep"
	textFilterClear kittui.TextID = "filter.clear"

	textPaneProjects kittui.TextID = "pane.projects"
	textPaneSessions kittui.TextID = "pane.sessions"
	textPaneInbox    kittui.TextID = "pane.inbox"

	textColProject kittui.TextID = "col.project"
	textColInbox   kittui.TextID = "col.inbox"
	textColTool    kittui.TextID = "col.tool"
	textColTitle   kittui.TextID = "col.title"
	textColStatus  kittui.TextID = "col.status"
	textColSince   kittui.TextID = "col.since"

	textStatusRunning kittui.TextID = "status.running"
	textStatusWaiting kittui.TextID = "status.waiting"
	textStatusIdle    kittui.TextID = "status.idle"
	textStatusError   kittui.TextID = "status.error"
	textStatusExited  kittui.TextID = "status.exited"

	textProjectsEmpty   kittui.TextID = "projects.empty"
	textProjectsLoading kittui.TextID = "projects.loading"
	textSessionsEmpty   kittui.TextID = "sessions.empty"
	textSessionsNoProj  kittui.TextID = "sessions.noproject"
	textInboxSoon       kittui.TextID = "inbox.soon"
	textMissingProgram  kittui.TextID = "missing.program"

	textErrChannel kittui.TextID = "err.channel"
	textErrService kittui.TextID = "err.service"
	textErrNoConn  kittui.TextID = "err.noconn"
	textErrRows    kittui.TextID = "err.rows"

	textTitleStop   kittui.TextID = "title.stop"
	textAskStop     kittui.TextID = "ask.stop"
	textWhatStop    kittui.TextID = "what.stop"
	textDetailTool  kittui.TextID = "detail.tool"
	textAnswerStop  kittui.TextID = "answer.stop"
	textAnswerKeep  kittui.TextID = "answer.keep"
	textResultStop  kittui.TextID = "result.stopped"
	textStopBusy    kittui.TextID = "stop.busy"
	textStopNothing kittui.TextID = "stop.nothing"

	textTitleDiscard kittui.TextID = "title.discard"
	textAskDiscard   kittui.TextID = "ask.discard"
	textWhatDiscard  kittui.TextID = "what.discard"

	textHintTheme    kittui.TextID = "hint.theme"
	textHintStart    kittui.TextID = "hint.start"
	textHintIcons    kittui.TextID = "hint.icons"
	textHintLanguage kittui.TextID = "hint.language"
	textHintMouse    kittui.TextID = "hint.mouse"
	textLangEnglish  kittui.TextID = "lang.english"
	textLangGerman   kittui.TextID = "lang.german"
	textLangSystem   kittui.TextID = "lang.system"
	textIconsUnicode kittui.TextID = "icons.unicode"
	textIconsNerd    kittui.TextID = "icons.nerdfont"

	textSavedStart  kittui.TextID = "settings.savedstart"
	textCannotSave  kittui.TextID = "settings.cannotsave"
	textSaveFailed  kittui.TextID = "settings.savefailed"
	textFormRetry   kittui.TextID = "settings.retry"
	textFormNoPlace kittui.TextID = "settings.noplace"
)

func catalog() i18n.Catalogs[kittui.TextID] {
	return withProjectTexts(i18n.Catalogs[kittui.TextID]{
		i18n.English: {
			textAreaProjects: "Projects",
			textAreaInbox:    "Inbox",
			textAreaSessions: "Sessions",

			textKeyArea:     "area",
			textKeyZone:     "zone",
			textKeyPrevArea: "previous area",
			textKeyNextArea: "next area",
			textKeyFull:     "full screen",
			textKeyColumns:  "columns",
			textKeyStop:     "stop session",
			textKeySessions: "sessions",
			textFilterKeep:  "keep filter",
			textFilterClear: "clear filter",

			textPaneProjects: "Projects",
			textPaneSessions: "Sessions of %s",
			textPaneInbox:    "Inbox of %s",

			textColProject: "Project",
			textColInbox:   "Inbox",
			textColTool:    "Tool",
			textColTitle:   "Title",
			textColStatus:  "Status",
			textColSince:   "Since",

			textStatusRunning: "running",
			textStatusWaiting: "waiting",
			textStatusIdle:    "idle",
			textStatusError:   "error",
			textStatusExited:  "exited",

			textProjectsEmpty:   "No project yet. Add one with {add}.",
			textProjectsLoading: "Reading the projects ...",
			textSessionsEmpty:   "No session in this project. Start one with {claude}, {codex} or {shell}.",
			textSessionsNoProj:  "Choose a project first.",
			textInboxSoon:       "The inbox of this project follows in a later release. Open entries: %d.",
			textMissingProgram:  "%s Next step: run 'qcrew setup' to install what qcrew needs.",

			textErrChannel: "The qcrew service does not answer: %s",
			textErrService: "%s %s",
			textErrNoConn:  "There is no connection to the qcrew service yet.",
			textErrRows:    "The list cannot show what the service sent: %s",

			textTitleStop:   "Stop session",
			textAskStop:     "Stop the session '%s'?",
			textWhatStop:    "The agent program ends and its window closes. Unsaved work in the session is lost. This cannot be undone.",
			textDetailTool:  "tool: %s",
			textAnswerStop:  "stop",
			textAnswerKeep:  "cancel",
			textResultStop:  "The session is stopped.",
			textStopBusy:    "A session is being stopped; wait for it.",
			textStopNothing: "There is no session to stop here.",

			textTitleDiscard: "Discard changes",
			textAskDiscard:   "Discard the changes to the settings?",
			textWhatDiscard:  "What you changed here is not saved.",

			textHintTheme:    "left and right choose the colour theme",
			textHintStart:    "the area shown at the next start",
			textHintIcons:    "symbols before the area names; none keeps plain text",
			textHintLanguage: "the language of this interface",
			textHintMouse:    "clicking works like the keys; hold Shift to select text while it is on",
			textLangEnglish:  "English",
			textLangGerman:   "Deutsch",
			textLangSystem:   "system language",
			textIconsUnicode: "Unicode symbols",
			textIconsNerd:    "Nerd Font symbols",

			textSavedStart:  "Saved on this device. The start area applies at the next start.",
			textCannotSave:  "This device has no place for the preferences.",
			textSaveFailed:  "The preferences could not be written: %s",
			textFormRetry:   "Check the folder and save again.",
			textFormNoPlace: "Set HOME so that qcrew can keep the preferences.",
		},
		i18n.German: {
			textAreaProjects: "Projekte",
			textAreaInbox:    "Postfach",
			textAreaSessions: "Sessions",

			textKeyArea:     "Bereich",
			textKeyZone:     "Zone",
			textKeyPrevArea: "voriger Bereich",
			textKeyNextArea: "nächster Bereich",
			textKeyFull:     "Vollbild",
			textKeyColumns:  "Spalten",
			textKeyStop:     "Session stoppen",
			textKeySessions: "Sessions",
			textFilterKeep:  "Filter behalten",
			textFilterClear: "Filter löschen",

			textPaneProjects: "Projekte",
			textPaneSessions: "Sessions von %s",
			textPaneInbox:    "Postfach von %s",

			textColProject: "Projekt",
			textColInbox:   "Postfach",
			textColTool:    "Tool",
			textColTitle:   "Titel",
			textColStatus:  "Status",
			textColSince:   "Seit",

			textStatusRunning: "läuft",
			textStatusWaiting: "wartet",
			textStatusIdle:    "untätig",
			textStatusError:   "Fehler",
			textStatusExited:  "beendet",

			textProjectsEmpty:   "Noch kein Projekt. Lege eines mit {add} an.",
			textProjectsLoading: "Lese die Projekte ...",
			textSessionsEmpty:   "Keine Session in diesem Projekt. Starte eine mit {claude}, {codex} oder {shell}.",
			textSessionsNoProj:  "Wähle zuerst ein Projekt.",
			textInboxSoon:       "Das Postfach dieses Projekts folgt in einer späteren Version. Offene Einträge: %d.",
			textMissingProgram:  "%s Nächster Schritt: 'qcrew setup' ausführen, um zu installieren, was qcrew braucht.",

			textErrChannel: "Der qcrew-Dienst antwortet nicht: %s",
			textErrService: "%s %s",
			textErrNoConn:  "Es besteht noch keine Verbindung zum qcrew-Dienst.",
			textErrRows:    "Die Liste kann nicht zeigen, was der Dienst geschickt hat: %s",

			textTitleStop:   "Session stoppen",
			textAskStop:     "Die Session '%s' stoppen?",
			textWhatStop:    "Das Agent-Programm endet und sein Fenster schließt sich. Nicht gesicherte Arbeit in der Session geht verloren. Das lässt sich nicht rückgängig machen.",
			textDetailTool:  "Tool: %s",
			textAnswerStop:  "stoppen",
			textAnswerKeep:  "abbrechen",
			textResultStop:  "Die Session ist gestoppt.",
			textStopBusy:    "Eine Session wird gerade gestoppt; warte darauf.",
			textStopNothing: "Hier gibt es keine Session zum Stoppen.",

			textTitleDiscard: "Änderungen verwerfen",
			textAskDiscard:   "Die Änderungen an den Einstellungen verwerfen?",
			textWhatDiscard:  "Was du hier geändert hast, wird nicht gespeichert.",

			textHintTheme:    "links und rechts wählen das Farbschema",
			textHintStart:    "der Bereich, der beim nächsten Start erscheint",
			textHintIcons:    "Symbole vor den Bereichsnamen; keine lässt nur Text",
			textHintLanguage: "die Sprache dieser Oberfläche",
			textHintMouse:    "Klicken wirkt wie die Tasten; mit Umschalt Text markieren, solange sie an ist",
			textLangEnglish:  "English",
			textLangGerman:   "Deutsch",
			textLangSystem:   "Systemsprache",
			textIconsUnicode: "Unicode-Symbole",
			textIconsNerd:    "Nerd-Font-Symbole",

			textSavedStart:  "Auf diesem Gerät gespeichert. Der Startbereich gilt ab dem nächsten Start.",
			textCannotSave:  "Dieses Gerät hat keinen Ort für die Einstellungen.",
			textSaveFailed:  "Die Einstellungen ließen sich nicht schreiben: %s",
			textFormRetry:   "Prüfe den Ordner und speichere erneut.",
			textFormNoPlace: "Setze HOME, damit qcrew die Einstellungen ablegen kann.",
		},
	})
}
