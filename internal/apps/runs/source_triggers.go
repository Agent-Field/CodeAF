package runs

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

type sourceTriggerClock struct {
	Definition string    `json:"definition"`
	Cursor     string    `json:"cursor,omitempty"`
	Since      time.Time `json:"since"`
	Next       time.Time `json:"next"`
}

// PollSources runs independently of the schedule clock and ready workers so
// a slow provider cannot prevent the engine from running queued work.
func (e *Engine) PollSources(ctx context.Context) error {
	if e.cfg.SourceTriggers == nil || e.cfg.Triggers == nil {
		return nil
	}
	e.sourceClock.Lock()
	defer e.sourceClock.Unlock()
	all, err := e.cfg.Triggers()
	now := e.cfg.Now()
	for _, t := range all {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if t.On == "connector" && schema.GraphSourceTrigger(t.Connector, t.Event) {
			if pollErr := e.pollSource(ctx, t, now); pollErr != nil {
				err = errors.Join(err, fmt.Errorf("%s source trigger %s: %w", t.App, t.ID, pollErr))
			}
		}
	}
	return err
}

func (e *Engine) pollSource(ctx context.Context, t Trigger, now time.Time) error {
	connection, _ := t.Match["connection"].(string)
	read, _ := t.Match["read"].(string)
	filter, _ := t.Match["query"].(string)
	minutes, _ := t.Match["minutes"].(float64)
	if !validID(t.App) || !validID(t.ID) || !validID(connection) || !validID(read) || len(filter) > 256 ||
		float64(int(minutes)) != minutes || t.When != "" {
		return errors.New("a source trigger needs a declared app connection, read, and record type")
	}
	clockKey := "source-trigger/" + t.App + "/" + t.ID
	configuration, _ := json.Marshal([]any{t.Connector, t.Event, t.Match, t.Run, t.Since})
	definition := fmt.Sprintf("%x", sha256.Sum256(configuration))
	e.mu.Lock()
	raw := e.checkpoints[clockKey]
	e.mu.Unlock()
	clock := sourceTriggerClock{Definition: definition, Since: t.Since}
	if len(raw) > 0 {
		prev, err := decode[sourceTriggerClock](raw)
		if err != nil {
			return err
		}
		if prev.Definition == definition {
			clock = prev
		}
	}
	if now.Before(clock.Next) {
		return nil
	}
	if clock.Since.IsZero() {
		clock.Since = now
	}
	batch, pollErr := e.cfg.SourceTriggers(ctx, connections.SourceTrigger{On: t.Event, App: t.App, Connection: connection, Read: read, Filter: filter,
		MinutesBefore: int(minutes), Now: now, Since: clock.Since, Cursor: clock.Cursor})
	for _, event := range batch.Events {
		if err := e.sourceRun(t, event); err != nil {
			return err
		}
	}
	clock.Next = now.Add(time.Minute)
	if pollErr == nil {
		clock.Cursor = batch.Cursor
		if batch.HasMore {
			clock.Next = now
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return errors.Join(e.saveLocked(nil, clockKey, clock), pollErr)
}

func (e *Engine) sourceRun(t Trigger, event connections.SourceTriggerEvent) error {
	hash := sha256.Sum256([]byte(t.On + "\x00" + event.Key))
	seq := int64(binary.BigEndian.Uint64(hash[:]) & (1<<63 - 1))
	if seq == 0 {
		seq = 1
	}
	receipt := triggerReceipt{Seq: seq, TriggerID: "source/" + t.App + "/" + t.ID, Record: event.Record,
		SourceEvent: fmt.Sprintf("%s:%x", t.Event, hash)}
	claimed, err := e.journal.AppRunClaimed(receipt.Seq, receipt.TriggerID)
	if err != nil || claimed {
		return err
	}
	if !event.Match {
		return e.journal.ClaimSourceTrigger(receipt.Seq, receipt.TriggerID)
	}
	return e.startTriggered(t, receipt, map[string]any{"id": event.Record, "type": event.Type})
}
