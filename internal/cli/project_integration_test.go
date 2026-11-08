//go:build integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type projectJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

func TestProjectCommandsAgainstTheService(t *testing.T) {
	env := serviceTestEnv(t)
	dir := filepath.Join(env.Root, "work", "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	// The first command starts the service on demand.
	code, out, errOut := run(t, "project", "add", dir, "--output", "json")
	var added projectJSON
	if code != 0 || json.Unmarshal([]byte(out), &added) != nil || added.Path != want || added.Name != "x" {
		t.Fatalf("add: code=%d out=%q err=%q", code, out, errOut)
	}
	if st := statusOf(t); !st.Running {
		t.Fatal("add did not start the service")
	}

	code, out, errOut = run(t, "project", "list", "--output", "json")
	var list []projectJSON
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || len(list) != 1 || list[0] != added {
		t.Fatalf("list: code=%d out=%q err=%q", code, out, errOut)
	}

	code, _, errOut = run(t, "project", "add", dir, "--name", "again")
	if code != 2 || !strings.HasPrefix(errOut, "qcrew: already-exists: ") || !strings.Contains(errOut, "qcrew project list") {
		t.Fatalf("second add: code=%d err=%q", code, errOut)
	}

	// Another configuration file than the running service's is refused, not served from the wrong file.
	code, _, errOut = run(t, "--config", filepath.Join(env.Root, "other.yaml"), "project", "list")
	if code != 1 || !strings.Contains(errOut, "config-mismatch") {
		t.Fatalf("other config: code=%d err=%q", code, errOut)
	}

	code, _, errOut = run(t, "project", "remove", added.ID, "--confirm")
	if code != 0 {
		t.Fatalf("remove: code=%d err=%q", code, errOut)
	}
	code, _, errOut = run(t, "project", "remove", added.ID, "--confirm")
	if code != 2 || !strings.Contains(errOut, "not-found") {
		t.Fatalf("second remove: code=%d err=%q", code, errOut)
	}

	config := filepath.Join(env.Home, ".qatlas", "qcrew", "config.yaml")
	info, err := os.Stat(config)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config file: %v, %v", info, err)
	}
	if backups, err := os.ReadDir(filepath.Join(filepath.Dir(config), "backups")); err != nil || len(backups) == 0 {
		t.Fatalf("backups: %v, %v", backups, err)
	}
}

// --config reaches the service the command starts, also as a path relative to the command's directory.
func TestProjectConfigReachesTheService(t *testing.T) {
	env := serviceTestEnv(t)
	t.Chdir(env.Root)
	if code, _, errOut := run(t, "--config", "custom.yaml", "project", "add", "--create", "made"); code != 0 {
		t.Fatalf("add: code=%d err=%q", code, errOut)
	}
	data, err := os.ReadFile(filepath.Join(env.Root, "custom.yaml"))
	if err != nil || !strings.HasPrefix(string(data), "schema: 1\n") || !strings.Contains(string(data), "name: made") {
		t.Fatalf("custom config: %q, %v", data, err)
	}
	if info, err := os.Stat(filepath.Join(env.Root, "made")); err != nil || !info.IsDir() {
		t.Fatalf("created directory: %v", err)
	}
}
