package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// Task tombstones are separate from visibility preferences. Their ids never
// return through a late landing, a search, or a recovered checkpoint.
const taskDeletedFile = ".deleted-tasks.json"

func taskDeletions(file string) map[string]bool { ids, _ := readTaskDeletions(file); return ids }

func readTaskDeletions(file string) (map[string]bool, error) {
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), taskDeletedFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids map[string]bool
	if err = json.Unmarshal(raw, &ids); err != nil {
		return nil, err
	}
	return ids, nil
}

func taskDeletionScope(rows []TaskIndexEntry, sessionID, root string) map[string]bool {
	ids := map[string]bool{root: true}
	for changed := true; changed; {
		changed = false
		for _, row := range rows {
			if row.SessionID == sessionID && ids[row.Parent] && !ids[row.ID] {
				ids[row.ID] = true
				changed = true
			}
		}
	}
	return ids
}

func taskIndexDeleted(path string, row TaskIndexEntry) bool {
	if row.SessionID == "" || filepath.Base(row.SessionID) != row.SessionID {
		return false
	}
	file := filepath.Join(filepath.Dir(path), row.SessionID, placeTranscript)
	if _, err := os.Stat(filepath.Join(filepath.Dir(file), conversationDeletedFile)); err == nil {
		return true
	}
	ids := taskDeletions(file)
	return ids[row.ID] || ids[row.Parent]
}

// DeleteTaskUnder validates the saved owner, then asks its live agent to stop
// just this subtree. A different process answers through its presence beat.
func DeleteTaskUnder(root, file, id string, owned func(string, string) (bool, error)) error {
	real, err := taskDeleteOwnerPath(root, file)
	if err != nil {
		return err
	}
	if id == "" {
		return errors.New("choose a task to delete")
	}
	if owned != nil {
		if found, err := owned(real, id); found || err != nil {
			return err
		}
	}
	journal, err := os.OpenFile(real, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer journal.Close()
	if err = filelock.Lock(journal, true, true); err != nil {
		if !filelock.IsBusy(err) {
			return err
		}
		return requestTaskDeletion(real, id)
	}
	defer filelock.Unlock(journal)
	return deleteTaskRecord(real, id, nil)
}

// DeleteTask keeps the conversation and siblings alive. Cancellation settles
// workers before their journals and resumable records are removed.
func (a *Agent) DeleteTask(id string) error {
	return deleteTaskRecord(a.config.SessionFile, id, a)
}

func deleteTaskRecord(file, id string, owner *Agent) (result error) {
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(file), ".task-delete.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	meta, err := LoadMeta(filepath.Dir(file))
	if err != nil {
		return err
	}
	if pending, e := pendingTaskCleanup(file, id); e != nil {
		return e
	} else if pending {
		return finishTaskCleanup(owner, file, meta.ID, id)
	}
	rows, err := taskDeleteRows(file, meta.ID, owner)
	if err != nil {
		return err
	}
	deleted, ids, err := taskDeleteScope(file, meta.ID, id, owner, rows)
	if err != nil {
		return err
	}
	committed := false
	rollback := taskDeleteRollback(owner, ids)
	defer func() {
		if result != nil && !committed {
			rollback()
		}
	}()
	if owner != nil {
		if err = owner.stopDeletingPlanTasks(ids); err != nil {
			return err
		}
		if err = owner.stopDeletingTasks(ids); err != nil {
			return err
		}
	}
	if err = saveTaskCleanup(file, id, ids, rows); err != nil {
		return err
	}
	if err = commitTaskDeletions(file, deleted, ids); err != nil {
		return err
	}
	committed = true
	return finishTaskCleanup(owner, file, meta.ID, id)
}

func (a *Agent) stopDeletingTasks(ids map[string]bool) error {
	g := a.markDeletingTasks(ids)
	for id := range ids {
		n, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			continue
		}
		if node := a.taskNode(n); node != nil {
			if _, err = g.stopFor(n, "deleted by the person"); err != nil {
				return err
			}
		} else {
			if _, _, err = a.stopBeltRow(n, "deleted by the person"); err != nil {
				return err
			}
		}
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for id := range ids {
		n, _ := strconv.ParseUint(id, 10, 64)
		node := a.taskNode(n)
		if node == nil {
			continue
		}
		var done <-chan struct{}
		func() { g.mu.Lock(); defer g.mu.Unlock(); done = node.done }()
		if done != nil {
			select {
			case <-done:
			case <-deadline.C:
				return errors.New("the task's work could not finish stopping; try again")
			}
		}
	}
	return nil
}

func (a *Agent) dropDeletedTasks(ids map[string]bool) error {
	g := a.tasker()
	if g == nil {
		return nil
	}
	func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		kept := g.order[:0]
		for _, id := range g.order {
			if ids[strconv.FormatUint(id, 10)] || g.deleted[id] {
				delete(g.nodes, id)
			} else {
				kept = append(kept, id)
			}
		}
		g.order = kept
	}()
	return g.store.write(g)
}

