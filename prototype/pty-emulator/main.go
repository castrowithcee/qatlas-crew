package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/charmbracelet/x/xpty"
)

type observation struct {
	queries    []string
	replyBytes int
	modes      []string
}

func (o *observation) observe(e *vt.Emulator) {
	e.SetCallbacks(vt.Callbacks{
		EnableMode:  func(m ansi.Mode) { o.modes = append(o.modes, fmt.Sprintf("enable %T(%v)", m, m)) },
		DisableMode: func(m ansi.Mode) { o.modes = append(o.modes, fmt.Sprintf("disable %T(%v)", m, m)) },
	})
	for _, query := range []struct {
		command int
		name    string
	}{
		{'c', "DA1"},
		{ansi.Command('>', 0, 'c'), "DA2"},
		{'n', "DSR"},
		{ansi.Command('?', 0, 'n'), "DEC DSR"},
	} {
		e.RegisterCsiHandler(query.command, func(params ansi.Params) bool {
			n, _, _ := params.Param(0, 0)
			o.queries = append(o.queries, fmt.Sprintf("%s %d", query.name, n))
			return false
		})
	}
	for _, command := range []int{10, 11} {
		e.RegisterOscHandler(command, func(data []byte) bool {
			if bytes.HasSuffix(data, []byte(";?")) {
				o.queries = append(o.queries, fmt.Sprintf("OSC %d", command))
			}
			return false
		})
	}
	e.RegisterCsiHandler(ansi.Command('?', 0, 'u'), func(params ansi.Params) bool {
		o.queries = append(o.queries, "Kitty keyboard query")
		_, _ = io.WriteString(e.InputPipe(), "\x1b[?0u")
		return true
	})
	e.RegisterCsiHandler(ansi.Command('>', 0, 'u'), func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 0)
		o.modes = append(o.modes, fmt.Sprintf("Kitty keyboard request %d (unsupported)", n))
		return true
	})
	e.RegisterCsiHandler(ansi.Command('>', 0, 'm'), func(params ansi.Params) bool {
		n, _, _ := params.Param(0, 0)
		if n == 4 {
			value, _, _ := params.Param(1, 0)
			o.modes = append(o.modes, fmt.Sprintf("modifyOtherKeys request %d (unsupported)", value))
			return true
		}
		return false
	})
	e.RegisterCsiHandler(ansi.Command('>', 0, 'q'), func(params ansi.Params) bool {
		o.queries = append(o.queries, "XTVERSION")
		_, _ = io.WriteString(e.InputPipe(), "\x1bP>|crew-prototype(0)\x1b\\")
		return true
	})
}

func checkDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	temp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(temp, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("working directory must be a fresh repository below the temporary directory")
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = abs
	root, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(root)) != abs {
		return "", errors.New("working directory must be the root of a temporary Git repository")
	}
	return abs, nil
}

func run(output io.Writer) error {
	var dir, input, expect string
	var duration time.Duration
	flags := flag.NewFlagSet("pty-emulator", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&dir, "dir", "", "fresh temporary Git repository root")
	flags.StringVar(&input, "input", "", "one input line sent after startup")
	flags.StringVar(&expect, "expect", "", "synthetic response marker to detect after input")
	flags.DurationVar(&duration, "duration", 15*time.Second, "maximum host run time")
	if flags.Parse(os.Args[1:]) != nil {
		return errors.New("invalid arguments")
	}
	if dir == "" || duration <= 0 || flags.NArg() == 0 || (expect != "" && (input == "" || strings.Contains(input, expect))) {
		return errors.New("invalid arguments")
	}
	root, err := checkDir(dir)
	if err != nil {
		return err
	}
	pty, err := xpty.NewPty(100, 30)
	if err != nil {
		return err
	}
	defer pty.Close()

	emulator := vt.NewEmulator(100, 30)
	defer emulator.Close()
	var seen observation
	seen.observe(emulator)
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	args := flags.Args()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	if err := pty.Start(cmd); err != nil {
		return err
	}
	if unixPTY, ok := pty.(*xpty.UnixPty); ok {
		_ = unixPTY.Slave().Close()
	}
	started := time.Now()
	var expected struct {
		sync.Mutex
		sentAt      time.Time
		preexisting bool
		seen        bool
		after       time.Duration
	}

	responsesDone := make(chan struct{})
	go func() {
		defer close(responsesDone)
		buf := make([]byte, 1024)
		for {
			n, err := emulator.Read(buf)
			if n > 0 {
				seen.replyBytes += n
				_, _ = pty.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	outputDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				if _, writeErr := emulator.Write(buf[:n]); writeErr != nil {
					outputDone <- writeErr
					return
				}
				if expect != "" {
					expected.Lock()
					if strings.Contains(emulator.String(), expect) {
						if expected.sentAt.IsZero() {
							expected.preexisting = true
						} else if !expected.preexisting && !expected.seen {
							expected.seen = true
							expected.after = time.Since(expected.sentAt)
						}
					}
					expected.Unlock()
				}
			}
			if err != nil {
				outputDone <- nil
				return
			}
		}
	}()
	inputDone := make(chan struct{})
	if input != "" {
		go func() {
			defer close(inputDone)
			select {
			case <-time.After(2 * time.Second):
				expected.Lock()
				if _, err := pty.Write([]byte(input + "\r")); err == nil {
					expected.sentAt = time.Now()
				}
				expected.Unlock()
			case <-ctx.Done():
			}
		}()
	} else {
		close(inputDone)
	}
	waitErr := xpty.WaitProcess(ctx, cmd)
	cancel()
	_ = pty.Close()
	<-inputDone
	if err := <-outputDone; err != nil {
		return err
	}
	_ = emulator.Close()
	<-responsesDone
	status := "exited"
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status = "timeout"
	} else if waitErr != nil {
		status = "failed"
	}
	fmt.Fprintf(output, "elapsed=%s status=%s alternate_screen=%t response_bytes=%d screen_sha256=%x\n", time.Since(started).Round(time.Millisecond), status, emulator.IsAltScreen(), seen.replyBytes, sha256.Sum256([]byte(emulator.String())))
	if expect != "" {
		fmt.Fprintf(output, "expect_seen=%t\n", expected.seen)
		if expected.seen {
			fmt.Fprintf(output, "expect_after=%s\n", expected.after.Round(time.Millisecond))
		}
	}
	for _, query := range seen.queries {
		fmt.Fprintln(output, "query:", query)
	}
	for _, mode := range seen.modes {
		fmt.Fprintln(output, "mode:", mode)
	}
	return nil
}

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "prototype failed")
		os.Exit(1)
	}
}
