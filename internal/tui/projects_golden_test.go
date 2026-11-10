//go:build e2e

package tui

import (
	"testing"
)

// The goldens of adding and removing projects and of a missing tool. Run `just snap-update` to rewrite them.
func TestGoldenProjectScreens(t *testing.T) {
	keys := func(k ...string) func(*Model) { return func(m *Model) { send(m, k...) } }
	typed := func(s string) func(*Model) { return func(m *Model) { typeIn(m, s) } }
	screens := []struct {
		name  string
		setup func(*fakeBackend)
		steps []func(*Model)
	}{
		{"add", nil, []func(*Model){keys("n"), typed("work/")}},
		{"add-new", nil, []func(*Model){keys("n"), typed("work/"), keys("enter", "right", "down"), typed("fresh")}},
		{"add-error", nil, []func(*Model){keys("n", "backspace", "backspace"), typed("rel"), keys("f2")}},
		{"remove", nil, []func(*Model){keys("d")}},
		// The check of the programs at start finds claude and codex missing.
		{"tool-missing", nil, []func(*Model){func(m *Model) { drain(m, m.Init()) }, keys("x")}},
	}
	for _, s := range screens {
		for lang, l := range goldenLangs {
			for size, wh := range goldenSizes {
				t.Run(s.name+"."+lang+"."+size, func(t *testing.T) {
					b := goldenBackend()
					if s.setup != nil {
						s.setup(b)
					}
					m, _ := newHomeModel(t, l, b, wh[0], wh[1])
					for _, step := range s.steps {
						step(m)
					}
					screen := m.render()
					checkScreen(t, screen, wh[0], wh[1])
					checkGolden(t, s.name+"."+lang+"."+size+".golden", screen+"\n")
				})
			}
		}
	}
}
