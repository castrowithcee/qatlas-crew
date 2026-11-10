//go:build integration

package session

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

func TestMain(m *testing.M) { os.Exit(testenv.TmuxMain(m)) }

// newTestManager returns a manager on the private test server; the sessions of the test project are closed
// when the test ends.
func newTestManager(t *testing.T, projectID string) (*Manager, *testenv.TmuxServer) {
	t.Helper()
	srv := testenv.Tmux(t)
	testenv.Setup(t)
	t.Setenv("SHELL", "/bin/sh")
	t.Cleanup(func() { _, _ = srv.Run("kill-session", "-t", "="+SessionName(projectID)) })
	return New(Server{Socket: srv.Socket()}), srv
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestShellSessionLifecycle(t *testing.T) {
	const project = "lifecycle234"
	m, srv := newTestManager(t, project)
	ctx := context.Background()
	dir := t.TempDir()

	s, err := m.Start(ctx, StartRequest{ProjectID: project, Dir: dir, Tool: "shell", Title: "my shell"})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.ID) != 12 || s.Project != project || s.Tool != "shell" || s.Title != "my shell" || s.Status != StatusRunning {
		t.Fatalf("started: %+v", s)
	}
	second, err := m.Start(ctx, StartRequest{ProjectID: project, Dir: dir, Tool: "shell"})
	if err != nil || second.Title != "shell" {
		t.Fatalf("second start: %+v, %v", second, err)
	}

	out, err := srv.Run("list-windows", "-t", "="+SessionName(project), "-F",
		"#{window_name} #{@qcrew-project} #{remain-on-exit} #{allow-rename} #{automatic-rename} #{pane_title}")
	if err != nil {
		t.Fatal(err)
	}
	if want := WindowName(s.ID) + " " + project + " on 0 0 my shell\n"; !strings.HasPrefix(out, want) {
		t.Fatalf("windows: %q, want prefix %q", out, want)
	}

	list, err := m.List(ctx)
	if err != nil || len(list) != 2 || list[0].ID != s.ID || list[0].Status != StatusRunning || !list[0].Started.Equal(s.Started) {
		t.Fatalf("list: %+v, %v", list, err)
	}

	// The shell prints its prompt, then the answer to a command typed into it.
	waitUntil(t, "the prompt", func() bool {
		text, err := m.Capture(ctx, s.ID, 10)
		return err == nil && strings.TrimSpace(text) != ""
	})
	if _, err := srv.Run("send-keys", "-t", list[0].window, "-l", "echo $((40+2))"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Run("send-keys", "-t", list[0].window, "Enter"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the command output", func() bool {
		text, _ := m.Capture(ctx, s.ID, 10)
		return strings.Contains("\n"+text+"\n", "\n42\n")
	})

	// An ended program leaves an exited session.
	if _, err := srv.Run("send-keys", "-t", list[0].window, "-l", "exit 3"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Run("send-keys", "-t", list[0].window, "Enter"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the exited status", func() bool {
		list, _ := m.List(ctx)
		return len(list) == 2 && list[0].Status == StatusExited
	})

	for _, id := range []string{s.ID, second.ID} {
		if stopped, err := m.Stop(ctx, id); err != nil || stopped.ID != id {
			t.Fatalf("stop %s: %+v, %v", id, stopped, err)
		}
	}
	if _, err := m.Stop(ctx, s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second stop: %v", err)
	}
	// The project's tmux session ends with its last window; the server and its other sessions stay.
	if _, err := srv.Run("has-session", "-t", "="+SessionName(project)); err == nil {
		t.Fatal("the empty project session is still there")
	}
	if _, err := srv.Run("has-session", "-t", "anchor"); err != nil {
		t.Fatalf("the server lost another session: %v", err)
	}
}

func TestNoServerMeansNoSessions(t *testing.T) {
	testenv.Setup(t)
	m := New(Server{Socket: testenv.ShortRuntime(t) + "/none"})
	if list, err := m.List(context.Background()); err != nil || len(list) != 0 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if _, err := m.Capture(context.Background(), "abcdefghijk2", 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("capture: %v", err)
	}
}

