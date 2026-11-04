package syncsetup

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ledgerAnswer seals a scripted paid call through the real seat and storage.
func (o *openChat) ledgerAnswer(text, id string) {
	o.t.Helper()
	executor.NoteModelCall(o.seat, executor.ModelCall{ID: id, Model: "m", Role: "turn", TokensIn: 100, TokensOut: 10, CostMicroUSD: 1000})
	appendTo(o.t, transcriptOf(o.cell), `{"type":"usage","timestamp":"2026-09-29T10:00:01Z","usage":{"model":"m","role":"turn","calls":1,"input":100,"output":10,"costUsd":0.001}}`+"\n")
	o.mustSay(text)
	executor.Settle(context.Background(), o.seat)
}

func TestTakeLedgerDuringLateAccounting(t *testing.T) {
	for _, own := range []bool{false, true} {
		name := "other_chat"
		if own {
			name = "same_chat_late_receipt"
		}
		t.Run(name, func(t *testing.T) {
			h := newTwoHomes(t)
			seedTree(t, h.work)
			t.Setenv(home.EnvVar, h.homeA)
			ledgerA := session.UsageLedgerPath()
			a := h.openA()
			a.ledgerAnswer("A paid call", "call-a")
			session.RecordUsage(ledgerA, session.UsageLine{ID: "call-a", Model: "m", Session: h.cell.ID, Input: 100, Output: 10, Calls: 1, USD: 0.001})
			if !session.FlushUsage() {
				t.Fatal("A flush")
			}
			h.durable(h.cell.ID, h.cell)
			if err := a.drive.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Setenv(home.EnvVar, h.homeB)
			took, err := h.continuerB().Take(t.Context(), h.cell.ID)
			if err != nil {
				t.Fatal(err)
			}
			b := h.openOn(h.b, h.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
			b.ledgerAnswer("B paid call", "call-b")
			session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{ID: "call-b", Model: "m", Session: h.cell.ID, Input: 100, Output: 10, Calls: 1, USD: 0.001})
			if !session.FlushUsage() {
				t.Fatal("B flush")
			}
			h.durable(h.cell.ID, took.Taken.Cell)
			if err := b.drive.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Setenv(home.EnvVar, h.homeA)
			start, done := make(chan struct{}), make(chan struct{})
			lateOwner := "another-live-chat"
			if own {
				lateOwner = h.cell.ID
			}
			go func() {
				defer close(done)
				<-start
				session.RecordUsage(ledgerA, session.UsageLine{Model: "m", Session: lateOwner, Role: "title", Calls: 1, Input: 30, Output: 3, USD: 0.003, Reconciled: true, GenerationID: "diag-late-title"})
				session.FlushUsage()
			}()
			released := false
			c := h.continuerA()
			c.opt.OnPhase = func(p handoff.Phase) {
				if p.Name == "acquire" && !p.Ended.IsZero() && !released {
					released = true
					close(start)
					<-done
				}
			}
			_, err = c.Take(t.Context(), h.cell.ID)
			if !released {
				close(start)
				<-done
				t.Fatal("take did not finish acquire")
			}
			if err != nil {
				t.Fatal(err)
			}
			if !session.FlushUsage() {
				t.Fatal("final flush")
			}
			rows, err := session.ReadUsage(ledgerA, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			var got float64
			for _, r := range rows {
				got += r.USD
				t.Logf("[ledger] session=%s role=%s usd=%.6f id=%s", r.Session, r.Role, r.USD, r.ID)
			}
			t.Logf("[ledger] case=%s expected=0.005000 recorded=%.6f rows=%d", name, got, len(rows))
			if math.Abs(got-0.005) > 0.0000001 {
				t.Fatalf("[ledger] lost or repeated spend: expected %.6f recorded %.6f", 0.005, got)
			}
		})
	}
}

func assertTakeLedger(t *testing.T, path, owner string, want float64, count int) {
	t.Helper()
	if !session.FlushUsage() {
		t.Fatal("ledger did not flush")
	}
	rows, err := session.ReadUsage(path, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	var n int
	for _, row := range rows {
		if row.Session == owner {
			sum += row.USD
			n++
		}
	}
	t.Logf("expected=$%.6f recorded=$%.6f rows=%d want_rows=%d", want, sum, n, count)
	if math.Abs(sum-want) > 1e-9 || n != count {
		t.Fatalf("expected=$%.6f recorded=$%.6f rows=%d want_rows=%d", want, sum, n, count)
	}
}

func TestTakeLedgerQueuedOrSettledLiveCall(t *testing.T) {
	for _, queued := range []bool{false, true} {
		name := "settled"
		if queued {
			name = "queued"
		}
		t.Run(name, func(t *testing.T) {
			h := newTwoHomes(t)
			seedTree(t, h.work)
			t.Setenv(home.EnvVar, h.homeA)
			path := session.UsageLedgerPath()
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			if queued {
				parked := make(chan struct{})
				var parkedOnce sync.Once
				// A written notice can hold the writer while later paid rows stay queued.
				// This controls scheduling through the public ledger interface, without a
				// test-only hook in the accounting or take implementation.
				stop := session.OnUsageWritten(func() { parkedOnce.Do(func() { close(parked); <-release }) })
				t.Cleanup(func() { unblock(); stop(); session.StopUsageWriter(path) })
				session.RecordUsage(path, session.UsageLine{Session: "writer-barrier", Input: 1})
				select {
				case <-parked:
				case <-time.After(10 * time.Second):
					t.Fatal("writer did not park")
				}
			}
			a := h.openA()
			session.RecordUsage(path, session.UsageLine{ID: "paid-call", Session: h.cell.ID, Model: "m", Input: 100, Output: 10, Calls: 1, USD: .001})
			if !queued && !session.FlushUsage() {
				t.Fatal("settled flush")
			}
			a.ledgerAnswer("one paid call", "paid-call")
			h.durable(h.cell.ID, h.cell)
			if err := a.drive.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			t.Setenv(home.EnvVar, h.homeB)
			if _, err := h.continuerB().Take(t.Context(), h.cell.ID); err != nil {
				t.Fatal(err)
			}
			t.Setenv(home.EnvVar, h.homeA)
			if _, err := h.continuerA().Take(t.Context(), h.cell.ID); err != nil {
				t.Fatal(err)
			}
			unblock()
			assertTakeLedger(t, path, h.cell.ID, .001, 1)
		})
	}
}

func TestTakeLedgerLegacyCallsAndIdenticalCalls(t *testing.T) {
	for _, tc := range []struct {
		name       string
		legacyRows int
	}{
		{"legacy_complete_prefix", 2}, {"legacy_partial_prefix", 1}, {"identical_calls", 0},
	} {
		name, legacy := tc.name, tc.legacyRows > 0
		t.Run(name, func(t *testing.T) {
			h := newTwoHomes(t)
			seedTree(t, h.work)
			t.Setenv(home.EnvVar, h.homeA)
			path := session.UsageLedgerPath()
			a := h.openA()
			for i := range 2 {
				id := fmt.Sprintf("call-%d", i)
				if legacy {
					a.answer("old paid call")
					if i < tc.legacyRows {
						appendTo(t, path, fmt.Sprintf(`{"at":"2026-09-29T10:00:01Z","session":%q,"model":"m","input":100,"output":10,"usd":0.001}`+"\n", h.cell.ID))
					}
				} else {
					a.ledgerAnswer("identical paid call", id)
					session.RecordUsage(path, session.UsageLine{ID: id, Session: h.cell.ID, Model: "m", Input: 100, Output: 10, Calls: 1, USD: .001})
				}
			}
			if !session.FlushUsage() {
				t.Fatal("flush before take")
			}
			h.durable(h.cell.ID, h.cell)
			if err := a.drive.Close(t.Context()); err != nil {
				t.Fatal(err)
			}
			t.Setenv(home.EnvVar, h.homeB)
			if _, err := h.continuerB().Take(t.Context(), h.cell.ID); err != nil {
				t.Fatal(err)
			}
			assertTakeLedger(t, session.UsageLedgerPath(), h.cell.ID, .002, 2)
			t.Setenv(home.EnvVar, h.homeA)
			if _, err := h.continuerA().Take(t.Context(), h.cell.ID); err != nil {
				t.Fatal(err)
			}
			assertTakeLedger(t, path, h.cell.ID, .002, 2)
		})
	}
}

func TestReviewMixedLegacyPrefixAndNewUnsealedReceipt(t *testing.T) {
	h := newTwoHomes(t)
	seedTree(t, h.work)
	t.Setenv(home.EnvVar, h.homeA)
	path := session.UsageLedgerPath()
	t.Cleanup(func() { session.StopUsageWriter(path) })
	a := h.openA()
	a.answer("old paid call one")
	a.answer("old paid call two")
	appendTo(t, path, fmt.Sprintf(`{"id":"old-independent-row-id","at":"2026-09-29T10:00:01Z","session":%q,"model":"m","input":100,"output":10,"usd":0.001}`+"\n", h.cell.ID))
	a.ledgerAnswer("new identified paid call", "new-saved")
	session.RecordUsage(path, session.UsageLine{ID: "new-saved", Session: h.cell.ID, Model: "m", Input: 100, Output: 10, Calls: 1, USD: .001})
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeB)
	if _, err := h.continuerB().Take(t.Context(), h.cell.ID); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeA)
	session.RecordUsage(path, session.UsageLine{ID: "new-unsealed-receipt", Session: h.cell.ID, Role: "title", Model: "m", Input: 30, Output: 3, Calls: 1, USD: .003, Reconciled: true, GenerationID: "late-new-receipt"})
	if !session.FlushUsage() {
		t.Fatal("late flush")
	}
	if _, err := h.continuerA().Take(t.Context(), h.cell.ID); err != nil {
		t.Fatal(err)
	}
	assertTakeLedger(t, path, h.cell.ID, .006, 4)
}

