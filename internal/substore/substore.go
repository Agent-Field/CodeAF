// Package substore is where subharness bundles live on disk.
//
// PRD §6 and §7. One directory per subharness under the state root, immutable
// version pages inside it, and the two mutable notes a subharness accumulates —
// what it has learnt, and how its last run went — beside them:
//
//	~/.aforge/subharnesses/<name>/v1.json      the version record: hash, parent, why
//	~/.aforge/subharnesses/<name>/v1/          the bundle that record describes
//	                              manifest.json
//	                              program.js
//	                              prompts/*.md
//	                              memory.md      the seed this version was minted with
//	                              evals/         inert until v3; the format exists now
//	~/.aforge/subharnesses/<name>/v2.json
//	~/.aforge/subharnesses/<name>/v2/
//	~/.aforge/subharnesses/<name>/memory.md    the LIVE memory, written by remember()
//	~/.aforge/subharnesses/<name>/last-run.json
//	~/.aforge/subharnesses/<name>/.mint.lock   the gate one mint at a time holds
//
// THERE IS NO HEAD FILE. The head is the highest version present, which is the
// rule internal/subharness/store.go states about its own pages and the reason it
// has never had a pointer that could disagree with what it pointed at.
//
// THE RECORD IS A FILE BESIDE THE DIRECTORY, NOT A FILE INSIDE IT, and that is
// the whole of how a version is claimed. os.Link cannot claim a directory, so
// the exclusive-create discipline this package inherited would have had nothing
// to grip if the record lived under v2/. Written beside it, `v2.json` is exactly
// the page the old store already minted this way: the first writer to link it
// into place owns v2, a second writer racing it is told the name is taken —
// refused, never shifted along to v3, for the reason [Store.write] gives — and
// the bundle directory is renamed into place only by the winner.
// See [Store.Mint].
//
// THE CLAIM IS NOT ON ITS OWN ENOUGH, and that is what `.mint.lock` is for. An
// exclusive create refuses two writers who computed the SAME version number, and
// two writers who read the store a moment apart do not: one of them counts past
// a record whose bundle has not landed yet and mints a second child of the same
// parent. [Store.gated] holds one writer at a time across the whole
// read-check-claim so that cannot be read a moment apart, and the exclusive
// create stays underneath it as the floor on a filesystem that will not lock.
//
// The store holds no cache. A bundle is small, read at launch and at dispatch,
// and edited by hand often enough that a stale read would be the more expensive
// mistake — the same judgement the harness store made, for the same reason.
package substore

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

const (
	// Root is the directory name under the state root, and it is the same word
	// under a project's `.aforge/` — see [ProjectDir].
	Root = "subharnesses"

	// ManifestFile, ProgramFile, PromptsDir, MemoryFile and EvalsDir are PRD §6's
	// bundle layout, spelled once. A bundle written by hand, by the design flow,
	// or by a `git pull` is the same five names.
	ManifestFile = "manifest.json"
	ProgramFile  = "program.js"
	PromptsDir   = "prompts"
	MemoryFile   = "memory.md"
	EvalsDir     = "evals"

	// lastRunFile is the tiny note the list rows read. It is NOT a run journal:
	// what a row draws is when, whether it finished, and what it cost, and
	// keeping only that means a name's row costs one small read rather than a
	// directory scan of every trace it ever wrote.
	lastRunFile = "last-run.json"

	// versionPrefix and versionSuffix spell a version record. v1.json, not
	// 1.json: the letter is what makes a bare `ls` read as a version list, and it
	// is what the directory beside it is called.
	versionPrefix = "v"
	versionSuffix = ".json"

	// mintPrefix marks a bundle being staged. It is dot-led so that a scan of a
	// name's directory never mistakes a half-written mint for a version, and it
	// is removed by the minter whether the mint won its claim or lost it.
	mintPrefix = ".mint-"

	// gateFile is the file one mint at a time holds while it reads a name's
	// versions and claims the next one — see [Store.gated]. It is dot-led like
	// the staging directories beside it so that no scan of a name's directory
	// mistakes it for a version, and it is CREATED ONCE AND NEVER REPLACED,
	// because a lock over an inode somebody is about to rename away is two locks
	// with one name.
	gateFile = ".mint.lock"
)

