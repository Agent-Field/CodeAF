package session

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Which task failures a person has looked at. The fact is shared by every
// window and device on this machine's state root, so it rides the conversation's
// own meta.json ([Meta.FailuresSeen]) under the same lock every other fact about
// the folder uses, and the world feed projects it; no surface keeps a copy of
// its own that could disagree.
//
// A FAILURE'S IDENTITY IS (task id, landing instant). The index row carries both
// and neither changes after the task lands, so a mark made against one failure
// can never cover another: a failure that lands later is past the watermark and
// unseen again with nobody doing anything.

// ErrNoSuchFailure says no failed task of this conversation landed at the
// instant named — the client's reading is stale, or the instant was invented.
// Nothing is written.
var ErrNoSuchFailure = errors.New("that failure is no longer on record")

// failureLanded reports a failed task that has a landing instant. A failed row
// without one carries no version to compare, so it can be neither marked nor
// counted unseen; rebuilt or very old rows are the only ones that lack it.
func failureLanded(e TaskIndexEntry) bool {
	return e.Status == string(TaskFailed) && !e.Live() && !e.EndedAt.IsZero()
}

// UnseenFailures counts landed-failed tasks that arrived after the watermark.
func (r SessionRow) UnseenFailures() int {
	n := 0
	for _, e := range r.Tasks.Rows {
		if failureLanded(e) && e.EndedAt.After(r.FailuresSeen) {
			n++
		}
	}
	return n
}

// NewestFailure is the conversation's current failure: the latest-landed failed
// task, or false when none has a landing instant.
func (r SessionRow) NewestFailure() (TaskIndexEntry, bool) {
	var newest TaskIndexEntry
	found := false
	for _, e := range r.Tasks.Rows {
		if failureLanded(e) && (!found || e.EndedAt.After(newest.EndedAt)) {
			newest, found = e, true
		}
	}
	return newest, found
}

// FailureSeenResult is what MarkFailureSeen settled on.
type FailureSeenResult struct {
	Through time.Time
	Changed bool
	Unseen  int
}

// MarkFailureSeen records that the failure which landed at `at` (and every one
// before it) has been looked at. `dir` is the conversation folder.
//
// IT IS IDEMPOTENT AND MONOTONIC: a repeat, or an older failure, changes
// nothing and reports Changed false. `at` must be the landing instant of a real
// failed task of this conversation; anything else is [ErrNoSuchFailure], so a
// caller can only mark what it was shown and never a failure still to come.
// A non-empty `task` must also be that failure's task id.
func MarkFailureSeen(dir, sessionID, task string, at time.Time) (FailureSeenResult, error) {
	sessionID, task = strings.TrimSpace(sessionID), strings.TrimSpace(task)
	if sessionID == "" || at.IsZero() {
		return FailureSeenResult{}, fmt.Errorf("mark failure seen: missing conversation or instant")
	}
	var result FailureSeenResult
	err := withMetaLock(dir, func() error {
		meta, err := LoadMeta(dir)
		if err != nil {
			return err
		}
		if meta.ID != sessionID {
			return fmt.Errorf("mark failure seen: no conversation %s at %s", sessionID, dir)
		}
		var mine []TaskIndexEntry
		for _, e := range ReadTaskIndex(filepath.Join(filepath.Dir(dir), taskIndexName)) {
			if strings.TrimSpace(e.SessionID) == sessionID {
				mine = append(mine, e)
			}
		}
		matched := false
		for _, e := range mine {
			if failureLanded(e) && e.EndedAt.Equal(at) && (task == "" || e.ID == task) {
				matched = true
				break
			}
		}
		if !matched {
			return ErrNoSuchFailure
		}
		through := meta.FailuresSeen
		if at.After(through) {
			through = at
			meta.FailuresSeen = at.UTC()
			if err := SaveMeta(dir, meta); err != nil {
				return err
			}
			result.Changed = true
		}
		result.Through = through
		row := SessionRow{Tasks: TaskRollup{Rows: mine}, FailuresSeen: through}
		result.Unseen = row.UnseenFailures()
		return nil
	})
	return result, err
}
