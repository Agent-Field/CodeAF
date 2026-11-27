package runs

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"time"

	"context"
	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/cell"
)

type Trigger struct {
	Since     time.Time      `json:"-"`
	App       string         `json:"-"`
	ID        string         `json:"id"`
	On        string         `json:"on"`
	Run       string         `json:"run"`
	Cron      string         `json:"cron"`
	Type      string         `json:"type"`
	At        string         `json:"at"`
	Lead      string         `json:"lead"`
	When      string         `json:"when"`
	Connector string         `json:"connector,omitempty"`
	Event     string         `json:"event,omitempty"`
	Match     map[string]any `json:"match,omitempty"`
	fieldFill bool
}

type Change struct {
	Seq           int64
	Type          string
	TypeID        string
	Batch         string
	ID            string
	OutboxVersion int
	CauseChain    []string
	SourceEvent   string
	Intake        *IntakeChange
	Before        map[string]any
	After         map[string]any
	Cause         string
}

// IntakeChange carries a channel's explicit run target from the data outbox.
type IntakeChange struct {
	App, Channel, Run, RecordType string
}

// Keep a bound on stored provenance when runs trigger other runs.
const maxRunCauseChain = 64

// triggerReceipt preserves the event provenance in the same checkpoint that
// owns the unique sequence and trigger claim. Run expressions see only the
// separately scoped record input.
type triggerReceipt struct {
	Seq           int64    `json:"seq"`
	TriggerID     string   `json:"trigger_id"`
	TypeID        string   `json:"type_id,omitempty"`
	Batch         string   `json:"batch,omitempty"`
	Record        string   `json:"record,omitempty"`
	OutboxVersion int      `json:"outbox_version,omitempty"`
	CauseChain    []string `json:"cause_chain,omitempty"`
	SourceEvent   string   `json:"source_event,omitempty"`
	IntakeType    string   `json:"intake_type,omitempty"`
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
		if t.On == "record.before" {
			if fireErr := e.fireBefore(t, now); fireErr != nil {
				err = errors.Join(err, fmt.Errorf("%s before trigger %s: %w", t.App, t.ID, fireErr))
			}
		}
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
	if len(changes) == 0 {
		return nil
	}
	var all []Trigger
	var err error
	if e.cfg.Triggers != nil {
		all, err = e.cfg.Triggers()
	}
	for _, change := range changes {
		if changeErr := e.recordChanged(change, all); changeErr != nil {
			err = errors.Join(err, changeErr)
		}
	}
	return err
}

func (e *Engine) recordChanged(change Change, all []Trigger) error {
	var err error
	if route := change.Intake; route != nil && route.Run != "" {
		target := Trigger{App: route.App, ID: "channel/" + route.Channel, On: "record.changed", Run: route.Run, Type: route.RecordType}
		err = e.matchRecordTriggers(change, []Trigger{target}, true)
	}
	return errors.Join(err, e.matchRecordTriggers(change, all, false))
}

func (e *Engine) matchRecordTriggers(change Change, all []Trigger, intake bool) error {
	var err error
	for _, t := range all {
		if t.On != "record.changed" || (change.TypeID == "" || e.cfg.RecordInput == nil) && t.Type != change.Type {
			continue
		}
		receipt := triggerReceipt{
			Seq: change.Seq, TriggerID: t.App + "/" + t.ID, TypeID: change.TypeID, Batch: change.Batch,
			Record: change.ID, OutboxVersion: change.OutboxVersion, SourceEvent: change.SourceEvent,
			CauseChain: change.CauseChain[max(0, len(change.CauseChain)-maxRunCauseChain):],
		}
		if intake {
			receipt.IntakeType = t.Type
		}
		if receipt.Seq > 0 {
			claimed, claimErr := e.journal.AppRunClaimed(receipt.Seq, receipt.TriggerID)
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
		if t.fieldFill && (scoped.After == nil || change.Cause == aiFieldStep || scoped.After["_rev"] == nil) {
			continue
		}
		record := maps.Clone(scoped.After)
		if record == nil {
			record = make(map[string]any)
		}
		record["id"], record["type"] = change.ID, scoped.Type
		input := map[string]any{"record": record, "before": scoped.Before, "after": scoped.After}
		if intake {
			input["channel"] = change.Intake.Channel
		}
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
			err = errors.Join(err, e.failTriggered(t, receipt, "trigger condition could not be evaluated: "+conditionErr.Error()))
			continue
		}
		if ok {
			startErr := e.startTriggered(t, receipt, input)
			err = errors.Join(err, startErr)
		}
	}
	return err
}

// startTriggered commits the run before publishing it to workers or clients.
func (e *Engine) startTriggered(t Trigger, receipt triggerReceipt, input map[string]any) error {
	if receipt.Seq == 0 {
		_, err := e.start(t.App, t.Run, nil, input, false, "", nil)
		return err
	}
	c, err := e.newCheckpoint(t.App, t.Run, nil, input, false)
	if errors.Is(err, ErrAIUnavailable) && t.fieldFill {
		return nil
	}
	if err != nil {
		return e.failTriggered(t, receipt, "trigger run could not be prepared: "+err.Error())
	}
	return e.publishTriggered(receipt, c)
}

