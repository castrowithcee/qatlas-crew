// Package codex is the adapter of Codex. It starts the tool with session flags only: its hooks, once the user
// has set them up, and its notify program and bell notifications, which stand in for them, report the status
// of the session to qcrew. It never writes to the tool's home directory.
package codex

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/control"
)

// SessionEnv names the variable that tells a hook which qcrew session its tool belongs to. Only the tool's
// process gets it, so a hook finds its session without the hook definition naming one.
const SessionEnv = "QCREW_SESSION"

// Adapter starts Codex.
type Adapter struct{}

// Name is the tool.
func (Adapter) Name() string { return adapter.Codex }

var (
	modelName  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,63}$`)
	effortName = regexp.MustCompile(`^[a-z]{1,16}$`)
)

// notifications are the prompts that ring the terminal bell: a permission, a plan-mode question and an
// asynchronous question. The end of a turn does not ring; notify reports it.
const notifications = `["approval-requested","plan-mode-prompt","async-question"]`

// Command starts the tool without its daemon, so that it keeps the environment of its window, with the model
// and the reasoning effort if the session names them, and with the flags of the fallback: the notify program
// for the end of a turn, and the bell for prompts. With no program path the notify flag is left out; Prepare
// refuses such a session.
func (Adapter) Command(s adapter.Session) []string {
	cmd := []string{adapter.Codex, "--no-daemon"}
	if s.Model != "" {
		cmd = append(cmd, "-m", s.Model)
	}
	if s.Effort != "" {
		cmd = append(cmd, "-c", "model_reasoning_effort="+tomlString(s.Effort))
	}
	if s.Program != "" {
		cmd = append(cmd, "-c", "notify=["+tomlString(s.Program)+`,"hook","codex","--notify"]`)
	}
	cmd = append(cmd, "-c", "tui.notifications="+notifications, "-c", `tui.notification_method="bel"`,
		"-c", `tui.notification_condition="always"`)
	return append(cmd, s.Args...)
}

// Env gives the tool's process the id of its qcrew session.
func (Adapter) Env(s adapter.Session) []string { return []string{SessionEnv + "=" + s.ID} }

// Prepare checks what the command line needs. It creates nothing.
func (Adapter) Prepare(s adapter.Session) error {
	switch {
	case !control.ValidSessionID(s.ID):
		return errors.New("the session id is not valid")
	case !filepath.IsAbs(s.Program):
		return errors.New("the path of the qcrew program is not known")
	case s.Model != "" && !modelName.MatchString(s.Model):
		return errors.New("the model name is not valid")
	case s.Effort != "" && !effortName.MatchString(s.Effort):
		return errors.New("the reasoning effort is not valid")
	}
	return nil
}

// Cleanup does nothing: the adapter keeps no files.
func (Adapter) Cleanup(adapter.Session) {}

// tomlString quotes s as a TOML basic string, the form a -c value of the tool is read in.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case unicode.IsControl(r):
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
