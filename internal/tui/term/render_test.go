package term

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

func TestRenderKeepsColorsAttributesAndCursor(t *testing.T) {
	em := vt.NewEmulator(10, 3)
	_, _ = em.Write([]byte("\x1b[1;31mred\x1b[0m \x1b[38;5;208;44mx\x1b[0m\r\nab"))
	got := render(em, 10, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(lines), got)
	}
	for _, want := range []string{"\x1b[31;1mred", "38;5;208", "44m"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("line 0 lacks %q: %q", want, lines[0])
		}
	}
	// The cursor sits after "ab" in reverse video.
	if !strings.Contains(lines[1], "\x1b[7m") {
		t.Errorf("cursor cell is not in reverse video: %q", lines[1])
	}
	_, _ = em.Write([]byte("\x1b[?25l"))
	if strings.Contains(render(em, 10, 3), "\x1b[7m") {
		t.Error("hidden cursor is drawn")
	}
}

func TestRenderCropsAndPadsToTheRectangle(t *testing.T) {
	em := vt.NewEmulator(10, 3)
	_, _ = em.Write([]byte("0123456789"))
	got := render(em, 4, 5)
	lines := strings.Split(got, "\n")
	if len(lines) != 5 || !strings.HasPrefix(lines[0], "0123") || strings.Contains(lines[0], "4") {
		t.Fatalf("not cropped to 4x5: %q", got)
	}
}

func TestRenderCleansControlCharactersOfCells(t *testing.T) {
	em := vt.NewEmulator(10, 1)
	_, _ = em.Write([]byte("a\x1b]0;title\x07\x1b]52;c;SGk=\x07b"))
	got := render(em, 10, 1)
	for _, bad := range []string{"\x1b]", "\x07", "title", "SGk="} {
		if strings.Contains(got, bad) {
			t.Errorf("output contains %q: %q", bad, got)
		}
	}
}
