package term

import (
	"os"
	"syscall"
	"unsafe"
)

// pollable replaces the PTY f by a non-blocking copy in the runtime poller and closes f. pty hands out a
// blocking descriptor, and Close cannot interrupt a blocking write to a child that reads nothing. The copy
// must never be asked for Fd, which would make it blocking again.
func pollable(f *os.File) (*os.File, error) {
	defer f.Close()
	fd, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_DUPFD_CLOEXEC, 0)
	if errno != 0 {
		return nil, errno
	}
	if err := syscall.SetNonblock(int(fd), true); err != nil {
		_ = syscall.Close(int(fd))
		return nil, err
	}
	return os.NewFile(fd, f.Name()), nil
}

// setSize is pty.Setsize without Fd, so the PTY stays non-blocking.
func setSize(f *os.File, cols, rows int) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}
	ws := struct{ rows, cols, x, y uint16 }{rows: uint16(rows), cols: uint16(cols)}
	var errno syscall.Errno
	if err := conn.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}