// Invalid definitions and conditions leave a visible receipt instead of
// blocking later events. Journal errors still hold the cursor for a retry.
func (e *Engine) failTriggered(t Trigger, receipt triggerReceipt, reason string) error {
	if receipt.Seq == 0 {
		return fmt.Errorf("%s trigger %s: %s", t.App, t.ID, reason)
	}
	now := e.cfg.Now()
	id, err := cell.NewID(now)
	if err != nil {
		return err
	}
	return e.publishTriggered(receipt, checkpoint{Run: Run{
		App: t.App, DefinitionID: t.Run, ID: id, State: Incomplete, Error: reason, StartedAt: now, EndedAt: now,
	}})
}

func (e *Engine) publishTriggered(receipt triggerReceipt, c checkpoint) error {
	c.TriggerReceipt = &receipt
	c.UpdatedAt = e.cfg.Now()
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	claimed, err := e.commitTriggered(receipt, c.Run, raw)
	if claimed {
		e.changed(c.Run)
	}
	return err
}

func (e *Engine) commitTriggered(receipt triggerReceipt, run Run, raw json.RawMessage) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return false, errors.New("app runs are closed")
	}
	key := runPrefix + run.ID
	claimed, err := e.journal.ClaimAppRun(receipt.Seq, receipt.TriggerID, key, raw)
	if claimed {
		e.checkpoints[key] = raw
		e.track(run)
	}
	return claimed, err
}

// fireBefore starts one run per record and scheduled time in its lead window.
func (e *Engine) fireBefore(t Trigger, now time.Time) error {
	if !validID(t.App) || !validID(t.ID) || !validID(t.Type) || t.At == "" {
		return errors.New("a before-record trigger needs an app, id, record type and timestamp field")
	}
	lead, err := duration(t.Lead)
	if err != nil {
		return err
	}
	if e.cfg.ReadRecords == nil {
		return errors.New("a before-record trigger needs app-scoped records")
	}
	rows, err := e.cfg.ReadRecords(t.App, t.Type)
	if err != nil {
		return err
	}
	for _, item := range rows {
		row, _ := item.(map[string]any)
		id, _ := row["id"].(string)
		value, _ := row[t.At].(string)
		at, err := time.Parse(time.RFC3339Nano, value)
		if !validID(id) || err != nil || now.Before(at.Add(-lead)) || !now.Before(at) {
			continue
		}
		key := "before/" + t.App + "/" + t.ID + "/" + id + "/" + at.UTC().Format(time.RFC3339Nano)
		e.mu.Lock()
		_, fired := e.checkpoints[key]
		e.mu.Unlock()
		if fired {
			continue
		}
		record := maps.Clone(row)
		record["type"] = t.Type
		input := map[string]any{"record": record}
		vars := maps.Clone(record)
		vars["record"], vars["input"], vars["me"] = record, input, "me"
		vars["now"] = now.UTC().Format(time.RFC3339Nano)
		e.addRecordReader(t.App, vars)
		match, err := condition(t.When, vars, nil)
		if err != nil {
			return err
		}
		if match {
			if _, err := e.start(t.App, t.Run, nil, input, false, key, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) fireGoogle(ctx context.Context, t Trigger, now time.Time) error {
	if !validID(t.App) || !validID(t.ID) || t.When != "" {
		return errors.New("invalid source trigger")
	}
	connection, _ := t.Match["connection"].(string)
	read, _ := t.Match["read"].(string)
	query, _ := t.Match["query"].(string)
	minutes, _ := t.Match["minutes"].(float64)
	if minutes < 0 || minutes > 1440 || float64(int(minutes)) != minutes {
		return errors.New("invalid Google trigger lead time")
	}
	key := "google-trigger/" + t.App + "/" + t.ID + "/" + t.Run
	e.mu.Lock()
	raw := e.checkpoints[key]
	e.mu.Unlock()
	var prev connections.GoogleCheckpoint
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &prev); err != nil {
			return err
		}
	}
	next, events, err := e.cfg.GooglePoll(ctx, connections.GoogleTrigger{
		App: t.App, Connection: connection, Read: read, Source: t.Connector,
		Event: t.Event, Query: query, LeadMinutes: int(minutes),
	}, prev, now)
	if err != nil {
		return err
	}
	for _, event := range events {
		claim := key + "/" + event.Key
		e.mu.Lock()
		_, used := e.checkpoints[claim]
		e.mu.Unlock()
		if !used {
			input := map[string]any{"id": event.Record, "type": event.Type}
			if _, err := e.start(t.App, t.Run, nil, input, false, claim, true); err != nil {
				return err
			}
		}
	}
	if reflect.DeepEqual(next, prev) {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.saveLocked(nil, key, next)
}
