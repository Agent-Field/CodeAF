package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── WHICH JOB IS THIS CHAT'S, AND IS IT STOPPED ─────────────────
//
// Every open job chat asks this ten times a second for as long as it is open
// (session.watchJobBudget), so reading the floor to answer it kept an idle
// engine busy in proportion to chats times jobs. [Store.TalkStop] answers from
// what it last read and proves nothing changed from the files themselves.
//
// THE PROOF. Every store write renames a fresh file into this folder. That
// changes the folder's time, and gives the document a new file identity, in
// this process or any other. So:
//   - The folder unchanged, and last changed long enough before the last sweep
//     that a coarse clock cannot hide a later write, means no document changed.
//   - Otherwise, a sweep compares each document's file with the one last read,
//     and reads only those that differ.
//
// Both are looked at BEFORE reading, so a write landing during a sweep leaves
// the older identity beside the newer content and the next look reads it again.
//
// THIS ANSWERS A WATCHER, NOT A GATE. [factory.CheckPersonStop] on the locked
// job still decides any grant of work; a watcher only notices a Stop sooner.

const (
	// folderTimeSlack is how recently the folder may have changed and still be
	// swept: wider than the coarsest file time on a supported disk.
	folderTimeSlack = 2 * time.Second
	// talkSweepEvery bounds how long a quiet folder goes unswept. A sweep resolves
	// each job's chat path again: a link retargeted under it changes no document.
	talkSweepEvery = time.Second
)

type talkDoc struct {
	file    os.FileInfo
	id      int
	created time.Time
	talk    string // the job's Talk as written
	key     string // talk as [FileKey], resolved again at every sweep
	stopped bool
}

type talkIndex struct {
	mu     sync.Mutex
	dir    os.FileInfo
	swept  time.Time
	retry  bool // a document could not be read, and is owed another try
	docs   map[string]talkDoc
	owners []talkDoc // the docs with a chat, newest first as [Store.List] orders
}

// TalkStop answers for the conversation with this [FileKey]: whether a job names
// it as its chat, and whether a person has stopped that job. It agrees with
// finding the first match in [Store.List], reading no document that has not
// changed since the last call on this store.
func (st *Store) TalkStop(key string) (manager, stopped bool, err error) {
	ix := &st.talks
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err := st.refreshTalks(ix); err != nil {
		return false, false, err
	}
	for _, doc := range ix.owners {
		if doc.key == key {
			return true, doc.stopped, nil
		}
	}
	return false, false, nil
}

func (st *Store) refreshTalks(ix *talkIndex) error {
	now := time.Now()
	dir, err := os.Stat(st.root)
	if err != nil {
		if os.IsNotExist(err) {
			*ix = talkIndex{}
			return nil
		}
		return err
	}
	if ix.quiet(dir, now) {
		return nil
	}
	entries, err := os.ReadDir(st.root)
	if err != nil {
		if os.IsNotExist(err) {
			*ix = talkIndex{}
			return nil
		}
		return err
	}
	docs := make(map[string]talkDoc, len(entries))
	retry := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
		if err != nil {
			continue
		}
		path := filepath.Join(st.root, name)
		info, err := os.Stat(path)
		if err != nil {
			retry = retry || !os.IsNotExist(err)
			continue
		}
		if last, ok := ix.docs[name]; ok && sameDocument(last.file, info) {
			last.key = FileKey(last.talk)
			docs[name] = last
			continue
		}
		d, err := st.read(path)
		switch {
		case err == nil:
			docs[name] = talkDoc{file: info, id: id, created: d.Item.Created, talk: d.Item.Talk, key: FileKey(d.Item.Talk), stopped: factory.StoppedByPerson(d.Item)}
		case errors.Is(err, ErrNewer):
			// Skipped until the file changes: another read could not answer differently.
			docs[name] = talkDoc{file: info, id: id}
		case !errors.Is(err, ErrNotFound):
			retry = true
		}
	}
	owners := make([]talkDoc, 0, 8)
	for _, doc := range docs {
		if doc.key != "" {
			owners = append(owners, doc)
		}
	}
	sort.Slice(owners, func(a, b int) bool {
		if owners[a].created.Equal(owners[b].created) {
			return owners[a].id > owners[b].id
		}
		return owners[a].created.After(owners[b].created)
	})
	ix.dir, ix.swept, ix.retry, ix.docs, ix.owners = dir, now, retry, docs, owners
	return nil
}

// quiet is whether the last sweep still stands, with dir the folder as it is now.
func (ix *talkIndex) quiet(dir os.FileInfo, now time.Time) bool {
	return ix.docs != nil && !ix.retry &&
		now.Sub(ix.swept) < talkSweepEvery &&
		os.SameFile(ix.dir, dir) && ix.dir.ModTime().Equal(dir.ModTime()) &&
		ix.swept.Sub(dir.ModTime()) >= folderTimeSlack
}

// sameDocument is whether info is still the file a document was read from.
func sameDocument(read, info os.FileInfo) bool {
	return os.SameFile(read, info) && read.Size() == info.Size() && read.ModTime().Equal(info.ModTime())
}

// FileKey is a path as one spelling, links resolved; empty for no path.
func FileKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return filepath.Clean(path)
}