func purgeTaskRecords(file, sessionID string, ids map[string]bool, rows []TaskIndexEntry, checkpointWrite bool) error {
	if err := purgeTaskIndex(file, sessionID, ids); err != nil {
		return err
	}
	if err := purgeTaskCheckpoint(file, ids, checkpointWrite); err != nil {
		return err
	}
	for _, r := range rows {
		if r.SessionID != sessionID || ids != nil && !ids[r.ID] {
			continue
		}
		journal := TaskRecordPath(r.TranscriptURI)
		if journal == "" || filepath.Clean(journal) == filepath.Clean(file) {
			continue
		}
		if taskJournalShared(r, sessionID, ids, rows) {
			continue
		}
		if err := purgeTaskJournal(file, journal); err != nil {
			return err
		}

	}
	return nil
}

func filterDeletedDocument(doc taskDocument, ids map[string]bool) taskDocument {
	rows := []TaskIndexEntry{}
	for _, r := range doc.Nodes {
		rows = append(rows, TaskIndexEntry{ID: strconv.FormatUint(r.ID, 10), Parent: taskIndexParent(r.Parent)})
	}
	for _, r := range doc.Runs {
		rows = append(rows, TaskIndexEntry{ID: strconv.FormatUint(r.ID, 10), Parent: taskIndexParent(r.Parent)})
	}
	for root := range ids {
		if ids[root] {
			for id := range taskDeletionScope(rows, "", root) {
				ids[id] = true
			}
		}
	}
	nodes := doc.Nodes[:0:0]
	for _, r := range doc.Nodes {
		if !ids[strconv.FormatUint(r.ID, 10)] {
			deps := r.DependsOn[:0:0]
			for _, d := range r.DependsOn {
				if !ids[strconv.FormatUint(d, 10)] {
					deps = append(deps, d)
				}
			}
			r.DependsOn = deps
			nodes = append(nodes, r)
		}
	}
	doc.Nodes = nodes
	runs := doc.Runs[:0:0]
	for _, r := range doc.Runs {
		if !ids[strconv.FormatUint(r.ID, 10)] {
			runs = append(runs, r)
		}
	}
	doc.Runs = runs
	return doc
}

func withTaskIndexLock(path string, write func() error) error {
	taskIndexMu.Lock()
	defer taskIndexMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = filelock.Lock(lock, true, false); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	return write()
}

type taskDeleteRequest struct {
	ID    string
	At    time.Time
	Token string
	Error string
}

func requestTaskDeletion(file, id string) error {
	req := taskDeleteRequest{ID: id, At: time.Now(), Token: strconv.FormatInt(time.Now().UnixNano(), 10)}
	raw, _ := json.Marshal(req)
	dir := filepath.Dir(file)
	pending := filepath.Join(dir, ".task-delete-request-"+req.Token)
	running := filepath.Join(dir, ".task-delete-running-"+req.Token)
	result := filepath.Join(dir, ".task-delete-result-"+req.Token)
	if err := os.WriteFile(pending, raw, 0600); err != nil {
		return err
	}
	defer os.Remove(pending)
	defer os.Remove(result)
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	claimed := false
	for {
		select {
		case <-timer.C:
			if !claimed {
				err := os.Remove(pending)
				if err == nil {
					return errors.New("the task's owner did not answer deletion; try again")
				}
				if _, err = os.Stat(running); err == nil {
					claimed = true
					timer.Reset(60 * time.Second)
					continue
				}
			}
			return errors.New("deletion has not answered yet; refresh before trying again")
		case <-tick.C:
			raw, err := os.ReadFile(result)
			if err != nil {
				continue
			}
			var answer taskDeleteRequest
			if json.Unmarshal(raw, &answer) != nil || answer.Token != req.Token {
				continue
			}
			if answer.Error != "" {
				return errors.New(answer.Error)
			}
			return nil
		}
	}
}

