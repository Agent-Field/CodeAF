package exec

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
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
	jobID     int
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
	return &Workspace{root: absolute, real: real, scratch: absolute, artifacts: map[string]map[string]bool{}}, nil
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

// Artifacts lists what a node wrote for the person who asked, in stable order.
// The harness's own records are held back: they flow into Outcome.Artifacts,
// from there into the head's files line and into every downstream leaf's
// "(files: …)" pointer, and none of those is a place to name a log.
func (w *Workspace) Artifacts(leaf string) []string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	paths := make([]string, 0, len(w.artifacts[leaf]))
	for path, deliverable := range w.artifacts[leaf] {
		if deliverable {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
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
