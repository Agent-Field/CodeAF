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

// TaskRecordTree returns the selected task and every descendant in one owner.
// The walk uses parent identities, never display order or titles.
func TaskRecordTree(rows []TaskIndexEntry, owner, task string) []TaskIndexEntry {
	latest := make(map[string]TaskIndexEntry)
	for _, row := range rows {
		if row.SessionID == owner {
			latest[row.ID] = row
		}
	}
	wanted := map[string]bool{task: true}
	for changed := true; changed; {
		changed = false
		for _, row := range latest {
			if !wanted[row.ID] && (row.Parent != "" && wanted[row.Parent] || row.PlanID != "" && wanted[row.PlanID]) {
				wanted[row.ID], changed = true, true
			}
			if wanted[row.ID] && row.PlanID != "" && !wanted[row.PlanID] {
				wanted[row.PlanID], changed = true, true
			}
		}
	}
	var tree []TaskIndexEntry
	if root, ok := latest[task]; ok {
		tree = append(tree, root)
	}
	for id, row := range latest {
		if id != task && wanted[id] {
			tree = append(tree, row)
		}
	}
	return tree
}

// TaskRecordActive includes waiting work that can still start or resume. An
// unknown unfinished state is not permission to permanently remove a record.
func TaskRecordActive(row TaskIndexEntry) bool {
	switch row.Status {
	case string(TaskDone), string(TaskFailed), string(TaskUnverified), string(TaskInterrupted), "cancelled", "canceled":
		return false
	default:
		return true
	}
}

// DeleteTaskRecord is the single-root entry point; descendants belong to it.
func DeleteTaskRecord(dir, owner, task string) error {
	_, err := DeleteTaskTree(dir, owner, task, nil)
	return err
}

// DeleteTaskTree checks the whole subtree before publishing tombstones and
// removing its saved records. Index writers share this lock, so a late append
// cannot slip between the activity check and removal. Live rows supplement
// tasks that have not yet written an index record, including plan rows.
func DeleteTaskTree(dir, owner, task string, live []TaskIndexEntry) ([]string, error) {
	if task == "" {
		return nil, fmt.Errorf("missing task id")
	}
	if _, err := deletionMeta(dir, owner); err != nil {
		return nil, err
	}
	index := TaskIndexPath((Place{Dir: dir}).Transcript())
	var ids, journals []string
	err := withTaskIndexLock(index, func() error {
		rows, err := taskDeletionRows(index)
		if err != nil {
			return err
		}
		// A disk record that became active after the live reading must still
		// veto deletion. Neither reading can erase the other's active work.
		tree := TaskRecordTree(append(append([]TaskIndexEntry(nil), rows...), live...), owner, task)
		if len(tree) == 0 {
			return fmt.Errorf("task record is no longer available")
		}
		wanted := make(map[string]bool)
		for _, row := range tree {
			wanted[row.ID] = true
			if row.PlanID != "" {
				wanted[row.PlanID] = true
			}
			if TaskRecordActive(row) {
				return fmt.Errorf("task or its subtasks are still active; stop them first")
			}
		}
		for _, row := range rows {
			if row.SessionID == owner && wanted[row.ID] && TaskRecordActive(row) {
				return fmt.Errorf("task or its subtasks are still active; stop them first")
			}
		}
		if err := withMetaLock(dir, func() error {
			meta, err := deletionMeta(dir, owner)
			if err != nil {
				return err
			}
			if meta.DeletedTasks == nil {
				meta.DeletedTasks = make(map[string]bool)
			}
			for id := range wanted {
				meta.DeletedTasks[id] = true
				delete(meta.ArchivedTasks, id)
				ids = append(ids, id)
			}
			return SaveMeta(dir, meta)
		}); err != nil {
			return err
		}
		journals, err = removeTaskIndexRowsLocked(index, owner, wanted)
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, journal := range journals {
		// A citation never authorizes deleting a parent transcript or project file.
		root := canonicalPath((Place{Dir: dir}).NodeJournals())
		path := canonicalPath(journal)
		rel, err := filepath.Rel(root, path)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return ids, err
			}
		}
	}
	return ids, nil
}

// taskDeletionRows reads every latest record, without the search index's cap.
// An unreadable index must refuse deletion rather than hide an active child.
func taskDeletionRows(path string) ([]TaskIndexEntry, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	latest := make(map[string]TaskIndexEntry)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var row TaskIndexEntry
		if json.Unmarshal(scanner.Bytes(), &row) == nil {
			latest[row.SessionID+"/"+row.ID] = row
		}
	}
	var rows []TaskIndexEntry
	for _, row := range latest {
		rows = append(rows, row)
	}
	return rows, scanner.Err()
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
	return meta.DeletedTasks[entry.ID] || meta.DeletedTasks[entry.Parent] || meta.DeletedTasks[entry.PlanID]
}

// removeTaskIndexRows retains every unrelated line, including unknown older
// records, rather than round-tripping the bounded public index reading.
func removeTaskIndexRows(path, owner, task string) ([]string, error) {
	var tasks map[string]bool
	if task != "" {
		tasks = map[string]bool{task: true}
	}
	var journals []string
	err := withTaskIndexLock(path, func() error {
		var err error
		journals, err = removeTaskIndexRowsLocked(path, owner, tasks)
		return err
	})
	return journals, err
}

func removeTaskIndexRowsLocked(path, owner string, tasks map[string]bool) ([]string, error) {
	var journals []string
	src, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer src.Close()
	dst, err := os.CreateTemp(filepath.Dir(path), ".tasks-delete-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(dst.Name())
	defer dst.Close()
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var entry TaskIndexEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.SessionID == owner && (tasks == nil || tasks[entry.ID]) {
			if journal := TaskRecordPath(entry.TranscriptURI); journal != "" {
				journals = append(journals, journal)
			}
			continue
		}
		if _, err := dst.Write(append(append([]byte(nil), scanner.Bytes()...), '\n')); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := dst.Close(); err != nil {
		return nil, err
	}
	return journals, os.Rename(dst.Name(), path)
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
		if !deleted[row.ID] && !deleted[row.Parent] && !deleted[row.PlanID] {
			kept = append(kept, row)
		}
	}
	return kept
}
