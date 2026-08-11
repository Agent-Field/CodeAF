package head

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
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
//   - The name is a name, not a path. Directories, traversal, absolute spellings
//     and hidden files are refused rather than sanitized, because a refusal the
//     model can read is a refusal it can correct, and a silently rewritten path
//     is an artifact nobody can find.
//   - It never clobbers. A file already at that name is left exactly as it is
//     and the new one is minted beside it, with the receipt saying so. The repair
//     doctrine means this tool is called precisely when a previous attempt was
//     broken, and overwriting the person's own edits to repair OUR mistake is the
//     wrong trade.
//   - What it writes, it can read back. artifact.go's boundary is that the only
//     openable bytes are ones the SYSTEM recorded a path for; a file this head
//     wrote is recorded exactly as firmly as a file a worker wrote, so it joins
//     that set rather than opening a second door beside it.

const (
	// writeArtifactMaxBytes bounds one document. It is generous by the standards
	// of everything else in this package because this is the one place where the
	// bytes ARE the deliverable rather than context for one, and it is bounded at
	// all because a runaway model must not fill a disk.
	writeArtifactMaxBytes = 1 << 20
	// writeArtifactNameBytes bounds a filename. Longer than this is a sentence
	// rather than a name, and several filesystems refuse it outright.
	writeArtifactNameBytes = 96
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
	name, err := artifactName(beltString(args, "name"))
	if err != nil {
		return err.Error(), true
	}
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
	path, err := writeArtifactFile(root, name, body)
	if err != nil {
		return err.Error(), true
	}
	run.head.recordWrittenArtifact(path)

	what := strings.TrimSpace(beltString(args, "what"))
	receipt := "Wrote " + path
	if what != "" {
		receipt += " — " + firstLine(what)
	}
	receipt += "."
	// No command seq: nothing was journaled, because a file is not a graph
	// mutation. It is still an ACT, so it is recorded — 5.20's rule that prose
	// turned into work is never a silent side effect does not care whether the
	// work was a command or a document.
	run.record(0, receipt)
	renamed := ""
	if filepath.Base(path) != name {
		renamed = fmt.Sprintf(" (%s was already there and was left alone)", name)
	}
	return fmt.Sprintf("wrote %d bytes to %s%s — name this path in your reply; it is what the person will open", len(body), path, renamed), false
}

// artifactName is the whole of the path boundary on the write side. It admits a
// plain filename and refuses everything else by name, so a model that reached
// for a path learns what to pass instead.
func artifactName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("name must be the file's own name with its extension, like architecture.svg or notes.md")
	}
	if len(name) > writeArtifactNameBytes {
		return "", fmt.Errorf("%q is too long for a filename — give it a short name with an extension", name)
	}
	if strings.ContainsAny(name, `/\`) || name != filepath.Clean(name) {
		return "", fmt.Errorf("%q names a path; pass a plain filename with no directories in it and it lands in the workspace", name)
	}
	if strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("%q is a hidden file; artifacts are things the person opens, so give it an ordinary name", name)
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", fmt.Errorf("that filename contains control characters")
		}
	}
	if filepath.Ext(name) == "" {
		return "", fmt.Errorf("%q has no extension — the extension is how the person's machine knows what to open it with", name)
	}
	return name, nil
}

// writeArtifactFile puts the bytes down without ever overwriting. It creates
// exclusively, so the check and the write are one operation and a file that
// appeared between them cannot be lost.
func writeArtifactFile(root, name, body string) (string, error) {
	extension := filepath.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	for attempt := 0; attempt <= writeArtifactCollisionCap; attempt++ {
		candidate := name
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d%s", stem, attempt+1, extension)
		}
		path := filepath.Join(root, candidate)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("%s could not be written: %w", path, err)
		}
		if _, err := file.WriteString(body); err != nil {
			file.Close()
			return "", fmt.Errorf("%s could not be written: %w", path, err)
		}
		if err := file.Close(); err != nil {
			return "", fmt.Errorf("%s could not be closed: %w", path, err)
		}
		return path, nil
	}
	return "", fmt.Errorf("%d files are already named like %s — give this one a different name", writeArtifactCollisionCap, name)
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
