package runs

import (
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
)

type Trigger struct {
	App  string `json:"-"`
	ID   string `json:"id"`
	On   string `json:"on"`
	Run  string `json:"run"`
	Cron string `json:"cron"`
	Type string `json:"type"`
	When string `json:"when"`
}

type Change struct {
	Type   string
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
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.saveLocked(nil, key, clock)
	}
	// Jump directly to the next future slot. Creation of the one catch-up
	// run and consumption of all missed slots commit together.
	_, err = e.start(t.App, t.Run, nil, nil, (now.After(prev.Next) && !e.started) || now.Sub(prev.Next) >= time.Minute, key, clock)
	return err
}

// RecordChanged starts matching triggers after the data store commits.
// It does not execute actions on the data store's writer goroutine.
func (e *Engine) RecordChanged(change Change) error {
	if e.cfg.Triggers == nil {
		return nil
	}
	all, err := e.cfg.Triggers()
	for _, t := range all {
		if t.On != "record.changed" || t.Type != change.Type {
			continue
		}
		scoped := change
		if e.cfg.RecordInput != nil {
			var scopeErr error
			scoped, scopeErr = e.cfg.RecordInput(t.App, change)
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
		record["id"], record["type"] = change.ID, change.Type
		input := map[string]any{"record": record, "before": scoped.Before, "after": scoped.After}
		vars := maps.Clone(record)
		delete(vars, "type") // The reserved CEL name is still available through record.type.
		vars["record"], vars["input"], vars["me"] = record, input, "me"
		vars["now"] = e.cfg.Now().UTC().Format(time.RFC3339Nano)
		ok, conditionErr := condition(t.When, vars, scoped.changed)
		if conditionErr != nil {
			err = errors.Join(err, fmt.Errorf("%s trigger %s: %w", t.App, t.ID, conditionErr))
			continue
		}
		if ok {
			_, startErr := e.start(t.App, t.Run, nil, input, false, "", nil)
			err = errors.Join(err, startErr)
		}
	}
	return err
}
