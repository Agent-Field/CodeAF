package main

import (
	"os"
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// factoryStopCheckDecodes counts the job documents the stop check decoded.
var factoryStopCheckDecodes atomic.Int64

// AutomaticRefusal reads the current durable job for every automatic wake.
// A person opening or reading the conversation does not change that job.
//
// IT DECODES NOTHING THAT HAS NOT CHANGED. Every job's manager asks this ten
// times a second while it is open (session's watchJobBudget), and each ask used
// to list and decode every job document on the floor: the third live run's
// engine idled at 145–188% of a core. The answer is remembered with the file
// it was read from — the job's own document, or the floor's folder when the
// conversation manages no job — and read again only once that file changed.
// Every store write renames a fresh file into place, so a Stop is seen at the
// next ask. PERF.md's "Idle factory manager checks" states the cost.
func (d seamRunDoor) AutomaticRefusal(conversation string) string {
	key := sameFileKey(conversation)
	if key == "" || d.st == nil {
		return ""
	}
	key = d.st.Root() + "\x00" + key
	if why, ok := stopChecks.answer(key, d); ok {
		return why
	}
	// The files are stat'd before they are read, so a write landing between
	// the two leaves the newer document under the older file's identity and
	// the next ask reads it again rather than keeping a stale answer. A job
	// found by listing the floor is remembered without its file, so the next
	// ask reads it once more, by id, the same safe way.
	floor, _ := os.Stat(d.st.Root())
	hint := stopChecks.job(key)
	var job os.FileInfo
	if hint != 0 {
		job, _ = os.Stat(d.st.ItemPath(hint))
	}
	it, err := d.managedFrom(conversation, hint)
	if err == factory.ErrNotManager {
		stopChecks.keep(key, stopCheck{floor: floor})
		return ""
	}
	if err != nil {
		stopChecks.forget(key)
		return "job could not be read; automatic work stopped"
	}
	why := ""
	if err := factory.CheckPersonStop(it); err != nil {
		why = err.Error()
	}
	if it.ID != hint {
		job = nil
	}
	stopChecks.keep(key, stopCheck{id: it.ID, file: job, refusal: why})
	return why
}

// stopCheck is one conversation's last answer and the file it came from: the
// job's document (id), or, for a conversation that manages no job, the
// floor's folder, whose identity changes whenever any job is written.
type stopCheck struct {
	id      int
	file    os.FileInfo
	floor   os.FileInfo
	refusal string
}

type stopCheckMemo struct {
	mu sync.Mutex
	by map[string]stopCheck
}

var stopChecks = &stopCheckMemo{by: map[string]stopCheck{}}

// answer is the remembered answer for key while its file is unchanged.
func (m *stopCheckMemo) answer(key string, d seamRunDoor) (string, bool) {
	m.mu.Lock()
	last, ok := m.by[key]
	m.mu.Unlock()
	if !ok {
		return "", false
	}
	if last.id == 0 {
		info, err := os.Stat(d.st.Root())
		return "", err == nil && sameFileInfo(last.floor, info)
	}
	info, err := os.Stat(d.st.ItemPath(last.id))
	if err != nil || !sameFileInfo(last.file, info) {
		return "", false
	}
	return last.refusal, true
}

// job is the job key's conversation was last found managing, 0 for none.
func (m *stopCheckMemo) job(key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.by[key].id
}

func (m *stopCheckMemo) keep(key string, c stopCheck) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.by[key] = c
}

func (m *stopCheckMemo) forget(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.by, key)
}

// sameFileInfo is whether now is still the file then was read from. Every
// store write renames a fresh file into place, so a write always shows as a
// different file; size and modification time also catch a write in place.
func sameFileInfo(then, now os.FileInfo) bool {
	return then != nil && now != nil && os.SameFile(then, now) && then.Size() == now.Size() && then.ModTime().Equal(now.ModTime())
}
