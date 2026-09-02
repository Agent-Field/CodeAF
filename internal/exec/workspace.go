package exec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Workspace is the shared directory a run writes into.
//
// It is shared rather than per-node on purpose. A dependent that needs the full
// text of an upstream artifact reads the file instead of receiving it inline,
// which is what keeps a large deliverable out of three contexts at once. That
// only works if there is one directory everyone can see.
//
// Collisions are avoided by construction rather than by locking: each node is
// given a distinct suggested output path derived from its id and title, and
// nodes that run at the same time are independent by the graph's own definition.
type Workspace struct {
	root string
	// real is root with symlinks resolved. On macOS /tmp is a symlink to
	// /private/tmp, so the same workspace has two honest spellings; an agent
	// that learned one from pwd must not be refused for using the other.
	real string
	// scratch is where the harness's own files land — spilled observations,
	// turn traces, background job logs.
	//
	// IT IS MEANT TO BE OUTSIDE THE ROOT ON EVERY LAYOUT, and that is a measured
	// repair rather than tidiness. It used to be the root itself for every caller
	// but one, on the reasoning that a directory the harness made for a job may
	// hold whatever the job needs. What it held was the leaf's own transcript:
	// `.aforge/trace/` carries the worker's turn-by-turn recorder, its raw event
	// stream and its patch, in the directory the worker was told to work in,
	// beside `.obs/` and `.aforge/jobs/`. A measured atomic leaf spent five of its
	// eleven turns listing that machinery and reading its OWN trace log back into
	// its own context — orientation bought at full price, of files it had written
	// itself a second earlier. The harness it was benchmarked against writes
	// nothing whatever into its working directory and pays for none of it.
	//
	// What deliberately stays in the root is work product: the NN-title.md a
	// sibling is meant to find. Machinery is not work product and does not.
	scratch string
	// personal records that the root is somebody's own directory rather than one
	// the harness made for a job.
	//
	// It used to be inferred from scratch having been moved, which was sound for
	// exactly as long as one caller moved it. With separation universal that
	// inference answers "a person's" for every layout, and the engine would never
	// again run in a directory it owns. They were always two facts; they are now
	// two fields.
	personal bool

	mutex     sync.Mutex
	artifacts map[string]map[string]bool
	// baseline is the tree as it stood the moment each leaf started, and
	// observed is what diffing it afterwards proved that leaf did to the world.
	// They are the answer to "what did this run leave behind" that no tool has
	// to volunteer — see [Workspace.WatchTree].
	baseline map[string]*TreeSnapshot
	observed map[string]map[string]ArtifactChange
	jobID    int
}

func NewWorkspace(root string) (*Workspace, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("workspace root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		real = absolute
	}
	return &Workspace{
		root: absolute, real: real, scratch: absolute,
		artifacts: map[string]map[string]bool{},
		baseline:  map[string]*TreeSnapshot{},
		observed:  map[string]map[string]ArtifactChange{},
	}, nil
}

// WithScratch sends the harness's own files somewhere other than the workspace.
// Every surface that runs leaves calls it — see [Workspace.scratch] for what it
// costs when nobody does. A caller that does not (a test, an embedder) keeps the
// old shape, which is the workspace itself.
func (w *Workspace) WithScratch(dir string) *Workspace {
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		if absolute, err := filepath.Abs(trimmed); err == nil {
			w.scratch = absolute
		}
	}
	return w
}

// OwnedByPerson records that this root is somebody's own directory. It is the
// one caller whose workspace is not its own — `aforge do -w` edits a person's
// project in place — and it is said explicitly rather than inferred from where
// the machinery went, because the machinery now always goes elsewhere.
func (w *Workspace) OwnedByPerson() *Workspace {
	w.personal = true
	return w
}

// Root is the absolute directory.
func (w *Workspace) Root() string { return w.root }

