package subharness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// THE REGISTRY ON DISK.
//
//	harnesses/
//	  triage-flake.hjson              the entry, at whatever version is current
//	  triage-flake/
//	    version/v1.hjson              every version that was ever current
//	    version/v2.hjson
//	    run/20260816T142530Z.json     one trace DAG per execution
//
// THE FLAT FILE IS THE POINTER and the directory beside it is the history. That
// split is what makes "version" mean something a person can act on: the entry is
// always the current shape, so a reader never has to sort filenames to find out
// what is live, and a snapshot is written on every save, so `run triage-flake at
// v2` can be answered by opening a file rather than by hoping nothing changed.
//
// ── THE VERSION IS THE STORE'S TO WRITE ──
//
// [Store.Save] sets it, always, from what is on disk plus one. A builder that
// could name its own version could name a version twice — two surfaces editing
// one harness, a model repeating itself, a restored backup — and every run
// record keyed on that number would then describe two different programs.
//
// ── WHY WRITES ARE ATOMIC ──
//
// A half-written entry is a harness that no longer exists: the reader is a
// panel, a tool call, and a trigger, and none of them can do anything sensible
// with a truncated file. Every write here goes to a temporary file beside the
// target and is renamed over it, which on every filesystem this product runs on
// is the moment the change becomes visible.

// Store is a registry rooted at one directory.
type Store struct{ root string }

// ErrNoHarness is what a name nobody registered answers with. Callers act on it
// — the tool says "no harness called that, here is the list" — so it is a
// sentinel rather than a string.
var ErrNoHarness = errors.New("subharness: no such harness")

// New opens the registry at root. Nothing is read and no directory is created:
// a surface builds one of these at boot and most sessions never touch it.
func New(root string) *Store { return &Store{root: root} }

// Root is the directory this store owns, for a surface that wants to say where
// its harnesses live.
func (s *Store) Root() string { return s.root }

// Path is the entry file for a name.
func (s *Store) Path(name string) string { return filepath.Join(s.root, name+Extension) }

// dir is the per-harness directory: the versions and the runs.
func (s *Store) dir(name string) string { return filepath.Join(s.root, name) }

// RunDir is where this harness's traces land — harnesses/<name>/run.
func (s *Store) RunDir(name string) string { return filepath.Join(s.dir(name), "run") }

// versionDir is where the snapshots land.
func (s *Store) versionDir(name string) string { return filepath.Join(s.dir(name), "version") }

// Load reads the current entry. It clamps and validates on the way out, so no
// caller downstream has to wonder whether a file it read is a file it may run.
func (s *Store) Load(name string) (Harness, error) {
	if err := ValidName(name); err != nil {
		return Harness{}, err
	}
	return s.read(s.Path(name))
}

// LoadVersion reads one pinned version. Version 0 means "whatever is current",
// which is what a subharness.call with no version pinned asks for.
func (s *Store) LoadVersion(name string, version int) (Harness, error) {
	if version <= 0 {
		return s.Load(name)
	}
	if err := ValidName(name); err != nil {
		return Harness{}, err
	}
	path := filepath.Join(s.versionDir(name), fmt.Sprintf("v%d%s", version, Extension))
	harness, err := s.read(path)
	if errors.Is(err, os.ErrNotExist) {
		return Harness{}, fmt.Errorf("%w: %s at v%d", ErrNoHarness, name, version)
	}
	return harness, err
}

