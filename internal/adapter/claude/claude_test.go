package claude

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const (
	testID   = "abcdefghij23"
	testUUID = "123e4567-e89b-42d3-a456-426614174000"
)

func session(t *testing.T, program string) adapter.Session {
	t.Helper()
	return adapter.Session{ID: testID, Dir: t.TempDir(), DataDir: t.TempDir(), Program: program, AgentSession: testUUID}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (create it with just snap-update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs:\n--- got\n%s--- want\n%s", path, got, want)
	}
}

func TestSettingsGolden(t *testing.T) {
	s := adapter.Session{ID: testID, Program: "/opt/qcrew/bin/qcrew"}
	spaced := adapter.Session{ID: testID, Program: "/home/a b/it's/qcrew"}
	for _, c := range []struct {
		name       string
		s          adapter.Session
		statusLine bool
	}{{"settings-statusline", s, true}, {"settings-no-statusline", s, false}, {"settings-spaces", spaced, true}} {
		got, err := Settings(c.s, c.statusLine)
		if err != nil {
			t.Fatal(err)
		}
		golden(t, c.name+".json", got)
	}
}

// The hook command must run the program word for word when a shell reads it.
func TestHookCommandQuoting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell")
	}
	dir := filepath.Join(t.TempDir(), "a b", "it's")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, "qcrew")
	if err := os.WriteFile(prog, []byte("#!/bin/sh\nprintf '%s|' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := Settings(adapter.Session{ID: testID, Program: prog}, true)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/bin/sh", "-c", parsed.Hooks["Stop"][0].Hooks[0].Command).Output()
	if err != nil || string(out) != "hook|claude|--session|"+testID+"|" {
		t.Fatalf("hook command printed %q, %v", out, err)
	}
}

func TestCommand(t *testing.T) {
	s := session(t, "/q")
	s.Args = []string{"--model", "x"}
	want := []string{"claude", "--session-id", testUUID, "--prompt-suggestions", "false", "--settings",
		filepath.Join(s.DataDir, "sessions", testID, "claude-settings.json"), "--model", "x"}
	if got := (Adapter{}).Command(s); !slices.Equal(got, want) {
		t.Errorf("Command = %q, want %q", got, want)
	}
}

func TestPrepareWithoutKnownHomeOmitsStatusLine(t *testing.T) {
	s := session(t, "/q")
	if err := (Adapter{Getenv: func(string) string { return "" }}).Prepare(s); err != nil {
		t.Fatal(err)
	}
	want, _ := Settings(s, false)
	if got, _ := os.ReadFile(SettingsPath(s)); !bytes.Equal(got, want) {
		t.Errorf("settings: %s", got)
	}
}

func TestPrepareWritesPrivateFile(t *testing.T) {
	s := session(t, "/q")
	home := t.TempDir()
	a := Adapter{Getenv: func(k string) string { return map[string]string{"HOME": home}[k] }}
	if err := a.Prepare(s); err != nil {
		t.Fatal(err)
	}
	dir := SessionDir(s)
	di, err := os.Stat(dir)
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("session folder: %v, %v", di, err)
	}
	fi, err := os.Stat(SettingsPath(s))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("settings file: %v, %v", fi, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("the folder holds more than the settings file: %v", entries)
	}
	want, _ := Settings(s, true)
	if got, _ := os.ReadFile(SettingsPath(s)); !bytes.Equal(got, want) {
		t.Errorf("settings: %s", got)
	}
	a.Cleanup(s)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("Cleanup left %s: %v", dir, err)
	}
	a.Cleanup(s) // nothing left is no error
}

func TestPrepareRefusals(t *testing.T) {
	good := session(t, "/q")
	a := Adapter{Getenv: func(string) string { return "/x" }}
	for name, mod := range map[string]func(*adapter.Session){
		"id":         func(s *adapter.Session) { s.ID = "../../etc" },
		"data":       func(s *adapter.Session) { s.DataDir = "rel" },
		"program":    func(s *adapter.Session) { s.Program = "qcrew" },
		"agent uuid": func(s *adapter.Session) { s.AgentSession = "" },
	} {
		s := good
		mod(&s)
		if err := a.Prepare(s); err == nil {
			t.Errorf("%s: Prepare accepted %+v", name, s)
		}
		a.Cleanup(s) // must not remove anything outside the data directory
	}
	if _, err := os.Stat(good.DataDir); err != nil {
		t.Fatal(err)
	}
}

func TestHasStatusLine(t *testing.T) {
	write := func(t *testing.T, path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	str := func(v string) *string { return &v }
	type files struct{ user, project, local *string }
	cases := []struct {
		name string
		f    files
		want bool
	}{
		{"none", files{}, false},
		{"other keys", files{str(`{"theme":"dark"}`), str(`{"model":"x"}`), str(`{}`)}, false},
		{"null", files{user: str(`{"statusLine":null}`)}, false},
		{"user", files{user: str(`{"statusLine":{"type":"command","command":"x"}}`)}, true},
		{"project", files{project: str(`{"statusLine":{"type":"command","command":"x"}}`)}, true},
		{"local", files{local: str(`{"statusLine":"x"}`)}, true},
		{"user invalid", files{user: str(`{`)}, true},
		{"project empty", files{project: str(``)}, true},
		{"local array", files{local: str(`[]`)}, true},
		{"user null document", files{user: str(`null`)}, true},
	}
	for _, c := range cases {
		home, proj := t.TempDir(), t.TempDir()
		for path, content := range map[string]*string{
			filepath.Join(home, ".claude", "settings.json"):       c.f.user,
			filepath.Join(proj, ".claude", "settings.json"):       c.f.project,
			filepath.Join(proj, ".claude", "settings.local.json"): c.f.local,
		} {
			if content != nil {
				write(t, path, *content)
			}
		}
		getenv := func(k string) string { return map[string]string{"HOME": home}[k] }
		if got := hasStatusLine(getenv, proj); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHasStatusLineLocations(t *testing.T) {
	cfg, home, proj := t.TempDir(), t.TempDir(), t.TempDir()
	withLine := `{"statusLine":{"type":"command","command":"x"}}`
	if err := os.WriteFile(filepath.Join(cfg, "settings.json"), []byte(withLine), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOME": home, "CLAUDE_CONFIG_DIR": cfg}
	getenv := func(k string) string { return env[k] }
	if !hasStatusLine(getenv, proj) {
		t.Error("CLAUDE_CONFIG_DIR is not read")
	}
	// With CLAUDE_CONFIG_DIR set, the home directory's file does not count.
	env["CLAUDE_CONFIG_DIR"] = t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(withLine), 0o644); err != nil {
		t.Fatal(err)
	}
	if hasStatusLine(getenv, proj) {
		t.Error("the home settings counted although CLAUDE_CONFIG_DIR names another directory")
	}
	// Unknown locations, a directory instead of a file and a relative CLAUDE_CONFIG_DIR leave the line alone.
	for name, e := range map[string]map[string]string{
		"no home":  {},
		"relative": {"CLAUDE_CONFIG_DIR": "rel"},
	} {
		if !hasStatusLine(func(k string) string { return e[k] }, proj) {
			t.Errorf("%s: the status line would be overridden blind", name)
		}
	}
	if err := os.MkdirAll(filepath.Join(proj, ".claude", "settings.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	env["CLAUDE_CONFIG_DIR"] = ""
	env["HOME"] = t.TempDir()
	if !hasStatusLine(getenv, proj) {
		t.Error("an unreadable settings file did not count as the user's")
	}
}
