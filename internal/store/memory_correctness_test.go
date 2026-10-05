package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestContextualWriteSerializesExactDuplicates(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "concurrent.db"))
	const writers = 16
	var wg sync.WaitGroup
	results := make(chan WriteResult, writers)
	failures := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row, err := graph.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Title: "Punctuation", Text: "!!!"})
			if err != nil {
				failures <- err
				return
			}
			results <- row
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	adds, skips := 0, 0
	var id string
	for result := range results {
		if result.Outcome == WriteOutcomeAdded {
			adds++
		} else {
			skips++
		}
		if id == "" {
			id = result.Memory.ID
		}
		if result.Memory.ID != id {
			t.Error("concurrent writes produced different identities")
		}
	}
	if adds != 1 || skips != writers-1 {
		t.Fatalf("adds=%d skips=%d", adds, skips)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rows, err := graph.ListMemories([]string{OwnerUser}, 100)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rebuilt rows=%v err=%v", rows, err)
	}
}

func TestContextualWriteValidatesBeforeDuplicateLookup(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "validation.db"))
	_, err := graph.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact, Text: "Known body"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = graph.Write(WriteRequest{Owner: OwnerUser, Type: "invented", Text: "Known body"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid duplicate type: %v", err)
	}
}

func TestContextualMutationOwnerSafetyAndReplay(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "owners.db"))
	owner := OwnerProject("alpha")
	foreign := OwnerProject("beta")
	row := mustAddMemory(t, graph, Memory{Owner: owner, Type: MemoryFact, Text: "Alpha fact"})
	if err := graph.ForgetMemoryForOwners([]string{foreign}, row.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign forget: %v", err)
	}
	if _, err := graph.SupersedeMemoryForOwners([]string{owner, foreign}, row.ID, Memory{Owner: foreign, Type: MemoryFact, Text: "Beta fact"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-owner supersede: %v", err)
	}
	if err := graph.ForgetMemoryForOwners([]string{owner}, row.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := graph.RestoreMemoryForOwners([]string{foreign}, row.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign restore: %v", err)
	}
	if err := graph.RestoreMemoryForOwners([]string{owner}, row.ID); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rows, err := graph.GetMemories([]string{owner}, []string{row.ID})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rebuild changed owner-safe mutations: %v %v", rows, err)
	}
}

func TestContextualSyncRedeliveryDoesNotUndoNewerLocalUpdate(t *testing.T) {
	origin := openTestStore(t, filepath.Join(t.TempDir(), "origin.db"))
	receiver := openTestStore(t, filepath.Join(t.TempDir(), "receiver.db"))
	row := mustAddMemory(t, origin, Memory{Owner: OwnerUser, Type: MemoryFact, Text: "Initial"})
	if err := origin.UpdateMemory(row.ID, "Label", "Remote revision", nil); err != nil {
		t.Fatal(err)
	}
	batch, _, _, err := origin.ExportMemoryEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.ApplyMemoryEvents(batch, nil); err != nil {
		t.Fatal(err)
	}
	if err := receiver.UpdateMemory(row.ID, "Label", "Newer local revision", nil); err != nil {
		t.Fatal(err)
	}
	if err := receiver.Rebuild(); err != nil {
		t.Fatal(err)
	}
	result, err := receiver.ApplyMemoryEvents(batch, nil)
	if err != nil || result.Applied != 0 || result.Skipped != len(batch) {
		t.Fatalf("redelivery=%+v %v", result, err)
	}
	kept, found, err := receiver.MemoryRecord(row.ID)
	if err != nil || !found || kept.Text != "Newer local revision" {
		t.Fatalf("redelivery rolled back local correction: %+v %v", kept, err)
	}
	if err := receiver.Rebuild(); err != nil {
		t.Fatal(err)
	}
	kept, _, err = receiver.MemoryRecord(row.ID)
	if err != nil || kept.Text != "Newer local revision" {
		t.Fatalf("replay changed correction: %+v %v", kept, err)
	}
}

