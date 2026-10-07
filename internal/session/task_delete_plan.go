package session

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// Plan containment is read from the store, never from the display's dependency
// grouping. Numeric graph rows and their plan identities describe the same work.
func taskDeletePlanRows(file, chat string) ([]TaskIndexEntry, error) {
	path := filepath.Join(filepath.Dir(file), planStoreFilename)
	paths := append(planArchivePaths(path), path)
	var rows []TaskIndexEntry
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		store, err := plandb.Open(path, "", "", "", "")
		if err != nil {
			return nil, err
		}
		tasks, err := taskDeleteStoreRows(store, chat)
		if err != nil {
			store.Close()
			return nil, err
		}
		for _, t := range tasks {
			parent := ""
			if t.ParentID != "" {
				parent = planStoreID(t.ParentID)
			}
			rows = append(rows, TaskIndexEntry{ID: planStoreID(t.ID), Parent: parent, SessionID: chat, Title: t.Title})
		}
		if err = store.Close(); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func taskDeletePlanAliases(file string, ids map[string]bool) {
	doc, ok := loadTaskCheckpoint(taskCheckpointPath(file))
	if !ok {
		return
	}
	for changed := true; changed; {
		changed = false
		link := func(number uint64, plan string) {
			if plan == "" {
				return
			}
			n := strconv.FormatUint(number, 10)
			p := planStoreID(strings.TrimPrefix(plan, "t-"))
			if ids[n] && !ids[p] {
				ids[p] = true
				changed = true
			}
			if ids[p] && !ids[n] {
				ids[n] = true
				changed = true
			}
		}
		for _, r := range doc.Nodes {
			link(r.ID, r.PlanID)
		}
		for _, r := range doc.Runs {
			link(r.ID, r.PlanTask)
		}
	}
}

// Cancelling a plan task stops its workers; the lifetime callback acknowledges
// their last record write. A root waits for the run's own landing as well.
func (a *Agent) stopDeletingPlanTasks(ids map[string]bool) error {
	var run *beltRun
	func() { a.beltMu.Lock(); defer a.beltMu.Unlock(); run = a.beltRun }()
	if run == nil {
		return nil
	}
	root := ids[planStoreID(run.root)] || ids[strconv.FormatUint(run.row, 10)]
	if err := a.cancelDeletingPlanTasks(run, ids, root); err != nil {
		return err
	}
	rows := []TaskIndexEntry{}
	for _, t := range run.store.Tasks() {
		parent := ""
		if t.ParentID != "" {
			parent = planStoreID(t.ParentID)
		}
		rows = append(rows, TaskIndexEntry{ID: planStoreID(t.ID), Parent: parent})
	}
	expandTaskDeleteScope(a.config.SessionFile, "", a, ids, rows)
	var wait []<-chan struct{}
	func() {
		a.beltMu.Lock()
		defer a.beltMu.Unlock()
		if root && run.over != nil {
			wait = append(wait, run.over)
		} else {
			for id, done := range run.workers {
				if ids[planStoreID(id)] {
					wait = append(wait, done)
				}
			}
		}
	}()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for _, done := range wait {
		select {
		case <-done:
		case <-timer.C:
			return errors.New("the task's workers are still stopping; try again")
		}
	}
	return nil
}

func purgeDeletedPlanTasks(file, chat string, ids map[string]bool) error {
	if err := validateDeletionPaths(file, chat, ids, nil); err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(file), planStoreFilename)
	paths := append(planArchivePaths(path), path)
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		store, err := plandb.Open(path, "", "", "", "")
		if err != nil {
			return err
		}
		tasks, err := taskDeleteStoreRows(store, chat)
		if err != nil {
			store.Close()
			return err
		}
		for _, task := range tasks {
			if ids != nil && !ids[planStoreID(task.ID)] {
				continue
			}
			if task.ParentID != "" && (ids == nil || ids[planStoreID(task.ParentID)]) {
				continue
			}
			removed, e := store.DeleteSubtree(task.ID)
			if e != nil {
				store.Close()
				return e
			}
			if e = purgePlanTaskFolders(file, removed); e != nil {
				store.Close()
				return e
			}

		}
		if err = store.Close(); err != nil {
			return err
		}
	}
	return purgeSelectedPlanFolders(file, ids)
}

