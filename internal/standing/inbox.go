package standing

// inbox.go is how news reaches a conversation whose window is not open. It is
// deliberately the simplest thing that works: one JSONL file inside the session
// folder, appended by whoever has news, drained whole the next time the person
// opens that conversation and shown under one "while you were away" fold.
//
// THE DRAIN RENAMES BEFORE IT READS, UNDER A PER-INBOX FLOCK, AND NOTHING IS
// SPENT UNTIL THE CALLER ACKNOWLEDGES IT. A note delivered while the fold is
// being built would otherwise be read and then deleted unseen; moving the file
// aside first means a racing delivery starts a fresh inbox that the next open
// finds, and the flock keeps a racing delivery out of the inode the drain
// renamed away.
//
// THE HANDOFF IS A CLAIM, NOT A DELETE. [Drain] reads the staged files and hands
// the caller one [DrainFile] per file; the files stay on disk and the
// drained-identity record stays untouched until [DrainFile.Ack]. A crash between
// the read and the acknowledgement therefore loses nothing: the next drain reads
// the same staged files again and hands the same notes over — a duplicate the
// person might have read, never a note nobody was ever shown. The spend is what
// [DrainFile.Ack] makes durable, and the session lane runs it from the settle
// callback of the durable delivery its fold was journaled as, so the
// acknowledgement follows the caller's own durable receipt rather than a volatile
// queue.
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
	// InboxLockName is the flock file's name inside an inbox folder. It is a
	// dot-name, so no reader that globs the folder's notes ever sees it, and it
	// travels WITH the folder so a symlink alias and a different TEMP directory
	// still resolve to one lock.
	//
	// IT IS EXPORTED BECAUSE IT IS A PERSISTENT RESIDENT OF A SESSION FOLDER. A
	// conversation's own folder is a standing inbox, so the first open of a
	// session leaves this file behind and any caller that asserts on what a
	// session folder holds has to name it rather than guess at it.
	InboxLockName = ".inbox.lock"
)

// errInboxFull is the visible backpressure of a bounded inbox. It is returned
// rather than swallowed, so the caller keeps its durable intent and can say so.
var errInboxFull = errors.New("standing: this inbox is full; the note was not delivered")

// ErrAlreadyDrained reports that a note's identity was already drained from this
// inbox, so nothing was appended now. IT IS NOT A FAILURE: the line is already in
// the person's hands, and the caller that owns a durable intent must treat it as
// settled — but it must NOT offer the line live or steer the model with it
// again, because the person has already read it once. [standingRunner.deliver]
// reads it that way; [Deliver] returns it instead of a silent nil so a live offer
// cannot be made over a line the fold already spent.
var ErrAlreadyDrained = errors.New("standing: this delivery was already drained")

// withInboxLock serializes EVERY mutation of one inbox folder — Deliver, Drain
// and the seen record — under a process flock, so a delivery racing a drain
// cannot write into the inode the drain just renamed away and then lose the
// line to the drain's delete.
//
// THE LOCK IS ADDRESSED BY THE INBOX, NOT BY THE PROCESS. It lives inside the
// inbox folder under a dot-name no reader scans ([InboxLockName]), and the
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
	lockPath := filepath.Join(canonical, InboxLockName)
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
				return ErrAlreadyDrained
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

// DrainFile is one staged inbox file and the acknowledgement that retires it:
// the notes read from it that the caller has NOT already durably received, and
// the file those notes came from.
//
// NOTHING IS SPENT UNTIL [DrainFile.Ack], AND THE CALLER MUST ACK ONLY AFTER ITS
// OWN RECEIPT IS DURABLE. The file stays on disk and the drained-identity record
// stays as it was until then, so a crash between the read and the acknowledgement
// hands the same notes again — a duplicate the person might have read, never a
// note nobody was shown. The session lane's durable receipt is the journal line
// its fold is written as ([durableDelivery] and [sessionFile.recorded]); it acks
// from the settle callback, which fires only after that line reached the file.
type DrainFile struct {
	// Notes are the notes read whole from this file, oldest first, EXCLUDING any
	// identity already in the drained-identity record: an identity there was
	// handed over and acknowledged on an earlier drain, so it is retired without
	// being shown again. A READ FAILURE in this very file yields no DrainFile at
	// all (it is kept and reported instead).
	Notes []Note

	dir  string
	path string
}

// Path is the staged file this acknowledgement retires. It is the identity a
// caller dedups repeated drains on — a second [Drain] before this one is
// acknowledged returns the same path for the same notes.
func (f DrainFile) Path() string { return f.path }

