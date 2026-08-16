package msgmodel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
)

type memoryMessageStore struct {
	sessions map[string]bool
	records  []MessageRecord
	parts    Parts
}

func (s *memoryMessageStore) SelectMessages(
	_ context.Context, sessionID string, limit int, before *Cursor,
) ([]MessageRecord, error) {
	rows := []MessageRecord{}
	for _, row := range s.records {
		if infoSessionID(row.Info) != sessionID {
			continue
		}
		if before != nil {
			time := row.TimeCreated
			if !(time < float64(before.Time) ||
				(time == float64(before.Time) && row.Info.MessageID() < before.ID)) {
				continue
			}
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].TimeCreated != rows[j].TimeCreated {
			return rows[i].TimeCreated > rows[j].TimeCreated
		}
		return rows[i].Info.MessageID() > rows[j].Info.MessageID()
	})
	if limit < len(rows) {
		rows = rows[:limit]
	}
	return rows, nil
}

func (s *memoryMessageStore) SessionExists(_ context.Context, sessionID string) (bool, error) {
	return s.sessions[sessionID], nil
}

func (s *memoryMessageStore) SelectParts(_ context.Context, messageIDs []string) (Parts, error) {
	wanted := map[string]bool{}
	for _, id := range messageIDs {
		wanted[id] = true
	}
	out := Parts{}
	for _, part := range s.parts {
		if wanted[part.Base().MessageID] {
			out = append(out, part)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Base(), out[j].Base()
		if a.MessageID != b.MessageID {
			return a.MessageID < b.MessageID
		}
		return a.ID < b.ID
	})
	return out, nil
}

func (s *memoryMessageStore) SelectMessage(
	_ context.Context, sessionID, messageID string,
) (MessageRecord, bool, error) {
	for _, row := range s.records {
		if row.Info.MessageID() == messageID && infoSessionID(row.Info) == sessionID {
			return row, true, nil
		}
	}
	return MessageRecord{}, false, nil
}

func (s *memoryMessageStore) SelectMessageParts(_ context.Context, messageID string) (Parts, error) {
	return s.SelectParts(context.Background(), []string{messageID})
}

func infoSessionID(info Info) string {
	switch value := info.(type) {
	case User:
		return value.SessionID
	case Assistant:
		return value.SessionID
	default:
		return ""
	}
}

func storedUser(id string, created float64) MessageRecord {
	return MessageRecord{
		Info: User{
			MessageBase: MessageBase{ID: id, SessionID: "ses_1"},
			Time:        TimeCreated{Created: uint64(created)},
			Agent:       "build",
			Model:       UserModel{ProviderID: "openrouter", ModelID: "m"},
		},
		TimeCreated: created,
	}
}

func TestPageHydratesAndPaginatesByTimeThenID(t *testing.T) {
	store := &memoryMessageStore{
		sessions: map[string]bool{"ses_1": true},
		records: []MessageRecord{
			storedUser("m1", 1), storedUser("m4", 2),
			storedUser("m2", 1), storedUser("m3", 2),
		},
		parts: Parts{
			TextPart{PartBase: PartBase{ID: "p4b", SessionID: "ses_1", MessageID: "m4"}, Text: "b"},
			TextPart{PartBase: PartBase{ID: "p3", SessionID: "ses_1", MessageID: "m3"}, Text: "three"},
			TextPart{PartBase: PartBase{ID: "p4a", SessionID: "ses_1", MessageID: "m4"}, Text: "a"},
		},
	}
	first, err := Page(context.Background(), store, PageInput{SessionID: "ses_1", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := messageIDs(first.Items); fmt.Sprint(got) != "[m3 m4]" {
		t.Fatalf("first page order = %v", got)
	}
	if !first.More || first.Cursor == nil {
		t.Fatalf("first page cursor/more = %#v", first)
	}
	if len(first.Items[0].Parts) != 1 || len(first.Items[1].Parts) != 2 {
		t.Fatalf("hydrated parts = %#v", first.Items)
	}
	if first.Items[1].Parts[0].Base().ID != "p4a" {
		t.Fatalf("parts not in id order: %#v", first.Items[1].Parts)
	}

	second, err := Page(context.Background(), store, PageInput{
		SessionID: "ses_1", Limit: 2, Before: first.Cursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := messageIDs(second.Items); fmt.Sprint(got) != "[m1 m2]" {
		t.Fatalf("second page order = %v", got)
	}
	if second.More || second.Cursor != nil {
		t.Fatalf("unexpected second-page continuation: %#v", second)
	}
}

func TestStreamKeepsGeneratorNewestFirstAcrossPages(t *testing.T) {
	store := &memoryMessageStore{sessions: map[string]bool{"ses_1": true}}
	for i := 1; i <= 53; i++ {
		store.records = append(store.records, storedUser(fmt.Sprintf("m%03d", i), float64(i)))
	}
	got, err := Stream(context.Background(), store, "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 53 || got[0].Info.MessageID() != "m053" || got[52].Info.MessageID() != "m001" {
		t.Fatalf("stream order/length: %d %s..%s", len(got), got[0].Info.MessageID(), got[len(got)-1].Info.MessageID())
	}
}

func TestPageAndGetNotFoundMessages(t *testing.T) {
	store := &memoryMessageStore{sessions: map[string]bool{"ses_1": true}}
	empty, err := Page(context.Background(), store, PageInput{SessionID: "ses_1", Limit: 5})
	if err != nil || empty.More || len(empty.Items) != 0 || empty.Items == nil {
		t.Fatalf("existing empty session = %#v, %v", empty, err)
	}
	_, err = Page(context.Background(), store, PageInput{SessionID: "missing", Limit: 5})
	if !errors.Is(err, ErrNotFound) || err.Error() != "msgmodel: not found: Session not found: missing" {
		t.Fatalf("page missing error = %v", err)
	}
	_, err = Get(context.Background(), store, "ses_1", "missing")
	if !errors.Is(err, ErrNotFound) || err.Error() != "msgmodel: not found: Message not found: missing" {
		t.Fatalf("get missing error = %v", err)
	}
}

func messageIDs(items []WithParts) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Info.MessageID())
	}
	return out
}

func TestDecodeCursorRejectsInvalidPayloads(t *testing.T) {
	for _, input := range []string{"***", "bnVsbA", "eyJpZCI6Im0iLCJ0aW1lIjotMX0"} {
		if _, err := DecodeCursor(input); err == nil {
			t.Errorf("DecodeCursor(%q) unexpectedly succeeded", input)
		}
	}
}
