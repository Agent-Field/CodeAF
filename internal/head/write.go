package head

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// The artifact law's door (12.5.1).
//
// Session bd3c78ed asked for an SVG architecture diagram. The head authored it
// inline as prose; the reply was cut mid-stream at 1,611 characters because the
// answering call ended at exactly 600 completion tokens; the half was journaled
// unmarked as if complete; asked to save and open it, the head offered, then
// discovered it held only the fragment, and stopped. Twelve provider calls,
// ~78k prompt tokens, zero deliverable, zero commands journaled.
//
// Every part of that failure had a cause and only one of them was the cap. The
// head had no way to put anything on disk. "Answer inline" was not the wrong
// choice among several — it was the ONLY route a deliverable had, so the output
// cap and the deliverable were competing for the same budget, and an output cap
// always wins. This file is the other route: anything the person will USE
// outside the conversation is born on disk and referenced by path.
//
// Three properties make it a door rather than a filesystem.
//
//   - The workspace is the default, not a cage. A person who says "save it to
//     /tmp/probe-story" gets /tmp/probe-story; only when they name nowhere does
//     the file land in the workspace. This tool used to refuse every path
//     outright, so the four probes that named a directory each had their
//     deliverable written somewhere else and were then ASKED, afterwards,
//     whether it should be moved — the product knowing it had disobeyed and
//     shipping anyway. A floor may still forbid a place, and when it does the
//     refusal is returned before anything is written, never discovered after.
//   - It revises what it wrote and never clobbers what it did not. A file this
//     conversation put on disk is updated in place, because "make the middle
//     column narrower" is a new version of one artifact and not a second
//     artifact: forking it to `-2` while calling it "the updated file" leaves
//     the person owning two files with no signal which is live. A file the
//     system did not write is left exactly as it is and the new one is minted
//     beside it, with the receipt saying so — overwriting the person's own bytes
//     to repair OUR mistake is still the wrong trade.
//   - What it writes, it can read back. artifact.go's boundary is that the only
//     openable bytes are ones the SYSTEM recorded a path for; a file this head
//     wrote is recorded exactly as firmly as a file a worker wrote, so it joins
//     that set rather than opening a second door beside it. Recording is by
//     absolute path, so a file written outside the workspace is as openable as
//     one written inside it.

const (
	// writeArtifactMaxBytes bounds one document. It is generous by the standards
	// of everything else in this package because this is the one place where the
	// bytes ARE the deliverable rather than context for one, and it is bounded at
	// all because a runaway model must not fill a disk.
	writeArtifactMaxBytes = 1 << 20
	// writeArtifactNameBytes bounds a filename. Longer than this is a sentence
	// rather than a name, and several filesystems refuse it outright.
	writeArtifactNameBytes = 96
	// writeArtifactPathBytes bounds the whole destination once a directory may
	// be named. It is a sanity bound rather than a rule about taste: most
	// filesystems refuse a path past about this length anyway, and the refusal
	// they give is not one a model can read.
	writeArtifactPathBytes = 1024
	// writeArtifactCollisionCap is how many times a name is minted around an
	// existing file before this gives up and says so.
	writeArtifactCollisionCap = 50
)

// WithWorkspace tells the head where artifacts are born.
//
// It is a builder rather than a constant because where a chat writes is a
// property of the surface that started it: `aforge chat` in a project means that
// project's directory, a room under rooms will mean the room's own. Unset, the
// head writes into the process's working directory — the same choice `aforge do`
// already makes for an errand pointed at somebody's own folder, and the one a
// person typing "write me the diagram" in a terminal expects.
func (h *Head) WithWorkspace(root string) *Head {
	if h == nil {
		return h
	}
	h.workspace = strings.TrimSpace(root)
	return h
}

// workspaceRoot resolves where this head writes, and reports whether it could.
// A surface with no writable directory at all is a fact the loop is told rather
// than a panic: 5.20's capability honesty means checking deliverability BEFORE
// offering, and a tool that says "I cannot put anything on disk here" is that
// check working.
func (h *Head) workspaceRoot() (string, error) {
	root := ""
	if h != nil {
		root = strings.TrimSpace(h.workspace)
	}
	if root == "" {
		working, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("no workspace is configured on this surface and the working directory could not be read: %w", err)
		}
		root = working
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%s is not a usable workspace: %w", root, err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return "", fmt.Errorf("%s could not be opened for writing: %w", absolute, err)
	}
	return absolute, nil
}