// Ack marks this file's notes as spent and removes the file, under the one
// inbox flock. It is idempotent: a file already gone is already retired.
//
// THE RECORD GOES DOWN BEFORE THE FILE COMES OFF. If the drained-identity record
// cannot be read or written, the file is KEPT and the failure rides out: the next
// drain re-hands the notes rather than losing them. Once the record holds the
// ids, a removal failure only means the file is read again — and its notes are
// skipped because they are spent — so it can never be shown twice or lost.
func (f DrainFile) Ack() error {
	if f.dir == "" || f.path == "" {
		return nil
	}
	return withInboxLock(f.dir, func() error {
		if err := rememberDrained(f.dir, f.Notes); err != nil {
			return err
		}
		if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	})
}

// drainSeen is the drained-identity record read once for one drain. A record
// that cannot be read is reported and treated as EMPTY FOR SKIPPING ONLY: the
// safe direction is to hand a note over again, never to withhold it because a
// record was corrupt.
func drainSeen(sessionDir string) (map[string]bool, []error) {
	var problems []error
	seenIDs, corrupt, err := readSeenIDs(seenPath(sessionDir))
	if err != nil {
		problems = append(problems, fmt.Errorf("standing: the drained-identity record could not be read: %w", err))
		seenIDs = nil
	} else if corrupt > 0 {
		problems = append(problems, fmt.Errorf("standing: the drained-identity record has %d unreadable line(s)", corrupt))
	}
	seen := make(map[string]bool, len(seenIDs))
	for _, id := range seenIDs {
		seen[id] = true
	}
	return seen, problems
}

// Drain reads a session's inbox, oldest first, and answers ONE [DrainFile] per
// staged file the caller must acknowledge. An absent inbox is an empty answer and
// no error.
//
// IT RECOVERS A LEFTOVER .draining FILE DETERMINISTICALLY, even alongside a live
// inbox: a crash after the rename and before the acknowledgement would otherwise
// lose the notes for good. It reads every staged file, and a file it could NOT
// read whole (an open failure, a read failure, or a line too long for the
// scanner) is KEPT and reported rather than handed over or removed.
//
// IT CONSULTS THE DRAINED-IDENTITY RECORD TO SKIP WHAT WAS ALREADY ACKNOWLEDGED.
// The record is written only by [DrainFile.Ack], which a caller runs only after
// its own receipt is durable, so an identity there means the line already reached
// the person through that receipt. Skipping it here is what stops a residual file
// — an acknowledgement whose removal failed — from replaying forever. An
// identity NOT in the record is handed over however long its file has sat.
//
// THE GUARANTEE IS A DURABLE AT-LEAST-ONCE HANDOFF, NOT EXACTLY-ONCE DISPLAY. A
// note acknowledged through [DrainFile.Ack] was handed to the caller AND recorded
// as spent; whether the caller then draws, folds or forwards it is outside this
// package. A crash before the acknowledgement shows the note again. The
// drained-identity window is [inboxSeenKeep]; beyond it a replay is shown again.
//
// A caller MUST use the notes it is handed even when the error is non-nil: the
// error says a sibling file could not be read, not that every note is unusable.
func Drain(sessionDir string) ([]DrainFile, error) {
	var (
		files    []DrainFile
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
		drained, seenProblems := drainSeen(sessionDir)
		problems = append(problems, seenProblems...)
		// READ EVERYTHING BEFORE ANYTHING IS OFFERED OR REMOVED. A file that
		// could not be read whole is kept for the next drain and its failure
		// reported; its readable neighbours are still handed out.
		for _, file := range staged {
			part, readErr := readInbox(file)
			if readErr != nil {
				problems = append(problems, fmt.Errorf("%s: %w", filepath.Base(file), readErr))
				continue
			}
			out := DrainFile{dir: sessionDir, path: file}
			for _, note := range part {
				if note.ID != "" {
					// ONE IDENTITY IS ONE NOTE ACROSS THE WHOLE DRAIN, and one
					// already spent is retired without being shown: a retry can
					// leave the same id staged in two files, and a residual file
					// can hold one already acknowledged.
					if drained[note.ID] {
						continue
					}
					drained[note.ID] = true
				}
				out.Notes = append(out.Notes, note)
			}
			files = append(files, out)
		}
		sort.SliceStable(files, func(a, b int) bool {
			return firstNoteAt(files[a]).Before(firstNoteAt(files[b]))
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, errors.Join(problems...)
}

// firstNoteAt is a staged file's oldest note instant, so a drain hands files back
// in the order the notes arrived. A file whose notes are all spent sorts first;
// it is retired either way.
func firstNoteAt(file DrainFile) time.Time {
	if len(file.Notes) == 0 {
		return time.Time{}
	}
	return file.Notes[0].At
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

// DrainProject reads a project's inbox, oldest first — [Drain] at the project's
// address, answering the same [DrainFile] acknowledgements its caller must run.
func DrainProject(root, workspace string) ([]DrainFile, error) {
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
