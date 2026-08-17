package main

// chatv3_migrate.go folds the flat layout into the folder one (docs/CHAT-V3.md,
// Decision 26's last law).
//
// ONE BOOT PASS, ONE-WAY, NEVER FATAL. A person who upgrades in the middle of a
// week of work must find their conversations where the new build looks for
// them, and must not be able to lose one to this pass. So every failure here is
// a file LEFT WHERE IT IS plus one line on the log: a corrupt old session must
// not cost anybody their new one, and a session another window is holding open
// is not touched at all.
//
// What moves, per flat transcript under ~/.aforge/v3/sessions/<workspace>/:
//
//	<stamp>_<rand>.jsonl        → projects/<workspace>/<id>/transcript.jsonl
//	<stamp>_<rand>.state.json   → projects/<workspace>/<id>/state.json
//	<stamp>_<rand>.tasks.json   → projects/<workspace>/<id>/tasks.json
//	~/.aforge/v3/tasks/<id>/    → projects/<workspace>/<id>/tasks/
//	<workspace>/tasks.jsonl     → projects/<workspace>/tasks.jsonl
//
// The id is the transcript header's own, which is what makes the folder's name
// the session's name from the first day of the new layout. A file whose header
// cannot be read has no name to be given and stays put.

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// migrateOnce is the "boot" in "one boot pass". Two doors can open a v3 launch
// in one process — the terminal and the engine — and the second of them has
// nothing left to do.
var migrateOnce sync.Once

// migrateV3Layout runs the pass, at most once per process, and returns nothing:
// there is no outcome a caller could act on. A machine that never ran the flat
// layout pays one failed stat for it.
func migrateV3Layout() { migrateOnce.Do(migrateV3Tree) }

// migrateV3Tree is the pass itself, without the once — which is what lets a
// test run it against a state root of its own.
func migrateV3Tree() {
	old := home.Join("v3", "sessions")
	info, err := os.Stat(old)
	if err != nil || !info.IsDir() {
		return
	}
	buckets, err := os.ReadDir(old)
	if err != nil {
		log.Printf("aforge: leaving the old session directory in place: %v", err)
		return
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		migrateV3Bucket(filepath.Join(old, bucket.Name()), bucket.Name())
	}
	// The old tree goes only when it is empty, which is the whole record of
	// whether anything was left behind: a directory that still holds a file is
	// a file this pass decided not to move.
	_ = os.Remove(old)
	_ = os.Remove(home.Join("v3", "tasks"))
}

// migrateV3Bucket folds one encoded-workspace directory. The bucket's NAME is
// carried across unchanged: it is the same encoding on both sides, and decoding
// it would be guessing at which dashes were separators.
func migrateV3Bucket(old, name string) {
	entries, err := os.ReadDir(old)
	if err != nil {
		log.Printf("aforge: leaving %s in place: %v", old, err)
		return
	}
	fresh := home.Join("v3", "projects", name)
	if err := os.MkdirAll(fresh, 0o700); err != nil {
		log.Printf("aforge: leaving %s in place: %v", old, err)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch {
		case entry.Name() == v3TaskIndexName:
			// The project's task index is the bucket's, and it is the bucket's on
			// the other side too — one level up from the session folders, which is
			// where internal/session's task_index.go now looks for it.
			migrateV3Move(filepath.Join(old, entry.Name()), filepath.Join(fresh, entry.Name()))
		case strings.HasSuffix(entry.Name(), ".jsonl"):
			migrateV3Session(filepath.Join(old, entry.Name()), fresh)
		}
	}
	// Whatever is left — an unreadable transcript, its sidecars — stays, and so
	// does the directory holding it.
	_ = os.Remove(old)
}

// v3TaskIndexName is the project's index, named here because this pass MOVES
// one where internal/session only ever writes one.
const v3TaskIndexName = "tasks.jsonl"

