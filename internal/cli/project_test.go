package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// golden compares got with testdata/<name>.golden, or rewrites the file with -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
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

var sampleProjects = []control.Project{
	{ID: "abcdefghijk2", Name: "app", Path: "/work/app", AddedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), InboxOpen: 2},
	{ID: "mnopqrstuvw7", Name: "my library", Path: "/work/clients/lib", AddedAt: time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)},
}

func TestProjectOutputGolden(t *testing.T) {
	for _, format := range []output.Format{output.FormatTable, output.FormatJSON} {
		var list, empty, one bytes.Buffer
		if err := writeProjects(&list, format, sampleProjects); err != nil {
			t.Fatal(err)
		}
		if err := writeProjects(&empty, format, nil); err != nil {
			t.Fatal(err)
		}
		if err := output.Encode(&one, format, projectObject(sampleProjects[0])); err != nil {
			t.Fatal(err)
		}
		golden(t, "project-list."+string(format), list.Bytes())
		golden(t, "project-list-empty."+string(format), empty.Bytes())
		golden(t, "project-add."+string(format), one.Bytes())
	}
}

func TestConfigPathResolution(t *testing.T) {
	env := testenv.Setup(t)
	a := &serviceAccess{}
	check := func(want string) {
		t.Helper()
		if got, err := a.configPath(); err != nil || got != want {
			t.Fatalf("config path %q, %v; want %q", got, err, want)
		}
	}
	check(filepath.Join(env.Home, ".qatlas", "qcrew", "config.yaml"))
	t.Setenv("QCREW_DIR", filepath.Join(env.Root, "dir"))
	check(filepath.Join(env.Root, "dir", "config.yaml"))
	t.Setenv("QCREW_CONFIG", filepath.Join(env.Root, "file.yaml"))
	check(filepath.Join(env.Root, "file.yaml"))
	a.configFlag = "~/flag.yaml"
	check(filepath.Join(env.Home, "flag.yaml"))
	a.configFlag = "rel.yaml"
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	check(filepath.Join(wd, "rel.yaml"))
}

func TestProjectRemoveNeedsConfirm(t *testing.T) {
	testenv.Setup(t)
	testenv.ShortRuntime(t)
	code, out, errOut := run(t, "--no-autostart", "project", "remove", "app")
	if code != 2 || out != "" || !strings.Contains(errOut, "confirmation-required") || !strings.Contains(errOut, "--confirm") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestProjectAddNeedsAPath(t *testing.T) {
	code, _, errOut := run(t, "project", "add")
	if code != 2 || !strings.Contains(errOut, "usage") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}
