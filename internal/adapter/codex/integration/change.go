package integration

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// keepBackups is how many backups of each file stay.
const keepBackups = 10

// Inspect reports the status of the integration. It only reads.
func (c Config) Inspect() (Report, error) {
	s, err := c.load()
	if err != nil {
		return Report{}, err
	}
	return s.report()
}

// Change is a planned change of both files. Nothing is written before Apply.
type Change struct {
	cfg      Config
	old, new snapshot
	// Warnings name what the change does to hooks of others, or what it leaves alone.
	Warnings []string
}

// Plan computes the change an action makes. It only reads.
func (c Config) Plan(a Action) (*Change, error) {
	s, err := c.load()
	if err != nil {
		return nil, err
	}
	next, warnings, err := s.plan(a)
	if err != nil {
		return nil, err
	}
	return &Change{cfg: c, old: s.snap, new: next, Warnings: warnings}, nil
}

// Empty tells whether the change leaves both files as they are.
func (ch *Change) Empty() bool {
	return sameFile(ch.old.hooks, ch.old.hooksExists, ch.new.hooks, ch.new.hooksExists) &&
		sameFile(ch.old.config, ch.old.configExists, ch.new.config, ch.new.configExists)
}

func sameFile(a []byte, aExists bool, b []byte, bExists bool) bool {
	return aExists == bExists && bytes.Equal(a, b)
}

// Preview is the change as a unified diff of both files, with control characters masked.
func (ch *Change) Preview() string {
	return unifiedDiff(ch.cfg.hooksPath(), ch.old.hooks, ch.new.hooks, ch.old.hooksExists, ch.new.hooksExists) +
		unifiedDiff(ch.cfg.configPath(), ch.old.config, ch.new.config, ch.old.configExists, ch.new.configExists)
}

// Apply writes the change: it checks that both files are still as the preview saw them, backs up the existing
// ones below the data folder, keeping the newest ten of each, and replaces each file atomically, keeping its
// permissions. now names the backups.
func (ch *Change) Apply(now time.Time) error {
	if ch.Empty() {
		return nil
	}
	cur, err := ch.cfg.readFiles()
	if err != nil {
		return err
	}
	if !sameFile(cur.hooks, cur.hooksExists, ch.old.hooks, ch.old.hooksExists) ||
		!sameFile(cur.config, cur.configExists, ch.old.config, ch.old.configExists) {
		return errors.New("the files changed since the preview; inspect them again")
	}
	if err := ch.backup(now); err != nil {
		return err
	}
	if err := os.Mkdir(ch.cfg.CodexHome, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("create the Codex home: %w", err)
	}
	files := []struct {
		path                string
		old, new            []byte
		oldExists, newExist bool
	}{
		{ch.cfg.hooksPath(), ch.old.hooks, ch.new.hooks, ch.old.hooksExists, ch.new.hooksExists},
		{ch.cfg.configPath(), ch.old.config, ch.new.config, ch.old.configExists, ch.new.configExists},
	}
	for _, f := range files {
		if sameFile(f.old, f.oldExists, f.new, f.newExist) {
			continue
		}
		if !f.newExist {
			if err := os.Remove(f.path); err != nil {
				return fmt.Errorf("remove %s: %w", filepath.Base(f.path), err)
			}
			syncDir(filepath.Dir(f.path))
			continue
		}
		if err := writeAtomic(f.path, f.new); err != nil {
			return fmt.Errorf("write %s: %w", filepath.Base(f.path), err)
		}
	}
	return nil
}

func (c Config) load() (*state, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	snap, err := c.readFiles()
	if err != nil {
		return nil, err
	}
	return c.read(snap)
}

func (c Config) readFiles() (snapshot, error) {
	var s snapshot
	var err error
	if s.hooks, s.hooksExists, err = readRegular(c.hooksPath()); err != nil {
		return s, err
	}
	s.config, s.configExists, err = readRegular(c.configPath())
	return s, err
}

// readRegular reads a file that is missing or regular. A symbolic link is refused: replacing it would cut the
// link, and following it would write outside the Codex home.
func readRegular(path string) ([]byte, bool, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !fi.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%s is not a regular file; qcrew does not change it", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	return data, true, err
}

// writeAtomic replaces path with data through a temporary file in the same folder, keeping the permissions of
// the file it replaces; a new file gets 0600.
func writeAtomic(path string, data []byte) (err error) {
	mode := fs.FileMode(0o600)
	if fi, err := os.Lstat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".qcrew-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(data); err == nil {
		if err = f.Chmod(mode); err == nil {
			err = f.Sync()
		}
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err == nil {
		syncDir(dir)
	}
	return err
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

func (c Config) backupDir() string { return filepath.Join(c.DataDir, "backups", "codex") }

var backupName = regexp.MustCompile(`^\d{8}T\d{6}\.\d{9}Z\.(hooks\.json|config\.toml)$`)

// backup copies each existing file to <time>.<name> in the backup folder and prunes the oldest of each name
// beyond keepBackups. Pruning removes only files whose names backup writes.
func (ch *Change) backup(now time.Time) error {
	dir := ch.cfg.backupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create the backup folder: %w", err)
	}
	stamp := now.UTC().Format("20060102T150405.000000000Z")
	for _, f := range []struct {
		name   string
		data   []byte
		exists bool
	}{{HooksFile, ch.old.hooks, ch.old.hooksExists}, {ConfigFile, ch.old.config, ch.old.configExists}} {
		if !f.exists {
			continue
		}
		path := filepath.Join(dir, stamp+"."+f.name)
		if err := writeNew(path, f.data); err != nil {
			return fmt.Errorf("back up %s: %w", f.name, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read the backup folder: %w", err)
	}
	byFile := map[string][]string{}
	for _, e := range entries {
		if m := backupName.FindStringSubmatch(e.Name()); m != nil && e.Type().IsRegular() {
			byFile[m[1]] = append(byFile[m[1]], e.Name())
		}
	}
	for _, names := range byFile {
		slices.Sort(names)
		for _, n := range names[:max(0, len(names)-keepBackups)] {
			if err := os.Remove(filepath.Join(dir, n)); err != nil {
				return fmt.Errorf("prune the backups: %w", err)
			}
		}
	}
	return nil
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

// mask makes control characters, invalid bytes and bidirectional controls of foreign text visible.
func mask(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f || r >= 0x80 && r < 0xa0 || r >= 0x202a && r <= 0x202e ||
			r >= 0x2066 && r <= 0x2069 || r == 0x200e || r == 0x200f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}