func (a *Agent) drainTaskDeletion() {
	if a.config.InTask || a.config.Place.Dir == "" {
		return
	}
	paths, _ := filepath.Glob(filepath.Join(a.config.Place.Dir, ".task-delete-request-*"))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req taskDeleteRequest
		if json.Unmarshal(raw, &req) != nil || time.Since(req.At) > 30*time.Second || req.At.After(time.Now()) || strings.ContainsAny(req.Token, "/\\") || req.Token == "" || filepath.Base(path) != ".task-delete-request-"+req.Token {
			_ = os.Remove(path)
			continue
		}
		running := filepath.Join(a.config.Place.Dir, ".task-delete-running-"+req.Token)
		if os.Rename(path, running) != nil {
			continue
		}
		guard.Go("deleting a task subtree", func() {
			defer os.Remove(running)
			if err := a.DeleteTask(req.ID); err != nil {
				req.Error = err.Error()
			}
			raw, _ := json.Marshal(req)
			_ = os.WriteFile(filepath.Join(a.config.Place.Dir, ".task-delete-result-"+req.Token), raw, 0600)
		})
	}
}

// The index reader loads each owner's tombstones once, then expands every
// deleted root over the full record before applying the display cap.
func filterDeletedTaskIndex(path string, rows []TaskIndexEntry) []TaskIndexEntry {
	deletions := map[string]map[string]bool{}
	gone := map[string]bool{}
	for _, row := range rows {
		if row.SessionID == "" || filepath.Base(row.SessionID) != row.SessionID {
			continue
		}
		if _, seen := deletions[row.SessionID]; seen {
			continue
		}
		file := filepath.Join(filepath.Dir(path), row.SessionID, placeTranscript)
		ids := taskDeletions(file)
		if _, err := os.Stat(filepath.Join(filepath.Dir(file), conversationDeletedFile)); err == nil {
			gone[row.SessionID] = true
		}
		for root := range ids {
			if ids[root] {
				for id := range taskDeletionScope(rows, row.SessionID, root) {
					ids[id] = true
				}
			}
		}
		deletions[row.SessionID] = ids
	}
	kept := rows[:0:0]
	for _, row := range rows {
		if !gone[row.SessionID] && !deletions[row.SessionID][row.ID] {
			kept = append(kept, row)
		}
	}
	return kept
}

// The same canonical owner boundary applies before either live or saved deletion.
func taskDeleteOwnerPath(root, file string) (string, error) {
	real, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", err
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.Base(real) != placeTranscript {
		return "", errors.New("that task is outside this machine's saved conversations")
	}
	meta, err := LoadMeta(filepath.Dir(real))
	if err != nil || meta.ID == "" || meta.ID != filepath.Base(filepath.Dir(real)) {
		return "", errors.New("that task has no saved conversation")
	}
	return real, nil
}

func taskDeleteRows(file, chat string, owner *Agent) ([]TaskIndexEntry, error) {
	rows := readTaskIndexRows(TaskIndexPath(file), 0, false)
	plans, err := taskDeletePlanRows(file, chat)
	if err != nil {
		return nil, err
	}
	rows = append(rows, plans...)
	if doc, ok := loadTaskCheckpoint(taskCheckpointPath(file)); ok {
		for _, r := range doc.Nodes {
			rows = append(rows, TaskIndexEntry{ID: strconv.FormatUint(r.ID, 10), Parent: taskIndexParent(r.Parent), SessionID: chat, Title: r.Title, TranscriptURI: taskURI(r.Journal)})
		}
		for _, r := range doc.Runs {
			rows = append(rows, TaskIndexEntry{ID: strconv.FormatUint(r.ID, 10), Parent: taskIndexParent(r.Parent), SessionID: chat, Title: r.Title})
		}
	}
	if owner != nil {
		rows = append(rows, owner.liveTaskRows()...)
		if g := owner.tasker(); g != nil {
			notices := func() []TaskNotice {
				g.mu.Lock()
				defer g.mu.Unlock()
				return append([]TaskNotice(nil), g.runRowsLocked()...)
			}()
			for _, r := range notices {
				rows = append(rows, TaskIndexEntry{ID: strconv.FormatUint(r.ID, 10), Parent: taskIndexParent(r.Parent), SessionID: chat, Title: r.Title})
			}
		}
	}
	return rows, nil
}

func taskDeleteRollback(owner *Agent, ids map[string]bool) func() {
	if owner == nil {
		return func() {}
	}
	graph := owner.tasker()
	if graph == nil {
		return func() {}
	}
	previous := map[uint64]bool{}
	func() {
		graph.mu.Lock()
		defer graph.mu.Unlock()
		for id := range ids {
			n, err := strconv.ParseUint(id, 10, 64)
			if err == nil {
				previous[n] = graph.deleted[n]
			}
		}
	}()
	return func() {
		graph.mu.Lock()
		defer graph.mu.Unlock()
		for id, was := range previous {
			if !was {
				delete(graph.deleted, id)
			}
		}
	}
}

