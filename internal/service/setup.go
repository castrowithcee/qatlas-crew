package service

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/setup"
)

// addSetupOps adds the operation that reports which programs qcrew needs are installed. It only looks;
// installing needs a terminal and stays with the command line.
func (s *server) addSetupOps() {
	s.ops[control.OpSetupCheck] = func(ctx context.Context, args json.RawMessage) (any, error) {
		if err := decodeArgs(args, &struct{}{}); err != nil {
			return nil, err
		}
		found := setup.Detect(ctx, s.setupEnv)
		out := control.SetupCheckResult{Dependencies: make([]control.SetupDependency, 0, len(found))}
		for _, st := range found {
			out.Dependencies = append(out.Dependencies, control.SetupDependency{
				Name: st.Name, Installed: st.Installed, Version: st.Version,
				Install: st.Plan.Display(), Sudo: st.Plan.Sudo(), Source: st.Source,
			})
		}
		return out, nil
	}
}