// ErrNotFound is what a read answers for a subharness or a version that was
// never minted. Callers tell "no such subharness" from "the disk is broken", so
// it is a sentinel rather than a formatted string.
var ErrNotFound = errors.New("substore: not found")

// ErrExists is what [Store.Mint] answers when the version it was told to write
// is already on disk. It is the loud half of the exclusive create: a mint that
// would have replaced a version somebody has already run is refused, never
// merged.
var ErrExists = errors.New("substore: that version is already written")

// Store is the bundles at one directory.
type Store struct {
	dir string

	// Notice is where this store says that it skipped something. A bundle that
	// does not validate is ABSENT FROM EVERY LIST — the codebase's law about a
	// capability that cannot work — and the line here is the only trace of it,
	// so that "my subharness vanished" is answerable by looking at the journal
	// rather than by guessing. Nil is silence, which is what a store nobody is
	// debugging should be; the surface that owns a journal wires it at launch.
	Notice func(line string)

	// Parse is the syntax check [Store.Mint] runs over program.js before it
	// writes anything. IT IS A FIELD BECAUSE THE PARSER IS THE RUNTIME'S, not
	// this package's: goja is not in this build's module graph, and a store that
	// imported a JavaScript engine to spell-check a file would make every surface
	// that lists subharnesses depend on the engine that runs them. Nil is no
	// check, and a bundle whose program is nonsense is then caught by the runtime
	// at its first run instead of by the mint — later than ideal, and honest,
	// which is the trade this field exists to let the runtime lane close.
	Parse func(program []byte) error

	// Now stamps version records and run notes. It is a field so a test can mint
	// twice in one second and still say which came first.
	Now func() time.Time
}

// now is the store's clock, defaulted here rather than at every call site.
func (s *Store) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// At opens the store at a directory. The directory is created on the first mint
// and not here, so listing a machine that has never written a subharness is not
// itself a mutation.
func At(dir string) *Store { return &Store{dir: dir} }

// Home opens the store aforge owns: ~/.aforge/subharnesses, moved wholesale by
// AFORGE_HOME like everything else durable, through the one package that reads
// that variable.
func Home() *Store { return At(home.Join(Root)) }

// ProjectDir names where a repository's own bundles would live. It is PRD §7's
// layer 2 and it is a NAME AND NOTHING ELSE in this phase: the org-sharing story
// needs no machinery beyond a directory a `git pull` fills, and the lookup that
// would consult it is one Registry.UseBundles(exec.LayerProject, …) away when
// the phase that wants it arrives.
func ProjectDir(repository string) string {
	return filepath.Join(repository, ".aforge", Root)
}

// Dir names the store's directory.
func (s *Store) Dir() string { return s.dir }