// write is the tool body. It returns what the loop may say and nothing more:
// the path that now exists, or the reason none does.
func (run *beltRun) write(args map[string]any) (string, bool) {
	body := beltString(args, "body")
	if strings.TrimSpace(body) == "" {
		return "body must carry the whole document, exactly as it should be on disk", true
	}
	if len(body) > writeArtifactMaxBytes {
		return fmt.Sprintf("that document is %d bytes, over the %d-byte ceiling for one write — split it, or have the workforce produce it",
			len(body), writeArtifactMaxBytes), true
	}
	root, err := run.head.workspaceRoot()
	if err != nil {
		return err.Error(), true
	}
	// Resolving the destination is the last thing that can refuse, and it is the
	// only one that touches the disk before the write — so a forbidden place is
	// said before a directory is made for it, not after a file is in one.
	target, err := artifactDestination(root, beltString(args, "name"), beltString(args, "dir"))
	if err != nil {
		return err.Error(), true
	}
	path, replaced, err := writeArtifactFile(target, body, run.head.wroteArtifact(target))
	if err != nil {
		return err.Error(), true
	}
	run.head.recordWrittenArtifact(path)

	what := strings.TrimSpace(beltString(args, "what"))
	verb := "Wrote "
	if replaced {
		verb = "Updated "
	}
	receipt := verb + path
	if what != "" {
		receipt += " — " + firstLine(what)
	}
	receipt += "."
	// No command seq: nothing was journaled, because a file is not a graph
	// mutation. It is still an ACT, so it is recorded — 5.20's rule that prose
	// turned into work is never a silent side effect does not care whether the
	// work was a command or a document.
	run.record(0, receipt)
	return writeArtifactResult(target, path, len(body), replaced), false
}

// writeArtifactResult is what the loop is allowed to say about where the bytes
// went, and it is written so the reply cannot be wrong. The three outcomes read
// differently on purpose: a revision must be called an update of the file they
// already have, and a mint-beside must be called a second file with the first
// one still there — the failure this replaces called a forked `-2` file "the
// updated file" while the original sat untouched next to it.
func writeArtifactResult(target, path string, size int, replaced bool) string {
	switch {
	case replaced:
		return fmt.Sprintf("replaced %s in place with %d bytes — same path, same file. It is the document they already have, so say it was updated; naming a new file would be false",
			path, size)
	case path != target:
		return fmt.Sprintf("wrote %d bytes to %s. %s was already there and is not this conversation's to overwrite, so it was left exactly as it is — say plainly that the original was kept and that this is a second file",
			size, path, target)
	default:
		return fmt.Sprintf("wrote %d bytes to %s — name this path in your reply; it is what the person will open", size, path)
	}
}

// artifactDestination resolves the one path this write will land on. It is the
// whole of the path boundary on the write side, and its shape is "honour what
// they named, refuse what a floor forbids, default to the workspace".
//
// A model holding a whole path reaches for the whole path, so a name carrying
// directories is SPLIT rather than refused: refusing it and being told to pass
// the parts separately reaches the same file one round trip later, and until
// this wave it reached a different file entirely.
func artifactDestination(root, rawName, rawDir string) (string, error) {
	name := strings.TrimSpace(rawName)
	if name == "" {
		return "", fmt.Errorf("name must be the file's own name with its extension, like architecture.svg or notes.md")
	}
	directory := expandArtifactHome(strings.TrimSpace(rawDir))
	if len(name)+len(directory) > writeArtifactPathBytes {
		return "", fmt.Errorf("that destination is longer than %d characters — give the file a short name in a directory you can spell", writeArtifactPathBytes)
	}
	if parent, base := filepath.Split(expandArtifactHome(name)); parent != "" {
		switch {
		case filepath.IsAbs(parent) || directory == "":
			directory = parent
		default:
			directory = filepath.Join(directory, parent)
		}
		name = base
	}
	if err := artifactName(name); err != nil {
		return "", err
	}
	directory, err := artifactDirectory(root, directory)
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, name), nil
}

// artifactName checks the file's own name once the directories are off it. What
// it refuses, it refuses by name, because a refusal the model can read is a
// refusal it can correct and a silently rewritten name is an artifact nobody
// can find.
func artifactName(name string) error {
	if name == "" {
		return fmt.Errorf("name must end in the file's own name with its extension, like architecture.svg or notes.md")
	}
	if len(name) > writeArtifactNameBytes {
		return fmt.Errorf("%q is too long for a filename — give it a short name with an extension", name)
	}
	if name != filepath.Clean(name) {
		return fmt.Errorf("%q is not a filename — give the file its own name and put the directory in dir", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("%q is a hidden file; artifacts are things the person opens, so give it an ordinary name", name)
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return fmt.Errorf("that filename contains control characters")
		}
	}
	if filepath.Ext(name) == "" {
		return fmt.Errorf("%q has no extension — the extension is how the person's machine knows what to open it with", name)
	}
	return nil
}

