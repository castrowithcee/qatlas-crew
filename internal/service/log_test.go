package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-crew/internal/testenv"
)

func TestLogRotates(t *testing.T) {
	dir := filepath.Join(testenv.Setup(t).Root, "logs")
	l, err := openLog(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	first := strings.Repeat("a", 90) + "\n"
	for _, s := range []string{first, first, "after\n"} {
		if _, err := l.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	// The second write passes the limit; the third one finds it passed and rotates first.
	old, err := os.ReadFile(filepath.Join(dir, "service.log.1"))
	if err != nil || string(old) != first+first {
		t.Fatalf("rotated file: %q, %v", old, err)
	}
	cur, err := os.ReadFile(filepath.Join(dir, "service.log"))
	if err != nil || string(cur) != "after\n" {
		t.Fatalf("current file: %q, %v", cur, err)
	}
	if info, _ := os.Stat(filepath.Join(dir, "service.log")); info.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v", info.Mode().Perm())
	}
}
