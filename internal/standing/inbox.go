package standing

// inbox.go is how news reaches a conversation whose window is not open. It is
// deliberately the simplest thing that works: one JSONL file inside the session
// folder, appended by whoever has news, drained whole the next time the person
// opens that conversation and shown under one "while you were away" fold.
//
// THE DRAIN RENAMES BEFORE IT READS. A note delivered while the fold is being
// built would otherwise be read and then deleted unseen; moving the file aside
// first means a racing delivery starts a fresh inbox that the next open finds.
//
// There are TWO addresses and one shape. A session's inbox is the one below; a
// PROJECT's inbox is the second half of this file, and it exists because not
// every conversation is a screen somebody comes back to — see its own comment.

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Deliver appends a note to a session's inbox.
//
// A NOTE THAT CARRIES AN IDENTITY IS NOT DELIVERED TWICE. The identity is the
// [Pending] record's id, written to the item before the line was carried out;
// a note whose identity was already delivered and drained is dropped here, so a
// restart that re-runs a delivery whose acknowledgement was lost does not put
// the same line in front of the person again. A note with no identity — every
// note written before this existed, and every writer that has none — is passed
// through untouched.
func Deliver(sessionDir string, note Note) error {
	if note.ID != "" && alreadyDrained(sessionDir, note.ID) {
		return nil
	}
	if note.At.IsZero() {
		note.At = time.Now()
	}
	line, err := json.Marshal(note)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(InboxPath(sessionDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(line); err != nil {
		return err
	}
	return file.Close()
}

// Drain reads and removes a session's inbox, oldest first. An absent inbox is
// an empty slice and no error.
//
// IT REMEMBERS WHAT IT DRAINED, so a delivery replayed after a restart whose
// note was already shown is recognised as spent ([Deliver]) rather than shown
// twice. The record is the ids alone, bounded by [inboxSeenKeep].
func Drain(sessionDir string) ([]Note, error) {
	path := InboxPath(sessionDir)
	staged := path + "." + newID() + ".draining"
	if err := os.Rename(path, staged); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer os.Remove(staged)
	notes := readInbox(staged)
	rememberDrained(sessionDir, notes)
	return notes, nil
}

// readInbox reads one inbox file, oldest first. A file that is not there, or
// one line of it that will not parse, is nothing rather than an error: an inbox
// is news and never a record anybody reconciles, so one unreadable line must
// not cost the person the rest of them.
func readInbox(path string) []Note {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var notes []Note
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
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
	// The file is already in the order it was written; the sort only matters
	// when two writers interleaved, and a stable sort keeps that order for the
	// notes that share a moment.
	sort.SliceStable(notes, func(a, b int) bool { return notes[a].At.Before(notes[b].At) })
	return notes
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
// by [inboxSeenKeep]. A failure here loses a dedup, never a note, so it is
// deliberately best effort.
func rememberDrained(sessionDir string, notes []Note) {
	ids := make([]string, 0, len(notes))
	for _, note := range notes {
		if note.ID != "" {
			ids = append(ids, note.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return
	}
	path := seenPath(sessionDir)
	kept := make([]string, 0, inboxSeenKeep)
	if file, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 4096), 1<<20)
		for scanner.Scan() {
			if line := strings.TrimSpace(string(scanner.Bytes())); line != "" {
				kept = append(kept, line)
			}
		}
		file.Close()
	}
	kept = append(kept, ids...)
	if len(kept) > inboxSeenKeep {
		kept = kept[len(kept)-inboxSeenKeep:]
	}
	var out strings.Builder
	for _, id := range kept {
		out.WriteString(id)
		out.WriteByte('\n')
	}
	_ = writeAtomic(path, []byte(out.String()))
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
	return readInbox(ProjectInboxPath(root, workspace))
}