// artifactDirectory resolves where the file lands and opens it for writing.
//
// An absolute directory is the person's own answer to "where", and it is taken.
// A relative one is read against the workspace and may not climb out of it: a
// bare "../reports" is as likely to be a model's guess as a person's
// instruction, and the spelling that means it is the absolute one. The single
// floor is aforge's own state root — the journal, the CAS, the craft repo live
// there, and a deliverable written among them is a deliverable that can corrupt
// the product's memory of itself. The workspace is exempt from that floor even
// when it sits under the state root, because the workspace is exactly the place
// deliverables belong.
func artifactDirectory(root, named string) (string, error) {
	if named == "" {
		return root, nil
	}
	directory := named
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(root, directory)
		if !artifactWithin(root, directory) {
			return "", fmt.Errorf("%q climbs out of the workspace; if that is where they want it, give dir as the full path from /", named)
		}
	}
	directory = filepath.Clean(directory)
	if !artifactWithin(root, directory) && artifactWithin(home.Dir(), directory) {
		return "", fmt.Errorf("%s is inside aforge's own state directory and nothing may be written there — it holds the journal, not their files. Say so, and write it somewhere of theirs or leave dir out for the workspace", directory)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("%s could not be opened for writing: %w", directory, err)
	}
	return directory, nil
}

// artifactWithin reports whether child is parent or sits under it. Both sides
// are resolved as far as they exist, because on this platform a workspace's own
// prefix is frequently a symlink and comparing a resolved child to an
// unresolved parent would answer no for every honest path.
func artifactWithin(parent, child string) bool {
	parent, child = artifactResolved(parent), artifactResolved(child)
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)))
}

// artifactResolved follows the symlinks of the deepest part of a path that
// exists and puts the rest back on the end, so a directory nobody has created
// yet still compares against a resolved parent.
func artifactResolved(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for current := path; ; current = filepath.Dir(current) {
		if real, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		rest = filepath.Join(filepath.Base(current), rest)
	}
}

// writeArtifactFile puts the bytes down. It replaces the file only when the
// caller says this path is one the system itself produced; otherwise it creates
// exclusively, so the check and the write are one operation and a file that
// appeared between them cannot be lost.
func writeArtifactFile(target, body string, revise bool) (string, bool, error) {
	if revise {
		if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
			return "", false, fmt.Errorf("%s could not be written: %w", target, err)
		}
		return target, true, nil
	}
	directory, name := filepath.Dir(target), filepath.Base(target)
	extension := filepath.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	for attempt := 0; attempt <= writeArtifactCollisionCap; attempt++ {
		candidate := name
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d%s", stem, attempt+1, extension)
		}
		path := filepath.Join(directory, candidate)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", false, fmt.Errorf("%s could not be written: %w", path, err)
		}
		if _, err := file.WriteString(body); err != nil {
			file.Close()
			return "", false, fmt.Errorf("%s could not be written: %w", path, err)
		}
		if err := file.Close(); err != nil {
			return "", false, fmt.Errorf("%s could not be closed: %w", path, err)
		}
		return path, false, nil
	}
	return "", false, fmt.Errorf("%d files are already named like %s — give this one a different name", writeArtifactCollisionCap, name)
}

// recordWrittenArtifact adds one path to the set this head may open again.
//
// It is process state rather than journal state, and that is a known shortfall
// rather than a design: the durable form is a message part naming the artifact,
// which is Wave 2's structured-parts work, and the ledger says so. What it buys
// today is the repair doctrine's second half — a head that wrote a document an
// hour ago can read it back to fix it, instead of offering to and discovering it
// cannot.
func (h *Head) recordWrittenArtifact(path string) {
	if h == nil || strings.TrimSpace(path) == "" {
		return
	}
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	if h.wrote == nil {
		h.wrote = make(map[string]bool, 4)
	}
	h.wrote[path] = true
}

// wroteArtifact reports whether this exact path is one this conversation put on
// disk. It is the only licence to overwrite: a revision of the product's own
// artifact is that artifact's next version, while a file the system never wrote
// is the person's, and the person's bytes are never replaced by a repair of
// ours. The set is process state, so the worst a restart can do is fork a file
// the way this always used to — never clobber one it should not have.
func (h *Head) wroteArtifact(path string) bool {
	if h == nil || strings.TrimSpace(path) == "" {
		return false
	}
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	return h.wrote[path]
}

// writtenArtifacts is the recorded set, copied. artifact.go decides what may be
// opened out of it; this only says what is in it.
func (h *Head) writtenArtifacts() []string {
	if h == nil {
		return nil
	}
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	paths := make([]string, 0, len(h.wrote))
	for path := range h.wrote {
		paths = append(paths, path)
	}
	return paths
}
