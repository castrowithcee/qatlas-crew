package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"unicode/utf8"
)

// A strict JSON reader that keeps the byte span of every value, so that hooks.json can be changed by inserting
// and deleting text while every other byte stays as it is.

type jkind byte

const (
	jScalar jkind = iota
	jString
	jArray
	jObject
)

type jvalue struct {
	kind       jkind
	start, end int
	members    []jmember // object
	elems      []*jvalue // array
	str        string    // string, decoded
	raw        string    // scalar text
}

type jmember struct {
	key   string
	start int // the opening quote of the key
	value *jvalue
}

func (v *jvalue) member(key string) *jvalue {
	for _, m := range v.members {
		if m.key == key {
			return m.value
		}
	}
	return nil
}

func (v *jvalue) isNull() bool { return v.kind == jScalar && v.raw == "null" }

const maxJSONDepth = 128

var jsonNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?`)

type jparser struct {
	data  []byte
	pos   int
	depth int
}

// parseJSON reads one JSON value. It refuses what serde_json, which Codex reads the file with, refuses too:
// invalid UTF-8, a byte order mark, comments and trailing commas. It also refuses duplicate keys.
func parseJSON(data []byte) (*jvalue, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("not valid UTF-8")
	}
	p := &jparser{data: data}
	p.space()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.space()
	if p.pos != len(data) {
		return nil, p.errorf("unexpected text after the value")
	}
	return v, nil
}

func (p *jparser) errorf(format string, a ...any) error {
	line := 1 + bytes.Count(p.data[:min(p.pos, len(p.data))], []byte("\n"))
	return fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, a...))
}

func (p *jparser) space() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jparser) peek(c byte) bool { return p.pos < len(p.data) && p.data[p.pos] == c }

func (p *jparser) value() (*jvalue, error) {
	if p.pos >= len(p.data) {
		return nil, p.errorf("unexpected end of the file")
	}
	switch p.data[p.pos] {
	case '{':
		return p.object()
	case '[':
		return p.array()
	case '"':
		start := p.pos
		s, err := p.str()
		if err != nil {
			return nil, err
		}
		return &jvalue{kind: jString, start: start, end: p.pos, str: s}, nil
	}
	start := p.pos
	n := 0
	for _, lit := range []string{"true", "false", "null"} {
		if bytes.HasPrefix(p.data[p.pos:], []byte(lit)) {
			n = len(lit)
		}
	}
	if n == 0 {
		n = len(jsonNumber.Find(p.data[p.pos:]))
	}
	if n == 0 {
		return nil, p.errorf("unexpected character")
	}
	p.pos += n
	return &jvalue{kind: jScalar, start: start, end: p.pos, raw: string(p.data[start:p.pos])}, nil
}

func (p *jparser) enter() error {
	if p.depth++; p.depth > maxJSONDepth {
		return p.errorf("nested too deeply")
	}
	return nil
}

func (p *jparser) object() (*jvalue, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	v := &jvalue{kind: jObject, start: p.pos}
	p.pos++
	p.space()
	if p.peek('}') {
		p.pos++
		v.end = p.pos
		return v, nil
	}
	seen := map[string]bool{}
	for {
		p.space()
		if !p.peek('"') {
			return nil, p.errorf("expected a key")
		}
		start := p.pos
		key, err := p.str()
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, p.errorf("duplicate key %q", key)
		}
		seen[key] = true
		p.space()
		if !p.peek(':') {
			return nil, p.errorf("expected a colon")
		}
		p.pos++
		p.space()
		val, err := p.value()
		if err != nil {
			return nil, err
		}
		v.members = append(v.members, jmember{key: key, start: start, value: val})
		p.space()
		switch {
		case p.peek(','):
			p.pos++
		case p.peek('}'):
			p.pos++
			v.end = p.pos
			return v, nil
		default:
			return nil, p.errorf("expected a comma or a closing brace")
		}
	}
}

func (p *jparser) array() (*jvalue, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	v := &jvalue{kind: jArray, start: p.pos}
	p.pos++
	p.space()
	if p.peek(']') {
		p.pos++
		v.end = p.pos
		return v, nil
	}
	for {
		p.space()
		el, err := p.value()
		if err != nil {
			return nil, err
		}
		v.elems = append(v.elems, el)
		p.space()
		switch {
		case p.peek(','):
			p.pos++
		case p.peek(']'):
			p.pos++
			v.end = p.pos
			return v, nil
		default:
			return nil, p.errorf("expected a comma or a closing bracket")
		}
	}
}

func isHex(c byte) bool { return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F' }

func (p *jparser) str() (string, error) {
	start := p.pos
	p.pos++
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		switch {
		case c == '"':
			p.pos++
			var s string
			if err := json.Unmarshal(p.data[start:p.pos], &s); err != nil {
				return "", p.errorf("invalid string")
			}
			return s, nil
		case c == '\\':
			if p.pos+1 >= len(p.data) {
				return "", p.errorf("unterminated string")
			}
			switch p.data[p.pos+1] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				p.pos += 2
			case 'u':
				if p.pos+6 > len(p.data) || !isHex(p.data[p.pos+2]) || !isHex(p.data[p.pos+3]) ||
					!isHex(p.data[p.pos+4]) || !isHex(p.data[p.pos+5]) {
					return "", p.errorf("invalid escape")
				}
				p.pos += 6
			default:
				return "", p.errorf("invalid escape")
			}
		case c < 0x20:
			return "", p.errorf("control character in a string")
		default:
			p.pos++
		}
	}
	return "", p.errorf("unterminated string")
}
