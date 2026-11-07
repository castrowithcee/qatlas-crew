package testenv

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestSetupPointsEverythingIntoTemp(t *testing.T) {
	realHome := os.Getenv("HOME")
	e := Setup(t)
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "TMPDIR"} {
		v := os.Getenv(k)
		if v == "" || !strings.HasPrefix(v, e.Root+string(os.PathSeparator)) {
			t.Errorf("%s = %q, want a directory under %q", k, v, e.Root)
		}
		if realHome != "" && v == realHome {
			t.Errorf("%s is the real home", k)
		}
		if st, err := os.Stat(v); err != nil || !st.IsDir() {
			t.Errorf("%s: %q is not an existing directory (%v)", k, v, err)
		}
	}
	if st, _ := os.Stat(e.Runtime); st.Mode().Perm() != 0o700 {
		t.Errorf("runtime dir mode = %v, want 0700", st.Mode().Perm())
	}
	for k, want := range map[string]string{"LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "NO_COLOR": "1", "COLUMNS": Columns, "LINES": Lines, "TZ": "UTC"} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestEnvironHasNoForeignVariables(t *testing.T) {
	e := Setup(t)
	for _, kv := range e.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if k == "PATH" {
			continue
		}
		if strings.Contains(v, "/") && !strings.HasPrefix(v, e.Root) {
			t.Errorf("%s=%q points outside the test directory", k, v)
		}
	}
}

func TestFakeClock(t *testing.T) {
	start := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	var c Clock = NewFakeClock(start)
	if !c.Now().Equal(start) {
		t.Fatalf("Now = %v", c.Now())
	}
	c.(*FakeClock).Advance(90 * time.Second)
	if got := c.Now().Sub(start); got != 90*time.Second {
		t.Fatalf("advanced by %v", got)
	}
}

func TestRandIsReproducible(t *testing.T) {
	if a, b := Rand(t, 42).Int63(), Rand(t, 42).Int63(); a != b {
		t.Fatalf("same seed gave %d and %d", a, b)
	}
}
