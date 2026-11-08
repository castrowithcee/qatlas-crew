package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/configfile"
)

// canonical is dir with every symbolic link resolved; temporary directories lie below one on some systems.
func canonical(t *testing.T, dir string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func newRegistry(t *testing.T) (*Registry, string) {
	t.Helper()
	root := t.TempDir()
	r, err := NewRegistry(filepath.Join(root, "qcrew", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 600, time.FixedZone("x", 3600)) }
	return r, root
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func wantKind(t *testing.T, err, kind error) {
	t.Helper()
	var refusal *Error
	if !errors.Is(err, kind) || !errors.As(err, &refusal) || refusal.Message == "" {
		t.Fatalf("got %v, want a refusal of kind %v", err, kind)
	}
}

func TestAddListGetRemove(t *testing.T) {
	r, root := newRegistry(t)
	dir := mkdir(t, filepath.Join(root, "work", "app"))
	if list, err := r.List(); err != nil || len(list) != 0 {
		t.Fatalf("list of a missing file: %v, %v", list, err)
	}
	p, err := r.Add(AddRequest{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !idPattern.MatchString(p.ID) || p.Name != "app" || p.Path != canonical(t, dir) {
		t.Fatalf("added %+v", p)
	}
	if want := time.Date(2026, 1, 2, 2, 4, 5, 0, time.UTC); !p.AddedAt.Equal(want) || p.AddedAt.Location() != time.UTC {
		t.Fatalf("added_at %v", p.AddedAt)
	}
	for _, ref := range []string{p.ID, "app"} {
		if got, err := r.Get(ref); err != nil || got != p {
			t.Fatalf("get %q: %+v, %v", ref, got, err)
		}
	}
	info, err := os.Stat(r.Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config file: %v, %v", info, err)
	}

	other := mkdir(t, filepath.Join(root, "work", "lib"))
	if _, err := r.Add(AddRequest{Path: other, Name: "the lib"}); err != nil {
		t.Fatal(err)
	}
	removed, err := r.Remove("app")
	if err != nil || removed != p {
		t.Fatalf("remove: %+v, %v", removed, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("remove touched the directory: %v", err)
	}
	list, err := r.List()
	if err != nil || len(list) != 1 || list[0].Name != "the lib" {
		t.Fatalf("list after remove: %+v, %v", list, err)
	}
	backups, err := os.ReadDir(filepath.Join(filepath.Dir(r.Path()), "backups"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("backups: %v, %v", backups, err)
	}
	wantKind(t, func() error { _, err := r.Remove("app"); return err }(), ErrNotFound)
	wantKind(t, func() error { _, err := r.Get("app"); return err }(), ErrNotFound)
}

func TestAddRefusals(t *testing.T) {
	r, root := newRegistry(t)
	dir := mkdir(t, filepath.Join(root, "a", "app"))
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := r.Add(AddRequest{Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		req  AddRequest
		kind error
	}{
		{"relative path", AddRequest{Path: "a/app"}, ErrInvalid},
		{"missing directory", AddRequest{Path: filepath.Join(root, "missing")}, ErrInvalid},
		{"file instead of directory", AddRequest{Path: file}, ErrInvalid},
		{"same path", AddRequest{Path: dir, Name: "other"}, ErrExists},
		{"same path through a link", AddRequest{Path: link, Name: "other"}, ErrExists},
		{"duplicate default name", AddRequest{Path: mkdir(t, filepath.Join(root, "b", "app"))}, ErrExists},
		{"duplicate name", AddRequest{Path: mkdir(t, filepath.Join(root, "c")), Name: "app"}, ErrExists},
		{"name equals an id", AddRequest{Path: mkdir(t, filepath.Join(root, "d")), Name: first.ID}, ErrExists},
		{"name with separator", AddRequest{Path: mkdir(t, filepath.Join(root, "e")), Name: "a/b"}, ErrInvalid},
		{"name with control character", AddRequest{Path: mkdir(t, filepath.Join(root, "f")), Name: "a\tb"}, ErrInvalid},
		{"name with outer space", AddRequest{Path: mkdir(t, filepath.Join(root, "g")), Name: " a"}, ErrInvalid},
		{"long name", AddRequest{Path: mkdir(t, filepath.Join(root, "h")), Name: strings.Repeat("x", MaxName+1)}, ErrInvalid},
	}
	before, _, err := r.store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		_, err := r.Add(c.req)
		var refusal *Error
		if !errors.Is(err, c.kind) || !errors.As(err, &refusal) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.kind)
		}
	}
	after, _, _ := r.store.Snapshot()
	if string(after) != string(before) {
		t.Errorf("a refused add changed the file:\n%s", after)
	}
}

func TestAddResolvesLinksAndCreates(t *testing.T) {
	r, root := newRegistry(t)
	real := mkdir(t, filepath.Join(root, "real"))
	link := filepath.Join(root, "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	p, err := r.Add(AddRequest{Path: link})
	if err != nil || p.Path != canonical(t, real) || p.Name != "real" {
		t.Fatalf("add through a link: %+v, %v", p, err)
	}

	fresh := filepath.Join(root, "new", "deep")
	p, err = r.Add(AddRequest{Path: fresh, Create: true})
	if err != nil || p.Name != "deep" {
		t.Fatalf("add --create: %+v, %v", p, err)
	}
	if info, err := os.Stat(fresh); err != nil || !info.IsDir() {
		t.Fatalf("created directory: %v, %v", info, err)
	}
	// A name that is taken is refused before the directory is made.
	never := filepath.Join(root, "never")
	_, err = r.Add(AddRequest{Path: never, Name: "deep", Create: true})
	wantKind(t, err, ErrExists)
	if _, err := os.Stat(never); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused add created the directory: %v", err)
	}
}

func TestIDs(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id, err := NewID()
		if err != nil || !idPattern.MatchString(id) || seen[id] {
			t.Fatalf("id %q, %v", id, err)
		}
		seen[id] = true
	}
}

// A taken id is drawn again rather than stored twice.
func TestAddDrawsAFreeID(t *testing.T) {
	r, root := newRegistry(t)
	ids := []string{"aaaaaaaaaaaa", "aaaaaaaaaaaa", "bbbbbbbbbbbb"}
	r.newID = func() (string, error) { id := ids[0]; ids = ids[1:]; return id, nil }
	if _, err := r.Add(AddRequest{Path: mkdir(t, filepath.Join(root, "one"))}); err != nil {
		t.Fatal(err)
	}
	p, err := r.Add(AddRequest{Path: mkdir(t, filepath.Join(root, "two"))})
	if err != nil || p.ID != "bbbbbbbbbbbb" {
		t.Fatalf("second add: %+v, %v", p, err)
	}
}

const canonicalYAML = `schema: 1
projects:
  - id: abcdefghijk2
    name: app
    path: /work/app
    added_at: 2026-01-02T03:04:05Z
  - id: mnopqrstuvw7
    name: my lib
    path: /work/lib
    added_at: 2026-02-03T04:05:06Z
`

func TestRoundTripIsByteIdentical(t *testing.T) {
	for _, in := range []string{canonicalYAML, "schema: 1\nprojects: []\n"} {
		c, err := Decode([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate(c); err != nil {
			t.Fatal(err)
		}
		out, err := Encode(c)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != in {
			t.Errorf("round trip changed the bytes:\n%s\nwant\n%s", out, in)
		}
	}
	if out, err := Encode(Config{Schema: Schema}); err != nil || string(out) != "schema: 1\nprojects: []\n" {
		t.Errorf("empty config: %q, %v", out, err)
	}
}

func TestDecodeAndValidateRefusals(t *testing.T) {
	entry := "  - id: abcdefghijk2\n    name: app\n    path: /work/app\n    added_at: 2026-01-02T03:04:05Z\n"
	cases := map[string]string{
		"empty":            "",
		"unknown key":      "schema: 1\nprojects: []\nnotify: []\n",
		"unknown entry":    "schema: 1\nprojects:\n" + entry + "    colour: red\n",
		"other schema":     "schema: 2\nprojects: []\n",
		"no schema":        "projects: []\n",
		"two documents":    "schema: 1\nprojects: []\n---\nschema: 1\n",
		"not yaml":         "schema: [\n",
		"bad id":           "schema: 1\nprojects:\n  - id: ABC\n    name: app\n    path: /work/app\n    added_at: 2026-01-02T03:04:05Z\n",
		"relative path":    "schema: 1\nprojects:\n  - id: abcdefghijk2\n    name: app\n    path: work/app\n    added_at: 2026-01-02T03:04:05Z\n",
		"unclean path":     "schema: 1\nprojects:\n  - id: abcdefghijk2\n    name: app\n    path: /work/../app\n    added_at: 2026-01-02T03:04:05Z\n",
		"no added_at":      "schema: 1\nprojects:\n  - id: abcdefghijk2\n    name: app\n    path: /work/app\n",
		"duplicate entry":  "schema: 1\nprojects:\n" + entry + entry,
		"duplicate name":   "schema: 1\nprojects:\n" + entry + strings.NewReplacer("abcdefghijk2", "mnopqrstuvw7", "/work/app", "/work/b").Replace(entry),
		"duplicate path":   "schema: 1\nprojects:\n" + entry + strings.NewReplacer("abcdefghijk2", "mnopqrstuvw7", "name: app", "name: b").Replace(entry),
		"name is other id": "schema: 1\nprojects:\n" + entry + strings.NewReplacer("abcdefghijk2", "mnopqrstuvw7", "name: app", "name: abcdefghijk2", "/work/app", "/work/b").Replace(entry),
	}
	for name, in := range cases {
		c, err := Decode([]byte(in))
		if err == nil {
			err = Validate(c)
		}
		if !errors.Is(err, ErrConfig) {
			t.Errorf("%s: got %v, want a configuration error", name, err)
		}
	}
}

// A broken file is reported and never overwritten.
func TestBrokenFileIsKept(t *testing.T) {
	r, root := newRegistry(t)
	if err := os.MkdirAll(filepath.Dir(r.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	broken := []byte("schema: 1\nprojects: []\nextra: true\n")
	if err := os.WriteFile(r.Path(), broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.List(); !errors.Is(err, ErrConfig) {
		t.Fatalf("list: %v", err)
	}
	if _, err := r.Add(AddRequest{Path: mkdir(t, filepath.Join(root, "x"))}); !errors.Is(err, ErrConfig) {
		t.Fatalf("add: %v", err)
	}
	if data, _ := os.ReadFile(r.Path()); string(data) != string(broken) {
		t.Fatalf("the broken file was changed: %q", data)
	}
}

// A change of the file that bypasses the lock while the registry saves makes the save fail; nothing of the
// registry's change is written.
func TestParallelChangeConflicts(t *testing.T) {
	r, root := newRegistry(t)
	if _, err := r.Add(AddRequest{Path: mkdir(t, filepath.Join(root, "one"))}); err != nil {
		t.Fatal(err)
	}
	handEdit := []byte(strings.Replace(canonicalYAML, "my lib", "renamed", 1))
	r.newID = func() (string, error) {
		if err := os.WriteFile(r.Path(), handEdit, 0o600); err != nil {
			return "", err
		}
		return NewID()
	}
	_, err := r.Add(AddRequest{Path: mkdir(t, filepath.Join(root, "two"))})
	var conflict *configfile.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("add during a hand edit: %v", err)
	}
	if data, _ := os.ReadFile(r.Path()); string(data) != string(handEdit) {
		t.Fatalf("the hand edit was overwritten:\n%s", data)
	}
}
