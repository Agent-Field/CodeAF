package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// Unicode words and short identifiers are part of people's conversations, and
// an FTS expression must never be supplied by a message or a query caller.
func TestConversationLookupPreservesUnicodeAndShortWords(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	_, err := graph.PostMessage(Message{SessionID: "a", Role: RoleUser, Body: "Our UI label is 東京 and the owner is Zoë."})
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"UI", "東京", "Zoë", `"UI" OR (*`} {
		hits, err := graph.FindConversationMessages(context.Background(), query, "", "", 8)
		if err != nil || len(hits) != 1 {
			t.Fatalf("%q: %v, %v", query, hits, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := graph.FindConversationMessages(ctx, "UI", "", "", 8); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation became a miss: %v", err)
	}
}

// An exact message belongs to one conversation even though sequence numbers
// are shared by every writer. Both the read size and its UTF-8 must survive a
// body larger than the exchange's allowance.
func TestConversationExchangeStaysInItsThreadAndBoundsLongText(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	var seq int64
	for i := 0; i < 7; i++ {
		msg, err := graph.PostMessage(Message{SessionID: "a", Role: RoleUser, Body: strings.Repeat("界", 5000)})
		if err != nil {
			t.Fatal(err)
		}
		if i == 3 {
			seq = msg.Seq
		}
		if _, err := graph.PostMessage(Message{SessionID: "b", Role: RoleAgent, Body: "foreign"}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := graph.ConversationExchange(context.Background(), "a", seq, 99, 99999)
	if err != nil || len(rows) != 5 {
		t.Fatalf("exchange: %v %v", rows, err)
	}
	for _, row := range rows {
		if row.SessionID != "a" || len(row.Body) > ConversationReadBytes || !utf8.ValidString(row.Body) || (row.Seq != seq && (!strings.HasSuffix(row.Body, "...") || len(row.Body) > ConversationExcerptBytes)) {
			t.Fatalf("invalid row: %+v", row)
		}
	}
	rows, err = graph.ConversationExchange(context.Background(), "b", seq, 2, 400)
	if err != nil || len(rows) != 0 {
		t.Fatalf("wrong owner accepted: %v %v", rows, err)
	}
}