// PersonalRoot reports that the root belongs to a person rather than to the
// harness.
//
// It is asked by any worker that would otherwise take the directory over: a
// directory the harness made for a job may be checked out, reset and swept; a
// directory somebody handed us holds their work and none of that is ours to do.
func (w *Workspace) PersonalRoot() bool { return w.personal }

// ScratchRoot is where this workspace's machinery lands. It equals Root only for
// a caller that never named one, which in the product is nobody.
func (w *Workspace) ScratchRoot() string { return w.scratch }

// ScratchPath maps a harness-owned relative path onto disk and returns, beside
// it, the spelling to show a model. The two differ only when scratch has been
// moved out of the workspace: a relative path would then name nothing an agent
// could open from its own cwd, so it is shown the absolute one.
func (w *Workspace) ScratchPath(relative string) (full, shown string, err error) {
	if w.scratch == w.root {
		full, err = w.Resolve(relative)
		return full, relative, err
	}
	cleaned := filepath.Clean(strings.TrimSpace(relative))
	if cleaned == "" || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", "", fmt.Errorf("scratch path %q is not relative", relative)
	}
	full = filepath.Join(w.scratch, cleaned)
	return full, full, nil
}

// Resolve maps a workspace-relative path onto disk, refusing anything that
// climbs out. Safety is not the point here — the point is that a path escaping
// the workspace is almost always a confused agent rather than an intended one,
// and failing loudly gives it something to correct.
func (w *Workspace) Resolve(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path is empty")
	}
	// Rooting the path before cleaning would quietly clamp "../x" to "x" and
	// write somewhere the caller did not ask for. Silently redirecting a write
	// is worse than refusing it: the agent believes it wrote one file, the file
	// appears at another name, and nothing ever says so.
	cleaned := filepath.Clean(trimmed)
	// An absolute path is fine when it lands inside the workspace — an agent
	// that just ran pwd writes absolute paths in good faith, and refusing them
	// cost a run its whole deliverable. Only a path genuinely outside is a
	// confused agent.
	if filepath.IsAbs(cleaned) {
		for _, root := range []string{w.root, w.real} {
			if relative, err := filepath.Rel(root, cleaned); err == nil &&
				relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
				return filepath.Join(w.root, relative), nil
			}
		}
		// Scratch is the run's own directory and a legitimate destination when
		// it has been moved out of the workspace. An intermediate leaf is
		// handed a path under it precisely so its working files stay out of a
		// person's project; refusing the path we handed out would send the
		// file straight back beside their work.
		if w.scratch != w.root {
			if relative, err := filepath.Rel(w.scratch, cleaned); err == nil &&
				relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
				return filepath.Join(w.scratch, relative), nil
			}
		}
		return "", fmt.Errorf("path %q is outside the workspace %s; stay within it", path, w.root)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes the workspace; use a path relative to it", path)
	}
	return filepath.Join(w.root, cleaned), nil
}

// Locate maps a recorded artifact path back onto disk.
//
// Recorded paths are workspace-relative, and a caller that stats one directly
// measures whatever sits at that name under its own working directory —
// usually nothing. An absolute path is no safer: the root has two honest
// spellings whenever it sits under a symlink (macOS /tmp -> /private/tmp), and
// only one of them is the spelling the file was recorded with. Both are tried
// here so the caller never has to know which one it holds.
func (w *Workspace) Locate(path string) (string, bool) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", false
	}
	var candidates []string
	if resolved, err := w.Resolve(trimmed); err == nil {
		candidates = append(candidates, resolved)
	}
	if filepath.IsAbs(trimmed) {
		candidates = append(candidates, trimmed)
	} else {
		candidates = append(candidates, filepath.Join(w.real, trimmed))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}
	return "", false
}

