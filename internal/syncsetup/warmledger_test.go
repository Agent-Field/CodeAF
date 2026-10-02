package syncsetup

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// answer is one scripted turn that also calls a model and ends: the turn-end
// seal carries the call in its receipt, as a real turn's seal does.
func (o *openChat) answer(text string) {
	o.t.Helper()
	executor.NoteModelCall(o.seat, executor.ModelCall{Model: "m", Role: "turn", TokensIn: 100, TokensOut: 10, CostMicroUSD: 1000})
	appendTo(o.t, transcriptOf(o.cell), `{"type":"usage","timestamp":"2026-09-29T10:00:01Z","usage":{"model":"m","role":"turn","calls":1,"input":100,"output":10,"costUsd":0.001}}`+"\n")
	o.mustSay(text)
	executor.Settle(context.Background(), o.seat)
}

// ledgerRowsOf counts the rows the machine whose home is dir keeps for the chat.
func ledgerRowsOf(t *testing.T, dir, id string) int {
	t.Helper()
	t.Setenv(home.EnvVar, dir)
	session.FlushUsage()
	body, err := os.ReadFile(session.UsageLedgerPath())
	if err != nil {
		return 0
	}
	return bytes.Count(body, []byte(`"session":"`+id+`"`))
}

// B takes a chat it already held an older copy of. Every turn made since then
// has its model calls on B's ledger and its spend on B's session row, once: the
// ledger is derived from the carried receipts, and a take brings the ones B
// lacks and no others.
func TestWarmTakeBringsTheLedgerRowsOfEveryTurnSinceTheOlderCopy(t *testing.T) {
	ctx := context.Background()
	h := newTwoHomes(t)
	seedTree(t, h.work)
	homeA := os.Getenv(home.EnvVar)
	if homeA == "" {
		homeA = t.TempDir()
	}

	t.Setenv(home.EnvVar, homeA)
	a := h.openA()
	a.answer("a one")
	a.answer("a two")
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.b.Home)
	took, err := h.continuerB().Take(ctx, h.cell.ID) // the older copy B keeps
	if err != nil {
		t.Fatal(err)
	}
	if got := ledgerRowsOf(t, h.b.Home, h.cell.ID); got != 2 {
		t.Fatalf("after the first take B's ledger has %d rows, want 2", got)
	}
	b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
	b.answer("b one")
	h.durable(h.cell.ID, took.Taken.Cell)
	if err := b.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}

	// A takes the chat back and makes four more turns, then B takes it again.
	t.Setenv(home.EnvVar, homeA)
	if _, err := h.continuerA().Take(ctx, h.cell.ID); err != nil {
		t.Fatal(err)
	}
	a2 := h.openA()
	for range 4 {
		a2.answer("a later")
	}
	h.durable(h.cell.ID, h.cell)
	if err := a2.drive.Close(ctx); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.b.Home)
	cb := h.continuerB()
	h.backToA()
	warm, err := cb.Take(ctx, h.cell.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got := ledgerRowsOf(t, h.b.Home, h.cell.ID); got != 7 {
		t.Fatalf("after the warm take B's ledger has %d rows, want the 7 model calls of every turn", got)
	}
	meta, err := session.LoadMeta(warm.Taken.Cell.Root)
	if err != nil {
		t.Fatal(err)
	}
	if want := 7 * 0.001; meta.SpentUSD < want-1e-6 || meta.SpentUSD > want+1e-6 {
		t.Fatalf("B's session row spend is $%v, want $%v", meta.SpentUSD, want)
	}
}
