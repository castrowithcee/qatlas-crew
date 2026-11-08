//go:build integration

package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/control"
)

func (c *conn) callWith(t *testing.T, op string, args any) control.Response {
	t.Helper()
	req, err := control.EncodeRequest(op, args)
	if err != nil {
		t.Fatal(err)
	}
	return c.send(t, string(req))
}

func TestProjectOperations(t *testing.T) {
	r := start(t, Options{})
	c := r.dial(t)
	dir := filepath.Join(r.home, "work", "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	resp := c.call(t, control.OpStatus)
	var st control.StatusResult
	if err := json.Unmarshal(resp.Result, &st); err != nil || st.Config != filepath.Join(control.DataDir(r.home), "config.yaml") {
		t.Fatalf("status: %+v, %v", st, err)
	}

	resp = c.callWith(t, control.OpProjectAdd, control.ProjectAddArgs{Path: dir})
	var added control.Project
	if err := json.Unmarshal(resp.Result, &added); err != nil || !resp.OK || added.Path != want || added.Name != "app" {
		t.Fatalf("add: %+v, %v", resp, err)
	}
	resp = c.callWith(t, control.OpProjectList, nil)
	var list control.ProjectListResult
	if err := json.Unmarshal(resp.Result, &list); err != nil || len(list.Projects) != 1 || list.Projects[0] != added {
		t.Fatalf("list: %+v, %v", resp, err)
	}
	resp = c.callWith(t, control.OpProjectGet, control.ProjectRefArgs{Ref: added.ID})
	var got control.Project
	if err := json.Unmarshal(resp.Result, &got); err != nil || got != added {
		t.Fatalf("get: %+v, %v", resp, err)
	}

	refusals := []struct {
		op   string
		args any
		code string
	}{
		{control.OpProjectAdd, control.ProjectAddArgs{Path: dir}, control.CodeAlreadyExists},
		{control.OpProjectAdd, control.ProjectAddArgs{Path: "work/app"}, control.CodeInvalidArgument},
		{control.OpProjectAdd, control.ProjectAddArgs{Path: filepath.Join(r.home, "missing")}, control.CodeInvalidArgument},
		{control.OpProjectAdd, nil, control.CodeInvalidArgument},
		{control.OpProjectAdd, map[string]any{"path": dir, "colour": "red"}, control.CodeInvalidRequest},
		{control.OpProjectGet, control.ProjectRefArgs{Ref: "nope"}, control.CodeNotFound},
		{control.OpProjectRemove, control.ProjectRefArgs{}, control.CodeInvalidArgument},
	}
	for _, tc := range refusals {
		if resp := c.callWith(t, tc.op, tc.args); resp.OK || resp.Code != tc.code || resp.Message == "" {
			t.Errorf("%s %v: %+v, want code %s", tc.op, tc.args, resp, tc.code)
		}
	}

	resp = c.callWith(t, control.OpProjectRemove, control.ProjectRefArgs{Ref: "app"})
	if err := json.Unmarshal(resp.Result, &got); err != nil || got != added {
		t.Fatalf("remove: %+v, %v", resp, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("remove touched the directory: %v", err)
	}

	// A hand edit that breaks the file is reported with its reason, not swallowed.
	if err := os.WriteFile(st.Config, []byte("schema: 7\nprojects: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resp := c.callWith(t, control.OpProjectList, nil); resp.OK || resp.Code != control.CodeConfigInvalid {
		t.Fatalf("list of a broken file: %+v", resp)
	}
}

func TestServiceUsesTheGivenConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "elsewhere", "qcrew.yaml")
	r := start(t, Options{Config: path})
	c := r.dial(t)
	if err := os.MkdirAll(filepath.Join(r.home, "p"), 0o755); err != nil {
		t.Fatal(err)
	}
	if resp := c.callWith(t, control.OpProjectAdd, control.ProjectAddArgs{Path: filepath.Join(r.home, "p")}); !resp.OK {
		t.Fatalf("add: %+v", resp)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the given configuration file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(control.DataDir(r.home), "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("the default file was written: %v", err)
	}
}
