package standing

// inbox.go is how news reaches a conversation whose window is not open. It is
// deliberately the simplest thing that works: one JSONL file inside the session
// folder, appended by whoever has news, drained whole the next time the person
// opens that conversation and shown under one "while you were away" fold.
//
// THE READER STAGES BEFORE IT READS. A note delivered while the fold is being
// built would otherwise be read and then deleted unseen; moving the file aside
// first means a racing delivery starts a fresh inbox that the next open finds.
//
// AND IT CONSUMES ONLY AFTER ITS EFFECT IS RECORDED (the scale audit's law L3,
// finding F2). The staged file used to be removed the moment it was read, so
// the notes lived only in memory until a later turn journaled them — and a
// window opened and closed with no turn, or a process killed in between, lost
// them. A staged file left by a crash was never read again either, because the
// reader only looked for inbox.jsonl. Now the file stays until the reader has
// recorded what it did with the notes and says so ([StagedInbox.Commit]), every
// staged file is read again at the next open, and each note carries an id the
// reader dedupes on, so a note read twice across a crash is folded once.
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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Deliver appends a note to a session's inbox.
func Deliver(sessionDir string, note Note) error {
	if note.At.IsZero() {
		note.At = time.Now()
	}
	if note.ID == "" {
		note.ID = newID()
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

// StagedInbox is an inbox taken for reading and not yet consumed: its notes,
// oldest first, and the staged files they came from.
type StagedInbox struct {
	Notes []Note
	files []string
}

// stagedSuffix ends the name of an inbox moved aside for reading.
const stagedSuffix = ".draining"

// StageInbox takes a session's inbox for reading: the live file is moved
// aside, and every staged file already there — one a reader took before it
// was killed — is read with it. Nothing is removed until [StagedInbox.Commit].
// An absent inbox is an empty stage and no error.
func StageInbox(sessionDir string) (StagedInbox, error) {
	path := InboxPath(sessionDir)
	if err := os.Rename(path, path+"."+newID()+stagedSuffix); err != nil && !os.IsNotExist(err) {
		return StagedInbox{}, err
	}
	files, err := filepath.Glob(path + ".*" + stagedSuffix)
	if err != nil {
		return StagedInbox{}, err
	}
	sort.Strings(files)
	var stage StagedInbox
	seen := map[string]bool{}
	for _, file := range files {
		stage.files = append(stage.files, file)
		for _, note := range readInbox(file) {
			if !seen[note.ID] {
				seen[note.ID] = true
				stage.Notes = append(stage.Notes, note)
			}
		}
	}
	sort.SliceStable(stage.Notes, func(a, b int) bool { return stage.Notes[a].At.Before(stage.Notes[b].At) })
	return stage, nil
}

// Commit consumes the stage: its files are removed. The reader calls it only
// once what it did with the notes is recorded, and a reader that never gets
// there leaves them for the next open.
func (s StagedInbox) Commit() {
	for _, file := range s.files {
		_ = os.Remove(file)
	}
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
		if note.ID == "" {
			// A note from before ids: its own bytes are the one thing that
			// names it the same way on every read.
			sum := sha256.Sum256(raw)
			note.ID = "line-" + hex.EncodeToString(sum[:8])
		}
		notes = append(notes, note)
	}
	// The file is already in the order it was written; the sort only matters
	// when two writers interleaved, and a stable sort keeps that order for the
	// notes that share a moment.
	sort.SliceStable(notes, func(a, b int) bool { return notes[a].At.Before(notes[b].At) })
	return notes
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
// cmd/aforge gives a session bucket under v3/projects, so a person who goes
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

// StageProjectInbox takes a project's inbox for reading — [StageInbox] at the
// project's address.
func StageProjectInbox(root, workspace string) (StagedInbox, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(workspace) == "" {
		return StagedInbox{}, nil
	}
	return StageInbox(ProjectInboxDir(root, workspace))
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
