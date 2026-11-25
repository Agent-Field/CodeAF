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
		switch event.Event {
		case data.EventRecordCreated, data.EventRecordUpdated, data.EventRecordDeleted:
			var change Change
			change, err = decode[Change](event.Payload)
			if err != nil {
				return fmt.Errorf("read app run outbox event %d: %w", event.Seq, err)
			}
			change.Seq, change.Type, change.ID = event.Seq, event.Type, event.Record
			normalize(change.Before)
			normalize(change.After)
			changes = append(changes, change)
		}
		cursor = event.Seq
	}
	if err := engine.RecordChanges(changes); err != nil {
		return err
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.saveLocked(nil, recordOutboxCursor, cursor)
}
