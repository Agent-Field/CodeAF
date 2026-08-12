package exec

import (
	"fmt"
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
	// turn traces, background job logs. It is the workspace itself whenever the
	// workspace belongs to the harness, which is every layout but one: an errand
	// that works directly in a person's own directory must not leave machinery
	// in it, so that caller points scratch at its private home instead.
	scratch string

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
// It is for the one caller whose workspace is not its own: `aforge do` edits a
// person's directory in place, and a run that left .obs and .aforge behind in
// someone's repository would be a mess they never asked for.
func (w *Workspace) WithScratch(dir string) *Workspace {
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		if absolute, err := filepath.Abs(trimmed); err == nil {
			w.scratch = absolute
		}
	}
	return w
}

// Root is the absolute directory.
func (w *Workspace) Root() string { return w.root }

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

// nextJobID gives every background process in the shared workspace a distinct
// log name. Registries remain per-leaf, but concurrent leaves must not append
// unrelated output to the same .aforge/jobs/N.log file.
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
// are clipped for display (a bundle's parts to 48 characters, a spliced job's
// to the same), and five parts of one ask share their opening words, so five
// distinct topics arrive here as one identical string. Siblings run
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
