package plandb

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"unicode"
)

// TreeDepthLimit and TreeTaskLimit mirror session's taskDepthLimit (three
// levels counting the root) and taskFanLimit. These stop runaway growth.
const (
	TreeDepthLimit = 2
	TreeTaskLimit  = 20
)

func normalizedTitle(title string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

// GrowthRefusal answers why worker-created tasks cannot join this run, or
// nothing when they fit. Internal review and person-approved writes bypass it.
func (s *Store) GrowthRefusal(parentID string, titles []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed.Error()
	}
	tx, err := s.rdb.Begin()
	if err != nil {
		return err.Error()
	}
	defer tx.Rollback()
	plan, err := loadState(tx)
	if err != nil {
		return err.Error()
	}
	plan, err = growthPlan(plan, tx)
	if err != nil {
		return err.Error()
	}
	return growthRefusal(plan, parentID, titles)
}

// Archiving finished work does not give a worker another slot or title.
func growthPlan(plan state, q rowQuerier) (state, error) {
	archived, err := loadArchived(q)
	if err != nil {
		return plan, err
	}
	if len(archived) == 0 {
		return plan, nil
	}
	plan.Tasks = maps.Clone(plan.Tasks)
	plan.Order = append([]string(nil), plan.Order...)
	for _, task := range archived {
		plan.Tasks[task.ID] = task
		plan.Order = append(plan.Order, task.ID)
	}
	return plan, nil
}

func growthRefusal(plan state, parentID string, titles []string) string {
	if parentID == "" {
		parentID = plan.RootID
	}
	named := make(map[string]*Task, len(plan.Tasks))
	count := 0
	for _, id := range plan.Order {
		task := plan.Tasks[id]
		if task == nil || task.Status == StatusCancelled {
			continue
		}
		named[normalizedTitle(task.Title)] = task
		if id != plan.RootID && (task.Role != RoleCheck || !task.ReviewCheck) {
			count++
		}
	}
	batch := make(map[string]bool, len(titles))
	for _, title := range titles {
		key := normalizedTitle(title)
		if task := named[key]; task != nil {
			return fmt.Sprintf("no: this run already has a task named %q (%s), so nothing was added. If that work is yours, do it in your own hands; if it is different work, name it for what is different.", task.Title, cliID(task.ID))
		}
		if batch[key] {
			return fmt.Sprintf("no: this batch names the same work %q more than once, so nothing was added. Do that work in your own hands, or name each different part for what is different.", title)
		}
		batch[key] = true
	}
	depth := 0
	for id := parentID; id != plan.RootID; depth++ {
		parent := plan.Tasks[id]
		if parent == nil || depth > len(plan.Tasks) {
			return fmt.Sprintf("no: %s is not a task under this run's root, so nothing was added.", cliID(parentID))
		}
		id = parent.ParentID
	}
	if depth >= TreeDepthLimit && len(titles) > 0 {
		return fmt.Sprintf("no: %s is already %d levels below this run's root task, which is as deep as tasks go here, so nothing was added. Do this work in your own hands.", cliID(parentID), TreeDepthLimit)
	}
	if count+len(titles) > TreeTaskLimit {
		return fmt.Sprintf("no: this run already holds %d tasks and this would exceed %d tasks, which is as many as one run may hand out, so nothing was added. Do the rest in your own hands, or finish and report what is left undone.", count, TreeTaskLimit)
	}
	return ""
}

// AddManyBounded applies GrowthRefusal inside the same write transaction as
// AddMany. Separate workers cannot race a read of the last available slot.
func (s *Store) AddManyBounded(specs []TaskSpec) ([]*Task, error) {
	return s.addMany(specs, true)
}

func boundedGrowthRefusal(plan state, q rowQuerier, specs []TaskSpec) error {
	plan, err := growthPlan(plan, q)
	if err != nil {
		return err
	}
	titles := make([]string, len(specs))
	for i, spec := range specs {
		titles[i] = spec.Title
	}
	// CLI batches share one parent. Check each parent as well so this atomic
	// door cannot admit an over-deep task through a mixed-parent batch.
	for _, spec := range specs {
		if refusal := growthRefusal(plan, spec.ParentID, titles); refusal != "" {
			return errors.New(refusal)
		}
	}
	return nil
}

func cliAddMany(st *Store, specs []TaskSpec) ([]*Task, error) {
	if cliRunBound() {
		return st.AddManyBounded(specs)
	}
	return st.AddMany(specs)
}
