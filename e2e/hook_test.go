//go:build e2e

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

// The hook command of a tool ends with exit code 0 and prints nothing, with a payload, without one and
// without a service, and it never starts the service.
func TestHookIsSilentWithoutService(t *testing.T) {
	e := testenv.Setup(t)
	runtimeDir := testenv.ShortRuntime(t)
	env := testenv.WithRuntime(e.Environ(), runtimeDir)
	for _, c := range []struct{ stdin, args string }{
		{`{"hook_event_name":"Stop","last_assistant_message":"Done"}`, "hook claude --session abcdefghij23"},
		{`{"context_window":{"used_percentage":12.5}}`, "hook claude-status --session abcdefghij23"},
		{``, "hook claude --session abcdefghij23"},
		{`{"hook_event_name":"Stop","x":"` + strings.Repeat("a", 300<<10) + `"}`, "hook claude --session abcdefghij23"},
		{`garbage`, "hook claude --session abcdefghij23"},
		{`{"hook_event_name":"Stop"}`, "hook claude --session nope"},
		{`{"hook_event_name":"Stop"}`, "hook claude --bogus"},
	} {
		cmd := exec.Command(binary, strings.Fields(c.args)...)
		cmd.Dir, cmd.Env = e.Home, env
		cmd.Stdin = strings.NewReader(c.stdin)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Errorf("%s with %.30q: err=%v stdout=%q stderr=%q", c.args, c.stdin, err, stdout.String(), stderr.String())
		}
	}
	if _, err := os.Lstat(filepath.Join(runtimeDir, "qcrew", "control.sock")); !os.IsNotExist(err) {
		t.Errorf("a hook started the service: %v", err)
	}
	if r := runIn(t, env, e.Home, "service", "status", "--output", "json"); !strings.Contains(r.stdout, `"running":false`) {
		t.Errorf("the service runs: %q", r.stdout)
	}
}
