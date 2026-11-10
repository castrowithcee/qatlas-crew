//go:build e2e

package tui

import (
	"strings"
	"testing"
)

// The session view with a shell in a tmux window: the layout with the list, the screen and the status line,
// and the full screen of the view. The cursor is hidden so the screen holds no escape sequence.
func TestGoldenSessionView(t *testing.T) {
	const script = "printf '\\033[?25l'; printf 'first line\\n\\303\\244\\303\\266\\303\\274 utf-8 line\\n'; exec sleep 600"
	screens := []struct {
		name string
		keys []string
	}{
		{"session-view", []string{"enter"}},
		{"full-session-view", []string{"enter", "right", "f"}},
	}
	for _, s := range screens {
		for lang, l := range goldenLangs {
			for size, wh := range goldenSizes {
				t.Run(s.name+"."+lang+"."+size, func(t *testing.T) {
					w := newWindows(t, script, "s1", "s2", "s3")
					m := newViewModel(t, l, goldenBackend(), wh[0], wh[1], w.attach)
					step(m, s.keys...)
					waitViewHas(t, m, "utf-8 line")
					screen := m.render()
					if strings.Contains(screen, "\x1b") {
						t.Fatalf("the screen holds an escape sequence: %q", screen)
					}
					checkScreen(t, screen, wh[0], wh[1])
					checkGolden(t, s.name+"."+lang+"."+size+".golden", screen+"\n")
				})
			}
		}
	}
}