func commitTaskDeletions(file string, deleted, ids map[string]bool) error {
	if deleted == nil {
		deleted = map[string]bool{}
	}
	for key := range ids {
		deleted[key] = true
	}
	raw, err := json.Marshal(deleted)
	if err != nil {
		return err
	}
	marker := filepath.Join(filepath.Dir(file), taskDeletedFile)
	if err = os.WriteFile(marker+".tmp", raw, 0600); err != nil {
		return err
	}
	if err = os.Rename(marker+".tmp", marker); err != nil {
		return err
	}
	return nil
}

func purgeTaskIndex(file, sessionID string, ids map[string]bool) error {
	path := TaskIndexPath(file)
	err := withTaskIndexLock(path, func() error {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var keep []string
		for _, line := range strings.Split(string(raw), "\n") {
			if line == "" {
				continue
			}
			var row TaskIndexEntry
			if json.Unmarshal([]byte(line), &row) == nil && row.SessionID == sessionID && (ids == nil || ids[row.ID]) {
				continue
			}
			keep = append(keep, line)
		}
		temp, err := os.CreateTemp(filepath.Dir(path), ".task-index-delete-")
		if err != nil {
			return err
		}
		name := temp.Name()
		defer os.Remove(name)
		_, err = temp.WriteString(strings.Join(keep, "\n") + "\n")
		if closeErr := temp.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		return os.Rename(name, path)
	})
	if err != nil {
		return err
	}
	return nil
}

func purgeTaskCheckpoint(file string, ids map[string]bool, checkpointWrite bool) error {
	var err error
	checkpoint := taskCheckpointPath(file)
	if ids == nil {
		if err = os.Remove(checkpoint); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if doc, ok := loadTaskCheckpoint(checkpoint); ok && checkpointWrite {
		doc = filterDeletedDocument(doc, ids)
		raw, _ := json.Marshal(doc)
		if err = os.WriteFile(checkpoint+".delete-tmp", raw, 0600); err != nil {
			return err
		}
		if err = os.Rename(checkpoint+".delete-tmp", checkpoint); err != nil {
			return err
		}
	}
	return nil
}

func purgeTaskJournal(file, journal string) error {
	real, e := filepath.EvalSymlinks(journal)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	allowed := false
	for _, root := range []string{filepath.Dir(file), filepath.Join(LooseTasksRoot(), filepath.Base(filepath.Dir(file)))} {
		base, e := filepath.EvalSymlinks(root)
		if e != nil {
			continue
		}
		rel, e := filepath.Rel(base, real)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			allowed = true
		}
	}
	if allowed {
		if e := os.Remove(real); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return nil
}

func taskJournalShared(r TaskIndexEntry, sessionID string, ids map[string]bool, rows []TaskIndexEntry) bool {
	for _, other := range rows {
		if (other.SessionID != sessionID || ids != nil && !ids[other.ID]) && other.TranscriptURI == r.TranscriptURI {
			return true
		}
	}
	return false
}

func taskDeleteScope(file, chat, id string, owner *Agent, rows []TaskIndexEntry) (map[string]bool, map[string]bool, error) {
	found := false
	for _, r := range rows {
		if r.SessionID == chat && r.ID == id {
			found = true
		}
	}
	deleted, err := readTaskDeletions(file)
	if err != nil {
		return nil, nil, err
	}
	if !found && !deleted[id] {
		return nil, nil, errors.New("this task is no longer available")
	}
	ids := taskDeletionScope(rows, chat, id)
	expandTaskDeleteScope(file, chat, owner, ids, rows)
	return deleted, ids, nil
}

func expandTaskDeleteScope(file, chat string, owner *Agent, ids map[string]bool, rows []TaskIndexEntry) {
	for before := -1; before != len(ids); {
		before = len(ids)
		taskDeletePlanAliases(file, ids)
		taskDeleteLiveAliases(owner, ids)
		for root := range ids {
			for child := range taskDeletionScope(rows, chat, root) {
				ids[child] = true
			}
		}
	}
}

func (a *Agent) markDeletingTasks(ids map[string]bool) *TaskGraph {
	g := a.tasker()
	if g != nil {
		func() {
			g.mu.Lock()
			defer g.mu.Unlock()
			if g.deleted == nil {
				g.deleted = map[uint64]bool{}
			}
			for id := range ids {
				n, err := strconv.ParseUint(id, 10, 64)
				if err == nil && n != 0 {
					g.deleted[n] = true
				}
			}
		}()
	}
	return g
}

func finishTaskDeletion(owner *Agent, file, chat string, ids map[string]bool, rows []TaskIndexEntry) error {
	var err error
	if owner != nil {
		if err = owner.dropDeletedTasks(ids); err != nil {
			return err
		}
	}
	if err = purgeDeletedPlanTasks(file, chat, ids); err != nil {
		return err
	}
	if err = purgeTaskRecords(file, chat, ids, rows, owner == nil); err != nil {
		return err
	}
	return nil
}