// Without a server, the first start creates it; the server ends with its last session.
func TestStartCreatesTheServer(t *testing.T) {
	testenv.Tmux(t) // skips without tmux
	testenv.Setup(t)
	t.Setenv("SHELL", "/bin/sh")
	m := New(Server{Socket: testenv.ShortRuntime(t) + "/sock"})
	ctx := context.Background()
	t.Cleanup(func() { _, _ = m.tmux(ctx, "kill-server") })

	s, err := m.Start(ctx, StartRequest{ProjectID: "fresh2345678", Dir: t.TempDir(), Tool: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	if list, err := m.List(ctx); err != nil || len(list) != 1 || list[0].ID != s.ID {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if _, err := m.Stop(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the server to end", func() bool {
		_, err := m.tmux(ctx, "list-sessions")
		return errors.Is(err, errNoServer)
	})
}

// A global destroy-unattached on, as a user configuration may set it, must not end a new project session.
func TestStartSurvivesDestroyUnattached(t *testing.T) {
	testenv.Tmux(t) // skips without tmux
	srv, err := testenv.StartTmux()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	// The anchor keeps the server alive and is exempt; every other session gets the global value.
	if _, err := srv.Run("set-option", "-t", "anchor", "destroy-unattached", "off", ";",
		"set-option", "-g", "destroy-unattached", "on"); err != nil {
		t.Fatal(err)
	}
	testenv.Setup(t)
	t.Setenv("SHELL", "/bin/sh")
	m := New(Server{Socket: srv.Socket()})
	ctx := context.Background()

	s, err := m.Start(ctx, StartRequest{ProjectID: "destroy23456", Dir: t.TempDir(), Tool: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	// Give the server a moment to act on the option before checking.
	time.Sleep(200 * time.Millisecond)
	list, err := m.List(ctx)
	if err != nil || len(list) != 1 || list[0].ID != s.ID || list[0].Status != StatusRunning {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if _, err := srv.Run("has-session", "-t", "="+SessionName("destroy23456")); err != nil {
		t.Fatalf("the project session is gone: %v", err)
	}
}

// fakeTool puts a program of the given name on the PATH, which tmux takes from the process that starts the
// session. It records its arguments in args.txt of dir and waits.
func fakeTool(t *testing.T, name string) (args string) {
	t.Helper()
	bin := t.TempDir()
	args = bin + "/args.txt"
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + args + "\nsleep 60\n"
	if err := os.WriteFile(bin+"/"+name, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return args
}

func TestClaudeSessionIsPreparedAndCleaned(t *testing.T) {
	const project = "claude234567"
	srv := testenv.Tmux(t)
	testenv.Setup(t)
	t.Setenv("SHELL", "/bin/sh")
	t.Cleanup(func() { _, _ = srv.Run("kill-session", "-t", "="+SessionName(project)) })
	argsFile := fakeTool(t, "claude")
	data := t.TempDir()
	m := New(Server{Socket: srv.Socket()}, WithDataDir(data),
		WithExecutable(func() (string, error) { return "/opt/qcrew/qcrew", nil }))
	ctx := context.Background()

	s, err := m.Start(ctx, StartRequest{ProjectID: project, Dir: t.TempDir(), Tool: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	settings := data + "/sessions/" + s.ID + "/claude-settings.json"
	if _, err := os.Stat(settings); err != nil {
		t.Fatalf("settings file: %v", err)
	}
	waitUntil(t, "the tool to record its arguments", func() bool { _, err := os.Stat(argsFile); return err == nil })
	raw, _ := os.ReadFile(argsFile)
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(got) != 6 || got[0] != "--session-id" || len(got[1]) != 36 || got[2] != "--prompt-suggestions" || got[3] != "false" ||
		got[4] != "--settings" || got[5] != settings {
		t.Fatalf("arguments: %q", got)
	}
	out, err := srv.Run("list-windows", "-a", "-F", "#{@qcrew-session} #{@qcrew-agent-session}")
	if err != nil || !strings.Contains(out, s.ID+" "+got[1]+"\n") {
		t.Fatalf("window options: %q, %v", out, err)
	}
	if _, err := m.Stop(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(data + "/sessions/" + s.ID); !os.IsNotExist(err) {
		t.Fatalf("the session folder is still there: %v", err)
	}
}

// A tool that cannot be prepared starts no window and leaves no files.
func TestFailedPrepareLeavesNothing(t *testing.T) {
	const project = "prepare23456"
	srv := testenv.Tmux(t)
	testenv.Setup(t)
	t.Setenv("SHELL", "/bin/sh")
	t.Cleanup(func() { _, _ = srv.Run("kill-session", "-t", "="+SessionName(project)) })
	fakeTool(t, "claude")
	data := t.TempDir()
	m := New(Server{Socket: srv.Socket()}, WithDataDir(data),
		WithExecutable(func() (string, error) { return "relative/qcrew", nil }))
	if _, err := m.Start(context.Background(), StartRequest{ProjectID: project, Dir: t.TempDir(), Tool: "claude"}); err == nil {
		t.Fatal("start succeeded without a usable qcrew program path")
	}
	if list, err := m.List(context.Background()); err != nil || len(list) != 0 {
		t.Fatalf("sessions after a failed start: %+v, %v", list, err)
	}
	if entries, _ := os.ReadDir(data); len(entries) != 0 {
		t.Fatalf("files left behind: %v", entries)
	}
}
