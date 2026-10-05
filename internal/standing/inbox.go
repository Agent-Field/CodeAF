package standing

// inbox.go is how news reaches a conversation whose window is not open. It is
// deliberately the simplest thing that works: one JSONL file inside the session
// folder, appended by whoever has news, drained whole the next time the person
// opens that conversation and shown under one "while you were away" fold.
//
// THE DRAIN RENAMES BEFORE IT READS, UNDER A PER-INBOX FLOCK. A note delivered
// while the fold is being built would otherwise be read and then deleted unseen;
// moving the file aside first means a racing delivery starts a fresh inbox that
// the next open finds, and the flock keeps a racing delivery out of the inode
// the drain renamed away. A staged file left by a crash is recovered by the next
// drain, not lost.
//
// There are TWO addresses and one shape. A session's inbox is the one below; a
// PROJECT's inbox is the second half of this file, and it exists because not
// every conversation is a screen somebody comes back to — see its own comment.

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// The boxes on one inbox. They bound a folder so a stuck writer surfaces as
// visible backpressure rather than growing the folder without limit, and bound
// one line so an oversized note is refused (and its caller keeps the pending)
// rather than truncating the reader's scan silently.
const (
	inboxMaxBytes = 16 << 20
	inboxMaxLine  = 1 << 20
)

// errInboxFull is the visible backpressure of a bounded inbox. It is returned
// rather than swallowed, so the caller keeps its durable intent and can say so.
var errInboxFull = errors.New("standing: this inbox is full; the note was not delivered")

// withInboxLock serializes EVERY mutation of one inbox folder — Deliver, Drain
// and the seen record — under a process flock, so a delivery racing a drain
// cannot write into the inode the drain just renamed away and then lose the
// line to the drain's delete.
//
// THE LOCK FILE LIVES IN THE SYSTEM TEMP DIRECTORY, NAMED BY A HASH OF THE
// ABSOLUTE INBOX DIR, so a session or project folder is never polluted with a
// lock file a reader would have to know to ignore. Two processes that resolve
// the same folder and the same temp directory serialize against each other; a
// process with a different TEMP would not, which is stated rather than hidden.
func withInboxLock(dir string, fn func() error) error {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return errors.New("standing: an inbox with no address")
	}
	if err := os.MkdirAll(trimmed, 0o700); err != nil {
		return err
	}
	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		absolute = trimmed
	}
	sum := sha256.Sum256([]byte(filepath.Clean(absolute)))
	lockPath := filepath.Join(os.TempDir(), "codeaf-inbox-"+hex.EncodeToString(sum[:16])+".lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer func() { _ = filelock.Unlock(lock) }()
	return fn()
}

