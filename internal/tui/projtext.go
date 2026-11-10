package tui

import (
	"maps"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The texts of adding and removing projects, of the folder picker and of starting sessions.
const (
	textKeyAdd    kittui.TextID = "key.add"
	textKeyRemove kittui.TextID = "key.remove"
	textKeyStart  kittui.TextID = "key.start"
	textKeyClaude kittui.TextID = "key.claude"
	textKeyCodex  kittui.TextID = "key.codex"
	textKeyShell  kittui.TextID = "key.shell"

	textBtnAdd         kittui.TextID = "button.add"
	textBtnRemove      kittui.TextID = "button.remove"
	textBtnToolMissing kittui.TextID = "button.toolmissing"

	textPickFolder     kittui.TextID = "pick.folder"
	textColFolder      kittui.TextID = "col.folder"
	textPickNoFolder   kittui.TextID = "pick.nofolder"
	textPickIn         kittui.TextID = "pick.in"
	textPickUnreadable kittui.TextID = "pick.unreadable"
	textPickTypeFolder kittui.TextID = "pick.typefolder"
	textPickOpen       kittui.TextID = "pick.open"
	textPickUp         kittui.TextID = "pick.up"
	textPickComplete   kittui.TextID = "pick.complete"
	textPickFolders    kittui.TextID = "pick.folders"

	textTitleAdd       kittui.TextID = "title.add"
	textAddLead        kittui.TextID = "add.lead"
	textAddUse         kittui.TextID = "add.use"
	textAddExisting    kittui.TextID = "add.existing"
	textAddNew         kittui.TextID = "add.new"
	textAddNewName     kittui.TextID = "add.newname"
	textAddName        kittui.TextID = "add.name"
	textHintUse        kittui.TextID = "hint.use"
	textHintNewName    kittui.TextID = "hint.newname"
	textHintName       kittui.TextID = "hint.name"
	textAnswerAdd      kittui.TextID = "answer.add"
	textAddNoPath      kittui.TextID = "add.nopath"
	textAddNotAbs      kittui.TextID = "add.notabs"
	textAddNoNew       kittui.TextID = "add.nonew"
	textAddBadNew      kittui.TextID = "add.badnew"
	textAddNextNew     kittui.TextID = "add.nextnew"
	textAddNoPlace     kittui.TextID = "add.noplace"
	textAddNextPlace   kittui.TextID = "add.nextplace"
	textAddExists      kittui.TextID = "add.exists"
	textAddNextHere    kittui.TextID = "add.nexthere"
	textAddNextTaken   kittui.TextID = "add.nexttaken"
	textAddNextMissing kittui.TextID = "add.nextmissing"
	textAddNextInvalid kittui.TextID = "add.nextinvalid"
	textAddNextRetry   kittui.TextID = "add.nextretry"
	textResultAdd      kittui.TextID = "result.added"

	textTitleRemove   kittui.TextID = "title.remove"
	textAskRemove     kittui.TextID = "ask.remove"
	textWhatRemove    kittui.TextID = "what.remove"
	textDetailPath    kittui.TextID = "detail.path"
	textAnswerRemove  kittui.TextID = "answer.remove"
	textResultRemove  kittui.TextID = "result.removed"
	textRemoveInUse   kittui.TextID = "remove.inuse"
	textRemoveBusy    kittui.TextID = "remove.busy"
	textRemoveNothing kittui.TextID = "remove.nothing"

	textStartBusy   kittui.TextID = "start.busy"
	textResultStart kittui.TextID = "result.started"
	textMissingTool kittui.TextID = "missing.tool"
)

// withProjectTexts adds the texts of this file to the catalogs.
func withProjectTexts(c i18n.Catalogs[kittui.TextID]) i18n.Catalogs[kittui.TextID] {
	for lang, texts := range projectTexts() {
		maps.Copy(c[lang], texts)
	}
	return c
}

func projectTexts() i18n.Catalogs[kittui.TextID] {
	return i18n.Catalogs[kittui.TextID]{
		i18n.English: {
			textKeyAdd:    "add project",
			textKeyRemove: "remove project",
			textKeyStart:  "start",
			textKeyClaude: "start a Claude Code session",
			textKeyCodex:  "start a Codex session",
			textKeyShell:  "start a shell session",

			textBtnAdd:         "+ project",
			textBtnRemove:      "remove",
			textBtnToolMissing: "%s: setup",

			textPickFolder:     "folder",
			textColFolder:      "Folder",
			textPickNoFolder:   "No folder here starts with what is typed.",
			textPickIn:         "Folders in %s",
			textPickUnreadable: "%s cannot be read.",
			textPickTypeFolder: "Type a path that starts with / or ~.",
			textPickOpen:       "open folder",
			textPickUp:         "folder up",
			textPickComplete:   "complete",
			textPickFolders:    "folders",

			textTitleAdd:       "Add project",
			textAddLead:        "Choose an existing folder, or the place of a new one.",
			textAddUse:         "use",
			textAddExisting:    "this folder",
			textAddNew:         "a new folder in it",
			textAddNewName:     "new folder",
			textAddName:        "name",
			textHintUse:        "left and right choose; a new folder is created by qcrew",
			textHintNewName:    "the name of the folder qcrew creates",
			textHintName:       "the name of the project in qcrew",
			textAnswerAdd:      "add",
			textAddNoPath:      "No folder is given.",
			textAddNotAbs:      "The path %s is not absolute.",
			textAddNoNew:       "The new folder has no name.",
			textAddBadNew:      "A folder name holds no / and is not . or ..",
			textAddNextNew:     "Type the name of the new folder.",
			textAddNoPlace:     "%s is no folder.",
			textAddNextPlace:   "Choose an existing folder as the place of the new one.",
			textAddExists:      "%s exists already.",
			textAddNextHere:    "Open it and use this folder, or type another name.",
			textAddNextTaken:   "Choose another folder or another name.",
			textAddNextMissing: "Choose a folder that exists, or a new folder in it.",
			textAddNextInvalid: "Correct the folder or the name and add again.",
			textAddNextRetry:   "Add again; if it keeps failing, run 'qcrew service status'.",
			textResultAdd:      "The project '%s' is added.",

			textTitleRemove:   "Remove project",
			textAskRemove:     "Remove the project '%s' from qcrew?",
			textWhatRemove:    "qcrew forgets the project. Its folder and the files in it stay as they are.",
			textDetailPath:    "path: %s",
			textAnswerRemove:  "remove",
			textResultRemove:  "The project '%s' is removed; its folder stays.",
			textRemoveInUse:   "Next step: stop its sessions with {stop} in the Sessions area, then remove it again.",
			textRemoveBusy:    "A project is being removed; wait for it.",
			textRemoveNothing: "There is no project to remove here.",

			textStartBusy:   "A session is being started; wait for it.",
			textResultStart: "The session '%s' is started.",
			textMissingTool: "%s is not installed. Next step: run 'qcrew setup' to install it.",
		},
		i18n.German: {
			textKeyAdd:    "Projekt anlegen",
			textKeyRemove: "Projekt entfernen",
			textKeyStart:  "starten",
			textKeyClaude: "Claude-Code-Session starten",
			textKeyCodex:  "Codex-Session starten",
			textKeyShell:  "Shell-Session starten",

			textBtnAdd:         "+ Projekt",
			textBtnRemove:      "entfernen",
			textBtnToolMissing: "%s: setup",

			textPickFolder:     "Ordner",
			textColFolder:      "Ordner",
			textPickNoFolder:   "Hier beginnt kein Ordner mit dem Getippten.",
			textPickIn:         "Ordner in %s",
			textPickUnreadable: "%s lässt sich nicht lesen.",
			textPickTypeFolder: "Tippe einen Pfad, der mit / oder ~ beginnt.",
			textPickOpen:       "Ordner öffnen",
			textPickUp:         "Ordner hoch",
			textPickComplete:   "vervollständigen",
			textPickFolders:    "Ordner",

			textTitleAdd:       "Projekt anlegen",
			textAddLead:        "Wähle einen vorhandenen Ordner oder den Ort für einen neuen.",
			textAddUse:         "verwenden",
			textAddExisting:    "diesen Ordner",
			textAddNew:         "neuen Ordner darin",
			textAddNewName:     "neuer Ordner",
			textAddName:        "Name",
			textHintUse:        "links und rechts wählen; einen neuen Ordner legt qcrew an",
			textHintNewName:    "der Name des Ordners, den qcrew anlegt",
			textHintName:       "der Name des Projekts in qcrew",
			textAnswerAdd:      "anlegen",
			textAddNoPath:      "Es ist kein Ordner angegeben.",
			textAddNotAbs:      "Der Pfad %s ist nicht absolut.",
			textAddNoNew:       "Der neue Ordner hat keinen Namen.",
			textAddBadNew:      "Ein Ordnername enthält kein / und ist nicht . oder ..",
			textAddNextNew:     "Tippe den Namen des neuen Ordners.",
			textAddNoPlace:     "%s ist kein Ordner.",
			textAddNextPlace:   "Wähle einen vorhandenen Ordner als Ort für den neuen.",
			textAddExists:      "%s gibt es schon.",
			textAddNextHere:    "Öffne ihn und verwende diesen Ordner, oder tippe einen anderen Namen.",
			textAddNextTaken:   "Wähle einen anderen Ordner oder einen anderen Namen.",
			textAddNextMissing: "Wähle einen vorhandenen Ordner oder einen neuen Ordner darin.",
			textAddNextInvalid: "Korrigiere Ordner oder Namen und lege erneut an.",
			textAddNextRetry:   "Lege erneut an; scheitert es weiter, führe 'qcrew service status' aus.",
			textResultAdd:      "Das Projekt '%s' ist angelegt.",

			textTitleRemove:   "Projekt entfernen",
			textAskRemove:     "Das Projekt '%s' aus qcrew entfernen?",
			textWhatRemove:    "qcrew vergisst das Projekt. Sein Ordner und die Dateien darin bleiben unverändert.",
			textDetailPath:    "Pfad: %s",
			textAnswerRemove:  "entfernen",
			textResultRemove:  "Das Projekt '%s' ist entfernt; sein Ordner bleibt.",
			textRemoveInUse:   "Nächster Schritt: seine Sessions im Bereich Sessions mit {stop} stoppen, dann erneut entfernen.",
			textRemoveBusy:    "Ein Projekt wird gerade entfernt; warte darauf.",
			textRemoveNothing: "Hier gibt es kein Projekt zum Entfernen.",

			textStartBusy:   "Eine Session wird gerade gestartet; warte darauf.",
			textResultStart: "Die Session '%s' ist gestartet.",
			textMissingTool: "%s ist nicht installiert. Nächster Schritt: 'qcrew setup' ausführen, um es zu installieren.",
		},
	}
}
