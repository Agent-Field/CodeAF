package head

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A finished job says what it found in two places, and until now the head could
// only reach one of them. The node's summary is what the worker chose to type;
// the file beside it is what the worker actually wrote. depth.go already says as
// much in its own words — "the file path beside it is how a document is read" —
// while no reader existed anywhere in the process. So the head, asked "what is
// the answer?", could see a path, offer to go and look, and then not go, because
// there was nowhere for it to go. It made a promise the machine could not keep,
// which is worse than saying it did not know. This file is the reader that makes
// the offer honest.
//
// It is a security boundary, and the boundary is drawn by the graph rather than
// by the filesystem. The only bytes this can return come from a path the graph
// itself points at — a job's fold pointers and the paths its own settled summary
// names. A path the model invents, a path the user types, a path one directory
// across from a real artifact: none of them is openable, because the argument
// the caller passes is only ever used to CHOOSE among recorded paths, never to
// build one. Whatever is asked for, what gets opened is an entry from that
// recorded set and nothing else.
//
// The second half of the boundary is that a recorded path must still be what it
// looked like when it was recorded. It is resolved through every symlink and
// required to land in the directory it was recorded in, so an artifact name that
// is really a link out of the workspace opens nothing. Nothing here writes, and
// nothing here journals a command: like board, manual and result, it is a read.

const (
	// beltArtifactBytes is one artifact read's whole budget. It sits a little
	// above beltResultBytes for the same reason that one does above a deep
	// slice: this read was chosen rather than guessed, the model spent a call
	// naming this exact file, and the file is usually the answer itself rather
	// than context for one.
	beltArtifactBytes = 6 << 10
	// beltArtifactTailBytes is how much of an over-long file's end survives the
	// elision. A document's conclusion is the part a verdict question wants and
	// it is the part a head-only truncation always throws away, so the tail is
	// bought explicitly rather than left to luck.
	beltArtifactTailBytes = beltArtifactBytes / 3
	// beltArtifactCap bounds the recorded set one job can offer. resultFiles
	// already caps each node at deepFileCap; this bounds the union across a job
	// and its parts, because the set is rendered into an error message the model
	// has to read.
	beltArtifactCap = 12
)