func (s *Store) read(path string) (Harness, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Harness{}, fmt.Errorf("%w: %s", ErrNoHarness, strings.TrimSuffix(filepath.Base(path), Extension))
		}
		return Harness{}, err
	}
	harness, err := Decode(data)
	if err != nil {
		return Harness{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	Clamp(&harness)
	if err := Validate(&harness); err != nil {
		return harness, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return harness, nil
}

// List is every registered harness, by name, alphabetically. A file that does
// not parse is SKIPPED rather than fatal: one broken entry must not be able to
// take the whole panel down, and the entry that broke says so when somebody
// opens it.
func (s *Store) List() ([]Harness, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Harness
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), Extension) {
			continue
		}
		harness, err := s.read(filepath.Join(s.root, entry.Name()))
		if err != nil {
			continue
		}
		out = append(out, harness)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

// Save registers a harness and returns it as it was written — version stamped,
// numbers clamped.
//
// THE VERSION BUMPS ON EVERY SAVE, including one that changes nothing. That is
// the honest reading of what a save IS: somebody approved a shape, and the
// record of what they approved is the version. A store that suppressed the bump
// for an identical program would be deciding, on a byte comparison, that two
// approvals were one — and the run history keys on the number.
func (s *Store) Save(h Harness) (Harness, error) {
	Clamp(&h)
	if err := Validate(&h); err != nil {
		return h, err
	}
	previous := 0
	if existing, err := s.Load(h.Name); err == nil {
		previous = existing.Version
	} else if !errors.Is(err, ErrNoHarness) {
		// A file that is there and does not parse. Overwriting it would throw
		// away a version somebody may still want, and the version number it
		// carried is exactly what cannot be read — so this is a refusal.
		return h, fmt.Errorf("subharness: %s exists and cannot be read: %w", h.Name, err)
	}
	h.Version = previous + 1
	h.Updated = time.Now().UTC()

	data, err := Encode(h)
	if err != nil {
		return h, err
	}
	if err := os.MkdirAll(s.versionDir(h.Name), 0o755); err != nil {
		return h, err
	}
	// The SNAPSHOT lands before the pointer. If the process dies between them
	// the registry still says v(N-1), which is a registry that is merely behind;
	// the other order leaves a pointer at a version with no file under it.
	snapshot := filepath.Join(s.versionDir(h.Name), fmt.Sprintf("v%d%s", h.Version, Extension))
	if err := writeAtomic(snapshot, data); err != nil {
		return h, err
	}
	if err := writeAtomic(s.Path(h.Name), data); err != nil {
		return h, err
	}
	return h, nil
}

// Remove retires an entry. THE HISTORY STAYS: the runs under it are the record
// of work that actually happened, and deleting a shape must not delete the
// evidence of what it did. A person who wants the traces gone can delete the
// directory, which is a thing they can see.
func (s *Store) Remove(name string) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if err := os.Remove(s.Path(name)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrNoHarness, name)
		}
		return err
	}
	return nil
}

// ── run history ─────────────────────────────────────────────────────────────

// runStamp is the filename a run is saved under: UTC, sortable, and legal on
// every filesystem — which is why it is not RFC 3339, whose colons are not.
const runStamp = "20060102T150405Z"

// SaveRun writes one trace DAG under harnesses/<name>/run/<ts>.json and returns
// the path.
//
// A SECOND RUN IN THE SAME SECOND gets a suffix rather than overwriting the
// first. Two runs of one harness a second apart is what a parallel.split of
// subharness.call nodes looks like from here, and a history that silently lost
// half of them would be a history that lies about width.
func (s *Store) SaveRun(run Run) (string, error) {
	if err := ValidName(run.Harness); err != nil {
		return "", err
	}
	dir := s.RunDir(run.Harness)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := run.Started.UTC().Format(runStamp)
	if run.Started.IsZero() {
		stamp = time.Now().UTC().Format(runStamp)
	}
	data, err := run.encode()
	if err != nil {
		return "", err
	}
	for suffix := 0; suffix < 100; suffix++ {
		name := stamp + ".json"
		if suffix > 0 {
			name = fmt.Sprintf("%s-%d.json", stamp, suffix)
		}
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := writeAtomic(path, data); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("subharness: too many runs of %s in one second", run.Harness)
}

// Runs is this harness's history, newest first. The traces themselves are not
// read — a history list is a list of when, and a trace is kilobytes.
func (s *Store) Runs(name string) ([]string, error) {
	if err := ValidName(name); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.RunDir(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	// Newest first, and NOT by plain string comparison. A second run in the
	// same second is <stamp>-1.json, and '-' sorts BEFORE '.', so a string sort
	// puts the older file first inside a collided second — which made "the last
	// run" the first of two runs a second apart. The order is therefore the two
	// fields the name actually carries: the stamp, then the collision number.
	sort.Slice(names, func(a, b int) bool {
		stampA, suffixA := runOrder(names[a])
		stampB, suffixB := runOrder(names[b])
		if stampA != stampB {
			return stampA > stampB
		}
		return suffixA > suffixB
	})
	dir := s.RunDir(name)
	paths := make([]string, 0, len(names))
	for _, file := range names {
		paths = append(paths, filepath.Join(dir, file))
	}
	return paths, nil
}

// runOrder splits a run filename into the two things it sorts by: the timestamp
// it was written at, and which run of that second it was.
func runOrder(name string) (string, int) {
	stem := strings.TrimSuffix(name, ".json")
	at := strings.LastIndexByte(stem, '-')
	if at < 0 {
		return stem, 0
	}
	suffix, err := strconv.Atoi(stem[at+1:])
	if err != nil {
		return stem, 0
	}
	return stem[:at], suffix
}

// LoadRun reads one trace back.
func (s *Store) LoadRun(path string) (Run, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Run{}, err
	}
	return decodeRun(data)
}

// LastRun is the newest trace for a harness, and false when it has never run.
func (s *Store) LastRun(name string) (Run, bool) {
	paths, err := s.Runs(name)
	if err != nil || len(paths) == 0 {
		return Run{}, false
	}
	run, err := s.LoadRun(paths[0])
	if err != nil {
		return Run{}, false
	}
	return run, true
}

// writeAtomic writes bytes so that a reader sees the old file or the new one and
// never half of either.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		os.Remove(name)
		return err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
