package runs

import (
	"context"
	"errors"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/data"
)

const recordOutboxCursor = "outbox/records"
const recordOutboxPage = 128

// ConsumeOutbox reads one page of committed record changes. The cursor can
// lag a run commit because replay checks the receipt in the run journal.
func (d Data) ConsumeOutbox(ctx context.Context, engine *Engine) error {
	engine.outbox.Lock()
	defer engine.outbox.Unlock()
	db, err := d.Store()
	if err != nil {
		return err
	}
	engine.mu.Lock()
	raw := engine.checkpoints[recordOutboxCursor]
	engine.mu.Unlock()
	var after int64
	if raw != nil {
		after, err = decode[int64](raw)
		if err != nil {
			return err
		}
	}
	through, err := db.Cursor()
	if err != nil {
		return err
	}
	if raw == nil {
		return engine.advanceOutbox(through)
	}
	if after == through {
		return nil
	}
	if after > through {
		return errors.New("app run cursor is ahead of the data outbox")
	}
	events, err := db.Outbox(after, recordOutboxPage)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		return fmt.Errorf("app run outbox history after %d is missing", after)
	}
	changes := make([]Change, 0, len(events))
	cursor := after
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if event.Seq > through {
			break
		}
		if event.Seq != cursor+1 {
			return fmt.Errorf("app run outbox history after %d is missing", cursor)
		}
		cursor = event.Seq
		switch event.Event {
		case data.EventFreshnessChanged:
			// A native source observation can commit a record revision.
			// Refresh-only invalidations have no record history batch.
			if event.SourceEvent == "" || event.Batch == "" || event.Record == "" {
				continue
			}
			fallthrough
		case data.EventRecordCreated, data.EventRecordUpdated, data.EventRecordDeleted:
			changes = append(changes, Change{
				Seq: event.Seq, Type: event.Type, TypeID: event.TypeID, ID: event.Record, Batch: event.Batch,
				OutboxVersion: event.Version, CauseChain: event.CauseChain, SourceEvent: event.SourceEvent, Cause: event.Cause,
			})
		}
	}
	if err := engine.RecordChanges(changes); err != nil {
		return err
	}
	return engine.advanceOutbox(cursor)
}

func (e *Engine) advanceOutbox(cursor int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.saveLocked(nil, recordOutboxCursor, cursor)
}
