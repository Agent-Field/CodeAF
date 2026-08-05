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

	mutex     sync.Mutex
	artifacts map[int]map[string]bool
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
	return &Workspace{root: absolute, real: real, artifacts: map[int]map[string]bool{}}, nil
}

// Root is the absolute directory.
func (w *Workspace) Root() string { return w.root }

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

// Record notes that a node produced a file.
func (w *Workspace) Record(nodeID int, path string) {
	relative, err := filepath.Rel(w.root, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return
	}
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.artifacts[nodeID] == nil {
		w.artifacts[nodeID] = map[string]bool{}
	}
	w.artifacts[nodeID][relative] = true
}

// Artifacts lists what a node wrote, in stable order.
func (w *Workspace) Artifacts(nodeID int) []string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	paths := make([]string, 0, len(w.artifacts[nodeID]))
	for path := range w.artifacts[nodeID] {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// obsDir holds spilled tool output. It is dot-prefixed so an agent listing the
// workspace sees its own deliverables rather than the machinery behind them.
const obsDir = ".obs"

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)

// SuggestPath derives a distinct output path for a node. Deriving it from the
// id as well as the title means two nodes with similar titles — which the
// planner does produce — cannot land on the same file.
func SuggestPath(nodeID int, title string) string {
	slug := strings.Trim(nonWord.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if slug == "" {
		slug = "output"
	}
	return fmt.Sprintf("%02d-%s.md", nodeID, slug)
}
