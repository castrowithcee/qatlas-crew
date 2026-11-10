//go:build !linux

package term

import (
	"os"

	"github.com/creack/pty"
)

// pollable keeps the blocking PTY outside Linux: pty itself falls back to blocking I/O there, since kqueue
// does not take every PTY. Close then cannot interrupt a write to a child that reads nothing while another
// process holds the PTY open.
func pollable(f *os.File) (*os.File, error) { return f, nil }

func setSize(f *os.File, cols, rows int) error {
	return pty.Setsize(f, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}
