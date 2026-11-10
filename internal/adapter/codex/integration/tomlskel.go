package integration

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A reader of the outline of a TOML file: its table headers and keys with their byte spans. Values are only
// skipped, never interpreted, so that config.toml is changed by editing whole tables of qcrew as text and the
// rest of the file stays byte for byte.

type tomlTable struct {
	path  []string
	array bool
	start int // the start of the header line
	end   int // the end of its last key line, line break included; trailing comments are not part of it
}

type tomlKey struct {
	table int      // index into tables, -1 for the root table
	path  []string // full path from the root
	local []string // path as written
	value string   // raw value text
}

type tomlDoc struct {
	data   []byte
	tables []tomlTable
	keys   []tomlKey
}

const maxTOMLDepth = 64

var tomlDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type tscan struct {
	d   []byte
	pos int
}

func parseTOML(data []byte) (*tomlDoc, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("not valid UTF-8")
	}
	doc := &tomlDoc{data: data}
	s := &tscan{d: data}
	cur := -1
	var curPath []string
	for {
		lineStart := s.pos
		s.blank()
		if s.eof() {
			break
		}
		switch c := s.d[s.pos]; {
		case c == '\n' || s.at("\r\n"):
			s.newline()
		case c == '#':
			s.comment()
		case c == '[':
			array := s.at("[[")
			s.pos++
			if array {
				s.pos++
			}
			path, err := s.key()
			if err != nil {
				return nil, err
			}
			closing := "]"
			if array {
				closing = "]]"
			}
			if !s.at(closing) {
				return nil, s.errorf("expected %s", closing)
			}
			s.pos += len(closing)
			if err := s.lineEnd(); err != nil {
				return nil, err
			}
			doc.tables = append(doc.tables, tomlTable{path: path, array: array, start: lineStart, end: s.pos})
			cur, curPath = len(doc.tables)-1, path
		default:
			local, err := s.key()
			if err != nil {
				return nil, err
			}
			if !s.at("=") {
				return nil, s.errorf("expected =")
			}
			s.pos++
			s.blank()
			vs := s.pos
			if err := s.skipValue(0); err != nil {
				return nil, err
			}
			value := string(s.d[vs:s.pos])
			if err := s.lineEnd(); err != nil {
				return nil, err
			}
			full := append(slices.Clone(curPath), local...)
			doc.keys = append(doc.keys, tomlKey{table: cur, path: full, local: local, value: value})
			if cur >= 0 {
				doc.tables[cur].end = s.pos
			}
		}
	}
	return doc, nil
}

func (s *tscan) errorf(format string, a ...any) error {
	line := 1 + bytes.Count(s.d[:min(s.pos, len(s.d))], []byte("\n"))
	return fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, a...))
}

func (s *tscan) eof() bool          { return s.pos >= len(s.d) }
func (s *tscan) at(t string) bool   { return bytes.HasPrefix(s.d[s.pos:], []byte(t)) }
func (s *tscan) peekIs(c byte) bool { return !s.eof() && s.d[s.pos] == c }

func (s *tscan) blank() {
	for !s.eof() && (s.d[s.pos] == ' ' || s.d[s.pos] == '\t') {
		s.pos++
	}
}

func (s *tscan) newline() bool {
	switch {
	case s.at("\n"):
		s.pos++
	case s.at("\r\n"):
		s.pos += 2
	default:
		return false
	}
	return true
}

func (s *tscan) comment() {
	for !s.eof() && s.d[s.pos] != '\n' && !s.at("\r\n") {
		s.pos++
	}
}

// gap skips blanks, line breaks and comments, as they may stand between the elements of an array.
func (s *tscan) gap() {
	for {
		s.blank()
		switch {
		case s.peekIs('#'):
			s.comment()
		case s.newline():
		default:
			return
		}
	}
}

func (s *tscan) lineEnd() error {
	s.blank()
	if s.peekIs('#') {
		s.comment()
	}
	if s.eof() || s.newline() {
		return nil
	}
	return s.errorf("expected the end of the line")
}

func (s *tscan) key() ([]string, error) {
	var path []string
	for {
		s.blank()
		part, err := s.keyPart()
		if err != nil {
			return nil, err
		}
		path = append(path, part)
		s.blank()
		if !s.peekIs('.') {
			return path, nil
		}
		s.pos++
	}
}

func isBare(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '_' || c == '-'
}

func (s *tscan) keyPart() (string, error) {
	switch {
	case s.peekIs('"'):
		return s.basicString()
	case s.peekIs('\''):
		return s.literalString()
	}
	start := s.pos
	for !s.eof() && isBare(s.d[s.pos]) {
		s.pos++
	}
	if s.pos == start {
		return "", s.errorf("expected a key")
	}
	return string(s.d[start:s.pos]), nil
}

func (s *tscan) basicString() (string, error) {
	s.pos++
	var b strings.Builder
	for !s.eof() {
		c := s.d[s.pos]
		switch {
		case c == '"':
			s.pos++
			return b.String(), nil
		case c == '\\':
			r, err := s.escape()
			if err != nil {
				return "", err
			}
			b.WriteRune(r)
		case c == '\n' || c == '\r':
			return "", s.errorf("line break in a string")
		default:
			b.WriteByte(c)
			s.pos++
		}
	}
	return "", s.errorf("unterminated string")
}

