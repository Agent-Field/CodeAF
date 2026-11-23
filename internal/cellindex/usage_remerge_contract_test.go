package cellindex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	storetest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Reopening a chat that has only transcript usage lines and folded task summaries
// must leave the machine ledger unchanged across repeated reopens.
func TestReopeningAReceiptlessChatManyTimesLeavesTheLedgerAlone(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	c := chatCell(t)
	writeTranscript(t, c)
	path := session.Place{Dir: c.Root}.Transcript()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 3, 15, 9, 30, 0, 0, time.UTC)
	const nonFoldedCount = 4
	for i := range nonFoldedCount {
		at := base.Add(time.Duration(i+1) * time.Second)
		usd := float64(i+1) * 0.01
		raw, err := json.Marshal(map[string]any{
			"type":      "usage",
			"timestamp": at.Format(time.RFC3339Nano),
			"usage": map[string]any{
				"model":   "m",
				"role":    "turn",
				"calls":   1,
				"input":   100 + i,
				"output":  20 + i,
				"costUsd": usd,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
		session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{
			At:      at,
			Session: c.ID,
			Model:   "m",
			Role:    "turn",
			Calls:   1,
			Input:   100 + i,
			Output:  20 + i,
			USD:     usd,
		})
	}
	for _, folded := range []map[string]any{
		{"costUsd": 0.25, "aux": true, "folded": true},
		{"costUsd": 0.25, "aux": true},
	} {
		raw, err := json.Marshal(map[string]any{
			"type":      "usage",
			"timestamp": base.Add(10 * time.Second).Format(time.RFC3339Nano),
			"usage":     folded,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	for step := range 5 {
		if step == 0 {
			if _, err := Rebuild(c, ""); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := Refresh(c, ""); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != nonFoldedCount {
			t.Fatalf("reopen %d: ledger rows=%d, want %d", step+1, len(rows), nonFoldedCount)
		}
	}
}

// Refreshing a chat after a take imports only the calls added on the other copy.
func TestATakeImportsExactlyTheOtherCopysCalls(t *testing.T) {
	bin := celltest.Engine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	c := chatCell(t)
	writeTranscript(t, c)
	rec, err := cellstore.NewRecorder(nil, cellstore.Engine{Binary: bin, DataRoot: storetest.EngineDir(t)}, c, filepath.Join(t.TempDir(), "wal"), cellstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupRecorder(t, rec)

	session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{
		ID: "call-local-1", Session: c.ID, Model: "m", Role: "turn", Calls: 1, Input: 50, Output: 5, USD: 0.005,
	})
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	rec.NoteModelCall(executor.ModelCall{ID: "call-local-1", Model: "m", Role: "turn", TokensIn: 50, TokensOut: 5, CostMicroUSD: 5000})
	if err := rec.Around(context.Background(), executor.Call{Tool: "edit", Args: []byte(`{}`)}, executor.EffectLocal, cellstore.AgentRun, func() ([]byte, bool) { return nil, false }); err != nil {
		t.Fatal(err)
	}
	rec.NoteModelCall(executor.ModelCall{ID: "call-other-2", Model: "m", Role: "turn", TokensIn: 80, TokensOut: 8, CostMicroUSD: 8000})
	if err := rec.Around(context.Background(), executor.Call{Tool: "edit", Args: []byte(`{}`)}, executor.EffectLocal, cellstore.AgentRun, func() ([]byte, bool) { return nil, false }); err != nil {
		t.Fatal(err)
	}

	if _, err := Refresh(c, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Refresh(c, ""); err != nil {
		t.Fatal(err)
	}
	rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("ledger rows=%d, want 2: %+v", len(rows), rows)
	}
	if got := totalOf(t); got != (total{in: 130, out: 13, micro: 13000}) {
		t.Fatalf("got %+v, want {in:130 out:13 micro:13000}", got)
	}
}

// A chat whose turns sealed model-call receipts reopens without duplicating spend.
func TestAReceiptedChatReopensExactlyAsBefore(t *testing.T) {
	bin := celltest.Engine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	c := chatCell(t)
	writeTranscript(t, c)
	rec, err := cellstore.NewRecorder(nil, cellstore.Engine{Binary: bin, DataRoot: storetest.EngineDir(t)}, c, filepath.Join(t.TempDir(), "wal"), cellstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupRecorder(t, rec)

	rec.NoteModelCall(executor.ModelCall{ID: "receipt-call-1", Model: "m", Role: "turn", TokensIn: 120, TokensOut: 15, CostMicroUSD: 12000})
	if err := rec.Around(context.Background(), executor.Call{Tool: "edit", Args: []byte(`{}`)}, executor.EffectLocal, cellstore.AgentRun, func() ([]byte, bool) { return nil, false }); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(c, ""); err != nil {
		t.Fatal(err)
	}
	want := totalOf(t)
	if want != (total{in: 120, out: 15, micro: 12000}) {
		t.Fatalf("initial rebuild total=%+v", want)
	}
	for range 3 {
		if _, err := Rebuild(c, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := Refresh(c, ""); err != nil {
			t.Fatal(err)
		}
		if got := totalOf(t); got != want {
			t.Fatalf("receipted chat reopen changed total: got %+v, want %+v", got, want)
		}
	}
}

// Two calls with distinct explicit identities are both kept even when every other field matches.
func TestTwoCallsWithDifferentExplicitIdsAreBothBilledEvenWhenAlike(t *testing.T) {
	path := filepath.Join(t.TempDir(), session.UsageLedgerName)
	t.Cleanup(func() { session.StopUsageWriter(path) })
	at := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	first := session.UsageLine{
		ID: "explicit-one", CallID: "explicit-one",
		At: at, Session: "sess-same", Model: "m", Role: "turn", Calls: 1, Input: 40, Output: 10, USD: 0.004,
	}
	second := session.UsageLine{
		ID: "explicit-two", CallID: "explicit-two",
		At: at, Session: "sess-same", Model: "m", Role: "turn", Calls: 1, Input: 40, Output: 10, USD: 0.004,
	}
	if session.UsageCallID(first) == session.UsageCallID(second) {
		t.Fatalf("distinct explicit IDs produced the same call ID: %q", session.UsageCallID(first))
	}
	both, err := missingUsage(path, []session.UsageLine{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 2 {
		t.Fatalf("missingUsage kept %d of 2 distinct explicit calls: %+v", len(both), both)
	}
	session.RecordUsage(path, first)
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	rem, err := missingUsage(path, []session.UsageLine{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if len(rem) != 1 || session.UsageCallID(rem[0]) != "explicit-two" {
		t.Fatalf("missingUsage after booking first = %+v, want only explicit-two", rem)
	}
}

// A line with no time or session has no identity, so it never matches a row by
// identity. It is still covered by its session's uncovered rows, the way both
// unified rules count an unidentified line: reopening books it at most once and
// never again on a later reopen.
func TestALineWithNoTimeOrSessionHasNoIdentityButIsStillCounted(t *testing.T) {
	path := filepath.Join(t.TempDir(), session.UsageLedgerName)
	t.Cleanup(func() { session.StopUsageWriter(path) })
	at := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	noTime := session.UsageLine{Session: "sess-a", Model: "m", Role: "turn", Calls: 1, Input: 10, Output: 2, USD: 0.001}
	noSession := session.UsageLine{At: at, Model: "m", Role: "turn", Calls: 1, Input: 10, Output: 2, USD: 0.001}
	if id := session.UsageCallID(noTime); id != "" {
		t.Fatalf("line without timestamp got identity %q", id)
	}
	if id := session.UsageCallID(noSession); id != "" {
		t.Fatalf("line without session got identity %q", id)
	}
	// One row of the same session, billed before identities existed.
	session.RecordImportedUsage(path, session.UsageLine{At: at, Session: "sess-a", Model: "m", Role: "turn", Calls: 1, Input: 10, Output: 2, USD: 0.001})
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	lines := []session.UsageLine{noTime, noTime}
	got, err := missingUsage(path, lines, "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("first reopen books %d lines, want the 1 the session's row does not cover: %+v", len(got), got)
	}
	for _, line := range got {
		session.RecordImportedUsage(path, line)
	}
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	for reopen := 2; reopen <= 4; reopen++ {
		got, err = missingUsage(path, lines, "sess-a")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("reopen %d books %d lines again, want none: %+v", reopen, len(got), got)
		}
	}
}

// An old journal usage line without an explicit ID matches the live ledger row that billed it.
func TestAnOldJournalLineFindsTheLiveRowThatBilledIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), session.UsageLedgerName)
	t.Cleanup(func() { session.StopUsageWriter(path) })
	at := time.Date(2026, 4, 2, 15, 4, 5, 123000000, time.UTC)
	session.RecordUsage(path, session.UsageLine{
		At: at, Session: "sess-old-journal", Model: "m", Role: "turn", Calls: 1, Input: 75, Output: 25, USD: 0.015,
	})
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	journalLine := session.UsageLine{
		At: at, Session: "sess-old-journal", Model: "m", Role: "turn", Calls: 1, Input: 75, Output: 25, USD: 0.015,
	}
	missing, err := missingUsage(path, []session.UsageLine{journalLine})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("old journal line did not match its live ledger row: missing=%+v", missing)
	}
}

// A chat recorded now shares an explicit call identity between its ledger row and its saved record.
func TestAChatRecordedNowReopensByItsExplicitId(t *testing.T) {
	bin := celltest.Engine(t)
	t.Setenv(home.EnvVar, t.TempDir())
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	path := (session.Place{Dir: c.Root}).Transcript()
	if err := session.SeedConversation(path, "", "Recorded now", "hello"); err != nil {
		t.Fatal(err)
	}
	rec, err := cellstore.NewRecorder(nil, cellstore.Engine{Binary: bin, DataRoot: storetest.EngineDir(t)}, c, filepath.Join(t.TempDir(), "wal"), cellstore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupRecorder(t, rec)

	session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{
		ID: "live-explicit-id", Session: c.ID, Model: "m", Role: "", Calls: 1, Input: 200, Output: 40, USD: 0.025,
	})
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	rec.NoteModelCall(executor.ModelCall{
		ID: "live-explicit-id", CallID: "live-explicit-id",
		Model: "m", Role: "turn", TokensIn: 200, TokensOut: 40, CostMicroUSD: 25000,
	})
	if err := rec.Around(context.Background(), executor.Call{Tool: "edit", Args: []byte(`{}`)}, executor.EffectLocal, cellstore.AgentRun, func() ([]byte, bool) { return nil, false }); err != nil {
		t.Fatal(err)
	}

	before, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil || len(before) != 1 {
		t.Fatalf("before reopen rows=%v err=%v", before, err)
	}
	if _, err := Rebuild(c, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Refresh(c, ""); err != nil {
		t.Fatal(err)
	}
	after, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("reopen added a duplicate row: before=%d after=%d rows=%+v", len(before), len(after), after)
	}
}
