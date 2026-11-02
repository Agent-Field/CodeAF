package taskdoor

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// MaxFinishedAttempts bounds durable replay protection after copy cleanup.
const MaxFinishedAttempts = 1000

func (s *attemptStore) rememberFinished(id string) error {
	next := make(map[string]time.Time, len(s.finished)+1)
	for key, at := range s.finished {
		next[key] = at
	}
	next[id] = s.clock.Now()
	if len(next) > MaxFinishedAttempts {
		ids := make([]string, 0, len(next))
		for key := range next {
			if key != id {
				ids = append(ids, key)
			}
		}
		sort.Slice(ids, func(i, j int) bool {
			if next[ids[i]].Equal(next[ids[j]]) {
				return ids[i] < ids[j]
			}
			return next[ids[i]].Before(next[ids[j]])
		})
		for _, key := range ids[:len(next)-MaxFinishedAttempts] {
			delete(next, key)
		}
	}
	if err := atomicJSON(filepath.Join(s.root, "finished.json"), next); err != nil {
		return err
	}
	// The tombstone's directory entry must survive before the bulky attempt is removed.
	directory, err := os.Open(s.root)
	if err != nil {
		return err
	}
	if err = errors.Join(directory.Sync(), directory.Close()); err != nil {
		return err
	}
	s.finished = next
	return nil
}
