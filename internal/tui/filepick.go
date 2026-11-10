package tui

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// folderSource reads the folders the picker offers. It only reads; nothing in the interface writes to the file
// system. Tests point it at a temporary directory.
type folderSource struct {
	ReadDir func(name string) ([]fs.DirEntry, error)
	Stat    func(name string) (fs.FileInfo, error)
}

// osFolders reads the file system of the host.
func osFolders() folderSource { return folderSource{ReadDir: os.ReadDir, Stat: os.Stat} }

// expandHome resolves a leading "~" of a typed path to home. It is false for "~user" and for "~" without a
// home; every other text comes back unchanged.
func expandHome(text, home string) (string, bool) {
	if text != "~" && !strings.HasPrefix(text, "~/") {
		return text, !strings.HasPrefix(text, "~")
	}
	if home == "" {
		return "", false
	}
	return home + text[1:], true
}

// shortHome writes an absolute path below home with "~", the way a person types it.
func shortHome(abs, home string) string {
	switch {
	case home == "" || home == "/":
		return abs
	case abs == home:
		return "~"
	case strings.HasPrefix(abs, home+"/"):
		return "~" + abs[len(home):]
	}
	return abs
}

// splitTyped cuts a typed path after its last slash: the folder part and the start of a name in it.
func splitTyped(text string) (dir, prefix string) {
	i := strings.LastIndex(text, "/")
	return text[:i+1], text[i+1:]
}

// subfolders lists the names of the folders in dir, symbolic links to folders included, sorted without regard
// to case. Hidden folders come only where hidden is set. It reads one directory and never descends.
func subfolders(src folderSource, dir string, hidden bool) ([]string, error) {
	entries, err := src.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
		isDir := e.IsDir()
		if !isDir && e.Type()&fs.ModeSymlink != 0 && src.Stat != nil {
			if info, err := src.Stat(filepath.Join(dir, name)); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return names, nil
}

// withPrefix keeps the names that start with prefix.
func withPrefix(names []string, prefix string) []string {
	var out []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	return out
}

// completeName completes prefix among names: a single match becomes the whole name with a slash, several
// become their longest common start. It is false where nothing is added.
func completeName(names []string, prefix string) (string, bool) {
	matches := withPrefix(names, prefix)
	switch len(matches) {
	case 0:
		return prefix, false
	case 1:
		return matches[0] + "/", true
	}
	common := matches[0]
	for _, m := range matches[1:] {
		for !strings.HasPrefix(m, common) {
			common = common[:len(common)-1]
		}
	}
	return common, len(common) > len(prefix)
}

// typeable reports whether a folder name can stand in the text field: it holds no control character.
func typeable(name string) bool {
	return !strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) || !unicode.IsPrint(r) })
}

// The fixed IDs of the picker.
const (
	pickPathField kittui.FieldID = "path"
	pickUpRow     kittui.ItemID  = "up"
)

// folderPicker chooses a folder: a text field for its path, which resolves "~" and completes names with tab,
// and the list of the folders in the folder the path names, with ".." to go up. It reads the folder anew only
// when the typed folder changes, and never searches below it.
type folderPicker struct {
	keys *kittui.Keymap
	src  folderSource
	home string
	form *kittui.Form
	list *kittui.List
	// onList says that the list, not the field, has the focus.
	onList bool
	// dirText is the folder part of the typed path the list was read for, abs its resolved form, names the
	// folders in it and readErr why it could not be read.
	dirText, abs string
	names        []string
	readErr      error
	read         bool
	// rows maps each row of the list to its folder; the row of ".." has none.
	rows map[kittui.ItemID]string
}