// Deliver appends a note to a session's inbox.
//
// A NOTE THAT CARRIES AN IDENTITY IS NOT DELIVERED TWICE, while the identity is
// inside the drained-identity window ([inboxSeenKeep]). The identity is the
// [Pending] record's id, written to the item before the line was carried out; a
// note whose identity was already delivered and drained is dropped here, so a
// replay after a restart whose acknowledgement was lost does not put the same
// line in front of the person again. A note with no identity — every note
// written before this existed, and every writer that has none — is passed
// through untouched. This is a durable-inbox guarantee, not a display one: the
// boundary is stated in [Drain].
func Deliver(sessionDir string, note Note) error {
	if note.At.IsZero() {
		note.At = time.Now()
	}
	line, err := json.Marshal(note)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if len(line) > inboxMaxLine {
		return fmt.Errorf("standing: a note larger than %d bytes was refused", inboxMaxLine)
	}
	return withInboxLock(sessionDir, func() error {
		if note.ID != "" && alreadyDrained(sessionDir, note.ID) {
			return nil
		}
		path := InboxPath(sessionDir)
		if info, err := os.Stat(path); err == nil && info.Size()+int64(len(line)) > inboxMaxBytes {
			return errInboxFull
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		if _, err := file.Write(line); err != nil {
			file.Close()
			return err
		}
		return file.Close()
	})
}

// Drain reads and removes a session's inbox, oldest first. An absent inbox is
// an empty slice and no error.
//
// IT RECOVERS A LEFTOVER .draining FILE DETERMINISTICALLY, even alongside a live
// inbox: a crash after the rename and before the delete would otherwise lose the
// notes for good. It reads every staged file, and a file it could NOT read whole
// (an open failure, a read failure, or a line too long for the scanner) is KEPT
// and reported rather than deleted. It remembers what it drained, so a replay is
// recognised as spent.
//
// THE GUARANTEE IS DURABLE PERSISTENCE AND A RECOVERABLE READ HANDOFF, NOT USER
// DISPLAY ACKNOWLEDGEMENT: a note removed here has been handed to the caller,
// and whether the caller then draws, folds or forwards it is outside this
// package. The drained-identity window is [inboxSeenKeep]; beyond it a replay is
// shown again. No exactly-once claim is made about anything else.
func Drain(sessionDir string) ([]Note, error) {
	var (
		notes    []Note
		problems []error
	)
	err := withInboxLock(sessionDir, func() error {
		path := InboxPath(sessionDir)
		// Move the live inbox aside so a delivery that starts after this point
		// lands in a fresh file. The lock already orders writers; the rename is
		// what makes the isolation legible to a reader of the folder.
		if _, err := os.Stat(path); err == nil {
			if err := os.Rename(path, path+"."+newID()+".draining"); err != nil {
				return err
			}
		}
		staged, err := drainFiles(sessionDir)
		if err != nil {
			return err
		}
		for _, file := range staged {
			part, readErr := readInbox(file)
			if readErr != nil {
				// NEVER DELETE WHAT COULD NOT BE READ WHOLE; the notes are kept for
				// the next drain and the failure is reported.
				problems = append(problems, fmt.Errorf("%s: %w", filepath.Base(file), readErr))
				continue
			}
			if err := os.Remove(file); err != nil {
				problems = append(problems, err)
			}
			notes = append(notes, part...)
		}
		sort.SliceStable(notes, func(a, b int) bool { return notes[a].At.Before(notes[b].At) })
		if err := rememberDrained(sessionDir, notes); err != nil {
			problems = append(problems, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return notes, errors.Join(problems...)
}

// drainFiles lists the staged .draining files under a session folder, oldest
// name first, so recovery is deterministic rather than directory-order.
func drainFiles(sessionDir string) ([]string, error) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	prefix := filepath.Base(InboxPath(sessionDir)) + "."
	var out []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".draining") {
			continue
		}
		out = append(out, filepath.Join(sessionDir, name))
	}
	sort.Strings(out)
	return out, nil
}

// readInbox reads one inbox file, oldest first. One line that will not parse is
// skipped rather than costing the person the rest, but a READ FAILURE — an open
// error, a read error, or a line longer than [inboxMaxLine] — is returned so the
// caller does not delete a file it could not read whole.
func readInbox(path string) ([]Note, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var notes []Note
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), inboxMaxLine)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var note Note
		if err := json.Unmarshal(raw, &note); err != nil {
			continue
		}
		// ONE IDENTITY, ONE NOTE. A torn or repeated append of the same
		// delivery is one line to the person, not two.
		if note.ID != "" {
			if seen[note.ID] {
				continue
			}
			seen[note.ID] = true
		}
		notes = append(notes, note)
	}
	if err := scanner.Err(); err != nil {
		return notes, err
	}
	// The file is already in the order it was written; the sort only matters
	// when two writers interleaved, and a stable sort keeps that order for the
	// notes that share a moment.
	sort.SliceStable(notes, func(a, b int) bool { return notes[a].At.Before(notes[b].At) })
	return notes, nil
}

// inboxSeenKeep bounds the drained-identity record so recognising a replayed
// delivery cannot grow a session folder without limit.
const inboxSeenKeep = 4096

// seenPath is the drained-identity record beside an inbox.
func seenPath(sessionDir string) string { return filepath.Join(sessionDir, "inbox.seen") }

// alreadyDrained reports whether a delivery identity was already drained from
// this inbox. A missing or unreadable record reads as "not drained": the cost
// of a duplicate is one line the person has already seen, and the cost of a
// false positive is a line they never see at all.
func alreadyDrained(sessionDir, id string) bool {
	file, err := os.Open(seenPath(sessionDir))
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		if strings.TrimSpace(string(scanner.Bytes())) == id {
			return true
		}
	}
	return false
}

