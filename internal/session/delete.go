package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// DeleteConversation removes one stopped conversation and every record it owns.
// The journal lock is held through removal so another window cannot resume it
// between the final ownership check and deletion. Borrowed project files stay put.
func DeleteConversation(dir, id string) error {
	meta, err := deletionMeta(dir, id)
	if err != nil {
		return err
	}
	file, err := os.OpenFile((Place{Dir: dir}).Transcript(), os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err != nil {
		return fmt.Errorf("conversation is still in use: %w", err)
	}
	defer filelock.Unlock(file)
	if _, err := removeTaskIndexRows(TaskIndexPath((Place{Dir: dir}).Transcript()), id, ""); err != nil {
		return err
	}
	var failure error
	reapSession(context.Background(), dir, meta, func(note string) { failure = errors.New(note) })
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		if failure != nil {
			return failure
		}
		return fmt.Errorf("could not remove conversation: %s", dir)
	}
	return nil
}

// deletionMeta requires the selected folder to identify the selected owner.
// A symlink or a missing identity must never turn deletion into a parent-folder
// operation, even when the list was built from stale metadata.
func deletionMeta(dir, id string) (Meta, error) {
	if dir == "" || id == "" || filepath.Base(dir) != id {
		return Meta{}, fmt.Errorf("conversation identity does not match its folder")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return Meta{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Meta{}, fmt.Errorf("conversation is not a session folder")
	}
	meta, err := LoadMeta(dir)
	if err != nil {
		return Meta{}, err
	}
	if meta.ID != id {
		return Meta{}, fmt.Errorf("conversation identity does not match its folder")
	}
	return meta, nil
}

// DeleteTaskRecord removes only one task's saved citation and journal. The
// tombstone prevents a late completion or an in-memory graph from publishing
// the record again. It does not cancel the parent or its other work.
func DeleteTaskRecord(dir, owner, task string) error {
	if task == "" {
		return fmt.Errorf("missing task id")
	}
	if _, err := deletionMeta(dir, owner); err != nil {
		return err
	}
	index := TaskIndexPath((Place{Dir: dir}).Transcript())
	if err := withMetaLock(dir, func() error {
		meta, err := deletionMeta(dir, owner)
		if err != nil {
			return err
		}
		if meta.DeletedTasks == nil {
			meta.DeletedTasks = make(map[string]bool)
		}
		meta.DeletedTasks[task] = true
		delete(meta.ArchivedTasks, task)
		return SaveMeta(dir, meta)
	}); err != nil {
		return err
	}
	journals, err := removeTaskIndexRows(index, owner, task)
	if err != nil {
		return err
	}
	for _, journal := range journals {
		// Only this conversation's task journals belong to the record. A malformed
		// citation cannot authorize removing the parent transcript or project files.
		root := canonicalPath((Place{Dir: dir}).NodeJournals())
		path := canonicalPath(journal)
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

// withTaskIndexLock serializes appends and rewrites across processes. The lock
// uses a stable sidecar because a rewrite replaces the index inode itself.
func withTaskIndexLock(path string, write func() error) error {
	taskIndexMu.Lock()
	defer taskIndexMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	return write()
}

func taskRecordDeleted(index string, entry TaskIndexEntry) bool {
	if entry.SessionID == "" || filepath.Base(entry.SessionID) != entry.SessionID {
		return false
	}
	meta, _ := LoadMeta(filepath.Join(filepath.Dir(index), entry.SessionID))
	return meta.DeletedTasks[entry.ID]
}

// removeTaskIndexRows retains every unrelated line, including unknown older
// records, rather than round-tripping the bounded public index reading.
func removeTaskIndexRows(path, owner, task string) ([]string, error) {
	var journals []string
	err := withTaskIndexLock(path, func() error {
		src, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.CreateTemp(filepath.Dir(path), ".tasks-delete-*")
		if err != nil {
			return err
		}
		defer os.Remove(dst.Name())
		defer dst.Close()
		scanner := bufio.NewScanner(src)
		scanner.Buffer(make([]byte, 4096), 4<<20)
		for scanner.Scan() {
			var entry TaskIndexEntry
			if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.SessionID == owner && (task == "" || entry.ID == task) {
				if journal := TaskRecordPath(entry.TranscriptURI); journal != "" {
					journals = append(journals, journal)
				}
				continue
			}
			if _, err := dst.Write(append(append([]byte(nil), scanner.Bytes()...), '\n')); err != nil {
				return err
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		if err := dst.Close(); err != nil {
			return err
		}
		return os.Rename(dst.Name(), path)
	})
	return journals, err
}

// keepTaskRecords also filters live graph snapshots and older writers' late
// completions. Metadata is read once per owner, not once per task.
func keepTaskRecords(index string, rows []TaskIndexEntry) []TaskIndexEntry {
	byOwner := make(map[string]map[string]bool)
	kept := rows[:0]
	for _, row := range rows {
		deleted, known := byOwner[row.SessionID]
		if !known && row.SessionID != "" && filepath.Base(row.SessionID) == row.SessionID {
			meta, _ := LoadMeta(filepath.Join(filepath.Dir(index), row.SessionID))
			deleted = meta.DeletedTasks
			byOwner[row.SessionID] = deleted
		}
		if !deleted[row.ID] {
			kept = append(kept, row)
		}
	}
	return kept
}
