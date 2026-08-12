package exec

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// What a command left behind, and why the write tools alone could never see it.
//
// The artifact registry used to be populated by the write family only — write,
// edit, and the media tools — which encodes the assumption that a file arrives
// in the workspace by being typed into a tool call. Most of the useful ones do
// not. A script run under `sh` renders a chart, a build writes a binary, a
// converter emits a document: the leaf did the work, the file is on disk, and
// nothing in the product ever learned it existed. One measured run saved a
// 204KB plot the person had asked to see, never named it, was failed by the
// delivery gate for a message that "did not contain the script", and spent a
// whole continuation node retyping a file already sitting in the workspace.
//
// The swe executor had already solved its half of this by reading the tree
// before and after (see SWE.recordArtifacts). It can afford two reads because it
// runs once per leaf; this runs once per shell call, so it takes the cheaper
// half of the same idea: note the moment the command starts, then read the tree
// once afterwards and keep what was written since. One sweep, bounded, and the
// files it finds go into the same registry the write tools use, under the same
// node identity, so everything downstream — the files footer, the gate's
// evidence, a continuation's inputs — is fixed by this one record.
//
// It is deliberately a heuristic and deliberately generous in the safe
// direction: a file wrongly named as produced is noise in a list, while a file
// missed is a deliverable the person never hears about.

const (
	// producedScanLimit bounds one sweep. A workspace is usually a handful of
	// files, but a leaf that ran a build or unpacked an archive can have tens of
	// thousands, and this runs after every shell call — so the sweep stops
	// rather than walking a tree whose size is nobody's plan.
	producedScanLimit = 6000

	// producedPerCall bounds what one command may claim. A command that touched
	// hundreds of files did something whose output is a tree rather than a
	// deliverable, and a footer naming three hundred paths names nothing.
	producedPerCall = 24

	// producedSlack absorbs filesystem timestamp granularity. Some filesystems
	// record whole seconds, so a file written in the same second the command
	// began can carry a stamp fractionally before it. The cost of the slack is
	// re-recording a file an earlier tool call already recorded, which is a set
	// membership that was already true.
	producedSlack = time.Second
)

// producedMark is the instant a command starts, moved back far enough that a
// coarse filesystem cannot hide a file behind it.
func producedMark(now time.Time) time.Time { return now.Add(-producedSlack) }

// producedSince names the workspace files written at or after mark, newest
// first, bounded. Paths are absolute, which is what Workspace.Record expects.
func (w *Workspace) producedSince(mark time.Time) []string {
	if w == nil {
		return nil
	}
	type stamped struct {
		path string
		when time.Time
	}
	var found []stamped
	visited := 0
	_ = filepath.WalkDir(w.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// An unreadable entry is not this function's problem to report: the
			// command already ran and its own output said whatever went wrong.
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if visited++; visited > producedScanLimit {
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
		// Dot-files are the harness's own machinery (.obs spills, .aforge job
		// logs and traces) and the tooling's (.git objects, editor state). None
		// of them is anybody's deliverable, and the spill directory in
		// particular is written by the very turn that would record it.
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		if info.ModTime().Before(mark) {
			return nil
		}
		found = append(found, stamped{path: path, when: info.ModTime()})
		return nil
	})
	// Newest first, so that when the cap bites it keeps what the command most
	// recently produced rather than whatever sorts early in the alphabet. Ties
	// break on the path, so the list is the same list twice.
	sort.Slice(found, func(i, j int) bool {
		if found[i].when.Equal(found[j].when) {
			return found[i].path < found[j].path
		}
		return found[i].when.After(found[j].when)
	})
	if len(found) > producedPerCall {
		found = found[:producedPerCall]
	}
	paths := make([]string, 0, len(found))
	for _, file := range found {
		paths = append(paths, file.path)
	}
	return paths
}

// producedSkipDir names the trees that are somebody else's files sitting in
// this workspace: dependency installs, caches, and version-control storage. A
// leaf that ran `pip install` or `npm install` produced thousands of files and
// delivered none of them.
func producedSkipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "site-packages", "__pycache__", "bower_components", "venv":
		return true
	}
	return false
}

// recordProduced files everything the workspace gained since mark under this
// leaf's node identity — the same registry, keyed the same way, as a write.
func (t *Toolbox) recordProduced(mark time.Time) {
	if t.workspace == nil {
		return
	}
	for _, path := range t.workspace.producedSince(mark) {
		t.workspace.Record(t.leaf, path)
	}
}
