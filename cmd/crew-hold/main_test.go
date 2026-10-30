package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSyntheticHost(t *testing.T) {
	if os.Getenv("CREW_HOLD_TEST_HOST") != "1" {
		return
	}
	fmt.Println("READY")
	time.AfterFunc(15*time.Second, func() { os.Exit(0) })
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Printf("RECEIVED:%s", line)
	os.Exit(0)
}

func TestSyntheticInterruptHost(t *testing.T) {
	if os.Getenv("CREW_HOLD_TEST_INTERRUPT_HOST") != "1" {
		return
	}
	stty := exec.Command("stty", "raw", "-echo")
	stty.Stdin = os.Stdin
	if err := stty.Run(); err != nil {
		t.Fatal(err)
	}
	fmt.Print("READY")
	input := make(chan byte)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			b, err := reader.ReadByte()
			if err != nil {
				return
			}
			input <- b
		}
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case b := <-input:
			if b == 0x1b {
				fmt.Print("INTERRUPTED")
				for b := range input {
					if b == 'x' {
						fmt.Print("HOST_OPEN")
						os.Exit(0)
					}
				}
			}
		case <-ticker.C:
			fmt.Print("WORKING")
		}
	}
}

func TestHolderProcess(t *testing.T) {
	if os.Getenv("CREW_HOLD_TEST_HOLDER") != "1" {
		return
	}
	if err := serve(os.Getenv("CREW_HOLD_TEST_RUNTIME"), []string{os.Args[0], "-test.run=^TestSyntheticHost$"}); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundProcess(t *testing.T) {
	if os.Getenv("CREW_HOLD_TEST_BACKGROUND") != "1" {
		return
	}
	if err := background(os.Getenv("CREW_HOLD_TEST_ROOT")); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "example-project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	runtime, err := os.MkdirTemp(root, "crew-hold-")
	if err != nil {
		t.Fatal(err)
	}
	holderCmd := exec.Command(os.Args[0], "-test.run=^TestHolderProcess$")
	holderCmd.Dir = project
	holderCmd.Env = append(os.Environ(), "CREW_HOLD_TEST_HOLDER=1", "CREW_HOLD_TEST_HOST=1", "CREW_HOLD_TEST_RUNTIME="+runtime)
	if err := holderCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if holderCmd.ProcessState == nil {
			holderCmd.Process.Kill()
			holderCmd.Wait()
		}
	})
	socket := filepath.Join(runtime, "hold.sock")
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(socket); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatal(err)
	}
	startBackground := func() (*exec.Cmd, io.WriteCloser, <-chan message) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestBackgroundProcess$")
		cmd.Env = append(os.Environ(), "CREW_HOLD_TEST_BACKGROUND=1", "CREW_HOLD_TEST_ROOT="+root)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				cmd.Process.Kill()
				cmd.Wait()
			}
		})
		messages := make(chan message, 16)
		go func() {
			defer close(messages)
			dec := json.NewDecoder(stdout)
			for {
				var msg message
				if dec.Decode(&msg) != nil {
					return
				}
				messages <- msg
			}
		}()
		return cmd, stdin, messages
	}
	readUntil := func(messages <-chan message, want string) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		var got strings.Builder
		for !strings.Contains(got.String(), want) {
			select {
			case msg, ok := <-messages:
				if !ok {
					t.Fatal("background closed before output")
				}
				if msg.Session != filepath.Base(runtime) && msg.Op != "ready" {
					t.Fatalf("unexpected session %q", msg.Session)
				}
				data, err := base64.StdEncoding.DecodeString(msg.Data)
				if err != nil {
					t.Fatal(err)
				}
				got.Write(data)
			case <-deadline:
				t.Fatalf("output %q missing", want)
			}
		}
	}
	first, _, firstMessages := startBackground()
	readUntil(firstMessages, "READY")
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err == nil {
		t.Fatal("hard stop unexpectedly succeeded")
	}
	if err := holderCmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("holder ended with background: %v", err)
	}
	second, stdin, secondMessages := startBackground()
	readUntil(secondMessages, "READY")
	if err := json.NewEncoder(stdin).Encode(message{Op: "resize", Session: filepath.Base(runtime), Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(stdin).Encode(message{Op: "input", Session: filepath.Base(runtime), Data: base64.StdEncoding.EncodeToString([]byte("hello\n"))}); err != nil {
		t.Fatal(err)
	}
	readUntil(secondMessages, "RECEIVED:hello")
	stdin.Close()
	if err := second.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := holderCmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestHolderReconnect(t *testing.T) {
	runtime := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREW_HOLD_TEST_HOST", "1")
	done := make(chan error, 1)
	go func() { done <- serve(runtime, []string{os.Args[0], "-test.run=TestSyntheticHost"}) }()
	socket := filepath.Join(runtime, "hold.sock")
	var conn net.Conn
	var err error
	for i := 0; i < 100; i++ {
		conn, err = net.Dial("unix", socket)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(socket)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("socket mode %o", info.Mode().Perm())
	}
	info, err = os.Stat(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("runtime mode %o", info.Mode().Perm())
	}
	readUntil := func(c net.Conn, want string) {
		t.Helper()
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		dec := json.NewDecoder(c)
		var got strings.Builder
		for !strings.Contains(got.String(), want) {
			var msg message
			if err := dec.Decode(&msg); err != nil {
				t.Fatal(err)
			}
			data, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				t.Fatal(err)
			}
			got.Write(data)
		}
	}
	readUntil(conn, "READY")
	conn.Close()
	conn, err = net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	readUntil(conn, "READY")
	if err := json.NewEncoder(conn).Encode(message{Op: "resize", Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(conn).Encode(message{Op: "input", Data: base64.StdEncoding.EncodeToString([]byte("hello\n"))}); err != nil {
		t.Fatal(err)
	}
	readUntil(conn, "RECEIVED:hello")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host did not exit")
	}
}

func TestRingBufferBounded(t *testing.T) {
	h := &holder{clients: make(map[chan []byte]net.Conn)}
	h.publish([]byte(strings.Repeat("x", bufferLimit+9)))
	if len(h.buffer) != bufferLimit {
		t.Fatalf("buffer length %d", len(h.buffer))
	}
}

func TestStopGroupSignalsOnlyBoundChild(t *testing.T) {
	const child = 12345
	var sent []syscall.Signal
	lookup := func(pid int) (int, error) {
		if pid == 0 {
			return 100, nil
		}
		if pid == child {
			return child, nil
		}
		return 0, syscall.ESRCH
	}
	if err := stopGroup(child, child, lookup, func(pid int, sig syscall.Signal) error {
		if pid != -child {
			t.Fatalf("signaled process group %d", pid)
		}
		sent = append(sent, sig)
		return nil
	}, func(d time.Duration) {
		if d != stopGrace {
			t.Fatalf("grace period %s", d)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[0] != syscall.SIGTERM || sent[1] != syscall.SIGKILL {
		t.Fatalf("signals %v", sent)
	}
	for _, tc := range []struct {
		name  string
		pid   int
		group int
		own   int
	}{
		{"zero PID", 0, 0, 100},
		{"negative PID", -1, -1, 100},
		{"group mismatch", child, child + 1, 100},
		{"holder group", child, child, child},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := stopGroup(tc.pid, tc.group, func(pid int) (int, error) {
				if pid == 0 {
					return tc.own, nil
				}
				return child, nil
			}, func(int, syscall.Signal) error {
				t.Fatal("unsafe signal")
				return nil
			}, func(time.Duration) {})
			if err == nil {
				t.Fatal("unsafe group accepted")
			}
		})
	}
	checks := 0
	sent = nil
	err := stopGroup(child, child, func(pid int) (int, error) {
		if pid == 0 {
			return 100, nil
		}
		checks++
		if checks == 2 {
			return child + 1, nil
		}
		return child, nil
	}, func(_ int, sig syscall.Signal) error {
		sent = append(sent, sig)
		return nil
	}, func(time.Duration) {})
	if err == nil || len(sent) != 1 || sent[0] != syscall.SIGTERM {
		t.Fatalf("changed binding: err=%v signals=%v", err, sent)
	}
}

func TestEscInterruptKeepsHostOpen(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "example-project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}
	t.Chdir(project)
	runtime := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREW_HOLD_TEST_INTERRUPT_HOST", "1")
	done := make(chan error, 1)
	go func() { done <- serve(runtime, []string{os.Args[0], "-test.run=^TestSyntheticInterruptHost$"}) }()
	socket := filepath.Join(runtime, "hold.sock")
	var conn net.Conn
	var err error
	for i := 0; i < 100; i++ {
		conn, err = net.Dial("unix", socket)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	dec := json.NewDecoder(conn)
	readUntil := func(want string) {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var got strings.Builder
		for !strings.Contains(got.String(), want) {
			var msg message
			if err := dec.Decode(&msg); err != nil {
				t.Fatal(err)
			}
			data, err := base64.StdEncoding.DecodeString(msg.Data)
			if err != nil {
				t.Fatal(err)
			}
			got.Write(data)
		}
	}
	readUntil("WORKING")
	if err := json.NewEncoder(conn).Encode(message{Op: "input", Data: base64.StdEncoding.EncodeToString([]byte{0x1b})}); err != nil {
		t.Fatal(err)
	}
	readUntil("INTERRUPTED")
	if err := json.NewEncoder(conn).Encode(message{Op: "input", Data: base64.StdEncoding.EncodeToString([]byte{'x'})}); err != nil {
		t.Fatal(err)
	}
	readUntil("HOST_OPEN")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("host did not exit")
	}
}