// migrateV3Session folds one flat transcript and everything derived from it.
func migrateV3Session(transcript, bucket string) {
	// A LIVE SESSION IS NOT TOUCHED. Another window is holding this file open
	// and writing to it; moving it out from under that process would cost
	// somebody the conversation they are in the middle of. It stays flat, and
	// the next launch after that window closes folds it.
	if session.InUse(transcript) {
		return
	}
	header, ok := migrateV3Header(transcript)
	if !ok {
		log.Printf("aforge: %s has no readable session header; leaving it in the old layout", transcript)
		return
	}
	dir := filepath.Join(bucket, header.ID)
	if _, err := os.Stat(dir); err == nil {
		log.Printf("aforge: %s is already folded into %s; leaving the old file in place", transcript, dir)
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("aforge: leaving %s in the old layout: %v", transcript, err)
		return
	}
	// READ BEFORE MOVE: the summary is what fills the picker's row, and it is
	// cheaper to take it while the path is still the one on hand.
	summary, spoken := session.Peek(transcript)

	place := session.Place{Dir: dir}
	if !migrateV3Move(transcript, place.Transcript()) {
		// Nothing else follows a transcript that would not move: the folder is
		// left empty rather than half-filled, which is a folder the launch groom
		// removes on its own.
		return
	}
	stem := strings.TrimSuffix(transcript, filepath.Ext(transcript))
	migrateV3Move(stem+".state.json", place.State())
	migrateV3Move(stem+".tasks.json", place.Tasks())
	// The node journals lived in a parallel tree keyed by session id. A node's
	// transcript now sits beside the conversation that commissioned it.
	migrateV3Move(home.Join("v3", "tasks", header.ID), place.NodeJournals())

	meta := session.Meta{
		ID: header.ID,
		// The header's cwd is the only record of where the session ran: the
		// encoded bucket name cannot be decoded back into a path, because
		// nothing in it says which dashes were separators. A header that
		// recorded none leaves the field empty, which is a blank column in a
		// picker and never a session that will not open.
		Workspace: strings.TrimSpace(header.Cwd),
		Model:     strings.TrimSpace(header.Model),
	}
	if at, err := time.Parse(time.RFC3339Nano, header.Timestamp); err == nil {
		meta.Created = at
	}
	if spoken {
		meta.Title = summary.Title
		meta.LastUserAt = summary.At
	}
	if err := session.SaveMeta(dir, meta); err != nil {
		// The folder is already correct; only its citation is missing, and the
		// session's next turn writes one (internal/session's placemeta.go).
		log.Printf("aforge: %s moved but its meta.json could not be written: %v", place.Transcript(), err)
	}
}

// migrateV3Header reads the one line this pass needs: the session's own id, and
// what the header remembers about where it ran.
//
// It scans forward until it finds a header rather than reading the first line,
// because a file whose opening lines were half-written by a crash is exactly
// the file this must not give up on — but it stops at a bounded number of
// lines, because a file with no header in its first few thousand is not a
// transcript.
func migrateV3Header(path string) (migrateHeader, bool) {
	file, err := os.Open(path)
	if err != nil {
		return migrateHeader{}, false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for read := 0; read < migrateHeaderLines && scanner.Scan(); read++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var header migrateHeader
		if json.Unmarshal([]byte(line), &header) != nil {
			continue
		}
		if header.Type != "session" || strings.TrimSpace(header.ID) == "" {
			continue
		}
		header.ID = strings.TrimSpace(header.ID)
		return header, true
	}
	return migrateHeader{}, false
}

// migrateHeaderLines bounds the search for a header.
const migrateHeaderLines = 4096

// migrateHeader is the header as this pass reads it — the same fields
// internal/session's sessionHeader writes, and only the ones a folder needs.
type migrateHeader struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	Model     string `json:"model"`
	Timestamp string `json:"timestamp"`
}

// migrateV3Move renames one path into the new layout and reports whether the
// thing is now there. A source that does not exist is not a failure — most
// sessions have no sidecars and no node journals — and a destination that
// already exists is left alone, because whatever is already in the new layout
// was put there by a build that knew more about it than this pass does.
func migrateV3Move(from, to string) bool {
	if _, err := os.Stat(from); err != nil {
		return false
	}
	if _, err := os.Stat(to); err == nil {
		log.Printf("aforge: %s is already in place; leaving %s where it is", to, from)
		return false
	} else if !errors.Is(err, fs.ErrNotExist) {
		log.Printf("aforge: leaving %s where it is: %v", from, err)
		return false
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		log.Printf("aforge: leaving %s where it is: %v", from, err)
		return false
	}
	if err := os.Rename(from, to); err != nil {
		log.Printf("aforge: leaving %s where it is: %v", from, err)
		return false
	}
	return true
}
