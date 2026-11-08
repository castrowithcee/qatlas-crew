package session

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// escapeSeq matches one escape sequence at the start of a string: CSI, OSC ended by BEL or ST, or a
// two-character escape with optional intermediates.
var escapeSeq = regexp.MustCompile(`^\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[ -/]*[0-~])`)

var sgrSeq = regexp.MustCompile(`^\x1b\[([0-9;:]*)m$`)

var ruleLine = regexp.MustCompile(`^─{3,}\s*$`)

type cell struct {
	r     rune
	quiet bool
}

// Plain returns the text of a `capture-pane -e` dump without escape sequences. On the input line of an agent
// (Claude Code: ❯ between two rule lines; Codex: ›) only the glyph, one space after it and the text in the
// default foreground without faint are kept, which drops a greyed input suggestion that was never typed.
func Plain(text string) string {
	var faint, coloured bool
	sgr := func(params string) {
		codes := strings.Split(params, ";")
		for i := 0; i < len(codes); i++ {
			if codes[i] == "" {
				faint, coloured = false, false
				continue
			}
			head, _, _ := strings.Cut(codes[i], ":")
			code := 0 // an empty first sub-parameter counts as 0
			if head != "" {
				var err error
				if code, err = strconv.Atoi(head); err != nil {
					continue
				}
			}
			switch {
			case code == 0:
				faint, coloured = false, false
			case code == 2:
				faint = true
			case code == 22:
				faint = false
			case code == 39:
				coloured = false
			case code >= 30 && code <= 37, code >= 90 && code <= 97:
				coloured = true
			case code == 38 || code == 48 || code == 58:
				if code == 38 {
					coloured = true
				}
				if !strings.Contains(codes[i], ":") && i+1 < len(codes) {
					// The colour arguments follow as separate parameters: 5;n or 2;r;g;b.
					switch codes[i+1] {
					case "5":
						i += 2
					case "2":
						i += 4
					}
				}
			}
		}
	}

	raws := strings.Split(text, "\n")
	rows := make([][]cell, len(raws))
	plain := make([]string, len(raws))
	for n, raw := range raws {
		var cells []cell
		for i := 0; i < len(raw); {
			if raw[i] == 0x1b {
				if m := escapeSeq.FindString(raw[i:]); m != "" {
					if p := sgrSeq.FindStringSubmatch(m); p != nil {
						sgr(p[1])
					}
					i += len(m)
					continue
				}
			}
			r, size := utf8.DecodeRuneInString(raw[i:])
			cells = append(cells, cell{r, faint || coloured})
			i += size
		}
		rows[n] = cells
		var b strings.Builder
		for _, c := range cells {
			b.WriteRune(c.r)
		}
		plain[n] = b.String()
	}

	out := make([]string, len(rows))
	for n, cells := range rows {
		out[n] = plain[n]
		if len(cells) == 0 {
			continue
		}
		glyph := cells[0].r
		claude := glyph == '❯' && n > 0 && n+1 < len(rows) && ruleLine.MatchString(plain[n-1]) && ruleLine.MatchString(plain[n+1])
		if !claude && glyph != '›' {
			continue
		}
		var b strings.Builder
		for at, c := range cells {
			if at == 0 || (at == 1 && c.r == ' ') || !c.quiet {
				b.WriteRune(c.r)
			}
		}
		out[n] = b.String()
	}
	return strings.Join(out, "\n")
}
