package subharness

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// The registry is a directory, and that is the whole of it. There is no index
// file, no database, and no in-memory authority that a file on disk could
// disagree with: the name is the path, listing is reading the directory, and a
// harness someone dropped in by hand is a harness. A registry a person can
// inspect with ls is a registry they can trust, and every surface that shows
// harnesses is showing the same thing.
//
// Layout under the root:
//
//	harnesses/<name>.hjson          the current revision — the plain path
//	harnesses/<name>/rev/<n>.hjson  every revision that was ever current
//	harnesses/<name>/run/<ts>.json  one file per run
//
// The current file is a pointer in the sense the design doc means: it says which
// revision is now. The rev/ copies are what make pinning honest — a
// subharness.call that pinned v1 must still be able to read v1 after v2 lands,
// or the pin was only ever a comment.

// ExtEntry and ExtRun are the two file extensions the registry writes. Entries
// are .hjson because they are meant to be read and edited by people; what this
// package writes is the strict-JSON subset of hjson, which every hjson reader
// accepts and encoding/json can load back.
const (
	ExtEntry = ".hjson"
	ExtRun   = ".json"
)

// ErrNotFound is returned for a name, or a pinned revision, that the registry
// does not have. Callers distinguish "no such harness" from "the disk is
// broken", and errors.Is is how.
var ErrNotFound = errors.New("subharness: not found")

// Registry is one directory of entries.
type Registry struct{ root string }

// New opens a registry at a root directory. An empty root is the product's
// answer: <state root>/harnesses, so a caller that has no opinion gets the one
// place aforge keeps these.
func New(root string) *Registry {
	if strings.TrimSpace(root) == "" {
		root = home.Join("harnesses")
	}
	return &Registry{root: root}
}

// Root is the directory the registry reads and writes.
func (r *Registry) Root() string { return r.root }

// Path is the plain registry path for a name: the current revision's file.
func (r *Registry) Path(name string) string {
	return filepath.Join(r.root, name+ExtEntry)
}

// dir is a name's own directory, holding its revisions and its runs.
func (r *Registry) dir(name string) string { return filepath.Join(r.root, name) }

// RevPath is where one revision is archived.
func (r *Registry) RevPath(name string, revision int) string {
	return filepath.Join(r.dir(name), "rev", strconv.Itoa(revision)+ExtEntry)
}

// RunDir is where a name's runs are saved.
func (r *Registry) RunDir(name string) string {
	return filepath.Join(r.dir(name), "run")
}

// Save validates an entry and stores it as the current revision, archiving a
// copy under rev/.
//
// A revision may not be rewritten with different content. That refusal is the
// only guarantee a pin is worth anything: somewhere there is a call that read
// v2 and decided to depend on it, and if v2 can be edited in place then what it
// depends on is a name and a hope. Re-saving byte-identical content succeeds, so
// a caller that saves what it already saved is not punished for it.
func (r *Registry) Save(e Entry) error {
	if err := e.Validate(); err != nil {
		return err
	}
	encoded, err := encode(e)
	if err != nil {
		return err
	}
	revPath := r.RevPath(e.Name, e.Revision)
	switch stored, err := os.ReadFile(revPath); {
	case err == nil && !bytes.Equal(bytes.TrimSpace(stored), bytes.TrimSpace(encoded)):
		return fmt.Errorf("subharness %q: revision %d is already stored and differs; bump the revision", e.Name, e.Revision)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if current, err := r.Load(e.Name); err == nil && current.Revision > e.Revision {
		return fmt.Errorf("subharness %q: revision %d is behind the stored %d", e.Name, e.Revision, current.Revision)
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err := writeFile(revPath, encoded); err != nil {
		return err
	}
	return writeFile(r.Path(e.Name), encoded)
}

// Load reads the current revision of a name.
func (r *Registry) Load(name string) (Entry, error) {
	if err := ValidName(name); err != nil {
		return Entry{}, err
	}
	return readEntry(r.Path(name), name, 0)
}

// LoadRevision reads one pinned revision. Revision 0 means "whatever is
// current", which is what an unpinned subharness.call carries, so a caller can
// pass its pin straight through without branching on zero.
func (r *Registry) LoadRevision(name string, revision int) (Entry, error) {
	if err := ValidName(name); err != nil {
		return Entry{}, err
	}
	if revision <= 0 {
		return r.Load(name)
	}
	return readEntry(r.RevPath(name, revision), name, revision)
}

// Names lists the harnesses in the registry, sorted. A file that is not an entry
// is not a harness and is skipped in silence: the directory belongs to the
// person as much as to the product.
func (r *Registry) Names() ([]string, error) {
	entries, err := os.ReadDir(r.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ExtEntry {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ExtEntry)
		if ValidName(name) != nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// List loads every entry the registry holds. A file that fails to parse is
// reported rather than skipped: a broken harness is a thing the person needs to
// hear about, and a listing that quietly omits it is how a typo becomes a
// mystery.
func (r *Registry) List() ([]Entry, error) {
	names, err := r.Names()
	if err != nil {
		return nil, err
	}
	list := make([]Entry, 0, len(names))
	for _, name := range names {
		e, err := r.Load(name)
		if err != nil {
			return list, err
		}
		list = append(list, e)
	}
	return list, nil
}

// Revisions lists the archived revisions of a name, ascending.
func (r *Registry) Revisions(name string) ([]int, error) {
	entries, err := os.ReadDir(filepath.Join(r.dir(name), "rev"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	revisions := make([]int, 0, len(entries))
	for _, entry := range entries {
		n, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ExtEntry))
		if err != nil || n < 1 {
			continue
		}
		revisions = append(revisions, n)
	}
	sort.Ints(revisions)
	return revisions, nil
}

// HostAll offers every registered harness's hosted triggers to a source. It is
// what a surface calls at startup: one line, and every command the person's
// harnesses declare exists.
func (r *Registry) HostAll(src Source) ([]Hosted, error) {
	entries, err := r.List()
	if err != nil {
		return nil, err
	}
	var mounted []Hosted
	for _, e := range entries {
		hosted, err := Host(src, e)
		mounted = append(mounted, hosted...)
		if err != nil {
			return mounted, err
		}
	}
	return mounted, nil
}

func readEntry(path, name string, revision int) (Entry, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if revision > 0 {
			return Entry{}, fmt.Errorf("%w: %s revision %d", ErrNotFound, name, revision)
		}
		return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return Entry{}, fmt.Errorf("subharness %q: %s: %w", name, filepath.Base(path), err)
	}
	// The file's own name is the authority on identity. An entry whose name field
	// drifted from its path would resolve differently depending on which one a
	// reader happened to trust.
	if e.Name != name {
		return Entry{}, fmt.Errorf("subharness %q: file names itself %q", name, e.Name)
	}
	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	return e, nil
}

func encode(e Entry) ([]byte, error) {
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// writeFile writes through a temporary file in the same directory, so a reader
// that arrives mid-write sees the old file or the new one and never half of
// either. The registry is read by long-lived surfaces while it is written by
// authoring commands; those two meet often enough to matter.
func writeFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
