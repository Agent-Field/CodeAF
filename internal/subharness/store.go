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

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// The layout under the state root. One directory per harness, immutable
// version pages inside it, and the runs beside them:
//
//	~/.aforge/harnesses/<name>/v1.json
//	~/.aforge/harnesses/<name>/v2.json
//	~/.aforge/harnesses/<name>/run/20260816T101112Z.json
//
// There is no head file. The head is the highest page present, which means the
// pointer cannot disagree with the pages it points at — the failure mode a
// separate head file exists to have.
const (
	// Root is the directory name under the state root.
	Root = "harnesses"
	// RunDir is where a run's trace lands, under the harness's directory.
	RunDir = "run"
	// pageSuffix and pagePrefix spell a version page. v1.json, not 1.json:
	// the letter is what makes a bare `ls` read as a version list.
	pagePrefix = "v"
	pageSuffix = ".json"
	// runStamp is the run filename. Sortable, second-resolution, UTC, and
	// legal on every filesystem aforge runs on — colons are not.
	runStamp = "20060102T150405Z"
)

// ErrNotFound is what a read returns for a harness or a version that was never
// saved. Callers distinguish "no such harness" from "the disk is broken", so
// this is a sentinel rather than a formatted string.
var ErrNotFound = errors.New("subharness: not found")

// Store is the durable registry at one directory. It holds no cache: a
// harness page is small, read rarely, and edited by hand often enough that a
// stale read would be the more expensive mistake.
type Store struct {
	dir string
	// Now stamps a run file. It is a field so a test can run two harnesses in
	// the same second and still get two names it chose.
	Now func() time.Time
}

// At opens the registry at a directory. The directory is created on first
// write, not here, so listing a machine that has never saved a harness is not
// itself a mutation.
func At(dir string) *Store {
	return &Store{dir: dir, Now: time.Now}
}

// Default opens the registry aforge owns: ~/.aforge/harnesses, moved wholesale
// by AFORGE_HOME like everything else durable.
func Default() *Store { return At(home.Join(Root)) }

// Dir names the registry directory.
func (s *Store) Dir() string { return s.dir }

// Save mints the next version and writes it. It is the only way a page is
// written, and it validates first: an invalid harness never reaches the disk,
// so every page a reader finds is one the runner can run.
//
// The version is minted, never chosen. A caller that already knows which
// version it means may say so — h.Id.Version — and Save refuses if the disk
// disagrees, which is how a distiller that read v2, thought about it, and came
// back to save finds out that someone else saved v3 in the meantime.
func (s *Store) Save(h Harness) (Harness, error) {
	h = h.Normalize()
	if !ValidName(h.Id.Name) {
		return Harness{}, fmt.Errorf("subharness: name %q is not a slug of at most %d bytes", h.Id.Name, MaxIdBytes)
	}
	versions, err := s.Versions(h.Id.Name)
	if err != nil {
		return Harness{}, err
	}
	next := 1
	if len(versions) > 0 {
		next = versions[len(versions)-1] + 1
	}
	if h.Id.Version != 0 && h.Id.Version != next {
		return Harness{}, fmt.Errorf("subharness: %s: asked to save v%d, but the next version is v%d",
			h.Id.Name, h.Id.Version, next)
	}
	if next > MaxVersion {
		return Harness{}, fmt.Errorf("subharness: %s: v%d is past the version cap of %d", h.Id.Name, next, MaxVersion)
	}
	h.Id.Version = next
	if err := Validate(h); err != nil {
		return Harness{}, err
	}
	data, err := Encode(h)
	if err != nil {
		return Harness{}, err
	}
	if err := os.MkdirAll(s.harnessDir(h.Id.Name), 0o755); err != nil {
		return Harness{}, err
	}
	if err := writeNew(s.page(h.Id.Name, next), data); err != nil {
		return Harness{}, fmt.Errorf("subharness: %s: write v%d: %w", h.Id.Name, next, err)
	}
	return h, nil
}

// Load reads a version pointer. Version 0 means the head — the highest page
// present — and any other integer means that page and only that page, which is
// what makes a pin durable: v1 reads the same bytes after v2 is saved.
func (s *Store) Load(name string, version int) (Harness, error) {
	if !ValidName(name) {
		return Harness{}, fmt.Errorf("subharness: name %q is not a slug of at most %d bytes", name, MaxIdBytes)
	}
	if version == 0 {
		head, err := s.Head(name)
		if err != nil {
			return Harness{}, err
		}
		version = head
	}
	data, err := os.ReadFile(s.page(name, version))
	if errors.Is(err, os.ErrNotExist) {
		return Harness{}, fmt.Errorf("%w: %s v%d", ErrNotFound, name, version)
	}
	if err != nil {
		return Harness{}, err
	}
	h, err := Decode(data)
	if err != nil {
		return Harness{}, fmt.Errorf("subharness: %s v%d: %w", name, version, err)
	}
	// The page says which version it is and so does its filename. They can
	// only disagree if somebody moved a file, and a harness that lies about
	// its own pointer would be recorded as that lie in every trace it runs.
	if h.Id.Version != version {
		return Harness{}, fmt.Errorf("subharness: %s v%d: the page says v%d", name, version, h.Id.Version)
	}
	return h, nil
}