// rememberDrained appends the drained notes' identities to the record, bounded
// by [inboxSeenKeep]. IT REPORTS a write failure rather than swallowing it: a
// lost dedup is possible, but it is not silent.
func rememberDrained(sessionDir string, notes []Note) error {
	ids := make([]string, 0, len(notes))
	for _, note := range notes {
		if note.ID != "" {
			ids = append(ids, note.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return err
	}
	path := seenPath(sessionDir)
	kept := readSeenIDs(path)
	kept = append(kept, ids...)
	if len(kept) > inboxSeenKeep {
		kept = kept[len(kept)-inboxSeenKeep:]
	}
	var out strings.Builder
	for _, id := range kept {
		out.WriteString(id)
		out.WriteByte('\n')
	}
	return writeAtomic(path, []byte(out.String()))
}

// readSeenIDs reads the drained-identity record, skipping blanks. A missing or
// unreadable file is an empty record, not an error.
func readSeenIDs(path string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var ids []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		if line := strings.TrimSpace(string(scanner.Bytes())); line != "" {
			ids = append(ids, line)
		}
	}
	return ids
}

// ── the project inbox: news for a project, not for one conversation ─────────
//
// A SESSION INBOX IS NOT ALWAYS A SCREEN. The fold above works because a
// session folder is a row on home and a conversation somebody reopens. An
// EXCHANGE — the short errand said at home's `ask here` box — is neither: its
// folder lives under the standing root precisely so home never lists it, and it
// is closed the moment home closes. So a firing whose origin is an exchange and
// whose windows are all shut has nowhere in the session layout to land, and the
// note would sit in a file no screen ever opens.
//
// The project inbox is that address. It belongs to the WORKSPACE rather than to
// a conversation, so home can draw it under the project and the next ordinary
// conversation opened in that project folds it into its own "while you were
// away". One JSONL file, the same [Note] shape and the same append, because the
// only thing that differs is who it is addressed to.

// projectsDirName is where the project inboxes live under the standing root.
// It sits beside exchanges/ and the item documents; nothing that walks the root
// mistakes it for an item, because an item is a `<id>.json` file and a folder
// with a runs/ inside it.
const projectsDirName = "projects"

// ProjectKey is the one name a workspace has under [projectsDirName]: the path
// with its separators turned to dashes, exactly the dumb one-way spelling
// cmd/codeaf gives a session bucket under v3/projects, so a person who goes
// looking recognises the folder names from the ones they already know.
//
// IT IS NEVER DECODED AND NEVER JOINED TO A BUCKET. Decoding would be guessing
// which dashes were separators (internal/session's world.go says so about the
// bucket), and nothing here reads a bucket name or hands one out — every caller
// on all three sides holds the workspace path itself, so this is a key and not
// an address anybody has to reverse.
func ProjectKey(workspace string) string {
	key := strings.ReplaceAll(filepath.Clean(strings.TrimSpace(workspace)), string(filepath.Separator), "-")
	key = strings.ReplaceAll(key, ":", "-")
	if !strings.HasPrefix(key, "-") {
		key = "-" + key
	}
	return key
}

// ProjectInboxDir is the folder one project's inbox sits in.
func ProjectInboxDir(root, workspace string) string {
	return filepath.Join(root, projectsDirName, ProjectKey(workspace))
}

// ProjectInboxPath is that folder's inbox.jsonl.
func ProjectInboxPath(root, workspace string) string {
	return InboxPath(ProjectInboxDir(root, workspace))
}

// DeliverProject appends a note to a project's inbox. It is [Deliver] with the
// address worked out, so the two inboxes cannot drift on their line shape.
func DeliverProject(root, workspace string, note Note) error {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(workspace) == "" {
		return errors.New("standing: a project inbox needs a root and a workspace")
	}
	return Deliver(ProjectInboxDir(root, workspace), note)
}

// DrainProject reads and removes a project's inbox, oldest first — [Drain] at
// the project's address.
func DrainProject(root, workspace string) ([]Note, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(workspace) == "" {
		return nil, nil
	}
	return Drain(ProjectInboxDir(root, workspace))
}

// PeekProjectInbox reads a project's inbox WITHOUT emptying it, oldest first.
//
// It is what a screen calls. Home draws what is waiting every time it redraws,
// and a read that emptied the file would mean the first draw of a project card
// consumed the news the conversation was supposed to fold in. Draining is the
// conversation's act and this is the looking.
func PeekProjectInbox(root, workspace string) []Note {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(workspace) == "" {
		return nil
	}
	notes, _ := readInbox(ProjectInboxPath(root, workspace))
	return notes
}
