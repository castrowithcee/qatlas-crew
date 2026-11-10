//go:build e2e

package term

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// scene prints every kind of cell style, then a marker, and keeps the window open.
const scene = `printf 'plain text\n'
printf '\033[31mred\033[0m \033[1;32;44mbold green on blue\033[0m\n'
printf '\033[38;5;208morange 256\033[0m \033[38;2;10;20;30;48;2;200;100;50mtruecolor\033[0m\n'
printf '\033[3mitalic\033[0m \033[4munderline\033[0m \033[7mreverse\033[0m \033[2mfaint\033[0m \033[9mstrike\033[0m\n'
printf 'wide: \346\227\245\346\234\254\350\252\236 e\314\201\n'
echo END
exec sleep 600`

var goldenSizes = map[string][2]int{"narrow": {60, 30}, "wide": {120, 40}}

// visible makes escape characters readable in a golden file.
func visible(s string) string { return strings.ReplaceAll(s, "\x1b", `\e`) }

func TestGoldenColoredShell(t *testing.T) {
	for name, size := range goldenSizes {
		t.Run(name, func(t *testing.T) {
			cols, rows := size[0], size[1]
			_, _, argv := newWindow(t, scene)
			tm := attach(t, argv, cols, rows)
			waitFor(t, "END", func() bool { return strings.Contains(plain(tm), "END") })
			// tmux repaints after the first output; wait until the picture rests.
			var last string
			stable := 0
			waitFor(t, "stable screen", func() bool {
				cur := tm.View(cols, rows)
				if cur == last {
					stable++
				} else {
					last, stable = cur, 0
				}
				return stable >= 10
			})
			got := visible(last) + "\n"
			path := filepath.Join("testdata", "colored-shell."+name+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run just snap-update)", err)
			}
			if string(want) != got {
				t.Errorf("screen differs from %s (run just snap-update and review the diff)\n--- got\n%s", path, got)
			}
		})
	}
}
