// Storage functions port src/session/message-v2.ts:678-721 and 1021-1098.
// The Drizzle-specific query construction is behind Store; Page retains the
// exact descending (time,id) pagination, one-row lookahead, hydration, and
// per-page reversal. Stream returns the generator's newest-first order.
package msgmodel

import (
	"context"
	"errors"
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

var ErrNotFound = errors.New("msgmodel: not found")

type MessageRecord struct {
	Info        Info
	TimeCreated float64
}

// Store methods must return messages in descending (time_created,id) order
// and parts in ascending (message_id,id) order, matching the source queries.
type Store interface {
	SelectMessages(ctx context.Context, sessionID string, limit int, before *Cursor) ([]MessageRecord, error)
	SessionExists(ctx context.Context, sessionID string) (bool, error)
	SelectParts(ctx context.Context, messageIDs []string) (Parts, error)
	SelectMessage(ctx context.Context, sessionID, messageID string) (MessageRecord, bool, error)
	SelectMessageParts(ctx context.Context, messageID string) (Parts, error)
}

type PageInput struct {
	SessionID string
	Limit     int
	Before    *string
}

type PageResult struct {
	Items  []WithParts `json:"items"`
	More   bool        `json:"more"`
	Cursor *string     `json:"cursor,omitempty"`
}

func Page(ctx context.Context, store Store, input PageInput) (PageResult, error) {
	var before *Cursor
	if input.Before != nil && *input.Before != "" {
		decoded, err := DecodeCursor(*input.Before)
		if err != nil {
			return PageResult{}, err
		}
		before = &decoded
	}
	rows, err := store.SelectMessages(ctx, input.SessionID, input.Limit+1, before)
	if err != nil {
		return PageResult{}, err
	}
	if len(rows) == 0 {
		ok, err := store.SessionExists(ctx, input.SessionID)
		if err != nil {
			return PageResult{}, err
		}
		if !ok {
			return PageResult{}, fmt.Errorf("%w: Session not found: %s", ErrNotFound, input.SessionID)
		}
		return PageResult{Items: []WithParts{}, More: false}, nil
	}

	more := len(rows) > input.Limit
	slice := rows
	if more {
		slice = rows[:input.Limit]
	}
	items, err := hydrateRecords(ctx, store, slice)
	if err != nil {
		return PageResult{}, err
	}
	reverseWithParts(items)
	result := PageResult{Items: items, More: more}
	if more && len(slice) > 0 {
		tail := slice[len(slice)-1]
		encoded, err := EncodeCursor(Cursor{ID: tail.Info.MessageID(), Time: jscompat.JSNumber(tail.TimeCreated)})
		if err != nil {
			return PageResult{}, err
		}
		result.Cursor = &encoded
	}
	return result, nil
}

func Stream(ctx context.Context, store Store, sessionID string) ([]WithParts, error) {
	const size = 50
	var before *string
	result := []WithParts{}
	for {
		next, err := Page(ctx, store, PageInput{SessionID: sessionID, Limit: size, Before: before})
		if err != nil {
			return nil, err
		}
		if len(next.Items) == 0 {
			break
		}
		for i := len(next.Items) - 1; i >= 0; i-- {
			result = append(result, next.Items[i])
		}
		if !next.More || next.Cursor == nil {
			break
		}
		before = next.Cursor
	}
	return result, nil
}

func MessageParts(ctx context.Context, store Store, messageID string) (Parts, error) {
	return store.SelectMessageParts(ctx, messageID)
}

func Get(ctx context.Context, store Store, sessionID, messageID string) (WithParts, error) {
	row, ok, err := store.SelectMessage(ctx, sessionID, messageID)
	if err != nil {
		return WithParts{}, err
	}
	if !ok {
		return WithParts{}, fmt.Errorf("%w: Message not found: %s", ErrNotFound, messageID)
	}
	parts, err := store.SelectMessageParts(ctx, messageID)
	if err != nil {
		return WithParts{}, err
	}
	return WithParts{Info: row.Info, Parts: nonnilParts(parts)}, nil
}

func hydrateRecords(ctx context.Context, store Store, rows []MessageRecord) ([]WithParts, error) {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Info.MessageID())
	}
	parts, err := store.SelectParts(ctx, ids)
	if err != nil {
		return nil, err
	}
	byMessage := make(map[string]Parts, len(ids))
	for _, part := range parts {
		base := part.Base()
		byMessage[base.MessageID] = append(byMessage[base.MessageID], part)
	}
	out := make([]WithParts, 0, len(rows))
	for _, row := range rows {
		out = append(out, WithParts{
			Info:  row.Info,
			Parts: nonnilParts(byMessage[row.Info.MessageID()]),
		})
	}
	return out, nil
}

func nonnilParts(parts Parts) Parts {
	if parts == nil {
		return Parts{}
	}
	return parts
}