// Names is every subharness with at least one complete version, in name order.
//
// A DIRECTORY IS A SUBHARNESS WHEN IT HOLDS A VERSION, which is deliberately not
// a check on the shape of its name. The rule for what a name may be lives in
// exec.Manifest.Validate and may live nowhere else; restating it here as a
// directory filter would be a second copy of it, and the manifest inside is
// validated for real at load anyway — a directory called something impossible
// simply has no loadable bundle in it.
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
		if !entry.IsDir() {
			continue
		}
		versions, err := s.Versions(entry.Name())
		if err != nil || len(versions) == 0 {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Versions lists one subharness's complete versions, ascending.
//
// COMPLETE MEANS BOTH HALVES ARE THERE — the record and the directory it
// describes. A record with no directory is the one crash window [Store.Mint]
// has: the claim was linked and the process died before the bundle was renamed
// into place. Such a version is dead rather than empty, it is never re-minted
// (the next version is counted from the records, not from this list), and
// leaving it out here is what keeps a reader from ever seeing half a mint.
func (s *Store) Versions(name string) ([]int, error) {
	records, err := s.records(name)
	if err != nil {
		return nil, err
	}
	var versions []int
	for _, version := range records {
		info, err := os.Stat(s.VersionDir(name, version))
		if err != nil || !info.IsDir() {
			s.notice("%s v%d: the version record is written but its bundle is not — skipping it", name, version)
			continue
		}
		versions = append(versions, version)
	}
	return versions, nil
}

// Head is the highest complete version. There is no head file; this is the
// whole of the rule.
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

// records lists the version numbers that have a record file, complete or not.
// [Store.Mint] counts the next version from these so that a version whose bundle
// never landed is still spent — a number that was once claimed is never handed
// out again, which is the only way a hash somebody wrote down stays a hash of
// the thing they wrote it down for.
func (s *Store) records(name string) ([]int, error) {
	entries, err := os.ReadDir(s.nameDir(name))
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
		version, ok := recordVersion(entry.Name())
		if !ok {
			continue
		}
		versions = append(versions, version)
	}
	sort.Ints(versions)
	return versions, nil
}

func (s *Store) nameDir(name string) string { return filepath.Join(s.dir, name) }

// VersionDir names one version's bundle directory. It is exported because a
// bundle's prompts and its program are files a runner opens by path, and a
// runtime that had to reconstruct this path itself would be a second copy of the
// layout this package is the authority on.
func (s *Store) VersionDir(name string, version int) string {
	return filepath.Join(s.nameDir(name), versionPrefix+strconv.Itoa(version))
}

func (s *Store) recordPath(name string, version int) string {
	return filepath.Join(s.nameDir(name), versionPrefix+strconv.Itoa(version)+versionSuffix)
}

func (s *Store) gatePath(name string) string {
	return filepath.Join(s.nameDir(name), gateFile)
}

func (s *Store) memoryPath(name string) string {
	return filepath.Join(s.nameDir(name), MemoryFile)
}

func (s *Store) lastRunPath(name string) string {
	return filepath.Join(s.nameDir(name), lastRunFile)
}

// recordVersion reads a version out of a record filename and reports false for
// everything else in the directory — the live memory, the last-run note, a
// staging directory, an editor's backup.
func recordVersion(filename string) (int, bool) {
	if !strings.HasPrefix(filename, versionPrefix) || !strings.HasSuffix(filename, versionSuffix) {
		return 0, false
	}
	digits := strings.TrimSuffix(strings.TrimPrefix(filename, versionPrefix), versionSuffix)
	version, err := strconv.Atoi(digits)
	if err != nil || version < 1 {
		return 0, false
	}
	return version, true
}

// notice says one line into whatever journal the surface wired, and nothing at
// all when nobody wired one.
func (s *Store) notice(format string, args ...any) {
	if s == nil || s.Notice == nil {
		return
	}
	s.Notice("subharness store: " + fmt.Sprintf(format, args...))
}

// writeNew writes a file that must not already exist, atomically.
//
// This is internal/subharness/store.go's dance, kept whole and for its reason:
// version records are immutable, so the EXCLUSIVE CREATE IS THE POINT — a second
// mint racing the first is told the name is taken instead of quietly replacing a
// version somebody has already read — and the temp-and-link is what keeps that
// exclusivity while never leaving a half-written page where a reader could find
// one.
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

// replace writes a file that IS allowed to already exist, atomically.
//
// Two files in this store are mutable and only two: the live memory a
// subharness accumulates, and the one-line note about its last run. Neither is a
// version and neither may use [writeNew], because an exclusive create over a
// file that is supposed to change would fail on the second run of every
// subharness. The rename is still atomic, so a reader sees the old note or the
// new one and never a torn one.
func replace(path string, data []byte) error {
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
	return os.Rename(name, path)
}