func (a *Agent) cancelDeletingPlanTasks(run *beltRun, ids map[string]bool, root bool) error {
	if root {
		if _, _, err := a.stopBeltRow(run.row, "deleted by the person"); err != nil {
			return err
		}
	} else {
		for id := range ids {
			if !strings.HasPrefix(id, "t-") {
				continue
			}
			key := strings.TrimPrefix(id, "t-")
			t := run.store.Task(key)
			if t != nil && !terminalStoreStatus(t.Status) {
				if _, err := run.store.Cancel(key, taskStoppedWord); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func purgePlanTaskFolders(file string, removed []string) error {
	for _, id := range removed {
		dir := plandb.TaskDir(filepath.Dir(file), id)
		// A symlinked records directory must never widen the deletion boundary.
		real, e := filepath.EvalSymlinks(dir)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		base, e := filepath.EvalSymlinks(filepath.Join(filepath.Dir(file), "tasks"))
		if e != nil {
			return e
		}
		if filepath.Clean(base) != filepath.Join(filepath.Dir(file), "tasks") || real != dir || filepath.Dir(real) != base {
			return errors.New("task records are outside the conversation")
		}
		if e = os.RemoveAll(real); e != nil {
			return e
		}
	}
	return nil
}

func taskDeleteStoreRows(store *plandb.Store, chat string) ([]*plandb.Task, error) {
	tasks := store.Tasks(plandb.Filter{Chat: chat})
	archived, err := store.Archived()
	if err != nil {
		return nil, err
	}
	for _, t := range archived {
		if t.Chat == chat {
			tasks = append(tasks, t)
		}
	}
	return tasks, nil
}

func taskDeleteLiveAliases(owner *Agent, ids map[string]bool) {
	if owner == nil {
		return
	}
	g := owner.tasker()
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	link := func(n uint64, p string) {
		if p == "" {
			return
		}
		id := strconv.FormatUint(n, 10)
		plan := planStoreID(strings.TrimPrefix(p, "t-"))
		if ids[id] || ids[plan] {
			ids[id], ids[plan] = true, true
		}
	}
	for _, n := range g.nodes {
		link(n.id, n.spec.planID)
	}
	for _, r := range g.runRowsLocked() {
		link(r.ID, r.PlanTask)
	}
}

func purgeConversationTaskDirectory(file string) error {
	dir := filepath.Join(filepath.Dir(file), "tasks")
	real, err := filepath.EvalSymlinks(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if real != dir {
		return errors.New("task records are outside the conversation")
	}
	return os.RemoveAll(dir)
}

func purgeSelectedPlanFolders(file string, ids map[string]bool) error {
	if ids != nil {
		var records []string
		for id := range ids {
			if strings.HasPrefix(id, "t-") {
				records = append(records, strings.TrimPrefix(id, "t-"))
			}
		}
		return purgePlanTaskFolders(file, records)
	}
	return purgeConversationTaskDirectory(file)
}

// Validate every owned deletion path before a tombstone or store mutation. A
// sibling reached through a symlink is not the record the person selected.
func validateDeletionPaths(file, chat string, ids map[string]bool, rows []TaskIndexEntry) error {
	root := filepath.Join(filepath.Dir(file), "tasks")
	real, err := filepath.EvalSymlinks(root)
	if err == nil && real != root {
		return errors.New("task records are outside the conversation")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if ids != nil && !ids[planStoreID(entry.Name())] {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return err
		}
		if real != dir {
			return errors.New("task records are outside the conversation")
		}
	}
	for _, row := range rows {
		if row.SessionID != chat || ids != nil && !ids[row.ID] || taskJournalShared(row, chat, ids, rows) {
			continue
		}
		journal := TaskRecordPath(row.TranscriptURI)
		if journal == "" || journal == file {
			continue
		}
		if err := validateTaskJournal(file, journal); err != nil {
			return err
		}
	}
	return nil
}

func validateTaskJournal(file, journal string) error {
	real, err := filepath.EvalSymlinks(journal)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, root := range []string{filepath.Dir(file), filepath.Join(LooseTasksRoot(), filepath.Base(filepath.Dir(file)))} {
		base, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		rel, e := filepath.Rel(root, journal)
		lexical := e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
		realRel, e := filepath.Rel(base, real)
		owned := e == nil && realRel != ".." && !strings.HasPrefix(realRel, ".."+string(filepath.Separator))
		if lexical || owned {
			if !lexical || real != filepath.Join(base, rel) {
				return errors.New("task journal points to another record")
			}
		}
	}
	return nil
}
