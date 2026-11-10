// Package claude is the adapter of Claude Code. It starts the tool with a settings file of its own, so that
// the tool's hooks report the status of the session to qcrew, and never changes the user's or the project's
// settings.
package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-crew/internal/adapter"
	"github.com/castrowithcee/qatlas-crew/internal/control"
)

// SettingsFile is the name of the settings file in the session's folder.
const SettingsFile = "claude-settings.json"

// maxSettings bounds the settings files read to look for a status line; a larger file counts as the user's.
const maxSettings = 1 << 20

// hookTimeout is the seconds Claude Code waits for a hook. The hook command ends itself after two.
const hookTimeout = 5

// events are the hook events that report to qcrew, and their matcher where one narrows them. The status of
// the session follows from them; see the service.
var events = []struct{ name, matcher string }{
	{"PermissionRequest", ""}, {"Notification", ""}, {"Stop", ""}, {"SessionStart", ""}, {"SessionEnd", ""},
	{"UserPromptSubmit", ""}, {"PreToolUse", "AskUserQuestion"}, {"PostToolUse", ""}, {"PostToolUseFailure", ""},
}

// Adapter starts Claude Code. Getenv reads the environment of the service, which the tool inherits.
type Adapter struct {
	Getenv func(string) string
}

// Name is the tool.
func (Adapter) Name() string { return adapter.Claude }

// SessionDir is the folder of the files of session s.
func SessionDir(s adapter.Session) string { return filepath.Join(s.DataDir, "sessions", s.ID) }

// SettingsPath is the path of the settings file of session s.
func SettingsPath(s adapter.Session) string { return filepath.Join(SessionDir(s), SettingsFile) }

// Command starts the tool with the given session id, without prompt suggestions, which would show up as input
// on the screen, and with the settings file of the session.
func (Adapter) Command(s adapter.Session) []string {
	cmd := []string{adapter.Claude, "--session-id", s.AgentSession, "--prompt-suggestions", "false",
		"--settings", SettingsPath(s)}
	return append(cmd, s.Args...)
}

// Env is empty: the settings file carries what the hooks need.
func (Adapter) Env(adapter.Session) []string { return nil }

var agentSession = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func check(s adapter.Session) error {
	switch {
	case !control.ValidSessionID(s.ID):
		return errors.New("the session id is not valid")
	case !filepath.IsAbs(s.DataDir):
		return errors.New("the data directory is not an absolute path")
	case !filepath.IsAbs(s.Program):
		return errors.New("the path of the qcrew program is not known")
	case !agentSession.MatchString(s.AgentSession):
		return errors.New("the tool session id is not valid")
	}
	return nil
}

// Prepare writes the settings file of the session, readable by the user only. The status line goes in only if
// the user has none of their own.
func (a Adapter) Prepare(s adapter.Session) error {
	if err := check(s); err != nil {
		return err
	}
	getenv := a.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	data, err := Settings(s, !hasStatusLine(getenv, s.Dir))
	if err != nil {
		return err
	}
	dir := SessionDir(s)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create the session folder: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("protect the session folder: %w", err)
	}
	return writeAtomic(filepath.Join(dir, SettingsFile), data)
}

// Cleanup removes the session's folder.
func (Adapter) Cleanup(s adapter.Session) {
	if control.ValidSessionID(s.ID) && filepath.IsAbs(s.DataDir) {
		_ = os.RemoveAll(SessionDir(s))
	}
}

// writeAtomic writes data to path with mode 0600 so that the tool never reads half a file.
func writeAtomic(path string, data []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return fmt.Errorf("write the settings: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("write the settings: %w", err)
	}
	return nil
}

type hookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

type hookGroup struct {
	Matcher string      `json:"matcher,omitempty"`
	Hooks   []hookEntry `json:"hooks"`
}

type settings struct {
	Hooks      map[string][]hookGroup `json:"hooks"`
	StatusLine *hookEntry             `json:"statusLine,omitempty"`
}

// Settings returns the content of the settings file of s: a hook for every event that reports to qcrew and,
// with statusLine, the status line that reports the fill of the context window.
func Settings(s adapter.Session, statusLine bool) ([]byte, error) {
	command := func(kind string) string {
		return shellQuote(s.Program) + " hook " + kind + " --session " + s.ID
	}
	out := settings{Hooks: map[string][]hookGroup{}}
	for _, e := range events {
		out.Hooks[e.name] = []hookGroup{{Matcher: e.matcher,
			Hooks: []hookEntry{{Type: "command", Command: command("claude"), Timeout: hookTimeout}}}}
	}
	if statusLine {
		out.StatusLine = &hookEntry{Type: "command", Command: command("claude-status")}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

var safeWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellQuote quotes a word for the POSIX shell that runs the hook command.
func shellQuote(word string) string {
	if safeWord.MatchString(word) {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// hasStatusLine reports whether the user has a status line of their own, read from the user settings of Claude
// Code and the two settings files of the project, and never written. A file that exists but cannot be read as
// a JSON object counts as one: overriding what cannot be seen could replace the user's status line. So does
// not knowing where the user settings are.
func hasStatusLine(getenv func(string) string, projectDir string) bool {
	var files []string
	switch dir, home := getenv("CLAUDE_CONFIG_DIR"), getenv("HOME"); {
	case dir != "" && filepath.IsAbs(dir):
		files = append(files, filepath.Join(dir, "settings.json"))
	case dir == "" && filepath.IsAbs(home):
		files = append(files, filepath.Join(home, ".claude", "settings.json"))
	default:
		return true
	}
	files = append(files, filepath.Join(projectDir, ".claude", "settings.json"),
		filepath.Join(projectDir, ".claude", "settings.local.json"))
	for _, f := range files {
		info, err := os.Stat(f)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxSettings {
			return true
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return true
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(data, &obj) != nil || obj == nil {
			return true
		}
		if v, ok := obj["statusLine"]; ok && !bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return true
		}
	}
	return false
}