func TestReviewMixedLegacyPrefixAndNewUnbilledMarker(t *testing.T) {
	h := newTwoHomes(t)
	seedTree(t, h.work)
	t.Setenv(home.EnvVar, h.homeA)
	path := session.UsageLedgerPath()
	t.Cleanup(func() { session.StopUsageWriter(path) })
	a := h.openA()
	a.answer("old paid call one")
	a.answer("old paid call two")
	appendTo(t, path, fmt.Sprintf(`{"id":"old-independent-row-id","at":"2026-09-29T10:00:01Z","session":%q,"model":"m","input":100,"output":10,"usd":0.001}`+"\n", h.cell.ID))
	a.ledgerAnswer("new identified paid call", "new-saved")
	session.RecordUsage(path, session.UsageLine{ID: "new-saved", Session: h.cell.ID, Model: "m", Input: 100, Output: 10, Calls: 1, USD: .001})
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	h.durable(h.cell.ID, h.cell)
	if err := a.drive.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeB)
	if _, err := h.continuerB().Take(t.Context(), h.cell.ID); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, h.homeA)
	session.RecordUnbilledCall(path, session.UsageLine{Session: h.cell.ID, Model: "m"})
	if !session.FlushUsage() {
		t.Fatal("late flush")
	}
	if _, err := h.continuerA().Take(t.Context(), h.cell.ID); err != nil {
		t.Fatal(err)
	}
	assertTakeLedger(t, path, h.cell.ID, .003, 4)
}
