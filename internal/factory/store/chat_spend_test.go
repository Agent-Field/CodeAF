package store

import (
	"math"
	"testing"
)

func TestChatSpend(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.ChatSpend("ws_a", "hosted:one"); err != nil || got != 0 {
		t.Fatalf("fresh chat spend = %g, %v", got, err)
	}
	for _, row := range []struct {
		ws, chat, key string
		cost          float64
	}{
		{"ws_a", "hosted:one", "first", 0.0123},
		{"ws_a", "hosted:one:own", "second", 0.0246},
		{"ws_a", "hosted:one:own", "second", 0.0246},
		{"ws_a", "hosted:ones", "third", 1},
		{"ws_a", "agent", "fourth", 1},
		{"ws_b", "hosted:one", "first", 2},
	} {
		if _, err := st.AddSpend(row.ws, Spend{At: "2026-11-24T12:00:00Z", By: "abir", USD: row.cost, Chat: row.chat, Key: row.key}); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := st.ChatSpend("ws_a", "hosted:one"); err != nil || math.Abs(got-0.0369) > 1e-12 {
		t.Fatalf("chat spend = %g, %v", got, err)
	}
}
