//go:build linux

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const bufferLimit = 64 << 10

type message struct {
	Op      string `json:"op,omitempty"`
	Session string `json:"session,omitempty"`
	Data    string `json:"data,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
	Cols    uint16 `json:"cols,omitempty"`
}

type holder struct {
	mu      sync.Mutex
	pty     *os.File
	buffer  []byte
	clients map[chan []byte]net.Conn
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: crew-hold start <host> [args...] | attach <socket> | background <runtime-root> | serve <runtime> <host> [args...]")
	}
	var err error
	switch os.Args[1] {
	case "start":
		if len(os.Args) < 3 {
			fatal("start needs a host")
		}
		err = start(os.Args[2:])
	case "serve":
		if len(os.Args) < 4 {
			fatal("serve needs a runtime and host")
		}
		err = serve(os.Args[2], os.Args[3:])
	case "attach":
		if len(os.Args) != 3 {
			fatal("attach needs a socket")
		}
		err = attach(os.Args[2])
	case "background":
		if len(os.Args) != 3 {
			fatal("background needs a runtime root")
		}
		err = background(os.Args[2])
	default:
		fatal("unknown command")
	}
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(s string) { fmt.Fprintln(os.Stderr, s); os.Exit(1) }

func start(host []string) error {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	runtime, err := os.MkdirTemp(base, "crew-hold-")
	if err != nil {
		return err
	}
	if err = os.Chmod(runtime, 0700); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, append([]string{"serve", runtime}, host...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devnull.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
	if err = cmd.Start(); err != nil {
		return err
	}
	if err = cmd.Process.Release(); err != nil {
		return err
	}
	socket := filepath.Join(runtime, "hold.sock")
	for i := 0; i < 100; i++ {
		conn, dialErr := net.Dial("unix", socket)
		if dialErr == nil {
			conn.Close()
			fmt.Println(socket)
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("holder did not open socket in %s", runtime)
}

func serve(runtime string, host []string) error {
	info, err := os.Lstat(runtime)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return errors.New("unsafe runtime directory")
	}
	cmd := exec.Command(host[0], host[1:]...)
	pt, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer pt.Close()
	socket := filepath.Join(runtime, "hold.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer func() { ln.Close(); os.Remove(socket); os.Remove(runtime) }()
	if err = os.Chmod(socket, 0600); err != nil {
		return err
	}
	h := &holder{pty: pt, clients: make(map[chan []byte]net.Conn)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		chunk := make([]byte, 4096)
		for {
			n, err := pt.Read(chunk)
			if n > 0 {
				h.publish(chunk[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		<-done
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			break
		}
		go h.client(conn)
	}
	return cmd.Wait()
}

func (h *holder) publish(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buffer = append(h.buffer, data...)
	if len(h.buffer) > bufferLimit {
		h.buffer = append([]byte(nil), h.buffer[len(h.buffer)-bufferLimit:]...)
	}
	for ch, conn := range h.clients {
		select {
		case ch <- append([]byte(nil), data...):
		default:
			conn.Close()
			close(ch)
			delete(h.clients, ch)
		}
	}
}

func (h *holder) client(conn net.Conn) {
	defer conn.Close()
	ch := make(chan []byte, 32)
	h.mu.Lock()
	ch <- append([]byte(nil), h.buffer...)
	h.clients[ch] = conn
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if _, ok := h.clients[ch]; ok {
			delete(h.clients, ch)
			close(ch)
		}
		h.mu.Unlock()
	}()
	go func() {
		enc := json.NewEncoder(conn)
		for data := range ch {
			if err := enc.Encode(message{Data: base64.StdEncoding.EncodeToString(data)}); err != nil {
				conn.Close()
				return
			}
		}
	}()
	dec := json.NewDecoder(conn)
	for {
		var msg message
		if err := dec.Decode(&msg); err != nil {
			return
		}
		switch msg.Op {
		case "input":
			data, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				return
			}
			if _, err := h.pty.Write(data); err != nil {
				return
			}
		case "resize":
			if msg.Rows == 0 || msg.Cols == 0 {
				return
			}
			if err := pty.Setsize(h.pty, &pty.Winsize{Rows: msg.Rows, Cols: msg.Cols}); err != nil {
				return
			}
		default:
			return
		}
	}
}

func attach(socket string) error {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return err
	}
	defer conn.Close()
	if rows, e := strconv.Atoi(os.Getenv("LINES")); e == nil && rows > 0 && rows <= 65535 {
		if cols, e := strconv.Atoi(os.Getenv("COLUMNS")); e == nil && cols > 0 && cols <= 65535 {
			if err := json.NewEncoder(conn).Encode(message{Op: "resize", Rows: uint16(rows), Cols: uint16(cols)}); err != nil {
				return err
			}
		}
	}
	go func() {
		enc := json.NewEncoder(conn)
		in := bufio.NewReader(os.Stdin)
		buf := make([]byte, 4096)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				if enc.Encode(message{Op: "input", Data: base64.StdEncoding.EncodeToString(buf[:n])}) != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	dec := json.NewDecoder(conn)
	for {
		var msg message
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		data, err := base64.StdEncoding.DecodeString(msg.Data)
		if err != nil {
			return err
		}
		if _, err := os.Stdout.Write(data); err != nil {
			return err
		}
	}
}

func background(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return errors.New("unsafe runtime root")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	sessions := make(map[string]net.Conn)
	defer func() {
		for _, conn := range sessions {
			conn.Close()
		}
	}()
	var outputMu sync.Mutex
	output := json.NewEncoder(os.Stdout)
	emit := func(msg message) error {
		outputMu.Lock()
		defer outputMu.Unlock()
		return output.Encode(msg)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "crew-hold-") || !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
			continue
		}
		socket := filepath.Join(dir, "hold.sock")
		info, err = os.Lstat(socket)
		if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
			continue
		}
		conn, err := net.DialTimeout("unix", socket, time.Second)
		if err != nil {
			continue
		}
		name := entry.Name()
		sessions[name] = conn
		go func() {
			dec := json.NewDecoder(conn)
			for {
				var msg message
				if dec.Decode(&msg) != nil {
					return
				}
				msg.Session = name
				if emit(msg) != nil {
					return
				}
			}
		}()
	}
	if err := emit(message{Op: "ready"}); err != nil {
		return err
	}
	dec := json.NewDecoder(os.Stdin)
	for {
		var msg message
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		conn, ok := sessions[msg.Session]
		if !ok || (msg.Op != "input" && msg.Op != "resize") {
			return errors.New("unknown session or operation")
		}
		if err := json.NewEncoder(conn).Encode(message{Op: msg.Op, Data: msg.Data, Rows: msg.Rows, Cols: msg.Cols}); err != nil {
			return err
		}
	}
}
