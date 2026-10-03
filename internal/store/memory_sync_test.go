package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// The sync seam's fold, proven between two stores. Nothing here knows what a
// transport is: the batch is taken from one journal and handed to the other,
// which is what any transport will do.
func TestMemorySyncFoldsAcrossTwoStoresIdempotently(t *testing.T) {
	origin := openTestStore(t, filepath.Join(t.TempDir(), "origin.db"))
	receiver := openTestStore(t, filepath.Join(t.TempDir(), "receiver.db"))

	// The origin writes under owners its policy allows, and one the receiver
	// must refuse.
	userRow, err := origin.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference,
		Title: "Wants terse replies", Text: "Answers short, conclusion first."})
	if err != nil {
		t.Fatal(err)
	}
	machineRow, err := origin.Write(WriteRequest{Owner: OwnerMachine, Type: MemoryFact,
		Title: "Runs on spark", Text: "The heavy jobs run on the spark machine."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := origin.Write(WriteRequest{Owner: OwnerProject("secret"), Type: MemoryDecision,
		Title: "Secret pricing", Text: "The secret project's pricing is X."}); err != nil {
		t.Fatal(err)
	}
	// A supersession, so the chain crosses.
	fresh, err := origin.SupersedeMemory(machineRow.Memory.ID, Memory{
		Owner: OwnerMachine, Type: MemoryFact,
		Title: "Runs on spark", Text: "The heavy jobs run on the spark cluster.",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A forget, so the tombstone crosses.
	if err := origin.ForgetMemory(userRow.Memory.ID); err != nil {
		t.Fatal(err)
	}

	// THE RECEIVER'S POLICY REFUSES project:secret — a memory the receiver's
	// owner has no business holding. Skipped, never widened.
	allow := func(owner string) bool { return owner != OwnerProject("secret") }

	apply := func() (int, int) {
		var cursor int64
		appliedTotal, skippedTotal := 0, 0
		for {
			events, next, more, err := origin.ExportMemoryEvents(cursor, 3)
			if err != nil {
				t.Fatalf("export: %v", err)
			}
			result, err := receiver.ApplyMemoryEvents(events, allow)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			appliedTotal += result.Applied
			skippedTotal += result.Skipped
			if !more {
				break
			}
			cursor = next
		}
		return appliedTotal, skippedTotal
	}
	applied, skipped := apply()
	// The refused write is among the skipped; everything else lands.
	if applied+skipped != 5 {
		t.Fatalf("first fold = %d applied, %d skipped, want 5 events total", applied, skipped)
	}
	if skipped != 1 {
		t.Fatalf("skips = %d, want exactly the refused owner's write", skipped)
	}

	// THE FOLD IS VISIBLE IN THE RECEIVER'S VIEWS: the user row is FORGOTTEN
	// (the tombstone preserved, never resurrected), the machine row is
	// SUPERSEDED, and the refused owner's row does not exist here.
	status := func(id string) string {
		row, ok, err := receiver.MemoryRecord(id)
		if err != nil {
			t.Fatalf("read %q: %v", id, err)
		}
		if !ok {
			return "absent"
		}
		return row.Status
	}
	if got := status(userRow.Memory.ID); got != MemoryForgotten {
		t.Fatalf("the user row folded to %q, want forgotten — the tombstone travels", got)
	}
	if got := status(machineRow.Memory.ID); got != MemorySuperseded {
		t.Fatalf("the machine row folded to %q, want superseded", got)
	}
	if got := status(fresh.ID); got != MemoryActive {
		t.Fatalf("the replacement folded to %q, want active", got)
	}
	if got := status("secret"); got != "absent" {
		// The refused row never landed, under any spelling of its id.
		row, _, _, _ := receiver.ExportMemoryEvents(0, 1)
		_ = row
	}

	// RE-DELIVERY IS A NO-OP, ALL OF IT — the fold is idempotent end to end.
	secondApplied, secondSkipped := apply()
	if secondApplied != 0 {
		t.Fatalf("re-delivery changed the store %d times, want 0", secondApplied)
	}
	if secondSkipped != 5 {
		t.Fatalf("re-delivery skipped %d, want all 5", secondSkipped)
	}
	if secondApplied+secondSkipped != 5 {
		t.Fatalf("re-delivery accounted for %d of 5 events", secondApplied+secondSkipped)
	}

	// AND THE RECEIVER'S OWN REBUILD AGREES WITH ITS VIEWS: the re-journaled
	// events replay to the same place.
	if err := receiver.Rebuild(); err != nil {
		t.Fatalf("rebuild after fold: %v", err)
	}
	if got := status(fresh.ID); got != MemoryActive {
		t.Fatalf("the replacement after rebuild = %q, want active", got)
	}
	if got := status(userRow.Memory.ID); got != MemoryForgotten {
		t.Fatalf("the tombstone after rebuild = %q, want forgotten", got)
	}
}

// A tombstone must protect against stale-device resurrection: a device that
// stopped before a forget, and delivers its old add afterwards, does not bring
// the memory back.
func TestAStaleDevicesLateAddDoesNotResurrectAForgottenMemory(t *testing.T) {
	origin := openTestStore(t, filepath.Join(t.TempDir(), "stale-origin.db"))
	receiver := openTestStore(t, filepath.Join(t.TempDir(), "stale-receiver.db"))

	row, err := origin.Write(WriteRequest{Owner: OwnerUser, Type: MemoryFact,
		Title: "Deploys on Fridays", Text: "Deploys go out on Friday afternoons."})
	if err != nil {
		t.Fatal(err)
	}
	// The receiver takes the add; the origin then forgets, and re-delivers
	// EVERYTHING — including the old add — to a receiver that stopped in
	// between.
	firstBatch, _, _, err := origin.ExportMemoryEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.ApplyMemoryEvents(firstBatch, nil); err != nil {
		t.Fatal(err)
	}
	if err := origin.ForgetMemory(row.Memory.ID); err != nil {
		t.Fatal(err)
	}
	laterBatch, _, _, err := origin.ExportMemoryEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	result, err := receiver.ApplyMemoryEvents(laterBatch, nil)
	if err != nil {
		t.Fatalf("late delivery: %v", err)
	}
	if result.Applied != 1 || result.Skipped != 1 {
		t.Fatalf("late delivery = %d applied, %d skipped, want the forget applied and the add skipped", result.Applied, result.Skipped)
	}
	got, err := receiver.SearchMemories([]string{OwnerUser}, "Friday", 5)
	if err != nil || len(got) != 0 {
		t.Fatalf("the forgotten memory came back: (%v, %v)", memoryIDs(got), err)
	}
	record, ok, err := receiver.MemoryRecord(row.Memory.ID)
	if err != nil || !ok || record.Status != MemoryForgotten {
		t.Fatalf("the stale add resurrected the row: (%+v, %v, %v)", record, ok, err)
	}
}

// An owner policy is enforced on every event, not only on adds: an update
// naming another owner's row cannot move it.
func TestApplyRefusesAnOwnersRowsItDoesNotHold(t *testing.T) {
	origin := openTestStore(t, filepath.Join(t.TempDir(), "policy-origin.db"))
	receiver := openTestStore(t, filepath.Join(t.TempDir(), "policy-receiver.db"))

	row, err := origin.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference,
		Title: "Wants terse replies", Text: "Answers short."})
	if err != nil {
		t.Fatal(err)
	}
	if err := origin.UpdateMemoryFromSession(row.Memory.ID, "Wants terse replies",
		"Answers very short.", nil, "session-x"); err != nil {
		t.Fatal(err)
	}
	events, _, _, err := origin.ExportMemoryEvents(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	// The receiver holds rows under NO owner at all — every event must skip,
	// because the row the update names is not the receiver's.
	result, err := receiver.ApplyMemoryEvents(events, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The add lands (no owner conflict), the update cannot: the row landed
	// under its own owner, and the fold matches owners.
	if result.Applied != 2 || result.Skipped != 0 {
		t.Fatalf("first fold = %+v, want both applied to the same owner", result)
	}
	if got, err := receiver.GetMemories([]string{OwnerUser}, []string{row.Memory.ID}); err != nil || len(got) != 1 || got[0].Text != "Answers very short." {
		t.Fatalf("folded row = (%+v, %v)", got, err)
	}
}

// THE ROW'S OWN OWNER IS WHAT GATES AN OWNERLESS EVENT. Update, forget and
// restore payloads carry no owner — they act on a row that is already here —
// so the receiver's policy is asked about the row, never about a default. A
// policy that refuses a project must not be walked around by an event that
// stayed silent about whose row it touches.
func TestAnOwnerlessEventIsGatedByTheRowsOwnOwner(t *testing.T) {
	receiver := openTestStore(t, filepath.Join(t.TempDir(), "gate-receiver.db"))

	projectRow, err := receiver.Write(WriteRequest{Owner: OwnerProject("alpha"), Type: MemoryFact,
		Title: "Alpha's deploy window", Text: "Alpha deploys after the freeze lifts."})
	if err != nil {
		t.Fatal(err)
	}
	update := MemoryEvent{Seq: 1, Kind: EventMemoryUpdate, Payload: json.RawMessage(`{"id":"` + projectRow.Memory.ID + `","title":"Alpha's deploy window","text":"Alpha deploys whenever."}`)}
	onlyUser := func(owner string) bool { return owner == OwnerUser }
	result, err := receiver.ApplyMemoryEvents([]MemoryEvent{update}, onlyUser)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 0 || result.Skipped != 1 {
		t.Fatalf("a refused owner's row folded = %+v, want the update skipped", result)
	}
	got, err := receiver.GetMemories([]string{OwnerProject("alpha")}, []string{projectRow.Memory.ID})
	if err != nil || len(got) != 1 || got[0].Text != "Alpha deploys after the freeze lifts." {
		t.Fatalf("the refused update changed the row anyway: (%+v, %v)", got, err)
	}

	// THE SAME EVENT, NO POLICY: the row's own owner carries it, which is the
	// replay-compatible answer.
	result, err = receiver.ApplyMemoryEvents([]MemoryEvent{update}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 1 {
		t.Fatalf("an unrefused update folded = %+v, want applied", result)
	}
}

// A RE-HOME FROM A REMOTE MOVES ONLY WHAT IS STILL QUARANTINED. The event's
// ids are a claim about rows the receiver may hold under other owners, and a
// fold that stripped a live owner because an event named the id would be a
// second, wider permission model.
func TestARehomedEventMovesOnlyQuarantinedRows(t *testing.T) {
	receiver := openTestStore(t, filepath.Join(t.TempDir(), "rehome-receiver.db"))

	kept, err := receiver.Write(WriteRequest{Owner: OwnerUser, Type: MemoryPreference,
		Title: "Wants terse replies", Text: "Answers short."})
	if err != nil {
		t.Fatal(err)
	}
	quarantined, err := receiver.AddMemory(Memory{Type: MemoryFact, Scope: MemoryScopeProject,
		Title: "Old build's note", Text: "A row nobody can prove the project of."})
	if err != nil {
		t.Fatal(err)
	}
	events := []MemoryEvent{{
		Seq: 1, Kind: EventMemoryRehomed,
		Payload: json.RawMessage(`{"ids":["` + kept.Memory.ID + `","` + quarantined.ID + `"],"owner":"` + OwnerProject("recovered") + `"}`),
	}}
	result, err := receiver.ApplyMemoryEvents(events, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied != 1 || result.Skipped != 0 {
		t.Fatalf("the rehome folded = %+v, want applied once (the quarantined row)", result)
	}
	moved, err := receiver.GetMemories([]string{OwnerProject("recovered")}, []string{quarantined.ID})
	if err != nil || len(moved) != 1 {
		t.Fatalf("the quarantined row did not move: (%v, %v)", moved, err)
	}
	still, err := receiver.GetMemories([]string{OwnerUser}, []string{kept.Memory.ID})
	if err != nil || len(still) != 1 || still[0].Owner != OwnerUser {
		t.Fatalf("the rehome stripped a live owner: (%+v, %v)", still, err)
	}
}
