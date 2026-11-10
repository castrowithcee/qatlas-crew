package term

import (
	"strings"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// screen is what render needs from the VT state.
type screen interface {
	Width() int
	Height() int
	CellAt(x, y int) *uv.Cell
	CursorPosition() uv.Position
	CursorHidden() bool
}

// render draws the top left width x height cells of the screen as lines joined by "\n". Rows and columns the
// screen does not have are blank. The text of every cell is cleaned of control characters and only SGR
// sequences made from the cell styles are added, so nothing a program printed can reach the outer terminal
// as a control sequence. The cursor is drawn as a cell in reverse video.
func render(s screen, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	cur := s.CursorPosition()
	showCursor := !s.CursorHidden()
	var b strings.Builder
	for y := 0; y < height; y++ {
		if y > 0 {
			b.WriteByte('\n')
		}
		var style uv.Style
		for x := 0; x < width; {
			var cell *uv.Cell
			if x < s.Width() && y < s.Height() {
				cell = s.CellAt(x, y)
			}
			next, text, w := uv.Style{}, " ", 1
			if cell != nil {
				next, text = cell.Style, clean(cell.Content)
				if cell.Width > 0 {
					w = cell.Width
				}
			}
			if x+w > width {
				// A wide character that does not fit into the last column.
				text, w, next = " ", 1, uv.Style{}
			}
			if showCursor && cur.X == x && cur.Y == y {
				next.Attrs ^= uv.AttrReverse
			}
			if !style.Equal(&next) {
				b.WriteString(uv.StyleDiff(&style, &next))
				style = next
			}
			b.WriteString(text)
			x += w
		}
		if !style.IsZero() {
			b.WriteString(ansi.ResetStyle)
		}
	}
	return b.String()
}

// clean returns the text of a cell with control characters replaced, an empty cell is a space.
func clean(s string) string {
	if s == "" {
		return " "
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return " "
		}
	}
	return s
}