func TestContextualSyncKeepsOwnerProvenTombstoneBeforeAdd(t *testing.T) {
	origin := openTestStore(t, filepath.Join(t.TempDir(), "origin.db"))
	row := mustAddMemory(t, origin, Memory{Owner: OwnerUser, Type: MemoryFact, Text: "Forget before delayed add"})
	if err := origin.ForgetMemory(row.ID); err != nil {
		t.Fatal(err)
	}
	batch, _, _, err := origin.ExportMemoryEvents(0, 100)
	if err != nil || len(batch) != 2 {
		t.Fatalf("batch=%v %v", batch, err)
	}
	receiver := openTestStore(t, filepath.Join(t.TempDir(), "receiver.db"))
	allowUser := func(owner string) bool { return owner == OwnerUser }
	result, err := receiver.ApplyMemoryEvents(batch[1:], allowUser)
	if err != nil || result.Applied != 1 {
		t.Fatalf("pending tombstone=%+v %v", result, err)
	}
	if err := receiver.Rebuild(); err != nil {
		t.Fatal(err)
	}
	result, err = receiver.ApplyMemoryEvents(batch[:1], allowUser)
	if err != nil || result.Skipped != 1 {
		t.Fatalf("delayed add=%+v %v", result, err)
	}
	rows, err := receiver.GetMemories([]string{OwnerUser}, []string{row.ID})
	if err != nil || len(rows) != 0 {
		t.Fatalf("delayed add resurrected forgotten memory: %v %v", rows, err)
	}
	blocked := openTestStore(t, filepath.Join(t.TempDir(), "blocked.db"))
	result, err = blocked.ApplyMemoryEvents(batch[1:], func(string) bool { return false })
	if err != nil || result.Applied != 0 || result.Skipped != 1 {
		t.Fatalf("policy failed to block unknown tombstone=%+v %v", result, err)
	}
	result, err = blocked.ApplyMemoryEvents(batch[:1], allowUser)
	if err != nil || result.Applied != 1 {
		t.Fatalf("refused tombstone leaked to allowed add=%+v %v", result, err)
	}
}

func TestContextualSyncOriginEchoDoesNotUndoNewerLocalUpdate(t *testing.T) {
	origin := openTestStore(t, filepath.Join(t.TempDir(), "origin.db"))
	relay := openTestStore(t, filepath.Join(t.TempDir(), "relay.db"))
	row := mustAddMemory(t, origin, Memory{Owner: OwnerUser, Type: MemoryFact, Text: "Original"})
	if err := origin.UpdateMemory(row.ID, "Label", "Old correction", nil); err != nil {
		t.Fatal(err)
	}
	batch, _, _, err := origin.ExportMemoryEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := relay.ApplyMemoryEvents(batch, nil); err != nil {
		t.Fatal(err)
	}
	if err := origin.UpdateMemory(row.ID, "Label", "Newest correction", nil); err != nil {
		t.Fatal(err)
	}
	echoed, _, _, err := relay.ExportMemoryEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	result, err := origin.ApplyMemoryEvents(echoed, nil)
	if err != nil || result.Applied != 0 {
		t.Fatalf("origin echo=%+v %v", result, err)
	}
	kept, found, err := origin.MemoryRecord(row.ID)
	if err != nil || !found || kept.Text != "Newest correction" {
		t.Fatalf("origin echo erased newer correction: %+v %v", kept, err)
	}
}

func TestContextualSyncSupersedePolicyUsesReplacementOwner(t *testing.T) {
	origin := openTestStore(t, filepath.Join(t.TempDir(), "origin.db"))
	receiver := openTestStore(t, filepath.Join(t.TempDir(), "receiver.db"))
	row := mustAddMemory(t, origin, Memory{Owner: OwnerUser, Type: MemoryFact, Text: "Original"})
	replacement, err := origin.SupersedeMemory(row.ID, Memory{Owner: OwnerUser, Type: MemoryFact, Text: "Replacement"})
	if err != nil {
		t.Fatal(err)
	}
	batch, _, _, err := origin.ExportMemoryEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	result, err := receiver.ApplyMemoryEvents(batch, func(owner string) bool { return owner == OwnerUser })
	if err != nil || result.Applied != 2 {
		t.Fatalf("user-only supersede=%+v %v", result, err)
	}
	kept, found, err := receiver.MemoryRecord(replacement.ID)
	if err != nil || !found || kept.Owner != OwnerUser {
		t.Fatalf("missing replacement=%+v %v", kept, err)
	}
}

func TestContextualSyncOwnerlessUnknownTombstoneCannotInventOwner(t *testing.T) {
	receiver := openTestStore(t, filepath.Join(t.TempDir(), "receiver.db"))
	payload, err := json.Marshal(memoryForgetPayload{ID: "delayed-legacy"})
	if err != nil {
		t.Fatal(err)
	}
	batch := []MemoryEvent{{Seq: 1, Kind: EventMemoryForget, Payload: payload}}
	result, err := receiver.ApplyMemoryEvents(batch, func(owner string) bool { return owner == OwnerUser })
	if err != nil || result.Applied != 0 || result.Skipped != 1 {
		t.Fatalf("ownerless tombstone=%+v %v", result, err)
	}
	if err := receiver.Rebuild(); err != nil {
		t.Fatal(err)
	}
	written := mustAddMemory(t, receiver, Memory{ID: "delayed-legacy", Owner: OwnerProject("private"), Type: MemoryFact, Text: "Must not be blocked by guessed owner"})
	if written.Status != MemoryActive {
		t.Fatalf("ownerless tombstone affected foreign owner: %+v", written)
	}
}