func newFolderPicker(keys *kittui.Keymap, text kittui.Translator, src folderSource, home, start string) (*folderPicker, error) {
	form, err := kittui.NewForm(kittui.FormSpec{Keys: formKeys(), Fields: []kittui.FormField{{ID: pickPathField,
		Kind: kittui.FieldText, Label: textPickFolder, Value: kittui.FormValue{Text: start}}}}, keys, text)
	if err != nil {
		return nil, err
	}
	list, err := kittui.NewList(kittui.ListSpec{Columns: []kittui.Column{{Title: textColFolder}},
		Keys: listKeys(), Search: kittui.SearchByKey, Empty: textPickNoFolder}, keys, text)
	if err != nil {
		return nil, err
	}
	p := &folderPicker{keys: keys, src: src, home: home, form: form, list: list, rows: map[kittui.ItemID]string{}}
	p.refresh()
	return p, nil
}

// Text is the path as typed.
func (p *folderPicker) Text() string { return p.form.Values()[pickPathField].Text }

func (p *folderPicker) setText(text string) {
	if p.form.SetValue(pickPathField, kittui.FormValue{Text: text}) == nil {
		p.refresh()
	}
}

// Folder is the absolute, cleaned folder the typed path names; false where it names none.
func (p *folderPicker) Folder() (string, bool) {
	abs, ok := expandHome(strings.TrimSpace(p.Text()), p.home)
	if !ok || !filepath.IsAbs(abs) {
		return "", false
	}
	return filepath.Clean(abs), true
}

// Shown is the folder the list shows, as the person types it.
func (p *folderPicker) Shown() string { return p.dirText }

// ReadError is why the folder of the list cannot be read; nil where it can.
func (p *folderPicker) ReadError() error { return p.readErr }

// refresh reads the folder of the typed path where it changed and shows its folders that start with the typed
// name.
func (p *folderPicker) refresh() {
	dirText, prefix := splitTyped(p.Text())
	if !p.read || dirText != p.dirText {
		p.dirText, p.names, p.readErr, p.read = dirText, nil, nil, true
		abs, ok := expandHome(dirText, p.home)
		switch {
		case ok && filepath.IsAbs(abs):
			p.abs = filepath.Clean(abs)
			// Hidden folders are read too; the prefix decides whether they show.
			p.names, p.readErr = subfolders(p.src, p.abs, true)
		default:
			p.abs = ""
		}
	}
	clear(p.rows)
	var items []kittui.ListItem
	if p.abs != "" && p.abs != "/" {
		items = append(items, kittui.ListItem{ID: pickUpRow, Cells: []string{".."}})
	}
	for i, name := range withPrefix(p.names, prefix) {
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			continue
		}
		id := kittui.ItemID("f" + strconv.Itoa(i))
		p.rows[id] = name
		items = append(items, kittui.ListItem{ID: id, Cells: []string{name + "/"}})
	}
	_ = p.list.SetItems(items) // the IDs are the picker's own and unique
}

// complete extends the typed name; false where it adds nothing.
func (p *folderPicker) complete() bool {
	dirText, prefix := splitTyped(p.Text())
	var names []string
	for _, n := range p.names {
		if typeable(n) && (!strings.HasPrefix(n, ".") || strings.HasPrefix(prefix, ".")) {
			names = append(names, n)
		}
	}
	done, ok := completeName(names, prefix)
	if ok {
		p.setText(dirText + done)
	}
	return ok
}

// open goes into the folder of the marked row, or up from the row of "..".
func (p *folderPicker) open(id kittui.ItemID) {
	if id == pickUpRow {
		p.up()
		return
	}
	if name, ok := p.rows[id]; ok && typeable(name) {
		p.setText(p.dirText + name + "/")
		p.list.Reset()
	}
}

// up shows the folder above the one the list shows.
func (p *folderPicker) up() {
	if p.abs == "" || p.abs == "/" {
		return
	}
	parent := shortHome(filepath.Dir(p.abs), p.home)
	if !strings.HasSuffix(parent, "/") {
		parent += "/"
	}
	p.setText(parent)
	p.list.Reset()
}

// Input is the input focus of the picker.
func (p *folderPicker) Input() kittui.InputFocus {
	if p.onList {
		return p.list.Input()
	}
	return p.form.Input()
}

// Focus gives the focus to the field, or to the list where it has rows.
func (p *folderPicker) Focus(list bool) { p.onList = list && p.hasRows() }