func (s *tscan) escape() (rune, error) {
	if s.pos+1 >= len(s.d) {
		return 0, s.errorf("unterminated string")
	}
	simple := map[byte]rune{'b': '\b', 't': '\t', 'n': '\n', 'f': '\f', 'r': '\r', 'e': 0x1b, '"': '"', '\\': '\\'}
	c := s.d[s.pos+1]
	if r, ok := simple[c]; ok {
		s.pos += 2
		return r, nil
	}
	n := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
	if n == 0 || s.pos+2+n > len(s.d) {
		return 0, s.errorf("invalid escape")
	}
	v, err := strconv.ParseUint(string(s.d[s.pos+2:s.pos+2+n]), 16, 32)
	if err != nil || !utf8.ValidRune(rune(v)) {
		return 0, s.errorf("invalid escape")
	}
	s.pos += 2 + n
	return rune(v), nil
}

func (s *tscan) literalString() (string, error) {
	s.pos++
	start := s.pos
	for !s.eof() {
		switch s.d[s.pos] {
		case '\'':
			s.pos++
			return string(s.d[start : s.pos-1]), nil
		case '\n', '\r':
			return "", s.errorf("line break in a string")
		}
		s.pos++
	}
	return "", s.errorf("unterminated string")
}

func (s *tscan) skipMultiline(delim string, escapes bool) error {
	s.pos += len(delim)
	for !s.eof() {
		if escapes && s.d[s.pos] == '\\' {
			s.pos += 2
			continue
		}
		if s.at(delim) {
			s.pos += len(delim)
			// Up to two more quotes end the content, not the string.
			for i := 0; i < 2 && s.peekIs(delim[0]); i++ {
				s.pos++
			}
			return nil
		}
		s.pos++
	}
	return s.errorf("unterminated string")
}

func (s *tscan) skipValue(depth int) error {
	if depth > maxTOMLDepth {
		return s.errorf("nested too deeply")
	}
	if s.eof() {
		return s.errorf("expected a value")
	}
	switch {
	case s.at(`"""`):
		return s.skipMultiline(`"""`, true)
	case s.at(`'''`):
		return s.skipMultiline(`'''`, false)
	case s.peekIs('"'):
		_, err := s.basicString()
		return err
	case s.peekIs('\''):
		_, err := s.literalString()
		return err
	case s.peekIs('['):
		s.pos++
		for {
			s.gap()
			if s.peekIs(']') {
				s.pos++
				return nil
			}
			if err := s.skipValue(depth + 1); err != nil {
				return err
			}
			s.gap()
			switch {
			case s.peekIs(','):
				s.pos++
			case s.peekIs(']'):
				s.pos++
				return nil
			default:
				return s.errorf("expected a comma or a closing bracket")
			}
		}
	case s.peekIs('{'):
		s.pos++
		for {
			s.gap()
			if s.peekIs('}') {
				s.pos++
				return nil
			}
			if _, err := s.key(); err != nil {
				return err
			}
			if !s.peekIs('=') {
				return s.errorf("expected =")
			}
			s.pos++
			s.blank()
			if err := s.skipValue(depth + 1); err != nil {
				return err
			}
			s.gap()
			switch {
			case s.peekIs(','):
				s.pos++
			case s.peekIs('}'):
				s.pos++
				return nil
			default:
				return s.errorf("expected a comma or a closing brace")
			}
		}
	}
	start := s.pos
	for {
		for !s.eof() && !strings.ContainsRune(" \t\r\n,]}#", rune(s.d[s.pos])) {
			s.pos++
		}
		// A date and a time may be separated by a space.
		if tomlDate.Match(s.d[start:s.pos]) && s.at(" ") && s.pos+1 < len(s.d) && '0' <= s.d[s.pos+1] &&
			s.d[s.pos+1] <= '9' {
			s.pos++
			continue
		}
		break
	}
	if s.pos == start {
		return s.errorf("expected a value")
	}
	return nil
}

// tomlStringValue decodes a raw value that is a one-line string.
func tomlStringValue(raw string) (string, bool) {
	s := &tscan{d: []byte(raw)}
	var v string
	var err error
	switch {
	case s.at(`"""`), s.at(`'''`):
		return "", false
	case s.peekIs('"'):
		v, err = s.basicString()
	case s.peekIs('\''):
		v, err = s.literalString()
	default:
		return "", false
	}
	return v, err == nil && s.eof()
}

// tomlQuote writes s as a TOML basic string.
func tomlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func hasPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

// stateTable returns the index of the table [hooks.state."<key>"], -1 if there is none. It fails when the
// approval, or a table above it, is written in a form that cannot be changed by editing whole tables: as a
// value, with dotted keys outside its own table, as an array of tables, or with sub-tables.
func (d *tomlDoc) stateTable(key string) (int, error) {
	target := []string{"hooks", "state", key}
	found := -1
	for i, t := range d.tables {
		switch {
		case t.array && hasPrefix(target, t.path), len(t.path) > len(target) && hasPrefix(t.path, target):
			return -1, fmt.Errorf("the approval %s is not a plain table", tomlQuote(key))
		case !t.array && slices.Equal(t.path, target):
			if found >= 0 {
				return -1, fmt.Errorf("the table of the approval %s is defined twice", tomlQuote(key))
			}
			found = i
		}
	}
	for _, k := range d.keys {
		inside := k.table >= 0 && slices.Equal(d.tables[k.table].path, target)
		if hasPrefix(target, k.path) || !inside && hasPrefix(k.path, target) {
			return -1, fmt.Errorf("the approval %s or a table above it is written as a value", tomlQuote(key))
		}
	}
	return found, nil
}

// approval reads the hash a table approves and whether it disables the hook.
func (d *tomlDoc) approval(table int) (hash string, disabled bool) {
	for _, k := range d.keys {
		if k.table != table || len(k.local) != 1 {
			continue
		}
		switch k.local[0] {
		case "trusted_hash":
			hash, _ = tomlStringValue(k.value)
		case "enabled":
			disabled = k.value == "false"
		}
	}
	return hash, disabled
}
