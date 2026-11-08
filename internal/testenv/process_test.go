package testenv

import (
	"os"
	"os/exec"
	"testing"
)

func TestProcessGone(t *testing.T) {
	if ProcessGone(os.Getpid()) {
		t.Error("the running test process counts as gone")
	}
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if !ProcessGone(pid) {
		t.Errorf("the finished child %d does not count as gone", pid)
	}
}
