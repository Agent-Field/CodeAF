package plandb

import "errors"

// DeleteSubtree removes records and references in one transaction. Tombstones
// keep a stale worker from admitting the same task again, including a run root.
func (s *Store) DeleteSubtree(id string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.beginWrite()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	fresh, err := loadState(tx)
	if err != nil {
		return nil, err
	}
	id = bareTaskID(id)
	archived, err := loadArchived(tx)
	if err != nil {
		return nil, err
	}
	all := map[string]*Task{}
	order := append([]string(nil), fresh.Order...)
	for key, t := range fresh.Tasks {
		all[key] = t
	}
	for _, t := range archived {
		all[t.ID] = t
		order = append(order, t.ID)
	}
	if all[id] == nil {
		return nil, errors.New("this task is no longer available")
	}
	doomed := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for key, t := range all {
			if doomed[t.ParentID] && !doomed[key] {
				doomed[key] = true
				changed = true
			}
		}
	}
	if fresh.Deleted == nil {
		fresh.Deleted = map[string]bool{}
	}
	var ids []string
	for _, key := range order {
		if doomed[key] {
			ids = append(ids, key)
			delete(fresh.Tasks, key)
			fresh.Deleted[key] = true
		}
	}
	// Hard prerequisites remain gates even when their records are removed.
	// Advice is not a gate and must not cancel otherwise independent work.
	for id := range doomed {
		cancelBlockedDependents(&fresh, id, "dependency was deleted", s.now().UTC())
	}
	for _, t := range fresh.Tasks {
		kept := t.Dependencies[:0:0]
		for _, d := range t.Dependencies {
			if !doomed[d.TaskID] {
				kept = append(kept, d)
			} else if d.Kind != DepSuggests && !terminal(t.Status) {
				t.Status = StatusCancelled
				t.Error = "dependency was deleted"
				t.ClaimedBy = ""
				t.Owner = ""
				t.CompletedAt = s.now().UTC()
			}
		}
		t.Dependencies = kept
	}
	fresh.Order = keepIDs(fresh.Order, doomed)
	fresh.Notes = keepNotes(fresh.Notes, doomed)
	fresh.Contexts = keepContexts(fresh.Contexts, doomed)
	recomputeComposite(&fresh)
	for _, key := range ids {
		for _, table := range []string{"live", "spend"} {
			if _, err = tx.Exec("DELETE FROM "+table+" WHERE task_id = ?", key); err != nil {
				return nil, err
			}
		}
		if _, err = tx.Exec("DELETE FROM archived_tasks WHERE id = ?", key); err != nil {
			return nil, err
		}
	}
	if err = saveState(tx, fresh); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.data = fresh
	return ids, nil
}
