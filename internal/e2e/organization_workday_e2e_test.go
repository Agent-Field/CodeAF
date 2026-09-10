//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// Twelve real conversations share one changing contract. Fixture membership is
// explicit; the exercise measures regular use, not discovery or automatic wakes.
func testOrganizationWorkingDay(t *testing.T) {
	w := newWorld(t)
	pinEveryTextModel(t)
	started := time.Now()
	ctx := context.Background()
	store, err := workspace.Open(orgStorePath())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engineering, err := store.Create(ctx, "Engineering")
	if err != nil {
		t.Fatal(err)
	}
	launch, err := store.Create(ctx, "Launch")
	if err != nil {
		t.Fatal(err)
	}
	groups := []workspace.Collection{engineering, launch}
	type chat struct {
		agent     *session.Agent
		place     session.Place
		dir, role string
	}
	roles := []string{"API design", "retry fix", "integration tests", "SDK", "migration", "security review", "release notes", "website copy", "support guide", "onboarding", "demo script", "launch checklist"}
	chats := make([]chat, len(roles))
	for i, role := range roles {
		dir := t.TempDir()
		a, p := w.open(dir, orgSeam(w))
		chats[i] = chat{a, p, dir, role}
		if err = store.Add(ctx, groups[i/6].ID, workspace.Ref{Kind: workspace.ConversationKind, ID: session.PlaceSession(p)}); err != nil {
			t.Fatal(err)
		}
	}
	// A cross-cutting chat belongs to both collections. Removing one path must
	// not remove the other path to the same current information.
	both := workspace.Ref{Kind: workspace.ConversationKind, ID: session.PlaceSession(chats[0].place)}
	if err = store.Add(ctx, launch.ID, both); err != nil {
		t.Fatal(err)
	}
	source, sourcePlace := w.open(t.TempDir(), orgSeam(w))
	targets := []workspace.Ref{{Kind: workspace.CollectionKind, ID: engineering.ID}, {Kind: workspace.CollectionKind, ID: launch.ID}}
	rawTargets, _ := json.Marshal(targets)
	marker := "CONTRACT-" + orgNonce(t)
	made := organizationSay(t, w, source, fmt.Sprintf("Record shared information titled Release contract: the response identifier is receipt_id; reference %s. Apply it to these explicit collections: %s. Retain information only; do not create tasks or write files.", marker, rawTargets))
	if _, at := orgActionReceipt(made, "shared_context", "create"); at < 0 {
		t.Fatal("source did not record context")
	}
	records, err := store.ContextFor(ctx, targets)
	if err != nil || len(records) != 1 {
		t.Fatalf("source records=%v err=%v", records, err)
	}
	record := records[0]
	sourceID := session.PlaceSession(sourcePlace)
	if record.Revision != 1 || record.Source.ID != sourceID {
		t.Fatal("incorrect initial provenance")
	}

	// Each output is inspected from disk. The answer and provenance never come
	// from a judge or from the assistant's claim that a file was written.
	report := func(index int, name, value string, revision int, extra, note string) {
		t.Helper()
		c := chats[index]
		prompt := fmt.Sprintf("We are working on %s. Which response identifier should our current draft use? %s Use the write tool to create %s as a JSON report with value, status (current or unavailable), record_id, revision, source_id, draft (one sentence), and note (the review code from the requested notes, or empty if none). Cite the exact current shared record ID, its revision number, and its originating conversation ID from the supplied shared context; these are not the reference code in its text. Use the current applicable Release contract shared information; do not use old answers if it no longer applies. Without current applicable information, use value unknown, status unavailable, empty IDs and revision 0. Report only: do not change organization, execute commands or create tasks. Do not discover unrelated collections.", c.role, extra, name)
		out := organizationSay(t, w, c.agent, prompt)
		for _, call := range out.Calls {
			if call.Name != "shared_context" && call.Name != "collections" {
				continue
			}
			var action struct {
				Action string `json:"action"`
			}
			if err := json.Unmarshal([]byte(call.Args), &action); err != nil {
				t.Fatal(err)
			}
			if action.Action != "list" && action.Action != "find" && action.Action != "show" && action.Action != "read" && action.Action != "history" {
				t.Fatalf("report mutated organization: %s", call.Args)
			}
		}
		raw, err := os.ReadFile(filepath.Join(c.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			organizationEvidence
			Note string `json:"note"`
		}
		if err = json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("invalid report: %s", raw)
		}
		if got.Value != value || got.Revision != revision || (note != "" && got.Note != note) {
			t.Errorf("%s: got %+v want value=%s revision=%d note=%s", c.role, got, value, revision, note)
		}
		if revision > 0 {
			if got.Status != "current" || got.RecordID != record.ID || got.SourceID != sourceID {
				t.Errorf("wrong provenance: %+v", got)
			}
		} else if got.Status != "unavailable" || got.RecordID != "" || got.SourceID != "" {
			t.Errorf("stale information used: %+v", got)
		}
		t.Logf("WORKDAY artifact=%s/%s %s", c.role, name, raw)
	}
	// Three simultaneous submissions exercise independent sessions against one
	// database without parallel subtests changing process-wide profile variables.
	for start := 0; start < len(chats); start += 3 {
		var batch sync.WaitGroup
		for i := start; i < start+3; i++ {
			batch.Add(1)
			go func(i int) { defer batch.Done(); report(i, "initial.json", "receipt_id", 1, "", "") }(i)
		}
		batch.Wait()
	}

	// Eight additional real user turns accumulate a substantial journal through
	// ordinary file reads and discussion. The notes are synthetic test documents,
	// not seeded assistant messages; each code must be read by the actual model.
	var noteBytes int
	for round := 0; round < 8; round++ {
		code := "REVIEW-" + orgNonce(t)
		var notes strings.Builder
		for line := 0; line < 110; line++ {
			fmt.Fprintf(&notes, "Review item %d.%d: retain compatibility coverage for cancellation, duplicate delivery, request retries and resumed clients. The proposed change needs a small test, clear error handling and a readable migration note.\n", round, line)
		}
		fmt.Fprintf(&notes, "Review code for this document: %s\n", code)
		filename := fmt.Sprintf("review-%d.md", round)
		if err = os.WriteFile(filepath.Join(chats[0].dir, filename), []byte(notes.String()), 0600); err != nil {
			t.Fatal(err)
		}
		noteBytes += notes.Len()
		report(0, fmt.Sprintf("discussion-%d.json", round), "receipt_id", 1, "Read "+filename+" completely, consider its compatibility concerns in the draft, and copy its review code into note.", code)
	}
	journal, err := os.ReadFile(chats[0].place.Transcript())
	if err != nil || len(journal) < 100000 {
		t.Fatalf("long conversation not exercised: bytes=%d err=%v", len(journal), err)
	}
	t.Logf("WORKDAY long_chat real_followups=8 notes_bytes=%d journal_bytes=%d", noteBytes, len(journal))

	idle := chats[11]
	before, err := os.ReadFile(filepath.Join(idle.dir, "initial.json"))
	if err != nil {
		t.Fatal(err)
	}
	turns := idle.agent.Usage().Turns
	wakes, stop := idle.agent.WatchWakes()
	defer stop()
	observedAt := time.Now()
	revised := organizationSay(t, w, source, fmt.Sprintf("Revise shared context %s at revision 1. Keep title Release contract, change text to: the response identifier is now operation_id; receipt_id is retired; reference %s. Keep the complete explicit targets %s. Revise the existing record only.", record.ID, marker, rawTargets))
	if _, at := orgActionReceipt(revised, "shared_context", "revise"); at < 0 {
		t.Fatal("source did not revise")
	}
	current, err := store.Context(ctx, record.ID)
	if err != nil || current.Revision != 2 {
		t.Fatalf("revision=%+v err=%v", current, err)
	}
	report(0, "revised.json", "operation_id", 2, "", "")
	report(6, "revised.json", "operation_id", 2, "", "")
	after, err := os.ReadFile(filepath.Join(idle.dir, "initial.json"))
	if err != nil || !bytes.Equal(before, after) || idle.agent.Usage().Turns != turns {
		t.Fatal("idle consumer changed during other chats' work")
	}
	select {
	case _, ok := <-wakes:
		if !ok {
			t.Fatal("idle wake observer closed")
		}
		t.Fatal("shared revision woke idle consumer")
	default:
	}
	t.Logf("WORKDAY idle chat=%s observed_for=%s artifact_unchanged=true turns_unchanged=true no_wake=true", idle.role, time.Since(observedAt))
	stop()
	report(11, "resumed.json", "operation_id", 2, "", "")

	reopen := func(i int) {
		if err := chats[i].agent.Close(); err != nil {
			t.Fatal(err)
		}
		chats[i].agent = w.openAt(chats[i].dir, chats[i].place, orgSeam(w))
	}
	reopen(0)
	report(0, "reopened.json", "operation_id", 2, "", "")
	if err = store.Remove(ctx, engineering.ID, both); err != nil {
		t.Fatal(err)
	}
	reopen(0)
	report(0, "one-membership.json", "operation_id", 2, "", "")
	if err = store.Remove(ctx, launch.ID, both); err != nil {
		t.Fatal(err)
	}
	reopen(0)
	report(0, "no-membership.json", "unknown", 0, "", "")
	if err = store.Add(ctx, launch.ID, both); err != nil {
		t.Fatal(err)
	}
	reopen(0)
	report(0, "rejoined.json", "operation_id", 2, "", "")
	withdrawal := organizationSay(t, w, source, fmt.Sprintf("Withdraw shared context %s at current revision 2. It is no longer reliable. Withdraw only; do not create replacement information or tasks.", record.ID))
	if _, at := orgActionReceipt(withdrawal, "shared_context", "withdraw"); at < 0 {
		t.Fatal("source did not withdraw")
	}
	for _, i := range []int{0, 6, 11} {
		reopen(i)
		report(i, "withdrawn.json", "unknown", 0, "", "")
	}
	history, err := store.ContextHistory(ctx, record.ID)
	if err != nil || len(history) != 3 || !history[2].Withdrawn {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	usd, models := ledgerSince(t, started)
	t.Logf("RECEIPT domain=working_day chats=%d cost=$%.6f models=%v elapsed=%s", len(chats), usd, models, time.Since(started))
	if usd > orgCap {
		t.Fatalf("working day exceeded $%.2f: $%.4f", orgCap, usd)
	}
	for _, model := range models {
		if model != e2eModel {
			t.Fatalf("unexpected model %s", model)
		}
	}
}
