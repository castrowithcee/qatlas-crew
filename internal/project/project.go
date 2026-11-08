package project

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-kit/configfile"
)

// Schema is the version of the configuration file this package reads and writes.
const Schema = 1

// MaxName is the longest project name in characters.
const MaxName = 64

// The kinds of error the operations return; errors.Is matches an *Error against its kind.
var (
	ErrInvalid  = errors.New("invalid project")
	ErrExists   = errors.New("project already exists")
	ErrNotFound = errors.New("project not found")
	// ErrConfig marks a configuration file that cannot be read as it is.
	ErrConfig = errors.New("invalid configuration")
)

// Error is a refusal with a message for the user.
type Error struct {
	Kind    error
	Message string
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Kind }

func refuse(kind error, format string, args ...any) error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// Project is a directory qcrew works in.
type Project struct {
	ID      string    `yaml:"id"`
	Name    string    `yaml:"name"`
	Path    string    `yaml:"path"`
	AddedAt time.Time `yaml:"added_at"`
}

// Config is the content of the configuration file. Rules of later features get their own keys here; until
// then a key this version does not know is refused, so a mistyped hand edit is never dropped silently.
type Config struct {
	Schema   int       `yaml:"schema"`
	Projects []Project `yaml:"projects"`
}

// idEncoding writes ids in lower case so that they read and type easily.
var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

var idPattern = regexp.MustCompile(`^[a-z2-7]{12}$`)

// NewID returns a random project id of 12 base32 characters (60 bits).
func NewID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("create a project id: %w", err)
	}
	return idEncoding.EncodeToString(b[:])[:12], nil
}

// ValidName checks a project name: not empty, at most MaxName characters, no control characters, no path
// separator and no surrounding white space.
func ValidName(name string) error {
	switch {
	case name == "":
		return refuse(ErrInvalid, "the project name is empty")
	case !utf8.ValidString(name):
		return refuse(ErrInvalid, "the project name is not valid UTF-8")
	case utf8.RuneCountInString(name) > MaxName:
		return refuse(ErrInvalid, "the project name is longer than %d characters", MaxName)
	case strings.TrimSpace(name) != name:
		return refuse(ErrInvalid, "the project name %q starts or ends with white space", name)
	case strings.ContainsAny(name, `/\`):
		return refuse(ErrInvalid, "the project name %q contains a path separator", name)
	case strings.ContainsFunc(name, unicode.IsControl):
		return refuse(ErrInvalid, "the project name %q contains a control character", name)
	}
	return nil
}

// Validate checks a configuration as a whole. It does not look at the file system: a project whose
// directory is gone stays registered until it is removed.
func Validate(c Config) error {
	if c.Schema != Schema {
		if c.Schema == 0 {
			return fmt.Errorf("%w: the key schema is missing; this qcrew reads schema %d", ErrConfig, Schema)
		}
		return fmt.Errorf("%w: schema %d is not supported; this qcrew reads schema %d", ErrConfig, c.Schema, Schema)
	}
	ids := make(map[string]bool, len(c.Projects))
	names := make(map[string]bool, len(c.Projects))
	paths := make(map[string]bool, len(c.Projects))
	for i, p := range c.Projects {
		at := fmt.Sprintf("project %d", i+1)
		if !idPattern.MatchString(p.ID) {
			return fmt.Errorf("%w: %s: the id %q is not 12 characters of a-z and 2-7", ErrConfig, at, p.ID)
		}
		if err := ValidName(p.Name); err != nil {
			return fmt.Errorf("%w: %s: %s", ErrConfig, at, err.Error())
		}
		if !filepath.IsAbs(p.Path) || filepath.Clean(p.Path) != p.Path {
			return fmt.Errorf("%w: %s: the path %q is not a clean absolute path", ErrConfig, at, p.Path)
		}
		if p.AddedAt.IsZero() {
			return fmt.Errorf("%w: %s: added_at is missing", ErrConfig, at)
		}
		switch {
		case ids[p.ID]:
			return fmt.Errorf("%w: %s: the id %s is used twice", ErrConfig, at, p.ID)
		case names[p.Name]:
			return fmt.Errorf("%w: %s: the name %q is used twice", ErrConfig, at, p.Name)
		case paths[p.Path]:
			return fmt.Errorf("%w: %s: the path %s is registered twice", ErrConfig, at, p.Path)
		}
		ids[p.ID], names[p.Name], paths[p.Path] = true, true, true
	}
	// A name that equals an id would make a reference by id or name ambiguous.
	for _, p := range c.Projects {
		if ids[p.Name] && p.Name != p.ID {
			return fmt.Errorf("%w: the name %q equals the id of another project", ErrConfig, p.Name)
		}
	}
	return nil
}

// Encode writes the canonical form of a configuration.
func Encode(c Config) ([]byte, error) {
	if c.Projects == nil {
		c.Projects = []Project{}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decode reads a configuration strictly: an unknown key, a second document or an empty file is an error.
func Decode(data []byte) (Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, fmt.Errorf("%w: the file is empty; it must start with schema: %d", ErrConfig, Schema)
		}
		return Config{}, fmt.Errorf("%w: %s", ErrConfig, err.Error())
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("%w: the file holds more than one YAML document", ErrConfig)
	}
	return c, nil
}

// Codec is the codec of the configuration file for configfile.NewStore.
func Codec() configfile.Codec[Config] {
	return configfile.Codec[Config]{Validate: Validate, Encode: Encode, Decode: Decode}
}

// Find returns the project whose id or, failing that, whose name is ref.
func (c Config) Find(ref string) (Project, int, bool) {
	for i, p := range c.Projects {
		if p.ID == ref {
			return p, i, true
		}
	}
	for i, p := range c.Projects {
		if p.Name == ref {
			return p, i, true
		}
	}
	return Project{}, -1, false
}
