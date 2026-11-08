package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// maxLogSize is the size above which the log moves to its single predecessor file.
const maxLogSize = 1 << 20

// logFile is an append-only log file with one rotation step: above the size limit the file becomes
// <name>.1, replacing an older one. It is safe for concurrent use.
type logFile struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

func openLog(dir string, max int64) (*logFile, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create the log directory: %w", err)
	}
	l := &logFile{path: filepath.Join(dir, "service.log"), max: max}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *logFile) open() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("cannot open the log file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("cannot read the log file: %w", err)
	}
	l.f, l.size = f, info.Size()
	return nil
}

func (l *logFile) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return 0, os.ErrClosed
	}
	if l.size > l.max {
		// A failed rotation keeps logging into the same file rather than losing messages.
		_ = l.f.Close()
		_ = os.Rename(l.path, l.path+".1")
		if err := l.open(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *logFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
