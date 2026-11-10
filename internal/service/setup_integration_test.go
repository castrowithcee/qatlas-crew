//go:build integration

package service

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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

	if d := res.Dependencies[2]; d.Name != setup.Codex || d.Integration != control.IntegrationMissing || res.Dependencies[1].Integration != control.IntegrationNotNeeded || res.Dependencies[0].Integration != "" {
		t.Errorf("integration: %+v", res.Dependencies)
	}

	// The preview of the Codex hooks writes nothing; the Codex home is the one below the test home.
	var prev control.SetupIntegrationResult
	if resp := c.callWith(t, control.OpSetupIntegration, control.SetupIntegrationArgs{}); !resp.OK || json.Unmarshal(resp.Result, &prev) != nil {
		t.Fatalf("preview: %+v", resp)
	}
	hooks := filepath.Join(r.home, ".codex", "hooks.json")
	if prev.Empty || !strings.Contains(prev.Preview, "+++ "+hooks) || !strings.Contains(prev.Preview, " hook codex") {
		t.Errorf("preview: %+v", prev)
	}
	if _, err := os.Stat(filepath.Dir(hooks)); err == nil {
		t.Errorf("the preview created the Codex home")
	}
	prev = control.SetupIntegrationResult{}
	if resp := c.callWith(t, control.OpSetupIntegration, control.SetupIntegrationArgs{Remove: true}); !resp.OK || json.Unmarshal(resp.Result, &prev) != nil || !prev.Empty || prev.Preview != "" {
		t.Errorf("remove preview: %+v %+v", resp, prev)
	}

	// Arguments the operation does not take are refused.
	if resp := c.callWith(t, control.OpSetupCheck, map[string]int{"x": 1}); resp.OK || resp.Code != control.CodeInvalidRequest {
		t.Errorf("unexpected arguments: %+v", resp)
	}
}