// DirectoryAt reports whether a directory already occupies a path.
//
// It exists for the one caller that offers a path rather than reading one: an
// output hint is an invitation to write a FILE at a name, and a directory
// already sitting at that name makes the invitation unfulfillable. A leaf handed
// it anyway did the only thing left — wrote its deliverable inside — reported
// the file as written, and settled done with the asked-for file absent from the
// workspace. Nothing here creates the path: an offered address that the offer
// itself brings into existence is a directory the next leaf must write around.
func (w *Workspace) DirectoryAt(path string) bool {
	located, ok := w.Locate(path)
	if !ok {
		return false
	}
	info, err := os.Stat(located)
	return err == nil && info.IsDir()
}

// HoldsNothingBut reports that the working directory contains no file a leaf
// could go and discover other than the ones named.
//
// It is the measured half of the sufficiency claim the brief makes (see
// [Linear.brief]). A leaf may only be told that its brief is the whole of what
// exists for its job if that is a fact about this directory, and the only honest
// way to hold a fact about a directory is to read it. So it is read: one bounded
// walk, at task assembly, skipping dot-entries — machinery lives outside the
// root now and .git is the tooling's — and the dependency trees producedSkipDir
// already names as somebody else's files.
//
// Cheap by construction and by shape. A harness-made job directory answers in
// one syscall because it is empty or holds only its siblings' deliverables; a
// person's repository answers false on the first source file it meets, before it
// has walked anything. An unreadable or unreasonably large tree answers false,
// because "could not tell" and "there is material here" must lead to the same
// silence.
func (w *Workspace) HoldsNothingBut(named []string) bool {
	if w == nil {
		return false
	}
	allowed := make(map[string]bool, len(named))
	for _, path := range named {
		if located, ok := w.Locate(path); ok {
			allowed[located] = true
			if resolved, err := filepath.EvalSymlinks(located); err == nil {
				allowed[resolved] = true
			}
		}
	}
	held := true
	visited := 0
	_ = filepath.WalkDir(w.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			held = false
			return fs.SkipAll
		}
		if visited++; visited > producedScanLimit {
			held = false
			return fs.SkipAll
		}
		if path == w.root {
			return nil
		}
		if entry.IsDir() {
			if producedSkipDir(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if allowed[path] {
			return nil
		}
		held = false
		return fs.SkipAll
	})
	return held
}

// Size reports an artifact's size on disk. The second result separates a file
// that is empty from one that is not there — a summary listing every artifact
// as 0 bytes looks like a run that produced nothing.
func (w *Workspace) Size(path string) (int64, bool) {
	located, ok := w.Locate(path)
	if !ok {
		return 0, false
	}
	info, err := os.Stat(located)
	if err != nil {
		return 0, false
	}
	return info.Size(), true
}

// Record notes that a node produced a file the person who asked for the work
// would call a deliverable.
//
// leaf is the identity everything one worker writes is filed under, and it is a
// string rather than a number for the reason SuggestPathFor is: the identity a
// caller has is not always a per-node integer. A store node's creation sequence
// is its whole splice's, so five siblings recorded under it shared one bucket
// and each of them was told the other four's files were its own. See Task.NodeKey
// for who supplies what.
func (w *Workspace) Record(leaf string, path string) { w.record(leaf, path, true) }

// RecordInternal notes a file the harness wrote for its own purposes — a
// background job's log, an extracted-document cache. They are real files in the
// workspace and the bookkeeping should know about them, but they are not the
// job's output: named to the user as "the files that job wrote", a process log
// and a PDF text dump stand beside the actual report as if they were peers.
// The .obs spill directory already solves this by never calling Record at all;
// these two cases need the record and only want it out of the answer.
func (w *Workspace) RecordInternal(leaf string, path string) { w.record(leaf, path, false) }

func (w *Workspace) record(leaf string, path string, deliverable bool) {
	relative, err := filepath.Rel(w.root, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return
	}
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.artifacts[leaf] == nil {
		w.artifacts[leaf] = map[string]bool{}
	}
	// A path recorded both ways is a deliverable: the harness happening to
	// touch a file the agent wrote does not demote it.
	w.artifacts[leaf][relative] = w.artifacts[leaf][relative] || deliverable
}

