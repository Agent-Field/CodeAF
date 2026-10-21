package cellstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/executor"
)

func noted(model string, in, out int, micro int64) executor.ModelCall {
	return executor.ModelCall{Model: model, Role: "turn", TokensIn: in, TokensOut: out, TokensCached: in / 2, CostMicroUSD: micro}
}

func seatWith(t *testing.T, c cell.Cell, e Engine) sealed {
	t.Helper()
	rec, err := NewRecorder(&stubExec{}, e, c, filepath.Join(t.TempDir(), "wal"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return sealed{Stance: executor.Stance{Class: executor.HostBound}, rec: rec}
}

func toolCall(t *testing.T, s sealed) {
	t.Helper()
	call := executor.Call{Tool: "bash", Args: []byte(`{"command":"x"}`)}
	if err := s.Around(context.Background(), call, func() ([]byte, bool) { return nil, false }); err != nil {
		t.Fatal(err)
	}
}

// A turn with two model calls carries both in its receipt, and the next seal
// does not carry them again.
func TestReceiptCarriesEveryModelCallSinceThePreviousSeal(t *testing.T) {
	c := newCell(t)
	fake := &fakeEngine{}
	seat := seatWith(t, c, fake.engine(t))
	executor.NoteModelCall(seat, noted("m-a", 100, 10, 500))
	executor.NoteModelCall(seat, noted("m-b", 200, 20, 700))
	toolCall(t, seat)
	head, _ := Head(c)
	got := head.Receipt.ModelCalls
	if len(got) != 2 || got[0].Model != "m-a" || got[1].CostMicroUSD != 700 || got[1].TokensCached != 100 || got[0].Role != "turn" {
		t.Fatalf("model calls %+v", got)
	}
	toolCall(t, seat)
	if head, _ = Head(c); len(head.Receipt.ModelCalls) != 0 {
		t.Fatalf("second seal repeated %+v", head.Receipt.ModelCalls)
	}
}

// A rewind restores an earlier turn, and the chain still holds what the
// rewound-past turns spent.
func TestRewindKeepsSpendAlreadyPaid(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		e := realEngine(t)
		c := newCell(t)
		seat := seatWith(t, c, e)
		for i, micro := range []int64{100, 200, 400} {
			executor.NoteModelCall(seat, noted("m", 10*(i+1), i+1, micro))
			writeTree(t, c.Root, map[string]string{"f.txt": string(rune('a' + i))}, 0o644)
			toolCall(t, seat)
		}
		before := micros(t, c)
		turns, _ := Turns(c)
		mustRewind(t, e, c, turns[0].ID)
		if after := micros(t, c); after != before || after != 700 {
			t.Fatalf("spend %d before rewind, %d after", before, after)
		}
	})
}

func micros(t *testing.T, c cell.Cell) (sum int64) {
	t.Helper()
	all, err := ModelCalls(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		sum += m.CostMicroUSD
	}
	return sum
}
