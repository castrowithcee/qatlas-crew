//go:build integration

package integration

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// testConfig points every path into the test's temporary home. The paths come from the test environment
// itself, never from the process environment, so a failing guard cannot reach real files.
func testConfig(t *testing.T) Config {
	t.Helper()
	e := testenv.Setup(t)
	c := Config{
		CodexHome: filepath.Join(e.Home, ".codex"),
		DataDir:   filepath.Join(e.Home, ".qatlas", "qcrew"),
		Program:   filepath.Join(e.Root, "bin", "qcrew"),
	}
	for _, p := range []string{c.CodexHome, c.DataDir} {
		if !strings.HasPrefix(p, e.Root+string(filepath.Separator)) {
			t.Fatalf("%s is outside the test directory", p)
		}
	}
	return c
}

func writeFile(t *testing.T, path, data string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) (string, fs.FileMode) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), fi.Mode().Perm()
}

func apply(t *testing.T, c Config, a Action, now time.Time) *Change {
	t.Helper()
	ch, err := c.Plan(a)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if err := ch.Apply(now); err != nil {
		t.Fatalf("apply: %v", err)
	}
	return ch
}

func status(t *testing.T, c Config) Status {
	t.Helper()
	r, err := c.Inspect()
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	return r.Status
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestApplyKeepsForeignContent(t *testing.T) {
	c := testConfig(t)
	if err := os.Mkdir(c.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	conf := strings.ReplaceAll(foreignConfig, "/home/u/.codex", c.CodexHome)
	writeFile(t, c.configPath(), conf, 0o640)
	writeFile(t, c.hooksPath(), foreignHooks, 0o644)
	if s := status(t, c); s != Missing {
		t.Fatalf("status %s before setup", s)
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	ch := apply(t, c, Setup, now)
	if !strings.Contains(ch.Preview(), "+++ "+c.hooksPath()) {
		t.Errorf("preview:\n%s", ch.Preview())
	}
	gotConf, confMode := readFile(t, c.configPath())
	gotHooks, hooksMode := readFile(t, c.hooksPath())
	if !strings.HasPrefix(gotConf, conf) || !isInsertion(foreignHooks, gotHooks) {
		t.Errorf("foreign content changed:\n%s\n%s", gotHooks, gotConf)
	}
	if confMode != 0o640 || hooksMode != 0o644 {
		t.Errorf("modes %o %o", confMode, hooksMode)
	}
	if s := status(t, c); s != Configured {
		t.Fatalf("status %s after setup", s)
	}
	if got := names(t, c.CodexHome); !slices.Equal(got, []string{ConfigFile, HooksFile}) {
		t.Errorf("Codex home holds %v", got)
	}

	apply(t, c, Remove, now.Add(time.Second))
	gotConf, confMode = readFile(t, c.configPath())
	gotHooks, hooksMode = readFile(t, c.hooksPath())
	if gotConf != conf || gotHooks != foreignHooks || confMode != 0o640 || hooksMode != 0o644 {
		t.Errorf("remove does not restore the files:\n%s\n%s", gotHooks, gotConf)
	}

	// Six more rounds make fourteen backups of each file; the newest ten stay, in the test home.
	for i := range 12 {
		a := Setup
		if i%2 == 1 {
			a = Remove
		}
		apply(t, c, a, now.Add(time.Duration(i+2)*time.Second))
	}
	backups := filepath.Join(c.DataDir, "backups", "codex")
	got := names(t, backups)
	for _, f := range []string{HooksFile, ConfigFile} {
		n := 0
		for _, name := range got {
			if strings.HasSuffix(name, "."+f) {
				n++
			}
		}
		if n != keepBackups {
			t.Errorf("%d backups of %s: %v", n, f, got)
		}
	}
	if newest := "20260102T030418.000000006Z." + ConfigFile; !slices.Contains(got, newest) {
		t.Errorf("newest backup %s missing: %v", newest, got)
	}
	data, mode := readFile(t, filepath.Join(backups, got[len(got)-1]))
	if mode != 0o600 || data == "" {
		t.Errorf("backup %s: mode %o, %d bytes", got[len(got)-1], mode, len(data))
	}
}

func TestApplyCreatesAndDeletesFiles(t *testing.T) {
	c := testConfig(t)
	apply(t, c, Setup, time.Unix(1, 0))
	fi, err := os.Stat(c.CodexHome)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("Codex home: %v %v", fi, err)
	}
	for _, p := range []string{c.hooksPath(), c.configPath()} {
		if _, mode := readFile(t, p); mode != 0o600 {
			t.Errorf("%s: mode %o", p, mode)
		}
	}
	if s := status(t, c); s != Configured {
		t.Fatalf("status %s", s)
	}
	apply(t, c, Remove, time.Unix(2, 0))
	if got := names(t, c.CodexHome); len(got) != 0 {
		t.Errorf("Codex home holds %v", got)
	}
	if s := status(t, c); s != Missing {
		t.Errorf("status %s", s)
	}
}

func TestApplyRefusesFilesChangedSincePreview(t *testing.T) {
	c := testConfig(t)
	if err := os.Mkdir(c.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, c.configPath(), "a = 1\n", 0o600)
	ch, err := c.Plan(Setup)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, c.configPath(), "a = 2\n", 0o600)
	if err := ch.Apply(time.Unix(1, 0)); err == nil {
		t.Fatal("no error")
	}
	if got, _ := readFile(t, c.configPath()); got != "a = 2\n" {
		t.Errorf("config.toml changed: %q", got)
	}
	if _, err := os.Stat(c.hooksPath()); err == nil {
		t.Errorf("hooks.json written")
	}
}

func TestPlanRefusesSymlinks(t *testing.T) {
	c := testConfig(t)
	if err := os.Mkdir(c.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(c.CodexHome), "elsewhere.json")
	writeFile(t, target, "{}", 0o600)
	if err := os.Symlink(target, c.hooksPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Plan(Setup); err == nil {
		t.Fatal("no error")
	}
	if got, _ := readFile(t, target); got != "{}" {
		t.Errorf("target changed: %q", got)
	}
}

func TestConfigNeedsAbsolutePaths(t *testing.T) {
	c := testConfig(t)
	for _, bad := range []Config{
		{CodexHome: ".codex", DataDir: c.DataDir, Program: c.Program},
		{CodexHome: c.CodexHome + "/../x", DataDir: c.DataDir, Program: c.Program},
		{CodexHome: c.CodexHome, DataDir: "", Program: c.Program},
		{CodexHome: c.CodexHome, DataDir: c.DataDir, Program: "qcrew"},
	} {
		if _, err := bad.Plan(Setup); err == nil {
			t.Errorf("%+v: no error", bad)
		}
	}
}