// nextJobID gives every background process started through THIS workspace
// handle a distinct log name. It is not distinct across handles, and it never
// was — the surfaces build one Workspace per claimed node — which was harmless
// only while each of those handles kept its logs inside its own job directory.
// With machinery pooled in one scratch home, two leaves both starting their
// first background job would write `1.log` on top of each other, so the leaf's
// own identity carries the rest of the uniqueness. See [jobLogName].
func (w *Workspace) nextJobID() int {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.jobID++
	return w.jobID
}

// Artifacts lists what a node left behind for the person who asked, in stable
// order. It is the union of two independent accounts, and it is a union because
// each of them alone has been measurably wrong.
//
// The CLAIMED account is the write family's: a path is here because a tool
// recorded it, which is also the worker's own statement that this file is what
// its work was for. The OBSERVED account is the filesystem's: a path is here
// because the tree gained or changed it while this leaf was running, whatever
// wrote it. Neither subsumes the other. A leaf that writes with a shell command
// is invisible to the first — on 2026-08-28 the delivery gate's audit said "the
// record shows nothing named out.txt was left behind" while out.txt sat on disk,
// and convicted a correct deliverable on that. A file rewritten inside one
// coarse filesystem second at exactly its old length is invisible to the second,
// and the tool that wrote it is not.
//
// The harness's own records are held back whichever account saw them: they flow
// into Outcome.Artifacts, from there into the head's files line and into every
// downstream leaf's "(files: …)" pointer, and none of those is a place to name a
// log. So is a deletion — see [Workspace.ArtifactFacts] for where deletions are
// kept and why they are not here.
func (w *Workspace) Artifacts(leaf string) []string {
	facts := w.ArtifactFacts(leaf)
	paths := make([]string, 0, len(facts))
	for _, fact := range facts {
		if fact.Change == ArtifactDeleted {
			continue
		}
		paths = append(paths, fact.Path)
	}
	return paths
}

// ArtifactChange is what the before-and-after read of the tree proved about one
// path. The zero value means the diff never saw it, which is the honest state of
// a file that only the write tool ever mentioned.
type ArtifactChange string

const (
	// ArtifactCreated is a path the tree did not hold when the leaf started.
	ArtifactCreated ArtifactChange = "created"
	// ArtifactChanged is a path whose length or write time moved under the leaf.
	ArtifactChanged ArtifactChange = "changed"
	// ArtifactDeleted is a path the tree held when the leaf started and does not
	// hold now.
	ArtifactDeleted ArtifactChange = "deleted"
)

// ArtifactFact is one path and the two independent things that are known about
// it: whether a tool claimed it and whether the world was seen to change it.
//
// Callers that only want the file list want [Workspace.Artifacts]. This exists
// for the ones that have to tell the two apart — an audit reporting what the
// worker said it produced against what the disk says happened cannot do its job
// from a merged list, and merging them is precisely how "the record shows
// nothing" came to outrank a file that existed.
type ArtifactFact struct {
	// Path is workspace-relative, the spelling everything downstream records.
	Path string
	// Claimed is the worker's own statement, through a write tool, that this
	// file is a deliverable of the work.
	Claimed bool
	// Observed is the filesystem's statement, through the before/after diff,
	// that this file moved while the leaf was running.
	Observed bool
	// Change is how it moved, and is empty when Observed is false.
	Change ArtifactChange
}

