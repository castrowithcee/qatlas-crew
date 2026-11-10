package service

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/adapter/codex/integration"
	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/setup"
)

// maxPreview is the most bytes of a preview the channel carries; the command line shows the whole change
// before it writes.
const maxPreview = 48 << 10

// integrationEvery is how long an inspection of the Codex hooks stands for the session list, which is read
// every two seconds.
const integrationEvery = 2 * time.Second

// addSetupOps adds the operations that report which programs qcrew needs are installed and whether the hooks
// of Codex are in place, and that preview a change of the hooks. They only look; installing and writing need
// a terminal and stay with the command line.
func (s *server) addSetupOps() {
	s.ops[control.OpSetupCheck] = func(ctx context.Context, args json.RawMessage) (any, error) {
		if err := decodeArgs(args, &struct{}{}); err != nil {
			return nil, err
		}
		found := setup.Detect(ctx, s.setupEnv)
		out := control.SetupCheckResult{Dependencies: make([]control.SetupDependency, 0, len(found))}
		for _, st := range found {
			d := control.SetupDependency{
				Name: st.Name, Installed: st.Installed, Version: st.Version,
				Install: st.Plan.Display(), Sudo: st.Plan.Sudo(), Source: st.Source,
			}
			switch st.Name {
			case setup.Claude:
				d.Integration = control.IntegrationNotNeeded
			case setup.Codex:
				d.Integration, d.IntegrationProblems = s.inspectCodex()
			}
			out.Dependencies = append(out.Dependencies, d)
		}
		return out, nil
	}
	s.ops[control.OpSetupIntegration] = func(_ context.Context, args json.RawMessage) (any, error) {
		var a control.SetupIntegrationArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		action := integration.Setup
		if a.Remove {
			action = integration.Remove
		}
		cfg, err := s.codexConfig()
		var ch *integration.Change
		if err == nil {
			ch, err = cfg.Plan(action)
		}
		if err != nil {
			return nil, &control.Error{Code: control.CodeRuntime, Message: "the Codex hooks cannot be read: " + err.Error()}
		}
		preview := ch.Preview()
		if len(preview) > maxPreview {
			preview = preview[:maxPreview] + "\n...\n"
		}
		return control.SetupIntegrationResult{Preview: preview, Warnings: ch.Warnings, Empty: ch.Empty()}, nil
	}
}

// codexConfig is where the service looks for the Codex hooks: the Codex home of its own environment and the
// program the hooks of its sessions call.
func (s *server) codexConfig() (integration.Config, error) {
	codexHome, err := integration.CodexHome(os.Getenv, s.home)
	if err != nil {
		return integration.Config{}, err
	}
	program, err := s.program()
	if err != nil {
		return integration.Config{}, err
	}
	return integration.Config{CodexHome: codexHome, DataDir: control.DataDir(s.home), Program: program}, nil
}

// inspectCodex reports whether the Codex hooks are in place, with what is stale or why the state is unknown.
func (s *server) inspectCodex() (string, []string) {
	cfg, err := s.codexConfig()
	var r integration.Report
	if err == nil {
		r, err = cfg.Inspect()
	}
	if err != nil {
		return control.IntegrationUnknown, []string{err.Error()}
	}
	return string(r.Status), r.Problems
}

// integrationCache keeps the last inspection of the Codex hooks for a while.
type integrationCache struct {
	mu     sync.Mutex
	at     time.Time
	status string
}

// sessionIntegration is what a session of tool shows about the hooks it depends on: for Codex the state of
// its hooks while they are not in place, else nothing.
func (s *server) sessionIntegration(tool string) string {
	if tool != adapter.Codex || s.home == "" {
		return ""
	}
	c := &s.codex
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at.IsZero() || time.Since(c.at) >= integrationEvery {
		c.status, _ = s.inspectCodex()
		c.at = time.Now()
	}
	if c.status == control.IntegrationConfigured {
		return ""
	}
	return c.status
}
