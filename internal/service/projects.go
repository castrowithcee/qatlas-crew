package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/castrowithcee/qatlas-kit/configfile"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/project"
)

func (s *server) addProjectOps() {
	s.ops[control.OpProjectList] = func(_ context.Context, args json.RawMessage) (any, error) {
		if err := decodeArgs(args, &struct{}{}); err != nil {
			return nil, err
		}
		list, err := s.projects.List()
		if err != nil {
			return nil, projectError(err)
		}
		out := control.ProjectListResult{Projects: make([]control.Project, 0, len(list))}
		for _, p := range list {
			out.Projects = append(out.Projects, wire(p))
		}
		return out, nil
	}
	s.ops[control.OpProjectGet] = func(_ context.Context, args json.RawMessage) (any, error) {
		var a control.ProjectRefArgs
		if err := decodeRef(args, &a); err != nil {
			return nil, err
		}
		p, err := s.projects.Get(a.Ref)
		if err != nil {
			return nil, projectError(err)
		}
		return wire(p), nil
	}
	s.ops[control.OpProjectAdd] = func(_ context.Context, args json.RawMessage) (any, error) {
		var a control.ProjectAddArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		if a.Path == "" {
			return nil, &control.Error{Code: control.CodeInvalidArgument, Message: "the path is missing"}
		}
		p, err := s.projects.Add(project.AddRequest{Path: a.Path, Name: a.Name, Create: a.Create})
		if err != nil {
			return nil, projectError(err)
		}
		s.log.Info("project added", "id", p.ID, "path", p.Path)
		return wire(p), nil
	}
	s.ops[control.OpProjectRemove] = func(_ context.Context, args json.RawMessage) (any, error) {
		var a control.ProjectRefArgs
		if err := decodeRef(args, &a); err != nil {
			return nil, err
		}
		p, err := s.projects.Remove(a.Ref)
		if err != nil {
			return nil, projectError(err)
		}
		s.log.Info("project removed", "id", p.ID, "path", p.Path)
		return wire(p), nil
	}
}

func wire(p project.Project) control.Project {
	return control.Project{ID: p.ID, Name: p.Name, Path: p.Path, AddedAt: p.AddedAt}
}

// decodeArgs reads the arguments strictly into v; absent arguments leave v as it is.
func decodeArgs(args json.RawMessage, v any) error {
	if len(args) == 0 || string(bytes.TrimSpace(args)) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &control.Error{Code: control.CodeInvalidRequest, Message: "the arguments do not fit the operation"}
	}
	return nil
}

func decodeRef(args json.RawMessage, a *control.ProjectRefArgs) error {
	if err := decodeArgs(args, a); err != nil {
		return err
	}
	if a.Ref == "" {
		return &control.Error{Code: control.CodeInvalidArgument, Message: "the project id or name is missing"}
	}
	return nil
}

// projectError gives a refusal of the project registry its code. Anything else is a runtime error whose
// text stays in the log.
func projectError(err error) error {
	var refusal *project.Error
	if errors.As(err, &refusal) {
		switch refusal.Kind {
		case project.ErrInvalid:
			return &control.Error{Code: control.CodeInvalidArgument, Message: refusal.Message}
		case project.ErrExists:
			return &control.Error{Code: control.CodeAlreadyExists, Message: refusal.Message}
		case project.ErrNotFound:
			return &control.Error{Code: control.CodeNotFound, Message: refusal.Message}
		}
	}
	switch {
	case errors.Is(err, project.ErrConfig):
		return &control.Error{Code: control.CodeConfigInvalid, Message: err.Error()}
	case errors.Is(err, configfile.ErrConflict):
		return &control.Error{Code: control.CodeConflict, Message: "the configuration file changed while it was saved; nothing was written"}
	}
	return err
}