func (p *folderPicker) hasRows() bool {
	_, ok := p.list.Selected()
	return ok
}

// Update reads a key. leave is 1 where the key moves the focus past the picker and -1 where it moves it before;
// handled is false for a key the picker leaves to its dialog.
func (p *folderPicker) Update(msg tea.KeyPressMsg) (leave int, handled bool) {
	is := func(id kittui.ActionID) bool { return p.keys.Is(msg, id, p.Input()) }
	if p.onList && p.list.Searching() {
		open, handled := p.list.Update(msg)
		if open != "" {
			p.open(open)
		}
		return 0, handled
	}
	if p.onList {
		switch {
		case is(actPrev):
			p.onList = false
			return 0, true
		case is(actNext):
			return 1, true
		case is(actLeft):
			p.up()
			return 0, true
		case is(actRight):
			if id, ok := p.list.Selected(); ok {
				p.open(id)
			}
			return 0, true
		}
		open, handled := p.list.Update(msg)
		switch {
		case open != "":
			p.open(open)
		case !handled && is(actUp):
			p.onList = false
			return 0, true
		}
		return 0, handled
	}
	switch {
	case is(actNext):
		if p.complete() {
			return 0, true
		}
		if p.hasRows() {
			p.onList = true
			return 0, true
		}
		return 1, true
	case is(actDown):
		if p.hasRows() {
			p.onList = true
			return 0, true
		}
		return 1, true
	case is(actPrev), is(actUp):
		return -1, true
	case is(actEnter):
		return 1, true
	case is(actBack), is(actSave):
		return 0, false
	}
	before := p.Text()
	_, handled = p.form.Update(msg)
	if p.Text() != before {
		p.refresh()
	}
	return 0, handled
}

// Click handles a click on a target of the field or the list; it gives the clicked part the focus.
func (p *folderPicker) Click(res kittui.MouseResult, onList bool) {
	if !onList {
		p.onList = false
		p.form.Click(res)
		return
	}
	p.list.SetFocused(p.onList)
	open, _ := p.list.Click(res)
	p.onList = true
	if open != "" {
		p.open(open)
	}
}

// Hints are the keys of the picker for the footer.
func (p *folderPicker) Hints() []kittui.Hint {
	if p.onList {
		return []kittui.Hint{{Action: actEnter, Label: textPickOpen, Priority: 1},
			{Action: actLeft, Label: textPickUp, Priority: 1},
			{Action: actUp, Label: kittui.TextKeyChoose, Kind: kittui.HintNavigation},
			{Action: actDown, Label: kittui.TextKeyChoose, Kind: kittui.HintNavigation}}
	}
	return []kittui.Hint{{Action: actNext, Label: textPickComplete, Priority: 1},
		{Action: actDown, Label: textPickFolders, Priority: 2}}
}

// pickerRoom is the least height the picker draws in: the field, the line of the folder and three rows.
const pickerRoom = 3 + 1 + 4

// View draws the field and, below it, the folder the list shows and its folders, in width by height cells. The
// targets are counted from the top left; listY is the first row of the list.
func (p *folderPicker) View(styles kittui.Styles, text kittui.Translator, width, height int) (view string, fieldTargets, listTargets []kittui.Target, listY int) {
	field, ft := p.form.View(styles, width, 3)
	p.list.SetFocused(p.onList)
	where := text.Lookup(textPickIn, p.dirText)
	if p.readErr != nil {
		where = text.Lookup(textPickUnreadable, p.dirText)
	}
	if p.dirText == "" {
		where = text.Lookup(textPickTypeFolder)
	}
	line := "  " + styles.Render(kittui.StyleHint, styles.Truncate(kittui.Escape(where), max(width-2, 1)))
	listY = 4
	rows, lt := p.list.View(styles, max(width-2, 1), max(height-listY, 1))
	lines := strings.Split(rows, "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	view = fit(field, width, 3) + "\n" + kittui.Pad(line, width) + "\n" + fit(strings.Join(lines, "\n"), width, max(height-listY, 1))
	return view, ft, kittui.OffsetTargets(lt, 2, listY), listY
}