// ArtifactFacts is the whole evidence record for one leaf, in stable path order:
// every file a tool claimed, every file the tree was seen to gain or change, and
// every file the tree was seen to LOSE.
//
// Deletions live here and deliberately not in [Workspace.Artifacts]. That list
// is a list of things to open — it becomes the head's files line, a dependent's
// "(files: …)" pointer, and the gate's roll call of what is on disk — and a path
// that no longer exists sends every one of those readers to nothing. The fact
// that the leaf removed it is still evidence about the run, so it is kept, in
// the one place whose readers are asking what happened rather than what to read.
func (w *Workspace) ArtifactFacts(leaf string) []ArtifactFact {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	facts := make(map[string]ArtifactFact, len(w.artifacts[leaf])+len(w.observed[leaf]))
	for path, deliverable := range w.artifacts[leaf] {
		if !deliverable {
			// Recorded by the harness for itself — a background job's log, an
			// extracted-document cache. RecordInternal exists to keep these out
			// of the answer, and a second sighting of the same file by the tree
			// diff must not smuggle it back in.
			continue
		}
		facts[path] = ArtifactFact{Path: path, Claimed: true}
	}
	for path, change := range w.observed[leaf] {
		if deliverable, recorded := w.artifacts[leaf][path]; recorded && !deliverable {
			continue
		}
		fact := facts[path]
		fact.Path, fact.Observed, fact.Change = path, true, change
		facts[path] = fact
	}
	ordered := make([]ArtifactFact, 0, len(facts))
	for _, fact := range facts {
		ordered = append(ordered, fact)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	return ordered
}

// observedArtifactLimit bounds what one leaf's tree diff may claim. A change
// set larger than this is a tree rather than a deliverable: a refactor that
// touches four hundred files is a real outcome, but four hundred paths pasted
// into three dependents' contexts is not.
const observedArtifactLimit = 50

// TreeSnapshot is the workspace as the filesystem itself showed it at one
// instant: every path a deliverable could be, with enough of each file recorded
// to tell it apart from its successor without asking the clock what time it is.
//
// It is ONE type with ONE walk behind it, and that is the point of it. The
// whole-leaf evidence record (WatchTree at the top of a run, RecordChanges at
// landing) and the per-call sweep after every tool call are the same question
// asked over different spans — "what did the tree gain, lose or change between
// these two moments" — and two implementations of one question are two answers
// waiting to disagree about what a deliverable is.
type TreeSnapshot struct {
	// files is workspace-relative path to stamp.
	files map[string]fileStamp
	// at is when the walk was taken, moved back by producedSlack so a
	// filesystem that records whole seconds cannot hide a file behind it. It is
	// only ever read for a PARTIAL snapshot, where absence proves nothing and
	// the file's own write time is the last tiebreak available — see diffTrees.
	at time.Time
	// partial says the walk hit its bound or could not read something, so
	// absence from files means "not seen" rather than "not there".
	partial bool
}

// fileStamp is how a file is told apart from itself a moment later.
//
// SIZE, MODE AND CONTENT ARE THE EVIDENCE; THE WRITE TIME IS THE LAST RESORT.
// That ordering is FAILSAFE.md rule 2 — source evidence from the world — applied
// to one file: a tool that preserves timestamps (cp -p, git checkout, tar,
// rsync -t) leaves a file whose clock says nothing happened while its bytes say
// everything did, and a formatter that rewrites a file with byte-identical
// content moves the clock while changing nothing at all. The coding engine's
// worktree fingerprint reached the same conclusion from the other end and is
// documented in PERF.md under the worktree fingerprint's budget.
//
// digest is empty for a file too large to read inside the snapshot's budget, and
// two stamps that both lack one fall back on the write time, which is the old
// behaviour and the narrower answer rather than a wrong one.
type fileStamp struct {
	size     int64
	mode     fs.FileMode
	modified time.Time
	digest   string
}

// same says whether two sightings of one path are two sightings of one file.
func (was fileStamp) same(now fileStamp) bool {
	if was.size != now.size || was.mode != now.mode {
		return false
	}
	if was.digest != "" && now.digest != "" {
		return was.digest == now.digest
	}
	// Neither sighting could be read inside the budget. The clock is all that is
	// left, and it is here rather than at the top of the function precisely so
	// that it is never consulted about a file whose bytes are known.
	return was.modified.Equal(now.modified)
}

// WatchTree remembers the workspace as it is right now, so that what a leaf
// changes can be told from what was already sitting there.
//
// Every executor calls it once at the top of a run and calls [Workspace.RecordChanges]
// at landing; between the two, the difference is this leaf's mark on the world.
// A second run of the same leaf re-baselines, which is correct — the observations
// already made are kept, and a retry is only asked what IT did — and a leaf that
// was never watched simply has no observed account, which is the pre-existing
// behaviour and never a wrong answer, only a narrower one.
func (w *Workspace) WatchTree(leaf string) {
	if w == nil || strings.TrimSpace(leaf) == "" {
		return
	}
	snapshot := w.Snapshot()
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.baseline == nil {
		w.baseline = map[string]*TreeSnapshot{}
	}
	w.baseline[leaf] = snapshot
}

// RecordChanges reads the tree again and files everything that moved since
// [Workspace.WatchTree] under this leaf's identity. It is the observed half of
// [Workspace.Artifacts] and the only half that is true of a file written by a
// shell command, a build, or anything else that never went through a tool.
//
// It walks rather than being folded into Artifacts because Artifacts is read on
// the leaf's hot path — the no-progress guard counts it twice a turn — and a
// tree walk per turn is a cost that buys nothing there: the toolbox already
// records what each shell call produced as it goes. This is the whole-leaf
// backstop for everything that record misses, and it runs once, at landing.
//
// Calling it more than once is safe and cheap-ish: each call re-reads the tree
// and merges, so a leaf that lands and then terminates background jobs can ask
// again and pick up what those jobs left.
func (w *Workspace) RecordChanges(leaf string) {
	if w == nil || strings.TrimSpace(leaf) == "" {
		return
	}
	baseline, watched := w.watchedBaseline(leaf)
	if !watched {
		return
	}
	changes := diffTrees(baseline, w.Snapshot())
	w.noteObserved(leaf, changes, boundedPaths(changes))
}

// watchedBaseline is the sighting [Workspace.WatchTree] took for the leaf, and
// whether one was taken. It is its own method so the mutex is held from a
// defer while the tree walk and the record, which take their own locks, happen
// outside it.
func (w *Workspace) watchedBaseline(leaf string) (*TreeSnapshot, bool) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	baseline, watched := w.baseline[leaf]
	return baseline, watched
}

