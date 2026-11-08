package testenv

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// ProcessGone reports whether the process pid has ended. A zombie counts as ended, because it runs nothing
// and only waits to be reaped. It sends signal 0 only, which delivers nothing; pass only PIDs of processes
// the test started itself.
func ProcessGone(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return true
	}
	// ps exists on Linux and macOS alike; the Linux process file system does not exist on macOS.
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		// ps exits non-zero when no such process exists.
		var exit *exec.ExitError
		return errors.As(err, &exit)
	}
	stat := strings.TrimSpace(string(out))
	return stat == "" || strings.HasPrefix(stat, "Z")
}
