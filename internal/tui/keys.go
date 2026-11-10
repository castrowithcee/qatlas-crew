package tui

import kittui "github.com/castrowithcee/qatlas-kit/tui"

// The actions of the key table.
const (
	actHelp      kittui.ActionID = "help"
	actBack      kittui.ActionID = "back"
	actForceQuit kittui.ActionID = "forcequit"
	actQuit      kittui.ActionID = "quit"
	actReload    kittui.ActionID = "reload"
	actUp        kittui.ActionID = "up"
	actDown      kittui.ActionID = "down"
	actLeft      kittui.ActionID = "left"
	actRight     kittui.ActionID = "right"
	actFirst     kittui.ActionID = "first"
	actLast      kittui.ActionID = "last"
	actPageUp    kittui.ActionID = "pageup"
	actPageDown  kittui.ActionID = "pagedown"
	actEnter     kittui.ActionID = "enter"
	actSearch    kittui.ActionID = "search"
	actArea      kittui.ActionID = "area"
	// actFull shows the pane of the active area alone, and shows the columns again.
	actFull kittui.ActionID = "full"
	// actStop asks to stop the session under the cursor.
	actStop kittui.ActionID = "stop"
	// actInstall asks to install the program under the cursor of the Setup area.
	actInstall kittui.ActionID = "install"
	actToggle  kittui.ActionID = "toggle"
	actNext    kittui.ActionID = "next"
	actPrev    kittui.ActionID = "prev"
	actSave    kittui.ActionID = "save"
	// actAdd opens the dialog that adds a project, actRemove asks to remove the chosen one.
	actAdd    kittui.ActionID = "add"
	actRemove kittui.ActionID = "remove"
	// Each starts a session of its tool in the chosen project.
	actClaude kittui.ActionID = "claude"
	actCodex  kittui.ActionID = "codex"
	actShell  kittui.ActionID = "shell"
	// The answers of a question.
	actYes kittui.ActionID = "yes"
	actNo  kittui.ActionID = "no"
)

// areaKeys are the keys of actArea, one per area in the order of the tab line.
var areaKeys = []string{"1", "2", "3", "4", "5"}

func keyTable() []kittui.Action {
	act := func(id kittui.ActionID, level kittui.KeyLevel, help kittui.TextID, keys ...string) kittui.Action {
		return kittui.Action{ID: id, Level: level, DefaultKeys: keys, Help: help}
	}
	return append([]kittui.Action{
		act(actHelp, kittui.LevelGlobal, kittui.TextKeyHelp, "?"),
		act(actBack, kittui.LevelGlobal, kittui.TextKeyBack, "esc"),
		act(actForceQuit, kittui.LevelGlobal, kittui.TextKeyQuit, "ctrl+c"),
		act(actQuit, kittui.LevelLetter, kittui.TextKeyQuit, "q"),
		act(actReload, kittui.LevelLetter, kittui.TextKeyReload, "r"),
		act(actUp, kittui.LevelMove, kittui.TextKeyUp, "up"),
		act(actDown, kittui.LevelMove, kittui.TextKeyDown, "down"),
		act(actLeft, kittui.LevelMove, kittui.TextKeyLeft, "left"),
		act(actRight, kittui.LevelMove, kittui.TextKeyRight, "right"),
		act(actFirst, kittui.LevelMove, kittui.TextKeyFirst, "home"),
		act(actLast, kittui.LevelMove, kittui.TextKeyLast, "end"),
		act(actPageUp, kittui.LevelMove, kittui.TextKeyPageUp, "pgup"),
		act(actPageDown, kittui.LevelMove, kittui.TextKeyPageDown, "pgdown"),
		act(actEnter, kittui.LevelMove, kittui.TextKeyEnter, "enter"),
		act(actSearch, kittui.LevelMove, kittui.TextKeySearch, "/"),
		act(actArea, kittui.LevelMove, textKeyArea, areaKeys...),
		act(actFull, kittui.LevelLetter, textKeyFull, "f"),
		act(actStop, kittui.LevelLetter, textKeyStop, "e"),
		act(actAdd, kittui.LevelLetter, textKeyAdd, "n"),
		act(actRemove, kittui.LevelLetter, textKeyRemove, "d"),
		act(actClaude, kittui.LevelLetter, textKeyClaude, "c"),
		act(actCodex, kittui.LevelLetter, textKeyCodex, "x"),
		act(actShell, kittui.LevelLetter, textKeyShell, "s"),
		act(actInstall, kittui.LevelLetter, textKeyInstall, "i"),
		act(actToggle, kittui.LevelMove, kittui.TextKeyToggle, "space"),
		act(actNext, kittui.LevelMove, kittui.TextKeyNext, "tab"),
		act(actPrev, kittui.LevelMove, kittui.TextKeyPrevious, "shift+tab"),
		act(actSave, kittui.LevelGlobal, kittui.TextKeySave, "f2"),
		act(actYes, kittui.LevelConfirm, kittui.TextKeyYes, "y"),
		act(actNo, kittui.LevelConfirm, kittui.TextKeyNo, "n", "esc"),
	}, kittui.LogViewActions()...)
}

// listKeys are the keys of both lists. The lists hold no groups, so they leave left and right to the panes.
func listKeys() kittui.ListKeys {
	return kittui.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actFirst,
		Last: actLast, Left: actLeft, Right: actRight, Enter: actEnter, Search: actSearch, Back: actBack}
}

func helpKeys() kittui.HelpKeys {
	return kittui.HelpKeys{Left: actLeft, Right: actRight, Up: actUp, Down: actDown, PageUp: actPageUp,
		PageDown: actPageDown, First: actFirst, Last: actLast, Back: actBack, Help: actHelp}
}

// formKeys are the actions of the key table the settings form reads.
func formKeys() kittui.FormKeys {
	return kittui.FormKeys{Up: actUp, Down: actDown, Next: actNext, Prev: actPrev, PageUp: actPageUp,
		PageDown: actPageDown, Left: actLeft, Right: actRight, First: actFirst, Last: actLast, Enter: actEnter,
		Toggle: actToggle, Save: actSave, Back: actBack}
}