// diffTrees is THE comparison of two sightings of the workspace, and every
// account of what a run left behind is built on it — the whole-leaf record and
// the per-call sweep alike. Paths are workspace-relative, the spelling
// [Workspace.record] keeps and everything downstream reads.
//
// FAILSAFE.md rule 2: a fail-safe sources its evidence from the WORLD. What the
// tree held before and what it holds now are both facts about the world; "was
// this file written after some instant on some clock" is a fact about a clock,
// and it was wrong in three ordinary cases — a write that lands inside the
// filesystem's own timestamp granularity, a tool that preserves the mtime it
// copied, and a rewrite whose bytes are the same. The first of those made
// TestABareLeafFilesTheFilesItsToolsLeaveBehind fail about one run in four.
func diffTrees(before, after *TreeSnapshot) map[string]ArtifactChange {
	if before == nil || after == nil {
		return nil
	}
	changes := make(map[string]ArtifactChange, 8)
	for path, now := range after.files {
		was, known := before.files[path]
		if known {
			if !was.same(now) {
				changes[path] = ArtifactChanged
			}
			continue
		}
		// A path missing from a PARTIAL earlier snapshot may be a file the leaf
		// wrote or a file the bounded walk never reached, and calling the second
		// one "created" would credit a leaf with a repository it merely stood
		// in. The file's own write time is the tiebreak, and it is used HERE and
		// nowhere else: this is the one case where the world's own answer was
		// never taken.
		if before.partial && now.modified.Before(before.at) {
			continue
		}
		changes[path] = ArtifactCreated
	}
	// Deletions are only reported when BOTH walks were whole. A path absent from
	// a bounded second walk is as likely to be beyond the bound as gone, and
	// reporting a file somebody still has as deleted is a worse error than
	// staying quiet about one they no longer do.
	if !before.partial && !after.partial {
		for path := range before.files {
			if _, still := after.files[path]; !still {
				changes[path] = ArtifactDeleted
			}
		}
	}
	return changes
}

