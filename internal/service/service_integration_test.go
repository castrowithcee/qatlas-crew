//go:build integration

package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"

	"github.com/castrowithcee/qatlas-crew/internal/control"
	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

type running struct {
	home     string
	finished chan struct{}
	err      error
}

// wait returns what Run returned, failing the test if it does not return in time.
func (r *running) wait(t *testing.T, what string) error {
	t.Helper()
	select {
	case <-r.finished:
		return r.err
	case <-time.After(5 * time.Second):
		t.Fatalf("the service did not end %s", what)
		return nil
	}
}

// start runs the service in this process, in a hermetic home, and returns once it listens. The test
// ends it again and checks that Run returned.
func start(t *testing.T, opts Options) *running {
	t.Helper()
	env := testenv.Setup(t)
	testenv.ShortRuntime(t)
	opts.Home = env.Home
	if opts.Version == "" {
		opts.Version = "test"
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	opts.Ready = func() { close(ready) }
	r := &running{home: env.Home, finished: make(chan struct{})}
	go func() {
		r.err = Run(ctx, opts)
		close(r.finished)
	}()
	t.Cleanup(func() {
		cancel()
		r.wait(t, "at cleanup")
	})
	select {
	case <-ready:
	case <-r.finished:
		t.Fatalf("the service ended before it was ready: %v", r.err)
	case <-time.After(5 * time.Second):
		t.Fatal("the service did not become ready")
	}
	return r
}

type conn struct {
	net.Conn
	br *bufio.Reader
}

func (r *running) dial(t *testing.T) *conn {
	t.Helper()
	c, err := Endpoint(r.home).Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &conn{c, bufio.NewReader(c)}
}

// send writes raw bytes and reads one answer.
func (c *conn) send(t *testing.T, raw string) control.Response {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	line, err := control.ReadLine(c.br)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := control.DecodeResponse(line)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (c *conn) call(t *testing.T, op string) control.Response {
	t.Helper()
	req, err := control.EncodeRequest(op, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c.send(t, string(req))
}

func TestPingStatusStop(t *testing.T) {
	r := start(t, Options{Version: "v9"})
	sock, err := Endpoint(r.home).Path()
	if err != nil {
		t.Fatal(err)
	}
	c := r.dial(t)
	if resp := c.call(t, control.OpPing); !resp.OK || string(resp.Result) != "{}" {
		t.Fatalf("ping: %+v", resp)
	}
	resp := c.call(t, control.OpStatus)
	var st control.StatusResult
	if err := json.Unmarshal(resp.Result, &st); err != nil || !resp.OK {
		t.Fatalf("status: %+v, %v", resp, err)
	}
	if st.Version != "v9" || st.PID != os.Getpid() || time.Since(st.StartedAt) > time.Minute || st.StartedAt.IsZero() {
		t.Fatalf("status result: %+v", st)
	}
	if resp := c.call(t, control.OpStop); !resp.OK {
		t.Fatalf("stop: %+v", resp)
	}
	if err := r.wait(t, "after stop"); err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if _, err := os.Lstat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the socket is still there: %v", err)
	}
}

func TestBadRequestsGetAnswers(t *testing.T) {
	r := start(t, Options{})
	c := r.dial(t)
	cases := []struct{ raw, code string }{
		{`{"v":1,"op":"nope"}` + "\n", control.CodeUnknownOperation},
		{`{"v":7,"op":"ping"}` + "\n", control.CodeUnsupportedVersion},
		{`{"v":1,"op":"PING"}` + "\n", control.CodeInvalidRequest},
		{`garbage` + "\n", control.CodeInvalidRequest},
		{`{"v":1,"op":"ping","args":[]}` + "\n", control.CodeInvalidRequest},
	}
	for _, tc := range cases {
		if resp := c.send(t, tc.raw); resp.OK || resp.Code != tc.code {
			t.Errorf("%q: %+v, want code %s", tc.raw, resp, tc.code)
		}
	}
	// The connection is still usable after refusals.
	if resp := c.call(t, control.OpPing); !resp.OK {
		t.Fatalf("ping after refusals: %+v", resp)
	}
	// An oversized message gets an answer and ends the connection; the service lives on.
	resp := r.dial(t).send(t, strings.Repeat("x", control.MaxMessage+10)+"\n")
	if resp.OK || resp.Code != control.CodeInvalidRequest {
		t.Fatalf("oversized: %+v", resp)
	}
	if resp := r.dial(t).call(t, control.OpPing); !resp.OK {
		t.Fatalf("ping after oversized: %+v", resp)
	}
}

func TestSecondServiceReportsRunning(t *testing.T) {
	r := start(t, Options{})
	err := Run(context.Background(), Options{Home: r.home, Version: "second"})
	if !errors.Is(err, ErrRunning) {
		t.Fatalf("second Run: %v", err)
	}
	if resp := r.dial(t).call(t, control.OpPing); !resp.OK {
		t.Fatalf("first service after the second start: %+v", resp)
	}
}

func TestReplacedProgramEndsService(t *testing.T) {
	replaced := make(chan struct{})
	r := start(t, Options{
		watchEvery: 10 * time.Millisecond,
		programReplaced: func() (string, bool, error) {
			select {
			case <-replaced:
				return "x", true, nil
			default:
				return "x", false, nil
			}
		},
	})
	sock, _ := Endpoint(r.home).Path()
	close(replaced)
	if err := r.wait(t, "after its program was replaced"); err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if _, err := os.Lstat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the socket is still there: %v", err)
	}
}

func TestContextEndsService(t *testing.T) {
	env := testenv.Setup(t)
	testenv.ShortRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- Run(ctx, Options{Home: env.Home, Ready: func() { close(ready) }}) }()
	<-ready
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no end")
	}
	logPath := filepath.Join(control.LogDir(env.Home), "service.log")
	data, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(data), "service started") || !strings.Contains(string(data), "service stopped") {
		t.Fatalf("log: %q, %v", data, err)
	}
	if _, err := Endpoint(env.Home).Dial(context.Background()); err == nil || errors.Is(err, localipc.ErrRunning) {
		t.Fatalf("dial after stop: %v", err)
	}
}
