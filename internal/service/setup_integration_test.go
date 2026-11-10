//go:build integration

package service

import (
	"context"
	"encoding/json"
	"os/exec"
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/setup"
)

func TestSetupCheckOperation(t *testing.T) {
	found := []string{setup.Tmux, "dnf"}
	r := start(t, Options{setupEnv: setup.Env{
		GOOS: "linux",
		LookPath: func(name string) (string, error) {
			if slices.Contains(found, name) {
				return "/fake/" + name, nil
			}
			return "", exec.ErrNotFound
		},
		Version: func(context.Context, string, []string) (string, error) { return "tmux 3.4\n", nil },
	}})
	c := r.dial(t)

	resp := c.callWith(t, control.OpSetupCheck, nil)
	var res control.SetupCheckResult
	if err := json.Unmarshal(resp.Result, &res); err != nil || !resp.OK || len(res.Dependencies) != 3 {
		t.Fatalf("check: %+v, %v", resp, err)
	}
	if d := res.Dependencies[0]; d.Name != setup.Tmux || !d.Installed || d.Version != "tmux 3.4" || d.Install != "" {
		t.Errorf("tmux: %+v", d)
	}
	d := res.Dependencies[1]
	if d.Name != setup.Claude || d.Installed || d.Install != "curl -fsSL https://claude.ai/install.sh | bash" || d.Sudo || d.Source == "" {
		t.Errorf("claude: %+v", d)
	}

	// Arguments the operation does not take are refused.
	if resp := c.callWith(t, control.OpSetupCheck, map[string]int{"x": 1}); resp.OK || resp.Code != control.CodeInvalidRequest {
		t.Errorf("unexpected arguments: %+v", resp)
	}
}