// noteObserved merges one diff into a leaf's observed record. It is the one
// door into that map, so the whole-leaf backstop and the per-call sweep — which
// both write to it, over spans that overlap — cannot keep two different sets of
// bookkeeping rules.
//
// paths is the caller's own bounded, ordered subset of changes; the two callers
// bound the same map differently and for different stated reasons (see
// observedArtifactLimit and producedPerCall).
func (w *Workspace) noteObserved(leaf string, changes map[string]ArtifactChange, paths []string) {
	if len(paths) == 0 {
		return
	}
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.observed == nil {
		w.observed = map[string]map[string]ArtifactChange{}
	}
	if w.observed[leaf] == nil {
		w.observed[leaf] = map[string]ArtifactChange{}
	}
	// Later sightings win. The per-call sweep and the whole-leaf backstop write
	// here over spans that overlap, and a path they disagree about is a path
	// that moved twice: a scratch file created by one call and removed by a
	// later one ends as a deletion, which is what keeps it out of the list of
	// things to open.
	for _, path := range paths {
		w.observed[leaf][path] = changes[path]
	}
}

// boundedPaths orders a change set and cuts it to what one leaf may claim. The
// order is the path's, so the same tree answers the same way twice — a cap that
// kept a different subset on every read would make two surfaces of one run
// disagree about which files exist.
func boundedPaths(changes map[string]ArtifactChange) []string {
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) > observedArtifactLimit {
		paths = paths[:observedArtifactLimit]
	}
	return paths
}

// Snapshot photographs every file a deliverable could be. It is what a caller
// holds across a span it wants the truth about — a whole leaf, or one tool
// call — and hands back to [Workspace.RecordChanges] or
// [Workspace.RecordProducedSince] at the other end.
//
// What it skips is exactly what the per-command sweep skips, from the same
// predicate: dot-entries, which are the harness's own machinery (.obs spills,
// .aforge job logs and traces) and the tooling's (.git, editor state), and the
// dependency trees producedSkipDir names — node_modules, vendor, site-packages,
// __pycache__, bower_components, venv. One predicate rather than two, because
// two lists of "what is not a deliverable" is two answers to one question.
//
// THE WALK IS BOUNDED AT producedScanLimit ENTRIES, which is 6000, AND THE BYTES
// IT READS AT snapshotDigestBudget. A workspace is usually a handful of files; a
// person's repository is not, and this runs at both ends of every leaf and both
// ends of every tool call. Past either bound the answer is partial or a stamp is
// digestless, and both of those are read as less than a whole answer rather than
// as a different one. PERF.md carries the budget.
func (w *Workspace) Snapshot() *TreeSnapshot {
	if w == nil {
		return nil
	}
	snapshot := &TreeSnapshot{files: make(map[string]fileStamp, 32), at: producedMark(time.Now())}
	budget := int64(snapshotDigestBudget)
	visited := 0
	_ = filepath.WalkDir(w.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// An unreadable entry is not this walk's to report. What it must not
			// do is treat "could not look" as "not there", so the answer becomes
			// partial and deletions go unclaimed.
			snapshot.partial = true
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if visited++; visited > producedScanLimit {
			snapshot.partial = true
			return fs.SkipAll
		}
		if path == w.root {
			return nil
		}
		if entry.IsDir() {
			if producedSkipDir(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(w.root, path)
		if err != nil || strings.HasPrefix(relative, "..") {
			return nil
		}
		stamp := fileStamp{size: info.Size(), mode: info.Mode().Perm(), modified: info.ModTime()}
		if info.Size() <= snapshotDigestFileLimit && budget >= info.Size() {
			if sum, ok := fileDigest(path); ok {
				stamp.digest = sum
				budget -= info.Size()
			}
		}
		snapshot.files[relative] = stamp
		return nil
	})
	return snapshot
}

// fileDigest reads one file's bytes and returns their hash. A file that cannot
// be read comes back without one rather than with a wrong one, and the stamp
// falls back on the clock exactly as it did before digests existed.
func fileDigest(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", false
	}
	return hex.EncodeToString(sum.Sum(nil)), true
}

