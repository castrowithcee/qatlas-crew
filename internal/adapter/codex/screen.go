package codex

import "strings"

// The features of the screen that ReadScreen looks for. They are unverified until test-ext has seen them on
// the screen of the real tool; keep them narrow and here.
const (
	// readyMarkers show in the footer while the tool waits for input.
	readyMarkerShortcuts = "? for shortcuts"
	readyMarkerSend      = "⏎ send"
	// errorGlyph starts a line that reports an error; the line must also name one of errorWords.
	errorGlyph = "■"
	// How many of the last non-empty lines count for the footer and for an error line.
	readyTail = 4
	errorTail = 15
)

var errorWords = []string{"error", "unexpected status"}

// ReadScreen reads the screen lines of a session, oldest first, as plain text: ready is set if the footer
// shows that the tool waits for input, and errLine is the last line near the bottom that reports an error,
// empty if there is none. The lines are foreign text and are only compared, never run.
func ReadScreen(lines []string) (ready bool, errLine string) {
	var rows []string
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			rows = append(rows, t)
		}
	}
	for i, row := range rows {
		fromEnd := len(rows) - i
		if fromEnd <= readyTail && (strings.Contains(row, readyMarkerShortcuts) || strings.Contains(row, readyMarkerSend)) {
			ready = true
		}
		if fromEnd <= errorTail && strings.HasPrefix(row, errorGlyph) {
			low := strings.ToLower(row)
			for _, w := range errorWords {
				if strings.Contains(low, w) {
					errLine = row
					break
				}
			}
		}
	}
	return ready, errLine
}
