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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// The boxes on one inbox. They bound a folder so a stuck writer surfaces as
// visible backpressure rather than growing the folder without limit, and bound
// one line so an oversized note is refused (and its caller keeps the pending)
// rather than truncating the reader's scan silently.
const (
	inboxMaxBytes = 16 << 20
	inboxMaxLine  = 1 << 20
	// inboxLockName is the flock file's name inside an inbox folder. It is a
	// dot-name, so no reader that globs the folder's notes ever sees it, and it
	// travels WITH the folder so a symlink alias and a different TEMP directory
	// still resolve to one lock.
	inboxLockName = ".inbox.lock"
)

// errInboxFull is the visible backpressure of a bounded inbox. It is returned
// rather than swallowed, so the caller keeps its durable intent and can say so.
var errInboxFull = errors.New("standing: this inbox is full; the note was not delivered")

// withInboxLock serializes EVERY mutation of one inbox folder — Deliver, Drain
// and the seen record — under a process flock, so a delivery racing a drain
// cannot write into the inode the drain just renamed away and then lose the
// line to the drain's delete.
//
// THE LOCK IS ADDRESSED BY THE INBOX, NOT BY THE PROCESS. It lives inside the
// inbox folder under a dot-name no reader scans ([inboxLockName]), and the
// folder is first canonicalised through any symlink alias, so two processes
// that name the same inbox — through a symlink, or with different TEMP
// directories — resolve to the same lock file and serialize. A lock named by
// os.TempDir() would not: a process with a different TEMP would take a
// different lock and could write the inode the drain renamed away.
func withInboxLock(dir string, fn func() error) error {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return errors.New("standing: an inbox with no address")
	}
	if err := os.MkdirAll(trimmed, 0o700); err != nil {
		return err
	}
	canonical := trimmed
	if resolved, err := filepath.EvalSymlinks(trimmed); err == nil {
		canonical = resolved
	}
	lockPath := filepath.Join(canonical, inboxLockName)
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
//
// A DEDUP RECORD THAT CANNOT BE READ IS NOT "NOTHING WAS DRAINED". The missing
// file is normal and reads as an empty record; a read/scanner failure does not.
// The note is still appended — a duplicate is the safe direction and a line
// withheld because a record was corrupt is not — but the failure is RETURNED
// so the caller that owns the durable intent keeps it and says so. It is never
// swallowed into a success by absence.
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
		var dedupErr error
		if note.ID != "" {
			drained, err := alreadyDrained(sessionDir, note.ID)
			switch {
			case err != nil:
				// CANNOT VERIFY THE RECORD. Deliver anyway, then report it: a
				// duplicate the person has seen before is the safe cost, and the
				// error keeps the caller's durable intent alive.
				dedupErr = fmt.Errorf("standing: the drained-identity record could not be read, so this delivery may repeat: %w", err)
			case drained:
				return nil
			}
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
		// THE DELIVERY IS ON DISK BEFORE IT IS ACKNOWLEDGED. A pending intent is
		// dropped by the caller as soon as this returns nil, so a crash between an
		// unflushed write and the acknowledgement must not lose the line: Sync
		// makes the append durable, and a Sync failure is reported rather than
		// read as success.
		if err := file.Sync(); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return dedupErr
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
//
// IT IS TRANSACTIONAL IN THE ONE ORDER THAT CANNOT LOSE A NOTE: read everything
// first, make the dedup record durable, and only then remove the files. What
// this package can promise is the queue-to-caller handoff — a note it returns
// has been read whole and, where it has an identity, recorded as spent — and
// that promise is kept even when something else in the drain failed:
//
//   - a file that could not be read WHOLE is kept and reported, never deleted;
//   - if [rememberDrained] cannot be written, NO file is deleted, so the whole
//     payload is retained for the next drain and the failure is returned;
//   - a file whose notes were recorded but whose later removal failed is kept
//     too, and the identity record recognises its notes as spent next time, so
//     that is a duplicate at worst and never a loss;
//   - notes gathered from more than one staged file are deduped against each
//     other in THIS drain as well as against the seen record;
//   - a dedup record that cannot be READ is reported and left untouched — it is
//     never read as an empty record and never rewritten, so the identities
//     already in it survive — and nothing is deleted under it, so a retry
//     re-hands the payload rather than losing it;
//   - a corrupt line in the record is counted and reported while the valid
//     identities beside it are still honoured.
//
// A caller MUST therefore use the notes it is handed even when the error is
// non-nil: the error says something in or around the drain failed, not that
// every note is unusable. Valid notes returned here are the person's own
// authorized work and are never discarded because a sibling file was malformed.
// A note whose identity was drained before — by this drain or an earlier one —
// is passed over. Losing a note is the direction this refuses to fail toward;
// repeating one is the stated cost.
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
		// THE DEDUP RECORD IS READ HONESTLY. A missing file is a normal empty
		// record; a corrupt line is counted and reported; a read or scanner
		// failure is fatal to the record and is never read as "nothing was
		// drained".
		seenIDs, corrupt, seenErr := readSeenIDs(seenPath(sessionDir))
		if corrupt > 0 {
			problems = append(problems, fmt.Errorf("standing: the drained-identity record has %d unreadable line(s)", corrupt))
		}
		drained := map[string]bool{}
		for _, id := range seenIDs {
			drained[id] = true
		}
		// READ EVERYTHING BEFORE ANYTHING IS DELETED. A file that could not be
		// read whole is kept for the next drain and its failure reported; its
		// readable neighbours are still handed out.
		readable := make([]string, 0, len(staged))
		for _, file := range staged {
			part, readErr := readInbox(file)
			if readErr != nil {
				problems = append(problems, fmt.Errorf("%s: %w", filepath.Base(file), readErr))
				continue
			}
			readable = append(readable, file)
			for _, note := range part {
				if note.ID != "" {
					// ONE IDENTITY IS ONE NOTE ACROSS THE WHOLE DRAIN, not only
					// within one file: a retry can leave the same id staged in
					// two files, and both would otherwise reach the person.
					if drained[note.ID] {
						continue
					}
					drained[note.ID] = true
				}
				notes = append(notes, note)
			}
		}
		sort.SliceStable(notes, func(a, b int) bool { return notes[a].At.Before(notes[b].At) })
		if seenErr != nil {
			// THE RECORD COULD NOT BE READ, SO IT IS NOT REWRITTEN AND NOTHING IS
			// DELETED. Rewriting it would drop the identities already inside it
			// (dedup state lost); deleting the files would lose the payload.
			// The notes read whole are still returned, and the failure rides out
			// with them.
			problems = append(problems, fmt.Errorf("standing: the drained-identity record could not be read: %w", seenErr))
			return nil
		}
		// THE DEDUP RECORD GOES DOWN BEFORE A SINGLE FILE IS REMOVED. If it
		// cannot be written, nothing is deleted: the payload stays for the next
		// drain, an unmarked note can only be a duplicate later, and the error
		// is returned to a caller that must still use the notes it was given.
		if err := rememberDrained(sessionDir, notes); err != nil {
			problems = append(problems, err)
			return nil
		}
		for _, file := range readable {
			if err := os.Remove(file); err != nil {
				problems = append(problems, err)
			}
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
// this inbox. A MISSING record is normal and reads as "not drained"; an
// UNREADABLE one is an error and NEVER reads as "not drained" — the cost of a
// duplicate (one line the person has already seen) is the safe direction, but a
// read failure is surfaced rather than hidden behind that safe default.
func alreadyDrained(sessionDir, id string) (bool, error) {
	file, err := os.Open(seenPath(sessionDir))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		if strings.TrimSpace(string(scanner.Bytes())) == id {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, nil
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
	kept, _, err := readSeenIDs(path)
	if err != nil {
		// NEVER REWRITE A RECORD THAT COULD NOT BE READ: doing so would drop
		// every identity already in it, which is dedup state lost.
		return err
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
	return writeAtomic(path, []byte(out.String()))
}

// readSeenIDs reads the drained-identity record. A MISSING file is the normal
// empty record (nil, 0, nil). It answers three things: the identities it could
// read, how many lines could not be an identity at all, and a fatal read error.
//
// A CORRUPT LINE IS COUNTED, NOT SILENTLY DROPPED: a record that came back with
// garbage in it must not read as a clean one, or "nothing was drained" is
// inferred from bytes nobody could use. An unreadable file or a scanner failure
// is a fatal error; the caller must not treat it as an empty record.
func readSeenIDs(path string) ([]string, int, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	defer file.Close()
	var (
		ids []string
		bad int
	)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(string(scanner.Bytes()))
		if line == "" {
			continue
		}
		if !plausibleIdentity(line) {
			bad++
			continue
		}
		ids = append(ids, line)
	}
	if err := scanner.Err(); err != nil {
		return ids, bad, err
	}
	return ids, bad, nil
}

// plausibleIdentity is the weakest shape a drained identity can have: bounded,
// valid UTF-8 and free of NUL. An identity is opaque to this package, but a
// record line that is not even a well-formed string is corruption and is
// reported rather than trusted.
func plausibleIdentity(line string) bool {
	return len(line) <= 1024 && utf8.ValidString(line) && !strings.ContainsRune(line, 0)
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