// Existing lists the files already sitting in the workspace, as absolute paths
// in stable order.
//
// It answers the one question the in-memory artifact register cannot: what did a
// PREVIOUS process leave here. A leaf whose run was interrupted — by its own
// time ceiling, or by the terminal closing — comes back to a fresh Workspace
// whose register is empty and a directory that is not, and the files in it are
// the whole of what that attempt has to hand on. Reading them off disk is the
// only honest source, because the register never survived.
//
// Only the top level, and never the harness's own dot-directories: a
// deliverable is written where the output hint points, which is here, and
// everything below a dot is machinery an agent was deliberately not shown.
func (w *Workspace) Existing() []string {
	entries, err := os.ReadDir(w.root)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		paths = append(paths, filepath.Join(w.root, entry.Name()))
	}
	sort.Strings(paths)
	return paths
}

// obsDir holds spilled tool output. It is dot-prefixed so an agent listing the
// workspace sees its own deliverables rather than the machinery behind them.
const obsDir = ".obs"

// traceDir holds the turn-by-turn flight recorders, and it is a different
// directory from obsDir for one measured reason.
//
// .obs is the one machinery directory a leaf is deliberately sent into: every
// decay stub and every spilled result names a path under it and tells the agent
// to read the part it needs. An agent that follows one of those pointers and
// then lists the directory around it finds the recorders too — its own, which is
// its whole transcript restated, and every concurrent sibling's, because the
// workspace is shared. That was observed: 8KB of another leaf's contract pulled
// into a context that had no business holding it, cross-contamination by
// construction rather than by any agent's mistake.
//
// Moving the recorders one directory across fixes it at the only place it can be
// fixed. There is no listing surface to filter — the leaf reads its workspace
// with a shell, and any exclusion it could be told about is one it could also
// ignore. What actually removes a file from reach is not being where the agent
// was sent. .aforge is where the harness's own bookkeeping already lives (job
// logs), it is already excluded from a coding worker's diff, and nothing ever
// hands a leaf a path under this subdirectory of it.
const traceDir = ".aforge/trace"

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// SuggestPath derives a distinct output path for a node from a numeric id that
// is unique within one graph — which is what a plan node's id is.
//
// It is not what every caller has. A store node's creation sequence is the
// splice's, shared by every sibling it created, and passing that here is how
// five parallel briefs on five different topics landed on one filename. Any
// caller whose identity is not a per-node number wants SuggestPathFor.
func SuggestPath(nodeID int, title string) string {
	return SuggestPathFor(fmt.Sprintf("%02d", nodeID), title)
}

// SuggestPathFor derives a distinct output path from an identity that is unique
// per node and a title that is only there to be read.
//
// Uniqueness has to come from the key alone. The title cannot carry it: titles
// are clipped for display — a spliced job's to 48 characters — and five parts
// of one ask share their opening words, so five distinct topics arrive here as
// one identical string. Siblings run
// concurrently by construction, so a shared name is not a warning in a log —
// it is four deliverables silently overwritten by the fifth.
func SuggestPathFor(key, title string) string {
	slug := pathSlug(title)
	if slug == "" {
		slug = "output"
	}
	owner := pathSlug(key)
	if owner == "" {
		return slug + ".md"
	}
	return owner + "-" + slug + ".md"
}

func pathSlug(text string) string {
	return strings.Trim(nonWord.ReplaceAllString(strings.ToLower(text), "-"), "-")
}
