//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

type projectEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

func TestProjects(t *testing.T) {
	e := testenv.Setup(t)
	env := testenv.WithRuntime(e.Environ(), testenv.ShortRuntime(t))
	sh := func(args ...string) result { return runIn(t, env, e.Home, args...) }
	t.Cleanup(func() {
		r := sh("service", "status", "--output", "json")
		var st serviceStatus
		_ = json.Unmarshal([]byte(r.stdout), &st)
		sh("service", "stop")
		if st.PID > 0 && !waitFor(func() bool { return testenv.ProcessGone(st.PID) }) {
			t.Errorf("service process %d is still running", st.PID)
		}
	})

	dir := filepath.Join(e.Root, "x")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r := sh("project", "add", dir); r.code != 0 || r.stderr != "" {
		t.Fatalf("add: code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
	// A relative path counts from the directory of the command, here the home directory.
	if err := os.Mkdir(filepath.Join(e.Home, "rel"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := sh("project", "add", "rel", "--name", "relative"); r.code != 0 {
		t.Fatalf("add relative: code=%d stderr=%q", r.code, r.stderr)
	}

	r := sh("project", "list", "--output", "json")
	var list []projectEntry
	if err := json.Unmarshal([]byte(r.stdout), &list); err != nil || r.code != 0 || len(list) != 2 {
		t.Fatalf("list: code=%d stdout=%q: %v", r.code, r.stdout, err)
	}
	wantRel, _ := filepath.EvalSymlinks(filepath.Join(e.Home, "rel"))
	if list[0].Path != want || list[0].Name != "x" || len(list[0].ID) != 12 || list[1].Path != wantRel {
		t.Fatalf("list entries: %+v", list)
	}
	r = sh("project", "list")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "ID ") || !strings.Contains(r.stdout, want) {
		t.Fatalf("list table: %q", r.stdout)
	}

	config := filepath.Join(e.Home, ".qatlas", "qcrew", "config.yaml")
	info, err := os.Stat(config)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config.yaml: %v, %v", info, err)
	}
	data, err := os.ReadFile(config)
	if err != nil || !strings.HasPrefix(string(data), "schema: 1\nprojects:\n") || !strings.Contains(string(data), "path: "+want+"\n") {
		t.Fatalf("config.yaml content: %q, %v", data, err)
	}
	backups, err := os.ReadDir(filepath.Join(filepath.Dir(config), "backups"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("backups: %v, %v", backups, err)
	}

	r = sh("project", "add", dir)
	if r.code != 2 || !strings.HasPrefix(r.stderr, "qcrew: already-exists: ") {
		t.Fatalf("second add: code=%d stderr=%q", r.code, r.stderr)
	}

	r = sh("project", "remove", "x")
	if r.code != 2 || !strings.Contains(r.stderr, "confirmation-required") || !strings.Contains(r.stderr, "repeat this request with --confirm") {
		t.Fatalf("remove without --confirm: code=%d stderr=%q", r.code, r.stderr)
	}
	if r = sh("project", "remove", "x", "--confirm", "--output", "json"); r.code != 0 || !strings.Contains(r.stdout, `"name":"x"`) {
		t.Fatalf("remove: code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("remove touched the directory: %v", err)
	}
	r = sh("project", "list", "--output", "json")
	if err := json.Unmarshal([]byte(r.stdout), &list); err != nil || len(list) != 1 || list[0].Name != "relative" {
		t.Fatalf("list after remove: %q, %v", r.stdout, err)
	}
}
