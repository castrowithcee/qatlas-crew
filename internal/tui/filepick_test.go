package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// folderTree makes the folders below a new temporary directory, which stands for the home folder.
func folderTree(t *testing.T, dirs ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestExpandHomeResolvesOnlyTheOwnHome(t *testing.T) {
	for _, c := range []struct {
		text, home, want string
		ok               bool
	}{
		{"~", "/h", "/h", true}, {"~/a/b", "/h", "/h/a/b", true}, {"/x/y", "/h", "/x/y", true},
		{"rel/x", "/h", "rel/x", true}, {"~bob/x", "/h", "~bob/x", false}, {"~/a", "", "", false},
	} {
		got, ok := expandHome(c.text, c.home)
		if got != c.want || ok != c.ok {
			t.Errorf("expandHome(%q, %q) = %q, %v; want %q, %v", c.text, c.home, got, ok, c.want, c.ok)
		}
	}
	if got := shortHome("/h/a", "/h"); got != "~/a" {
		t.Errorf("shortHome = %q", got)
	}
	if got := shortHome("/hx/a", "/h"); got != "/hx/a" {
		t.Errorf("shortHome of a sibling = %q", got)
	}
}

func TestSubfoldersListsFoldersOnlySortedWithoutCase(t *testing.T) {
	root := folderTree(t, "beta", "Alpha", ".hidden", "gamma/deep")
	if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "gamma"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "file"), filepath.Join(root, "filelink")); err != nil {
		t.Fatal(err)
	}
	got, err := subfolders(osFolders(), root, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Alpha", "beta", "gamma", "link"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, _ := subfolders(osFolders(), root, true); !slices.Contains(got, ".hidden") {
		t.Fatalf("hidden folders missing: %v", got)
	}
	if _, err := subfolders(osFolders(), filepath.Join(root, "missing"), false); err == nil {
		t.Fatal("a missing folder was read")
	}
}

func TestCompleteNameExtendsToTheCommonStart(t *testing.T) {
	names := []string{"qatlas-crew", "qatlas-kit", "other"}
	for _, c := range []struct {
		prefix, want string
		ok           bool
	}{
		{"q", "qatlas-", true}, {"qatlas-", "qatlas-", false}, {"qatlas-c", "qatlas-crew/", true},
		{"o", "other/", true}, {"x", "x", false},
	} {
		got, ok := completeName(names, c.prefix)
		if got != c.want || ok != c.ok {
			t.Errorf("completeName(%q) = %q, %v; want %q, %v", c.prefix, got, ok, c.want, c.ok)
		}
	}
}

func newTestPicker(t *testing.T, home, start string) *folderPicker {
	t.Helper()
	keys, err := kittui.NewKeymap(keyTable(), kittui.CharsetASCII)
	if err != nil {
		t.Fatal(err)
	}
	text, err := kittui.NewTranslator(i18n.English, catalog(), keys)
	if err != nil {
		t.Fatal(err)
	}
	p, err := newFolderPicker(keys, text, osFolders(), home, start)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// rowsOf are the cells of the rows the picker lists.
func rowsOf(p *folderPicker) []string {
	var rows []string
	if sel, ok := p.list.Selected(); ok {
		defer p.list.Select(sel)
	}
	for _, id := range append([]kittui.ItemID{pickUpRow}, slices.Sorted(func(yield func(kittui.ItemID) bool) {
		for id := range p.rows {
			if !yield(id) {
				return
			}
		}
	})...) {
		if p.list.Select(id) {
			it, _ := p.list.Current()
			rows = append(rows, it.Cells[0])
		}
	}
	return rows
}

func pressKeys(p *folderPicker, keys ...string) {
	for _, k := range keys {
		msg, ok := parseKey(k)
		if !ok {
			panic("unknown key " + k)
		}
		p.Update(msg)
	}
}

func typeText(p *folderPicker, text string) {
	for _, r := range text {
		p.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestPickerListsCompletesAndGoesUp(t *testing.T) {
	home := folderTree(t, "work/app", "work/lib", "notes")
	p := newTestPicker(t, home, "~/")
	if got := rowsOf(p); !slices.Equal(got, []string{"..", "notes/", "work/"}) {
		t.Fatalf("rows of ~/ = %v", got)
	}
	typeText(p, "w")
	if got := rowsOf(p); !slices.Equal(got, []string{"..", "work/"}) {
		t.Fatalf("rows after w = %v", got)
	}
	pressKeys(p, "tab")
	if p.Text() != "~/work/" {
		t.Fatalf("tab completed to %q", p.Text())
	}
	if got := rowsOf(p); !slices.Equal(got, []string{"..", "app/", "lib/"}) {
		t.Fatalf("rows of ~/work/ = %v", got)
	}
	if folder, ok := p.Folder(); !ok || folder != filepath.Join(home, "work") {
		t.Fatalf("folder = %q, %v", folder, ok)
	}
	pressKeys(p, "tab") // nothing to complete: the focus goes to the list
	if !p.onList {
		t.Fatal("tab without a completion did not reach the list")
	}
	pressKeys(p, "down", "enter")
	if p.Text() != "~/work/app/" {
		t.Fatalf("enter on app opened %q", p.Text())
	}
	pressKeys(p, "left")
	if p.Text() != "~/work/" {
		t.Fatalf("left went to %q", p.Text())
	}
	pressKeys(p, "left", "left")
	if want := shortHome(filepath.Dir(home), home) + "/"; p.Text() != want {
		t.Fatalf("up from home went to %q, want %q", p.Text(), want)
	}
}

func TestPickerAtTheRootHasNoWayUp(t *testing.T) {
	p := newTestPicker(t, "", "/")
	if got := rowsOf(p); slices.Contains(got, "..") {
		t.Fatalf("the root offers a way up: %v", got)
	}
	if folder, ok := p.Folder(); !ok || folder != "/" {
		t.Fatalf("folder = %q, %v", folder, ok)
	}
}

func TestPickerNamesARelativeOrUnreadablePath(t *testing.T) {
	home := folderTree(t, "a")
	p := newTestPicker(t, home, "rel/x")
	if _, ok := p.Folder(); ok {
		t.Fatal("a relative path names a folder")
	}
	p.setText("~/missing/")
	if p.ReadError() == nil {
		t.Fatal("a missing folder was read")
	}
	if got := rowsOf(p); !slices.Equal(got, []string{".."}) {
		t.Fatalf("rows of a missing folder = %v", got)
	}
}