// artifactSet is the openable set for one job: everything that job recorded,
// followed by everything its direct parts recorded. The parts are included
// because a one-leaf job writes its file under the leaf while the question is
// always asked about the job, and a boundary the caller cannot address is the
// same as no reader at all. cas:// pointers are dropped here: they are content
// addresses rather than filesystem paths, and there is nothing to open.
func (h *Head) artifactSet(node store.Node) []string {
	paths := make([]string, 0, beltArtifactCap)
	seen := make(map[string]bool, beltArtifactCap)
	add := func(candidates []string) {
		for _, path := range candidates {
			if strings.HasPrefix(path, "cas://") {
				continue
			}
			path = expandArtifactHome(path)
			if !filepath.IsAbs(path) || seen[path] || len(paths) == beltArtifactCap {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	add(resultFiles(node))
	nodes, err := h.store.ActiveNodes()
	if err != nil {
		return paths
	}
	for _, child := range nodes {
		if child.Parent != node.ID {
			continue
		}
		add(resultFiles(child))
	}
	return paths
}

// readArtifact opens one file a job wrote and renders it for a prompt. The
// error strings are written for the model rather than for a log, because a
// refusal it can read is a refusal it can correct on its next call.
func (h *Head) readArtifact(node store.Node, name string) (string, error) {
	paths := h.artifactSet(node)
	if len(paths) == 0 {
		return "", fmt.Errorf("%s recorded no files — what it came back with is all there is",
			surgeryTargetLabel(node))
	}
	picked, chosen := artifactPick(paths, name)
	if !chosen {
		if strings.TrimSpace(name) == "" {
			return "", fmt.Errorf("%s wrote more than one file — name one of: %s",
				surgeryTargetLabel(node), strings.Join(paths, ", "))
		}
		return "", fmt.Errorf("%q is not a file that job wrote; it wrote: %s",
			name, strings.Join(paths, ", "))
	}
	return readArtifactAt(picked)
}

// readArtifactAt is the opening half, shared by both recorded sets. Whatever
// chose the path, what happens to it afterwards is one rule.
func readArtifactAt(picked string) (string, error) {
	real, err := artifactRealPath(picked)
	if err != nil {
		return "", err
	}
	file, err := os.Open(real)
	if err != nil {
		return "", fmt.Errorf("%s is recorded but could not be opened: %w", picked, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("%s could not be read: %w", picked, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a document", picked)
	}
	body, err := readArtifactWindow(file, info.Size())
	if err != nil {
		return "", fmt.Errorf("%s could not be read: %w", picked, err)
	}
	if strings.TrimSpace(body) == "" {
		return picked + " (" + artifactSize(info.Size()) + ")\nthe file is empty", nil
	}
	return picked + " (" + artifactSize(info.Size()) + ")\n" + body, nil
}

// readWrittenArtifact opens one file this head wrote. It is readArtifact with a
// different recorded set and the identical boundary: the argument only ever
// CHOOSES among paths the system already recorded, and the chosen path must
// still resolve inside the directory it was recorded in. Nothing here builds a
// path out of what a caller typed, which is the whole of why the boundary holds.
func (h *Head) readWrittenArtifact(name string) (string, error) {
	paths := h.writtenArtifacts()
	if len(paths) == 0 {
		return "", fmt.Errorf("nothing has been written from this conversation — name the job that wrote the file, or write it first")
	}
	sort.Strings(paths)
	picked, chosen := artifactPick(paths, name)
	if !chosen {
		if strings.TrimSpace(name) == "" {
			return "", fmt.Errorf("more than one file has been written from this conversation — name one of: %s",
				strings.Join(paths, ", "))
		}
		return "", fmt.Errorf("%q is not a file written from this conversation; these are: %s",
			name, strings.Join(paths, ", "))
	}
	return readArtifactAt(picked)
}

// artifactPick turns whatever the caller typed into one entry of the recorded
// set, or into nothing. It matches on the whole path and on the file's own name
// because a model that has read a result knows the basename and often reaches
// for it alone. Nothing here constructs a path: every branch returns an entry
// the graph already recorded, which is the whole of why the boundary holds.
func artifactPick(paths []string, name string) (string, bool) {
	name = strings.Trim(strings.TrimSpace(name), `"'`)
	if name == "" {
		if len(paths) == 1 {
			return paths[0], true
		}
		return "", false
	}
	cleaned := filepath.Clean(expandArtifactHome(name))
	base := filepath.Base(cleaned)
	for _, path := range paths {
		if path == cleaned {
			return path, true
		}
	}
	for _, path := range paths {
		if filepath.Base(path) == base {
			return path, true
		}
	}
	return "", false
}

// artifactRealPath is the boundary's second half. A recorded name is trusted for
// where it sits, not for where it leads: resolved through its symlinks it has to
// end up in the same directory it was recorded in, so a worker that wrote a link
// pointing at the home directory hands the head a refusal rather than a file.
// The parent is resolved too, because on this platform the workspace's own
// prefix is frequently a link and comparing a resolved child to an unresolved
// parent would refuse every honest read.
func artifactRealPath(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%s is recorded but is not there any more: %w", path, err)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("%s is recorded but its directory could not be resolved: %w", path, err)
	}
	if filepath.Dir(real) != parent {
		return "", fmt.Errorf("%s points outside the directory it was recorded in and will not be opened", path)
	}
	return real, nil
}

// readArtifactWindow keeps one read inside beltArtifactBytes whatever the file
// weighs, and never reads more than that off the disk. A file over the budget
// comes back as its opening and its ending with a marker between them that says
// exactly how much is missing — an answer document's verdict is as often in the
// last paragraph as the first, and a silent truncation would let the model
// answer confidently from the half it happened to be given.
func readArtifactWindow(file *os.File, size int64) (string, error) {
	if size <= beltArtifactBytes {
		buffer := make([]byte, size)
		read, err := file.ReadAt(buffer, 0)
		if err != nil && read == 0 {
			return "", err
		}
		return string(buffer[:read]), nil
	}
	headBytes := beltArtifactBytes - beltArtifactTailBytes
	front := make([]byte, headBytes)
	if _, err := file.ReadAt(front, 0); err != nil {
		return "", err
	}
	back := make([]byte, beltArtifactTailBytes)
	if _, err := file.ReadAt(back, size-int64(beltArtifactTailBytes)); err != nil {
		return "", err
	}
	elided := size - int64(beltArtifactBytes)
	return artifactWholeRunes(front, false) +
		fmt.Sprintf("\n…[%d bytes elided of %d]…\n", elided, size) +
		artifactWholeRunes(back, true), nil
}

// artifactWholeRunes trims a byte window back to whole characters. A window cut
// at an arbitrary offset lands mid-rune about half the time in any non-English
// document, and a replacement character in the middle of the answer is the kind
// of damage nobody notices until it is quoted back to the user.
func artifactWholeRunes(window []byte, leading bool) string {
	if leading {
		// A tail window can only open mid-rune on a continuation byte, and a
		// rune is at most UTFMax long, so at most UTFMax-1 of them can precede
		// the first whole character.
		for attempt := 0; attempt < utf8.UTFMax && len(window) > 0; attempt++ {
			if window[0]&0xC0 != 0x80 {
				break
			}
			window = window[1:]
		}
		return string(window)
	}
	// A head window can only close mid-rune on an incomplete sequence, which is
	// exactly what DecodeLastRune reports as a one-byte error. A genuine U+FFFD
	// in the file decodes at its true width and is left alone.
	for attempt := 0; attempt < utf8.UTFMax && len(window) > 0; attempt++ {
		if rune, size := utf8.DecodeLastRune(window); rune != utf8.RuneError || size > 1 {
			break
		}
		window = window[:len(window)-1]
	}
	return string(window)
}

// artifactSize is the size line's whole job: telling the model whether what it
// is reading is the document or a window onto one.
func artifactSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d bytes", size)
	}
	return fmt.Sprintf("%.1f KB", float64(size)/1024)
}

// expandArtifactHome resolves the one prefix resultFiles admits besides an
// absolute path. It is done here rather than in resultFiles because the paths
// there are also rendered to the user, and "~/notes.md" is what they wrote.
func expandArtifactHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
