package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/cell"
)

type Trigger struct {
	Since time.Time `json:"-"`
	App   string    `json:"-"`
	ID    string    `json:"id"`
	On    string    `json:"on"`
	Run   string    `json:"run"`
	Cron  string    `json:"cron"`
	Type  string    `json:"type"`
	When  string    `json:"when"`
}

type Change struct {
	Seq    int64
	Type   string
	TypeID string
	Batch  string
	ID     string
	Before map[string]any
	After  map[string]any
}

type scheduleClock struct {
	Cron string    `json:"cron"`
	Run  string    `json:"run"`
	Next time.Time `json:"next"`
}

func (e *Engine) schedules() error {
	if e.cfg.Triggers == nil {
		return nil
	}
	all, err := e.cfg.Triggers()
	now := e.cfg.Now()
	for _, t := range all {
		if t.On == "schedule" {
			if fireErr := e.fire(t, now); fireErr != nil {
				err = errors.Join(err, fmt.Errorf("%s schedule %s: %w", t.App, t.ID, fireErr))
			}
		}
	}
	return err
}

func (e *Engine) fire(t Trigger, now time.Time) error {
	if !validID(t.App) || !validID(t.ID) {
		return errors.New("valid app and trigger ids are required")
	}
	key := "schedule/" + t.App + "/" + t.ID
	e.mu.Lock()
	raw := e.checkpoints[key]
	e.mu.Unlock()
	var prev scheduleClock
	if len(raw) > 0 {
		var err error
		prev, err = decode[scheduleClock](raw)
		if err != nil {
			return err
		}
	}
	schedule := automation.Schedule{Every: t.Cron}
	fresh := prev.Cron != t.Cron || prev.Run != t.Run || prev.Next.IsZero()
	if !fresh && now.Before(prev.Next) {
		return nil
	}
	next, err := schedule.Next(now)
	if err != nil {
		return err
	}
	if next.IsZero() || schedule.Interval() != 0 {
		return errors.New("a schedule needs a five-field cron expression")
	}
	clock := scheduleClock{Cron: t.Cron, Run: t.Run, Next: next}
	if fresh {
		prev.Next = time.Time{}
		// A saved definition can survive a quit before its first clock
		// scan. Its save time establishes the first scheduled slot.
		if !t.Since.IsZero() && t.Since.Before(now) {
			prev.Next, err = schedule.Next(t.Since)
			if err != nil {
				return err
			}
		}
		if prev.Next.IsZero() || now.Before(prev.Next) {
			e.mu.Lock()
			defer e.mu.Unlock()
			return e.saveLocked(nil, key, clock)
		}
	}
	// Jump directly to the next future slot. Creation of the one catch-up
	// run and consumption of all missed slots commit together.
	_, err = e.start(t.App, t.Run, nil, nil, prev.Next.Before(e.openedAt), key, clock)
	return err
}

// RecordChanged starts matching triggers after the data store commits.
// It does not execute actions on the data store's writer goroutine.
func (e *Engine) RecordChanged(change Change) error {
	return e.RecordChanges([]Change{change})
}

// RecordChanges loads trigger definitions once for a whole committed batch.
func (e *Engine) RecordChanges(changes []Change) error {
	if e.cfg.Triggers == nil || len(changes) == 0 {
		return nil
	}
	all, err := e.cfg.Triggers()
	for _, change := range changes {
		if changeErr := e.recordChanged(change, all); changeErr != nil {
			err = errors.Join(err, changeErr)
		}
	}
	return err
}

func (e *Engine) recordChanged(change Change, all []Trigger) error {
	var err error
	for _, t := range all {
		if t.On != "record.changed" || (change.TypeID == "" || e.cfg.RecordInput == nil) && t.Type != change.Type {
			continue
		}
		receipt := t.App + "/" + t.ID
		if change.Seq > 0 {
			claimed, claimErr := e.journal.AppRunClaimed(change.Seq, receipt)
			if claimErr != nil {
				err = errors.Join(err, claimErr)
				continue
			}
			if claimed {
				continue
			}
		}
		scoped := change
		if e.cfg.RecordInput != nil {
			var scopeErr error
			scoped.Type = t.Type
			scoped, scopeErr = e.cfg.RecordInput(t.App, scoped)
			if errors.Is(scopeErr, ErrNotFound) {
				continue
			}
			if scopeErr != nil {
				err = errors.Join(err, scopeErr)
				continue
			}
		}
		record := maps.Clone(scoped.After)
		if record == nil {
			record = make(map[string]any)
		}
		record["id"], record["type"] = change.ID, scoped.Type
		input := map[string]any{"record": record, "before": scoped.Before, "after": scoped.After}
		vars := maps.Clone(record)
		for field := range scoped.Before {
			if _, present := vars[field]; !present {
				vars[field] = nil
			}
		}
		vars["record"], vars["input"], vars["me"] = record, input, "me"
		vars["now"] = e.cfg.Now().UTC().Format(time.RFC3339Nano)
		e.addRecordReader(t.App, vars)
		ok, conditionErr := condition(t.When, vars, scoped.changed)
		if conditionErr != nil {
			err = errors.Join(err, e.failTriggered(t, change.Seq, receipt, "trigger condition could not be evaluated: "+conditionErr.Error()))
			continue
		}
		if ok {
			startErr := e.startTriggered(t, change.Seq, receipt, input)
			err = errors.Join(err, startErr)
		}
	}
	return err
}

// startTriggered commits the run before publishing it to workers or clients.
func (e *Engine) startTriggered(t Trigger, seq int64, receipt string, input map[string]any) error {
	if seq == 0 {
		_, err := e.start(t.App, t.Run, nil, input, false, "", nil)
		return err
	}
	c, err := e.newCheckpoint(t.App, t.Run, nil, input, false)
	if err != nil {
		return e.failTriggered(t, seq, receipt, "trigger run could not be prepared: "+err.Error())
	}
	return e.publishTriggered(seq, receipt, c)
}

// Invalid definitions and conditions leave a visible receipt instead of
// blocking later events. Journal errors still hold the cursor for a retry.
func (e *Engine) failTriggered(t Trigger, seq int64, receipt, reason string) error {
	if seq == 0 {
		return fmt.Errorf("%s trigger %s: %s", t.App, t.ID, reason)
	}
	now := e.cfg.Now()
	id, err := cell.NewID(now)
	if err != nil {
		return err
	}
	return e.publishTriggered(seq, receipt, checkpoint{Run: Run{
		App: t.App, DefinitionID: t.Run, ID: id, State: Incomplete, Error: reason, StartedAt: now, EndedAt: now,
	}})
}

func (e *Engine) publishTriggered(seq int64, receipt string, c checkpoint) error {
	c.UpdatedAt = e.cfg.Now()
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	claimed, err := e.commitTriggered(seq, receipt, c.Run, raw)
	if claimed {
		e.changed(c.Run)
	}
	return err
}

func (e *Engine) commitTriggered(seq int64, receipt string, run Run, raw json.RawMessage) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return false, errors.New("app runs are closed")
	}
	key := runPrefix + run.ID
	claimed, err := e.journal.ClaimAppRun(seq, receipt, key, raw)
	if claimed {
		e.checkpoints[key] = raw
		e.track(run)
	}
	return claimed, err
}
