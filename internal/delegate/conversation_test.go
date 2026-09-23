package delegate

import (
	"strings"
	"testing"
	"time"
)

// A call is written when it starts and again when it ends; the reader keeps
// the later record in the earlier one's place, so a call in flight is seen and
// then replaced by its answer.
func TestReadTurnsKeepsEachCallsLatestRecordInStartOrder(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	for _, turn := range []Turn{
		{Seq: 1, Started: start, Model: "m", Sent: []Said{{Role: "user", Text: "the brief"}}},
		{Seq: 1, Started: start, Ended: start.Add(time.Second), Model: "m", Reply: "reading the tests", Calls: []ToolUse{{Name: "bash", Args: "go test ./..."}}},
		{Seq: 2, Started: start.Add(2 * time.Second), Model: "m"},
	} {
		if err := AppendTurn(dir, turn); err != nil {
			t.Fatal(err)
		}
	}
	turns, err := ReadTurns(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || turns[0].Reply != "reading the tests" || turns[0].InFlight() || !turns[1].InFlight() {
		t.Fatalf("turns = %+v", turns)
	}
	if turns[0].Thread != MainThread {
		t.Fatalf("thread = %q, want the main one for a call that named none", turns[0].Thread)
	}
	if last, _ := ReadTurns(dir, 1); len(last) != 1 || last[0].Seq != 2 {
		t.Fatalf("last = %+v", last)
	}
}

func TestATurnIsWrittenCapped(t *testing.T) {
	dir := t.TempDir()
	sent := make([]Said, 20)
	for i := range sent {
		sent[i] = Said{Role: "tool", Tool: "bash", Text: strings.Repeat("é", 3000)}
	}
	if err := AppendTurn(dir, Turn{Seq: 1, Sent: sent, Reply: strings.Repeat("x", 5000), Calls: []ToolUse{{Name: "bash", Args: "a\nb  " + strings.Repeat("y", 500)}}}); err != nil {
		t.Fatal(err)
	}
	turns, _ := ReadTurns(dir, 0)
	turn := turns[0]
	if len(turn.Sent) != turnSaidMax || len(turn.Sent[0].Text) > turnTextCap || len(turn.Reply) != turnTextCap {
		t.Fatalf("sent %d, first %d bytes, reply %d bytes", len(turn.Sent), len(turn.Sent[0].Text), len(turn.Reply))
	}
	if args := turn.Calls[0].Args; len(args) > turnArgsCap || strings.Contains(args, "\n") {
		t.Fatalf("args = %q, want one line, capped", args)
	}
}

func TestReadTurnsOfARunThatCalledNothingIsEmpty(t *testing.T) {
	turns, err := ReadTurns(t.TempDir(), 0)
	if err != nil || len(turns) != 0 {
		t.Fatalf("turns %v err %v", turns, err)
	}
}
