//go:build integration

package testenv

import (
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(TmuxMain(m)) }

func TestSessionPerTest(t *testing.T) {
	s := Tmux(t)
	name := s.NewSession(t)
	if _, err := s.Run("has-session", "-t", name); err != nil {
		t.Fatalf("session %q missing: %v", name, err)
	}
	out, err := s.Run("list-sessions", "-F", "#{session_name}")
	if err != nil || !strings.Contains(out, name) {
		t.Fatalf("list-sessions = %q, %v", out, err)
	}
}

func TestStopEndsOnlyOwnServer(t *testing.T) {
	shared := Tmux(t)
	own, err := StartTmux()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := own.Run("has-session", "-t", "anchor"); err != nil {
		t.Fatalf("anchor session missing: %v", err)
	}
	own.Stop()
	if _, err := own.Run("has-session", "-t", "anchor"); err == nil {
		t.Fatal("server still answers after Stop")
	}
	if _, err := os.Stat(own.Socket()); !os.IsNotExist(err) {
		t.Fatalf("socket still present: %v", err)
	}
	if _, err := shared.Run("has-session", "-t", "anchor"); err != nil {
		t.Fatalf("the shared server was affected: %v", err)
	}
}
