package client

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/output"
)

func TestFailureMapsCodes(t *testing.T) {
	cases := map[string]output.Code{
		"invalid-request":     output.CodeInvalidRequest,
		"unsupported-version": output.CodeInvalidRequest,
		"unknown-operation":   output.CodeNotFound,
		"runtime":             output.CodeRuntime,
		"something-new":       output.CodeRuntime,
		"":                    output.CodeRuntime,
	}
	for in, want := range cases {
		f := Failure(in, "m")
		if f.Code != want {
			t.Errorf("code %q -> %q, want %q", in, f.Code, want)
		}
		if !output.ValidCode(f.Code) || f.NextStep == "" || f.Message == "" {
			t.Errorf("code %q gives an invalid failure: %+v", in, f)
		}
	}
}

func TestFailureCleansMessage(t *testing.T) {
	f := Failure("runtime", "bad\x1b[31m\nline"+strings.Repeat("x", 500))
	if strings.ContainsAny(f.Message, "\x1b\n") || len(f.Message) > 250 {
		t.Errorf("message not cleaned: %q", f.Message)
	}
	if got := Failure("runtime", " \n").Message; got != "the service reported an error" {
		t.Errorf("empty message: %q", got)
	}
}
