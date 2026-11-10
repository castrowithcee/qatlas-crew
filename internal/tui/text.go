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

	textViewTitle   kittui.TextID = "view.title"
	textViewTitleOf kittui.TextID = "view.titleof"
	textViewNone    kittui.TextID = "view.none"
	textViewEnded   kittui.TextID = "view.ended"
	textViewFailed  kittui.TextID = "view.failed"
	textViewStatus  kittui.TextID = "view.status"
	textViewContext kittui.TextID = "view.context"

	// The keys of the focused session view: the help text of the exit action, the footer line that names its
	// keys through the placeholder, and the label of enter.
	textKeyViewExit  kittui.TextID = "key.viewexit"
	textKeyViewFocus kittui.TextID = "key.viewfocus"
	textKeyControl   kittui.TextID = "key.control"

	// The Setup area.
	textAreaSetup      kittui.TextID = "area.setup"
	textKeyInstall     kittui.TextID = "key.install"
	textPaneSetup      kittui.TextID = "pane.setup"
	textPaneSetupLog   kittui.TextID = "pane.setuplog"
	textColProgram     kittui.TextID = "col.program"
	textColVersion     kittui.TextID = "col.version"
	textColInstall     kittui.TextID = "col.install"
	textSetupInstalled kittui.TextID = "setup.installed"
	textSetupMissing   kittui.TextID = "setup.missing"
	textSetupButton    kittui.TextID = "setup.button"
	textSetupSee       kittui.TextID = "setup.see"
	textSetupLoading   kittui.TextID = "setup.loading"
	textSetupEmpty     kittui.TextID = "setup.empty"
	textSetupNothing   kittui.TextID = "setup.nothing"
	textSetupHave      kittui.TextID = "setup.have"
	textSetupNoWay     kittui.TextID = "setup.noway"
	textSetupBusy      kittui.TextID = "setup.busy"
	textTitleInstall   kittui.TextID = "title.install"
	textAskInstall     kittui.TextID = "ask.install"
	textWhatInstall    kittui.TextID = "what.install"
	textDetailSudo     kittui.TextID = "detail.sudo"
	textAnswerInstall  kittui.TextID = "answer.install"
	textErrExec        kittui.TextID = "err.exec"
	textLogExit        kittui.TextID = "log.exit"
	textLogNoStart     kittui.TextID = "log.nostart"
	textLogFound       kittui.TextID = "log.found"
	textLogFoundPlain  kittui.TextID = "log.foundplain"
	textLogNotFound    kittui.TextID = "log.notfound"
	textLogNoCheck     kittui.TextID = "log.nocheck"
	textNextReady      kittui.TextID = "next.ready"
	textNextPath       kittui.TextID = "next.path"
	textNextFailed     kittui.TextID = "next.failed"
	textNextCheck      kittui.TextID = "next.check"

	// The Codex hooks in the Setup area and the session list.
	textColIntegration  kittui.TextID = "col.integration"
	textIntegNotNeeded  kittui.TextID = "integ.notneeded"
	textIntegConfigured kittui.TextID = "integ.configured"
	textIntegMissing    kittui.TextID = "integ.missing"
	textIntegStale      kittui.TextID = "integ.stale"
	textIntegUnknown    kittui.TextID = "integ.unknown"
	textIntegConfigure  kittui.TextID = "integ.configure"
	textIntegRemove     kittui.TextID = "integ.remove"
	textKeyHooks        kittui.TextID = "key.hooks"
	textKeyConfigure    kittui.TextID = "key.configure"
	textKeyRemoveHooks  kittui.TextID = "key.removehooks"
	textHooksBusy       kittui.TextID = "hooks.busy"
	textHooksNothing    kittui.TextID = "hooks.nothing"
	textHooksUnknown    kittui.TextID = "hooks.unknown"
	textHooksNoChange   kittui.TextID = "hooks.nochange"
	textTitleHooks      kittui.TextID = "title.hooks"
	textAskHooks        kittui.TextID = "ask.hooks"
	textAskHooksRemove  kittui.TextID = "ask.hooksremove"
	textWhatHooks       kittui.TextID = "what.hooks"
	textWhatHooksRemove kittui.TextID = "what.hooksremove"
	textDetailMore      kittui.TextID = "detail.more"
	textAnswerConfigure kittui.TextID = "answer.configure"
	textLogHooksExit    kittui.TextID = "log.hooksexit"
	textLogHooksNoStart kittui.TextID = "log.hooksnostart"
	textLogHooksState   kittui.TextID = "log.hooksstate"
	textLogHooksNoCheck kittui.TextID = "log.hooksnocheck"
	textNextHooksFailed kittui.TextID = "next.hooksfailed"
	textSessionNoHooks  kittui.TextID = "session.nohooks"
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
			textMissingProgram:  "%s Next step: run 'qcrew setup', or open the Setup area, to install what qcrew needs.",

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

			textViewTitle:   "Session view",
			textViewTitleOf: "Session view: %s",
			textViewNone:    "Choose a session to see it here.",
			textViewEnded:   "The session has ended.",
			textViewFailed:  "The session cannot be shown: %s",
			textViewStatus:  "Tool: %s  Status: %s",
			textViewContext: "Context: %d%%",

			textKeyViewExit:  "Leave the session view and return to qcrew",
			textKeyViewFocus: "{viewexit} back to qcrew",
			textKeyControl:   "control the session",

			textAreaSetup:      "Setup",
			textKeyInstall:     "install",
			textPaneSetup:      "Programs qcrew needs",
			textPaneSetupLog:   "Result",
			textColProgram:     "Program",
			textColVersion:     "Version",
			textColInstall:     "Install",
			textSetupInstalled: "installed",
			textSetupMissing:   "missing",
			textSetupButton:    "[install]",
			textSetupSee:       "see %s",
			textSetupLoading:   "Checking the programs ...",
			textSetupEmpty:     "The service reported no program to check.",
			textSetupNothing:   "There is no program to install here.",
			textSetupHave:      "%s is installed already.",
			textSetupNoWay:     "qcrew knows no supported way to install %s on this system; see %s.",
			textSetupBusy:      "An installation is running; wait for it.",
			textTitleInstall:   "Install program",
			textAskInstall:     "Install %s?",
			textWhatInstall:    "qcrew hands the terminal to 'qcrew setup install %s'. It shows each command again and asks before it runs it; a command with sudo asks a second time. Nothing runs without your yes there.",
			textDetailSudo:     "This command runs with sudo and asks for your password.",
			textAnswerInstall:  "install",
			textErrExec:        "The qcrew program cannot be found to run the installation: %s",
			textLogExit:        "%s: the install command ended with exit code %d.",
			textLogNoStart:     "%s: the install command could not be started: %s",
			textLogFound:       "%s: found now, %s.",
			textLogFoundPlain:  "%s: found now.",
			textLogNotFound:    "%s: not found yet.",
			textLogNoCheck:     "%s: the check afterwards failed: %s",
			textNextReady:      "Next step: none, %s is ready to use.",
			textNextPath:       "Next step: open a new shell or add the folder the installer named to PATH, then press r.",
			textNextFailed:     "Next step: read the output in the terminal, fix the cause and press i to try again. Exit code 2 means the command was not confirmed or needs a terminal.",
			textNextCheck:      "Next step: press r to check again.",

			textColIntegration:  "Integration",
			textIntegNotNeeded:  "not needed",
			textIntegConfigured: "configured",
			textIntegMissing:    "missing",
			textIntegStale:      "stale",
			textIntegUnknown:    "unknown",
			textIntegConfigure:  "[configure]",
			textIntegRemove:     "[remove]",
			textKeyHooks:        "Codex hooks",
			textKeyConfigure:    "configure hooks",
			textKeyRemoveHooks:  "remove hooks",
			textHooksBusy:       "A change is running; wait for it.",
			textHooksNothing:    "Only Codex has hooks to set up here.",
			textHooksUnknown:    "The state of the Codex hooks is unknown: %s",
			textHooksNoChange:   "The Codex hooks need no change.",
			textTitleHooks:      "Codex hooks",
			textAskHooks:        "Set up the Codex hooks of qcrew?",
			textAskHooksRemove:  "Remove the Codex hooks of qcrew?",
			textWhatHooks:       "qcrew hands the terminal to 'qcrew setup codex-hooks'. It shows the whole change again and asks before it writes; both files are backed up first, only the entries of qcrew change, and no hook of anyone else is approved.",
			textWhatHooksRemove: "qcrew hands the terminal to 'qcrew setup codex-hooks --remove'. It shows the whole change again and asks before it writes; both files are backed up first, and only the entries of qcrew go.",
			textDetailMore:      "... %d more changed lines",
			textAnswerConfigure: "configure",
			textLogHooksExit:    "Codex hooks: the command ended with exit code %d.",
			textLogHooksNoStart: "Codex hooks: the command could not be started: %s",
			textLogHooksState:   "Codex hooks now: %s.",
			textLogHooksNoCheck: "Codex hooks: the check afterwards failed: %s",
			textNextHooksFailed: "Next step: read the output in the terminal and press h to try again. Exit code 2 means the change was not confirmed or needs a terminal.",
			textSessionNoHooks:  "%s, no hooks",
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
			textMissingProgram:  "%s Nächster Schritt: 'qcrew setup' ausführen oder den Bereich Einrichtung öffnen, um zu installieren, was qcrew braucht.",

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

			textViewTitle:   "Session-Ansicht",
			textViewTitleOf: "Session-Ansicht: %s",
			textViewNone:    "Wähle eine Session, um sie hier zu sehen.",
			textViewEnded:   "Die Session ist beendet.",
			textViewFailed:  "Die Session lässt sich nicht anzeigen: %s",
			textViewStatus:  "Tool: %s  Status: %s",
			textViewContext: "Kontext: %d%%",

			textKeyViewExit:  "Die Session-Ansicht verlassen und zu qcrew zurückkehren",
			textKeyViewFocus: "{viewexit} zurück zu qcrew",
			textKeyControl:   "Session steuern",

			textAreaSetup:      "Setup",
			textKeyInstall:     "installieren",
			textPaneSetup:      "Programme, die qcrew braucht",
			textPaneSetupLog:   "Ergebnis",
			textColProgram:     "Programm",
			textColVersion:     "Version",
			textColInstall:     "Installation",
			textSetupInstalled: "installiert",
			textSetupMissing:   "fehlt",
			textSetupButton:    "[installieren]",
			textSetupSee:       "siehe %s",
			textSetupLoading:   "Prüfe die Programme ...",
			textSetupEmpty:     "Der Dienst meldet kein Programm zur Prüfung.",
			textSetupNothing:   "Hier gibt es kein Programm zu installieren.",
			textSetupHave:      "%s ist schon installiert.",
			textSetupNoWay:     "qcrew kennt auf diesem System keinen unterstützten Weg, %s zu installieren; siehe %s.",
			textSetupBusy:      "Eine Installation läuft; warte darauf.",
			textTitleInstall:   "Programm installieren",
			textAskInstall:     "%s installieren?",
			textWhatInstall:    "qcrew übergibt das Terminal an 'qcrew setup install %s'. Dort wird jeder Befehl noch einmal gezeigt und vor der Ausführung gefragt; ein Befehl mit sudo fragt ein zweites Mal. Ohne dein Ja läuft dort nichts.",
			textDetailSudo:     "Dieser Befehl läuft mit sudo und fragt nach deinem Passwort.",
			textAnswerInstall:  "installieren",
			textErrExec:        "Das qcrew-Programm für die Installation wurde nicht gefunden: %s",
			textLogExit:        "%s: Der Installationsbefehl endete mit Exit-Code %d.",
			textLogNoStart:     "%s: Der Installationsbefehl ließ sich nicht starten: %s",
			textLogFound:       "%s: jetzt gefunden, %s.",
			textLogFoundPlain:  "%s: jetzt gefunden.",
			textLogNotFound:    "%s: noch nicht gefunden.",
			textLogNoCheck:     "%s: Die Prüfung danach schlug fehl: %s",
			textNextReady:      "Nächster Schritt: keiner, %s ist einsatzbereit.",
			textNextPath:       "Nächster Schritt: eine neue Shell öffnen oder den Ordner, den der Installer nennt, in PATH aufnehmen, dann r drücken.",
			textNextFailed:     "Nächster Schritt: die Ausgabe im Terminal lesen, die Ursache beheben und i drücken, um es erneut zu versuchen. Exit-Code 2 heißt: der Befehl wurde nicht bestätigt oder braucht ein Terminal.",
			textNextCheck:      "Nächster Schritt: r drücken, um erneut zu prüfen.",

			textColIntegration:  "Integration",
			textIntegNotNeeded:  "nicht nötig",
			textIntegConfigured: "eingerichtet",
			textIntegMissing:    "fehlt",
			textIntegStale:      "veraltet",
			textIntegUnknown:    "unbekannt",
			textIntegConfigure:  "[einrichten]",
			textIntegRemove:     "[entfernen]",
			textKeyHooks:        "Codex-Hooks",
			textKeyConfigure:    "Hooks einrichten",
			textKeyRemoveHooks:  "Hooks entfernen",
			textHooksBusy:       "Eine Änderung läuft; warte darauf.",
			textHooksNothing:    "Nur Codex hat hier Hooks zum Einrichten.",
			textHooksUnknown:    "Der Zustand der Codex-Hooks ist unbekannt: %s",
			textHooksNoChange:   "Die Codex-Hooks brauchen keine Änderung.",
			textTitleHooks:      "Codex-Hooks",
			textAskHooks:        "Codex-Hooks von qcrew einrichten?",
			textAskHooksRemove:  "Codex-Hooks von qcrew entfernen?",
			textWhatHooks:       "qcrew übergibt das Terminal an 'qcrew setup codex-hooks'. Dort wird die ganze Änderung noch einmal gezeigt und vor dem Schreiben gefragt; beide Dateien werden vorher gesichert, nur die Einträge von qcrew ändern sich, und kein fremder Hook wird freigegeben.",
			textWhatHooksRemove: "qcrew übergibt das Terminal an 'qcrew setup codex-hooks --remove'. Dort wird die ganze Änderung noch einmal gezeigt und vor dem Schreiben gefragt; beide Dateien werden vorher gesichert, und nur die Einträge von qcrew werden entfernt.",
			textDetailMore:      "... %d weitere geänderte Zeilen",
			textAnswerConfigure: "einrichten",
			textLogHooksExit:    "Codex-Hooks: Der Befehl endete mit Exit-Code %d.",
			textLogHooksNoStart: "Codex-Hooks: Der Befehl ließ sich nicht starten: %s",
			textLogHooksState:   "Codex-Hooks jetzt: %s.",
			textLogHooksNoCheck: "Codex-Hooks: Die Prüfung danach schlug fehl: %s",
			textNextHooksFailed: "Nächster Schritt: die Ausgabe im Terminal lesen und h drücken, um es noch einmal zu versuchen. Exit-Code 2 heißt, die Änderung wurde nicht bestätigt oder braucht ein Terminal.",
			textSessionNoHooks:  "%s, ohne Hooks",
		},
	})
}