// Head is the highest version present.
func (s *Store) Head(name string) (int, error) {
	versions, err := s.Versions(name)
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return versions[len(versions)-1], nil
}

// Versions lists a harness's pages in ascending order. A harness that was
// never saved has no versions and is not an error — asking is how a caller
// finds out.
func (s *Store) Versions(name string) ([]int, error) {
	entries, err := os.ReadDir(s.harnessDir(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var versions []int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, ok := pageVersion(entry.Name())
		if !ok {
			continue
		}
		versions = append(versions, version)
	}
	sort.Ints(versions)
	return versions, nil
}

// Names lists every harness in the registry, in name order.
func (s *Store) Names() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || !ValidName(entry.Name()) {
			continue
		}
		versions, err := s.Versions(entry.Name())
		if err != nil {
			return nil, err
		}
		if len(versions) == 0 {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// SaveRun writes one run's trace under the harness's run directory and returns
// the path it landed at. Traces are kept whole rather than folded into a log
// because the run is the evidence: the dynamism ladder is only honest if the
// output-trace of what a run actually decided survives the run.
func (s *Store) SaveRun(t Trace) (string, error) {
	name := t.Id.Name
	if !ValidName(name) {
		return "", fmt.Errorf("subharness: name %q is not a slug of at most %d bytes", name, MaxIdBytes)
	}
	data, err := t.encode()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.harnessDir(name), RunDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := s.now().UTC().Format(runStamp)
	// Two runs in one second is ordinary — a fan of sub-harness calls, a test
	// — so the second one earns a suffix rather than overwriting the first.
	// The suffix is `_NN` and not `-NN` because filename order is meant to be
	// time order, and a dash sorts ahead of the dot that starts the extension:
	// `...Z-1.json` would list before the `...Z.json` it came after.
	for attempt := 0; ; attempt++ {
		filename := stamp + pageSuffix
		if attempt > 0 {
			filename = fmt.Sprintf("%s_%02d%s", stamp, attempt, pageSuffix)
		}
		path := filepath.Join(dir, filename)
		err := writeNew(path, data)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return path, nil
	}
}

// Runs lists a harness's saved traces, oldest first — which is filename order,
// because the stamp was chosen to make those the same thing.
func (s *Store) Runs(name string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.harnessDir(name), RunDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), pageSuffix) {
			continue
		}
		paths = append(paths, filepath.Join(s.harnessDir(name), RunDir, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

// LoadRun reads one saved trace back.
func (s *Store) LoadRun(path string) (Trace, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Trace{}, fmt.Errorf("%w: %s", ErrNotFound, path)
	}
	if err != nil {
		return Trace{}, err
	}
	return decodeTrace(data)
}

// LastTrace is the newest saved trace for one name, and false for a page nobody
// has run.
//
// IT IS THE PAGE STORE'S ANSWER TO "WHEN DID THIS LAST RUN", and it is the whole
// answer for a page: every run of one goes through the surface's single run door
// (cmd/aforge's v3RunHarness), whichever list started it, and that door saves a
// trace here. So a page run from `/harness` and a page run from `/subharness`
// both land in this directory, which is what lets the two doors agree.
//
// A TRACE THAT CANNOT BE READ IS NO TRACE. A row asking when something last ran
// is not the place somebody learns their disk is broken, and the emptiness law
// says a row with nothing to report reports nothing.
func (s *Store) LastTrace(name string) (Trace, bool) {
	paths, err := s.Runs(name)
	if err != nil || len(paths) == 0 {
		return Trace{}, false
	}
	// Runs come back oldest first — the stamp is the filename — so the newest is
	// the last one.
	trace, err := s.LoadRun(paths[len(paths)-1])
	if err != nil {
		return Trace{}, false
	}
	return trace, true
}

func (s *Store) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s *Store) harnessDir(name string) string { return filepath.Join(s.dir, name) }

func (s *Store) page(name string, version int) string {
	return filepath.Join(s.harnessDir(name), pagePrefix+strconv.Itoa(version)+pageSuffix)
}

// pageVersion reads a version out of a page filename, and reports false for
// anything else in the directory — the run directory, an editor's backup, a
// file a person left there.
func pageVersion(filename string) (int, bool) {
	if !strings.HasPrefix(filename, pagePrefix) || !strings.HasSuffix(filename, pageSuffix) {
		return 0, false
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(filename, pagePrefix), pageSuffix)
	version, err := strconv.Atoi(digits)
	if err != nil || version < 1 {
		return 0, false
	}
	return version, true
}

// writeNew writes a file that must not already exist, atomically. Version
// pages and run traces are both immutable, so the exclusive create is the
// point: a second Save racing the first fails loudly instead of quietly
// replacing a version somebody already read. The temp-and-link dance is what
// keeps that exclusivity while still never leaving a half-written page where a
// reader could find one.
func writeNew(path string, data []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Link(name, path)
}
