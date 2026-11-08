package control

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestEncodeGolden(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	req := func(op string, args any) string {
		b, err := EncodeRequest(op, args)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	ok := func(result any) string {
		b, err := EncodeOK(result)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	cases := []struct{ name, got, want string }{
		{"ping", req(OpPing, nil), `{"v":1,"op":"ping"}` + "\n"},
		{"args", req("echo-1", map[string]any{"a": 1}), `{"v":1,"op":"echo-1","args":{"a":1}}` + "\n"},
		{"ok empty", ok(struct{}{}), `{"ok":true,"result":{}}` + "\n"},
		{"ok none", ok(nil), `{"ok":true}` + "\n"},
		{"status", ok(StatusResult{Version: "v1.2.3", PID: 42, StartedAt: start}),
			`{"ok":true,"result":{"version":"v1.2.3","pid":42,"started_at":"2026-01-02T03:04:05Z"}}` + "\n"},
		{"error", string(EncodeError(CodeUnknownOperation, "unknown operation")),
			`{"ok":false,"code":"unknown-operation","message":"unknown operation"}` + "\n"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestValidOp(t *testing.T) {
	good := []string{"a", "status", "a1", "a-b-c", strings.Repeat("a", 64)}
	bad := []string{"", "1a", "-a", "A", "a_b", "a b", "a.b", "a\n", strings.Repeat("a", 65), "ä"}
	for _, n := range good {
		if !ValidOp(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range bad {
		if ValidOp(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
	if _, err := EncodeRequest("Bad", nil); err == nil {
		t.Error("EncodeRequest accepted an invalid name")
	}
}

func TestDecodeRequestRefusals(t *testing.T) {
	cases := []struct{ in, code string }{
		{`{"v":1,"op":"ping"}`, ""},
		{`{"v":1,"op":"ping","args":{}}`, ""},
		{`{"v":1,"op":"ping","args":null}`, ""},
		{`{"v":2,"op":"ping"}`, CodeUnsupportedVersion},
		{`{"op":"ping"}`, CodeUnsupportedVersion},
		{`{"v":1,"op":"Ping"}`, CodeInvalidRequest},
		{`{"v":1}`, CodeInvalidRequest},
		{`{"v":1,"op":"ping","args":[1]}`, CodeInvalidRequest},
		{`{"v":1,"op":"ping","args":"x"}`, CodeInvalidRequest},
		{`not json`, CodeInvalidRequest},
		{`{"v":1,"op":"ping"} trailing`, CodeInvalidRequest},
		{``, CodeInvalidRequest},
		{strings.Repeat("x", MaxMessage+1), CodeInvalidRequest},
	}
	for _, c := range cases {
		_, err := DecodeRequest([]byte(c.in))
		var ce *Error
		switch {
		case c.code == "" && err != nil:
			t.Errorf("%.40q: unexpected %v", c.in, err)
		case c.code != "" && (!errors.As(err, &ce) || ce.Code != c.code):
			t.Errorf("%.40q: got %v, want code %s", c.in, err, c.code)
		}
	}
}

func TestDecodeResponse(t *testing.T) {
	r, err := DecodeResponse([]byte(`{"ok":true,"result":{"x":1}}`))
	if err != nil || !r.OK || string(r.Result) != `{"x":1}` {
		t.Fatalf("ok answer: %+v, %v", r, err)
	}
	r, err = DecodeResponse([]byte(`{"ok":false,"code":"runtime","message":"m"}`))
	if err != nil || r.OK || r.Code != "runtime" || r.Message != "m" {
		t.Fatalf("failed answer: %+v, %v", r, err)
	}
	for _, bad := range []string{`{"ok":false}`, `[]`, ``} {
		if _, err := DecodeResponse([]byte(bad)); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestMaxMessageBoundary(t *testing.T) {
	pad := func(n int) []byte { return []byte(`{"v":1,"op":"x","args":{"p":"` + strings.Repeat("a", n) + `"}}`) }
	base := len(pad(0))
	exact := pad(MaxMessage - base)
	if len(exact) != MaxMessage {
		t.Fatalf("test setup: %d", len(exact))
	}
	if _, err := DecodeRequest(exact); err != nil {
		t.Errorf("a message of exactly MaxMessage bytes: %v", err)
	}
	if _, err := DecodeRequest(pad(MaxMessage - base + 1)); err == nil {
		t.Error("a message beyond MaxMessage was accepted")
	}
	if _, err := EncodeRequest("x", map[string]string{"p": strings.Repeat("a", MaxMessage)}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("EncodeRequest of a large message: %v", err)
	}
}

func TestReadLine(t *testing.T) {
	read := func(in string) ([]string, error) {
		br := bufio.NewReaderSize(strings.NewReader(in), 16)
		var out []string
		for {
			l, err := ReadLine(br)
			if err != nil {
				return out, err
			}
			out = append(out, string(l))
		}
	}
	got, err := read("a\r\nbb\n" + strings.Repeat("c", 100) + "\n")
	if !errors.Is(err, io.EOF) || len(got) != 3 || got[0] != "a" || got[1] != "bb" || len(got[2]) != 100 {
		t.Errorf("lines: %q, %v", got, err)
	}
	if _, err := read("abc"); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("unterminated: %v", err)
	}
	if _, err := read(strings.Repeat("x", MaxMessage+5) + "\n"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too long: %v", err)
	}
	if _, err := read(strings.Repeat("x", 3*MaxMessage)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too long without line end: %v", err)
	}
	exact := strings.Repeat("x", MaxMessage) + "\n"
	if got, err := read(exact); len(got) != 1 || !errors.Is(err, io.EOF) {
		t.Errorf("exact limit: %d lines, %v", len(got), err)
	}
	var buf bytes.Buffer
	buf.WriteString(strings.Repeat("y", MaxMessage+1) + "\n")
	if _, err := ReadLine(bufio.NewReader(&buf)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("limit+1: %v", err)
	}
}
