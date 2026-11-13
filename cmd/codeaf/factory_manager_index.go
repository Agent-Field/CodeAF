package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// managerIndex answers which job a conversation manages without decoding the
// whole floor on every look.
//
// Every live conversation of a job (its manager and each helper) asks
// [seamRunDoor.AutomaticRefusal] ten times a second for as long as it is open
// (session.watchJobBudget). A full Store.List decodes every job document; on a
// floor of 300 synced issues that is about 58 ms and 17 MB a look, so a job
// with nine open conversations kept six cores busy doing nothing. The index
// keeps what each job document said, keyed on that file's identity, and decodes
// a document again only when its file changed. It is shared by every door on
// the same factory root.
type managerIndex struct {
	mu sync.Mutex
	// dir is the root's identity at the last complete scan; a document write
	// renames a fresh file into the root, which changes it.
	dir     os.FileInfo
	scanned time.Time
	files   map[string]indexedJob
}

// indexedJob is what one job document said when its file looked like file.
type indexedJob struct {
	file    os.FileInfo
	id      int
	created time.Time
	// talk is the manager conversation as one spelling ([sameFileKey]).
	// rawTalk is kept while the conversation's file did not exist yet, so the
	// spelling is worked out again once it does.
	talk     string
	rawTalk  string
	resolved bool
	// stop is [factory.CheckPersonStop]'s refusal for this document.
	stop string
}

// managerRescan bounds how long a scan is trusted when the root itself shows
// no change, so a document rewritten in place is still noticed.
const managerRescan = 2 * time.Second

var managerIndexes sync.Map // factory root -> *managerIndex

func managerIndexOf(st *store.Store) *managerIndex {
	ix, _ := managerIndexes.LoadOrStore(filepath.Clean(st.Root()), &managerIndex{})
	return ix.(*managerIndex)
}

// sameIndexedFile is whether info is still the file a document was read from.
// Every store write renames a fresh file into place, so a write always shows as
// a different file; size and modification time also catch a write in place.
func sameIndexedFile(was, info os.FileInfo) bool {
	return was != nil && info != nil && os.SameFile(was, info) && was.Size() == info.Size() && was.ModTime().Equal(info.ModTime())
}

// managedID is the job whose manager conversation is want (a [sameFileKey]
// spelling): the newest such job, as Store.List's order would find first.
func (ix *managerIndex) managedID(st *store.Store, want string) (int, bool, error) {
	j, ok, err := ix.lookup(st, want)
	return j.id, ok, err
}

// personStop is [factory.CheckPersonStop]'s refusal for the job whose manager
// is want, read from that job's document as it is on disk now.
func (ix *managerIndex) personStop(st *store.Store, want string) (string, bool, error) {
	j, ok, err := ix.lookup(st, want)
	return j.stop, ok, err
}

func (ix *managerIndex) lookup(st *store.Store, want string) (indexedJob, bool, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			// The matched document moved under us; scan the root again.
			ix.dir = nil
		}
		if err := ix.refresh(st); err != nil {
			return indexedJob{}, false, err
		}
		best, found := ix.match(want, false)
		if !found {
			// A conversation recorded before its file existed may spell
			// differently now that it does.
			best, found = ix.match(want, true)
		}
		if !found {
			return indexedJob{}, false, nil
		}
		// The match is answered from its document as it is now, never from an
		// earlier look: a person's Stop must land on the next tick.
		name := strconv.Itoa(best.id) + ".json"
		info, err := os.Stat(filepath.Join(st.Root(), name))
		if err != nil {
			delete(ix.files, name)
			continue
		}
		if sameIndexedFile(best.file, info) {
			return best, true, nil
		}
		j, ok := readIndexedJob(st, best.id, info)
		if !ok {
			delete(ix.files, name)
			continue
		}
		ix.files[name] = j
		if j.talk == want {
			return j, true, nil
		}
	}
	return indexedJob{}, false, nil
}

// match is the newest indexed job whose conversation is want. With resolve,
// spellings that were not final are worked out again first.
func (ix *managerIndex) match(want string, resolve bool) (indexedJob, bool) {
	var best indexedJob
	found := false
	for name, j := range ix.files {
		if resolve && !j.resolved {
			j.talk, j.resolved = managerTalkKey(j.rawTalk)
			ix.files[name] = j
		}
		if j.talk == "" || j.talk != want {
			continue
		}
		if !found || j.created.After(best.created) || j.created.Equal(best.created) && j.id > best.id {
			best, found = j, true
		}
	}
	return best, found
}

// refresh re-reads the documents whose files changed since the last scan. A
// root that shows no change was scanned recently is trusted as it is.
func (ix *managerIndex) refresh(st *store.Store) error {
	root := st.Root()
	dir, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			ix.files, ix.dir = nil, nil
			return nil
		}
		return err
	}
	if ix.files != nil && sameIndexedFile(ix.dir, dir) && time.Since(ix.scanned) < managerRescan {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			ix.files, ix.dir = nil, nil
			return nil
		}
		return err
	}
	next := make(map[string]indexedJob, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		// ONLY A NUMBER NAMES AN ITEM, as Store.List reads the root.
		id, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
		if err != nil {
			continue
		}
		// The file is stat'd before it is read, so a write landing between
		// the two leaves the newer document under the older file's identity,
		// and the next scan reads it again.
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if was, ok := ix.files[name]; ok && sameIndexedFile(was.file, info) {
			next[name] = was
			continue
		}
		// An unreadable document is not remembered, so the next scan tries
		// it again; one torn document never hides the rest of the floor.
		if j, ok := readIndexedJob(st, id, info); ok {
			next[name] = j
		}
	}
	ix.files, ix.dir, ix.scanned = next, dir, time.Now()
	return nil
}

func readIndexedJob(st *store.Store, id int, info os.FileInfo) (indexedJob, bool) {
	it, err := st.GetSaved(id)
	if err != nil {
		return indexedJob{}, false
	}
	j := indexedJob{file: info, id: it.ID, created: it.Created, rawTalk: it.Talk}
	j.talk, j.resolved = managerTalkKey(it.Talk)
	if err := factory.CheckPersonStop(it); err != nil {
		j.stop = err.Error()
	}
	return j, true
}

// managerTalkKey is [sameFileKey] for a job's recorded conversation, and whether that
// spelling is final: a conversation whose file does not exist yet may resolve
// differently once it does (a temporary directory behind a link, say).
func managerTalkKey(talk string) (string, bool) {
	talk = strings.TrimSpace(talk)
	if talk == "" {
		return "", true
	}
	if real, err := filepath.EvalSymlinks(talk); err == nil {
		return filepath.Clean(real), true
	}
	return filepath.Clean(talk), false
}
