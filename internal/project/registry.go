package project

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/castrowithcee/qatlas-kit/configfile"
	"github.com/castrowithcee/qatlas-kit/projectpath"
)

// Registry reads and changes the projects of one configuration file. Every change runs under the store's
// cross-process lock, so that a hand edit made meanwhile is never overwritten.
type Registry struct {
	store *configfile.Store[Config]
	now   func() time.Time
	newID func() (string, error)
}

// NewRegistry returns the registry of the configuration file at path. The file need not exist yet.
func NewRegistry(path string) (*Registry, error) {
	store, err := configfile.NewStore(path, Codec())
	if err != nil {
		return nil, err
	}
	return &Registry{store: store, now: time.Now, newID: NewID}, nil
}

// Path is the absolute path of the configuration file.
func (r *Registry) Path() string { return r.store.Path() }

func (r *Registry) load() (Config, error) {
	c, _, err := r.store.LoadVersioned()
	if errors.Is(err, fs.ErrNotExist) {
		return Config{Schema: Schema}, nil
	}
	return c, err
}

// List returns the projects in the order they were added.
func (r *Registry) List() ([]Project, error) {
	c, err := r.load()
	if err != nil {
		return nil, err
	}
	return c.Projects, nil
}

// Get returns the project whose id or name is ref.
func (r *Registry) Get(ref string) (Project, error) {
	c, err := r.load()
	if err != nil {
		return Project{}, err
	}
	p, _, ok := c.Find(ref)
	if !ok {
		return Project{}, refuse(ErrNotFound, "no project has the id or name %q", ref)
	}
	return p, nil
}

// AddRequest describes a project to register.
type AddRequest struct {
	// Path is the directory; it must be absolute. Symbolic links are resolved before it is stored.
	Path string
	// Name defaults to the base name of the resolved path.
	Name string
	// Create makes the directory, with its parents, if it does not exist.
	Create bool
}

// Add registers a directory as a project. It changes nothing inside the directory, apart from creating it
// when asked to.
func (r *Registry) Add(req AddRequest) (Project, error) {
	if !filepath.IsAbs(req.Path) {
		return Project{}, refuse(ErrInvalid, "the path %q is not absolute", req.Path)
	}
	if req.Name != "" {
		if err := ValidName(req.Name); err != nil {
			return Project{}, err
		}
	}
	if req.Create {
		// Refuse a taken name before the directory exists, so that a refused request leaves nothing behind.
		c, err := r.load()
		if err != nil {
			return Project{}, err
		}
		if err := nameFree(c, req.Name); err != nil {
			return Project{}, err
		}
		if err := os.MkdirAll(req.Path, 0o755); err != nil {
			return Project{}, refuse(ErrInvalid, "cannot create the directory %s: %v", req.Path, cause(err))
		}
	}
	dir, err := resolveDir(req.Path)
	if err != nil {
		return Project{}, err
	}
	name := req.Name
	if name == "" {
		name = filepath.Base(dir)
		if err := ValidName(name); err != nil {
			return Project{}, refuse(ErrInvalid, "the directory name cannot be a project name: %s", err.Error())
		}
	}

	var added Project
	_, err = r.store.Update(func(c Config) (Config, error) {
		c.Schema = Schema // a missing file arrives as the zero value; an existing one has been validated
		for _, p := range c.Projects {
			if p.Path == dir {
				return c, refuse(ErrExists, "the directory %s is already the project %s (id %s)", dir, p.Name, p.ID)
			}
		}
		if err := nameFree(c, name); err != nil {
			return c, err
		}
		id, err := r.freeID(c)
		if err != nil {
			return c, err
		}
		added = Project{ID: id, Name: name, Path: dir, AddedAt: r.now().UTC().Truncate(time.Second)}
		c.Projects = append(c.Projects, added)
		return c, nil
	})
	if err != nil {
		return Project{}, err
	}
	return added, nil
}

// Remove unregisters the project whose id or name is ref. Its directory is left as it is.
func (r *Registry) Remove(ref string) (Project, error) {
	var removed Project
	_, err := r.store.Update(func(c Config) (Config, error) {
		p, i, ok := c.Find(ref)
		if !ok {
			return c, refuse(ErrNotFound, "no project has the id or name %q", ref)
		}
		removed = p
		c.Projects = append(c.Projects[:i:i], c.Projects[i+1:]...)
		return c, nil
	})
	if err != nil {
		return Project{}, err
	}
	return removed, nil
}

func nameFree(c Config, name string) error {
	if name == "" {
		return nil
	}
	for _, p := range c.Projects {
		if p.Name == name {
			return refuse(ErrExists, "the name %q is taken by the project at %s", name, p.Path)
		}
		if p.ID == name {
			return refuse(ErrExists, "the name %q is the id of the project at %s", name, p.Path)
		}
	}
	return nil
}

// freeID returns a new id that is neither an id nor a name in c.
func (r *Registry) freeID(c Config) (string, error) {
	for range 8 {
		id, err := r.newID()
		if err != nil {
			return "", err
		}
		if _, _, taken := c.Find(id); !taken {
			return id, nil
		}
	}
	return "", errors.New("cannot find a free project id")
}

// resolveDir returns the canonical form of an absolute path that must be an existing directory.
func resolveDir(path string) (string, error) {
	dir := projectpath.Canonical(path)
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", refuse(ErrInvalid, "the directory %s does not exist", path)
	case err != nil:
		return "", refuse(ErrInvalid, "cannot read %s: %v", path, cause(err))
	case !info.IsDir():
		return "", refuse(ErrInvalid, "%s is not a directory", path)
	}
	return dir, nil
}

// cause drops the operation and path of a file system error, which the message names already.
func cause(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
