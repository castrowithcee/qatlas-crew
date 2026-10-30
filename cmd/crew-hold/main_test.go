package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyntheticHost(t *testing.T) {
	if os.Getenv("CREW_HOLD_TEST_HOST") != "1" {
		return
	}
	fmt.Println("READY")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fmt.Printf("RECEIVED:%s", line)
	os.Exit(0)
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
