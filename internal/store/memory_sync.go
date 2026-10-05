package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The memory sync seam. THESE TWO METHODS ARE THE WHOLE OF WHAT SYNC NEEDS
// from the store, and neither of them knows what a transport is: one reads the
// journal's memory events after a cursor, the other folds events that arrived.
// PR #1738's sealed-object transport will be the thing that moves these
// batches; until a transport exists NOTHING calls them except tests — which is
// exactly what "ready" means here.
//
// The journal is already the replication log this needs:
//
//   - events are append-only and immutable, so export is a scan, not a merge;
//   - every payload carries the owner, so the applying side can enforce its
//     own visibility policy before it lands anything;
//   - memory rows are only ever written by these same event kinds, so an
//     applied event and a local write are indistinguishable to every reader —
//     including Rebuild, which replays the journal and reproduces the fold.
//
// WHAT THIS SEAM DOES NOT DECIDE: which owners may arrive (the caller's policy
// answers that), who pairs with whom, how events travel, or whether a remote
// device is who it says it is. Transport and trust are Phase 4's; the fold is
// here because the fold is the only part that must be identical on every
// device.
//
// ANALYTICS NEVER MASQUERADE AS REPLICATION. Ranking snapshots
// ([EventMemoryRanking]) and the outcome counters are this store's own
// evidence about its own model calls; they travel nowhere, because another
// device's evidence about ITS calls is not a fact about this one.

// MemoryEvent is one journal entry on the wire: the Event a store carries,
// with nothing local added. The sequence is the EXPORTING store's own — a
// receiver keeps cursors per origin and never re-uses a foreign seq as its own.
type MemoryEvent struct {
	Seq     int64
	Time    string
	NodeID  string
	Kind    EventKind
	Payload json.RawMessage
}

// memorySyncKinds are the kinds a memory sync moves. Every one of them is a
// kind whose payload the replay already understands, which is what makes an
// applied batch and a replayed journal the same fold.
func memorySyncKinds() []EventKind {
	return []EventKind{
		EventMemoryAdd, EventMemoryUpdate, EventMemorySupersede,
		EventMemoryForget, EventMemoryRestore,
		EventMemorySkipped, EventMemoryWriteFailed, EventMemoryRehomed,
	}
}

// ExportMemoryEvents reads the journal's memory events after a cursor, oldest
// first, bounded. The returned cursor is the last event's seq: hand it back on
// the next call and the export continues exactly where it stopped. A batch
// smaller than the limit has more=false — the export is drained.
func (s *Store) ExportMemoryEvents(after int64, limit int) ([]MemoryEvent, int64, bool, error) {
	if limit <= 0 {
		limit = 500
	}
	kinds := memorySyncKinds()
	placeholders := ""
	args := make([]any, 0, len(kinds)+2)
	args = append(args, after)
	for _, kind := range kinds {
		if placeholders != "" {
			placeholders += ", "
		}
		placeholders += "?"
		args = append(args, string(kind))
	}
	args = append(args, limit+1)
	rows, err := s.db.Query(`
		SELECT seq, ts, node_id, kind, payload
		FROM events
		WHERE seq > ? AND kind IN (`+placeholders+`)
		ORDER BY seq LIMIT ?`, args...)
	if err != nil {
		return nil, 0, false, fmt.Errorf("export memory events: %w", err)
	}
	defer rows.Close()
	events := make([]MemoryEvent, 0, limit)
	last := after
	for rows.Next() {
		var event MemoryEvent
		var timestamp, payload string
		if err := rows.Scan(&event.Seq, &timestamp, &event.NodeID, &event.Kind, &payload); err != nil {
			return nil, 0, false, fmt.Errorf("export memory events: %w", err)
		}
		event.Time = timestamp
		event.Payload = json.RawMessage(payload)
		events = append(events, event)
		last = event.Seq
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, fmt.Errorf("export memory events: %w", err)
	}
	more := len(events) > limit
	if more {
		events = events[:limit]
		last = events[len(events)-1].Seq
	}
	return events, last, more, nil
}

// AppliedMemoryEvents is what ApplyMemoryEvents did with a batch.
type AppliedMemoryEvents struct {
	// Applied counts events that changed this store.
	Applied int
	// Skipped counts events that arrived as no-ops: an id already here, a
	// tombstone that already stands, a supersession whose target this store
	// retired first, a change outside the receiver's owner policy. Skipped is
	// SUCCESS — it is what makes re-delivery safe and what makes the fold
	// deterministic instead of conflict-free-by-fiat.
	Skipped int
}

// ApplyMemoryEvents folds a batch from another store into this one, in the
// order the batch carries.
//
// THE FOLD RULES ARE DETERMINISTIC AND STRUCTURAL, and they are exactly the
// replay rules the local journal already follows:
//
//   - add of an id this store already holds is a skip (idempotent re-delivery);
//   - update and supersede apply only against an ACTIVE row, and only within
//     the owner the event or the row itself names — a store that has retired
//     the target first is not overwritten back to life by a slower delivery;
//   - forget is a tombstone and applies to any row that is not already
//     forgotten, or retains an owner-proven pending tombstone; a later add
//     checks both existing rows and pending tombstones;
//   - restore brings back only a forgotten row;
//   - re-home moves only a row that is still quarantined, exactly what the
//     live door would have moved.
//
// Conflicting updates use arrival order and can leave devices with different
// bodies if they fold opposite orders. This seam does not claim convergence.
// Delivery receipts prevent repeats from undoing subsequent local work.
//
// Each applied event is RE-JOURNALED under the receiving store's own sequence
// with its original payload, so the fold is durable, replayable, and
// indistinguishable in shape from local history. A skipped event is journaled
// NOWHERE — a no-op is not history, and the transaction that journaled it
// rolls back with the fold.
//
// The allow function is the RECEIVER's visibility policy, decided before
// anything lands: an owner the policy refuses is skipped, never written under
// a widened owner. For update, forget and restore of an existing row, policy
// is asked about THE ROW'S OWN owner, read in the same transaction. New forget
// events also carry owner proof, so an unknown-id tombstone may be journaled
// first and suppress a later add. Legacy ownerless unknown tombstones are
// skipped because their policy cannot be proved. A nil policy allows every
// owner, which is the local default — a store moving its own
// journal forward in a test.
func (s *Store) ApplyMemoryEvents(events []MemoryEvent, allow func(owner string) bool) (AppliedMemoryEvents, error) {
	var result AppliedMemoryEvents
	for _, event := range events {
		applied, err := s.applyOneMemoryEvent(event, allow)
		if err != nil {
			return result, fmt.Errorf("apply memory event %d (%s): %w", event.Seq, event.Kind, err)
		}
		if applied {
			result.Applied++
		} else {
			result.Skipped++
		}
	}
	return result, nil
}

func (s *Store) applyOneMemoryEvent(event MemoryEvent, allow func(owner string) bool) (bool, error) {
	if !eventSyncable(event.Kind) {
		// A LATER BUILD'S KINDS ARE NOT SILENTLY DROPPED. They arrive here as
		// an error, so an old build receiving a new journal reports it instead
		// of losing a change nobody will ever account for.
		return false, fmt.Errorf("%w: %q is not a memory event", ErrInvalid, event.Kind)
	}
	// THE OWNER IS READ BEFORE ANYTHING ELSE, because the policy is what makes
	// a receiver more than a dump. An event whose payload cannot say its owner
	// is an event from before the column existed; it is owned the way the
	// replay owns such a row, from its scope. The kinds that act on a row that
	// is already here (update, forget, restore) are checked INSIDE the fold,
	// against the row's own owner — a payload that names no owner cannot be
	// gated here without inventing one.
	if ownerBearingKind(event.Kind) {
		if allow != nil && !allow(memoryEventOwner(Event{Kind: event.Kind, Payload: event.Payload})) {
			return false, nil
		}
	}
	tx, err := s.beginWrite()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Delivery identity lives in the journal so rebuilding the views does not
	// forget which updates already arrived. Forwarders preserve an existing
	// identity rather than minting a new one on every replication hop.
	deliveryID, err := memoryDeliveryID(event)
	if err != nil {
		return false, err
	}
	seen, err := memoryDeliverySeen(tx, event, deliveryID)
	if err != nil {
		return false, err
	}
	if seen {
		return false, nil
	}

	// The event is journaled under THIS store's sequence first; the fold below
	// stamps its views with that sequence. A skip rolls the journal entry back
	// with the rest of the transaction — a no-op is not history.
	seq, _, err := appendEvent(tx, deliveryID, event.Kind, event.Payload)
	if err != nil {
		return false, err
	}
	local := Event{Seq: seq, NodeID: deliveryID, Kind: event.Kind, Payload: event.Payload}
	applied, err := foldMemoryEvent(tx, local, seq, s.fts, allow)
	if err != nil {
		return false, err
	}
	if !applied {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// ownerBearingKind answers whether an event's payload itself names the owner
// the policy is asked about. Add, supersede and re-home create or re-own
// rows, so their payloads carry the owner; update, forget and restore act on
// rows that already exist here and carry none.
func ownerBearingKind(kind EventKind) bool {
	switch kind {
	case EventMemoryAdd, EventMemorySupersede, EventMemoryRehomed:
		return true
	}
	return false
}

// foldMemoryEvent applies ONE memory event to the views, idempotently, and
// answers whether it changed anything. It enforces sync policy and receipt
// rules before reaching the same apply functions that Rebuild uses. Only
// admitted events enter the journal; Rebuild repeats their materialization.
func foldMemoryEvent(tx *sql.Tx, event Event, seq int64, fts bool, allow func(owner string) bool) (bool, error) {
	switch event.Kind {
	case EventMemoryAdd:
		var payload memoryPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return false, err
		}
		payload.Owner = ownerForReplay(payload)
		// A tombstone that arrived first remains in the journal under its proven
		// owner. A later add cannot turn delayed delivery into resurrection.
		pending, err := pendingMemoryTombstone(tx, payload.ID, payload.Owner, seq)
		if err != nil {
			return false, err
		}
		if pending {
			return false, nil
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM memories WHERE id = ?`, payload.ID).Scan(&exists); err != nil {
			return false, err
		}
		if exists != 0 {
			// ALREADY HERE. An add that re-arrives is re-delivery, and
			// re-delivery is a skip — never a second row, never a resurrection
			// of something this store has since retired.
			return false, nil
		}
		return true, applyMemoryAdd(tx, payload, seq, fts)
	case EventMemoryUpdate:
		var payload memoryUpdatePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return false, err
		}
		// THE GUARD'S OWNER IS THE ROW'S OWN. An update event carries no owner
		// — it acts on a row that is already here, under whatever owner it
		// arrived with — so the row itself is read and the receiver's policy
		// is asked about THAT. A user-labeled context must never be allowed to
		// rewrite a project's row: an event that cannot say whose row it
		// touches is gated by the row, not by a default.
		rowOwner, active := memoryRowOwner(tx, payload.ID)
		if !active {
			return false, nil
		}
		if allow != nil && !allow(rowOwner) {
			return false, nil
		}
		return true, applyMemoryUpdate(tx, payload, seq, fts)
	case EventMemorySupersede:
		var payload memorySupersedePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return false, err
		}
		payload.New.Owner = ownerForReplay(payload.New)
		// THE GUARD'S OWNER IS THE REPLACEMENT'S — a supersede is one decision
		// about the replacement's owner, and the wrapper payload does not carry
		// the owner at the top level for the policy read above to find.
		if !memoryActiveForOwner(tx, payload.OldID, payload.New.Owner) {
			// THE TARGET IS ALREADY GONE HERE — retired or forgotten by this
			// store's own history or by a faster delivery. Applying would
			// resurrect a retired row or add an orphan; skipping is the fold
			// rule, and the chain stays consistent with it.
			return false, nil
		}
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM memories WHERE id = ?`, payload.New.ID).Scan(&exists); err != nil {
			return false, err
		}
		if exists != 0 {
			return false, nil
		}
		return true, applyMemorySupersede(tx, payload, seq, fts)
	case EventMemoryForget:
		var payload memoryForgetPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return false, err
		}
		var status, rowOwner string
		err := tx.QueryRow(`SELECT owner, status FROM memories WHERE id = ?`, payload.ID).Scan(&rowOwner, &status)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// Only a tombstone that proves its owner can be kept before its row.
			// Legacy ownerless events cannot pass a receiver policy by guessing.
			owner := normalizeOwner(payload.Owner)
			if !ValidOwner(owner) || strings.TrimSpace(payload.ID) == "" {
				return false, nil
			}
			if allow != nil && !allow(owner) {
				return false, nil
			}
			return true, nil
		case err != nil:
			return false, err
		case status == MemoryForgotten:
			return false, nil
		}
		if payload.Owner != "" && normalizeOwner(payload.Owner) != rowOwner {
			return false, nil
		}
		// THE POLICY IS ASKED ABOUT THE ROW'S OWN OWNER — the payload names no
		// owner, and a tombstone the policy would refuse must not land because
		// the payload stayed silent.
		if allow != nil && !allow(rowOwner) {
			return false, nil
		}
		return true, applyMemoryForget(tx, payload, seq, fts)
	case EventMemoryRestore:
		var payload memoryRestorePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return false, err
		}
		var status, rowOwner string
		err := tx.QueryRow(`SELECT owner, status FROM memories WHERE id = ?`, payload.ID).Scan(&rowOwner, &status)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return false, nil
		case err != nil:
			return false, err
		case status != MemoryForgotten:
			return false, nil
		}
		if allow != nil && !allow(rowOwner) {
			return false, nil
		}
		return true, applyMemoryRestore(tx, payload, seq, fts)
	case EventMemoryRehomed:
		// A RE-HOME FROM THE ORIGIN APPLIES, policy-gated at the top of the
		// fold: the origin proved the row's project, and a receiver that
		// refused the news would keep a row in quarantine the origin has since
		// re-homed — divergence with no upside. The fold under it only ever
		// moves a row that is STILL quarantined ([applyRehomeOne]), so a
		// re-home can never strip an owner it disagrees with.
		//
		// THE TARGET OWNER IS VALIDATED, same as the live door. A re-home
		// event carrying an owner this build does not mint is refused: moving
		// a quarantined row to an invalid owner would be a quarantine escape.
		var payload struct {
			IDs []string `json:"ids"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return false, err
		}
		targetOwner := memoryEventOwner(event)
		if !ValidOwner(targetOwner) {
			// An owner this build does not understand is refused, not widened.
			return false, nil
		}
		// A RE-HOME TO QUARANTINE IS NOT A RE-HOME. The live door never moves a
		// row from quarantine to quarantine; the fold must agree. A row already
		// in quarantine that an event tries to "re-home" to OwnerLegacyProject
		// stays exactly where it is.
		if targetOwner == OwnerLegacyProject {
			return false, nil
		}
		moved := 0
		for _, id := range payload.IDs {
			applied, err := applyRehomeOne(tx, id, targetOwner, seq)
			if err != nil {
				return false, err
			}
			if applied {
				moved++
			}
		}
		return moved > 0, nil
	default:
		// Skipped and write-failed are observability events about the SENDING
		// device's own attempts: they say what that device went through, and
		// applying them here would manufacture local views out of somebody
		// else's attempt — and a no-op is not history, so the journal entry
		// the fold's caller wrote rolls back with the skip.
		return false, nil
	}
}

// memoryRowOwner answers the owner of an ACTIVE row, and whether it is active
// at all. It is the guard an in-place fold walks before it writes: an update
// that cannot find an active row changes nothing, and the owner it answers is
// what the receiver's policy is asked about.
func memoryRowOwner(tx *sql.Tx, id string) (string, bool) {
	var owner, status string
	if err := tx.QueryRow(`SELECT owner, status FROM memories WHERE id = ?`, id).Scan(&owner, &status); err != nil {
		return "", false
	}
	return owner, status == MemoryActive
}

// memoryActiveForOwner answers whether an id is an active row under the
// event's own owner — the read a fold must do before an in-place change, so no
// receiver can move another owner's row. Applied from a transaction, it takes
// the querier rather than the store.
func memoryActiveForOwner(tx *sql.Tx, id, owner string) bool {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM memories WHERE id = ? AND owner = ? AND status = ?`,
		id, owner, MemoryActive).Scan(&count); err != nil {
		return false
	}
	return count == 1
}

// eventSyncable answers whether an event kind is one the fold understands at
// all. Kinds this build does not know are refused loudly by the apply door —
// see [Store.ApplyMemoryEvents] — while the replay treats an unknown kind as
// the error it has always been.
func eventSyncable(kind EventKind) bool {
	for _, candidate := range memorySyncKinds() {
		if candidate == kind {
			return true
		}
	}
	return false
}

// memoryEventOwner reads the owner out of whichever payload shape the event
// carries, for the policy's sake. An event from before the column answers the
// way the replay answers it. An owner this build does not understand is
// quarantined — a policy gate that widens to user is not a gate.
func memoryEventOwner(event Event) string {
	if event.Kind == EventMemorySupersede {
		var payload memorySupersedePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return OwnerLegacyProject
		}
		return ownerForReplay(payload.New)
	}
	var payload memoryPayload
	if err := json.Unmarshal(event.Payload, &payload); err == nil {
		return ownerForReplay(payload)
	}
	// A payload this build cannot parse carries no owner. The quarantine is the
	// honest answer for a row whose provenance is unknown.
	return OwnerLegacyProject
}

// memoryDeliveryID preserves the originating event identity across forwarding.
func memoryDeliveryID(event MemoryEvent) (string, error) {
	if strings.HasPrefix(event.NodeID, "remote-memory:") {
		return event.NodeID, nil
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("remote-memory:%x", sha256.Sum256(encoded)), nil
}

// memoryDeliverySeen recognizes receipts and echoes of this store's own events.
// An originating store has no receipt yet, so matching payloads are checked
// against their original journal identity before an echo can undo newer work.
func memoryDeliverySeen(tx *sql.Tx, incoming MemoryEvent, deliveryID string) (bool, error) {
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM events WHERE node_id = ?", deliveryID).Scan(&count); err != nil {
		return false, err
	}
	if count != 0 {
		return true, nil
	}
	rows, err := tx.Query("SELECT seq, ts, node_id, kind, payload FROM events WHERE kind = ? AND payload = ?", incoming.Kind, string(incoming.Payload))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var candidate MemoryEvent
		var payload string
		if err := rows.Scan(&candidate.Seq, &candidate.Time, &candidate.NodeID, &candidate.Kind, &payload); err != nil {
			return false, err
		}
		candidate.Payload = json.RawMessage(payload)
		identity, err := memoryDeliveryID(candidate)
		if err != nil {
			return false, err
		}
		if identity == deliveryID {
			return true, nil
		}
	}
	return false, rows.Err()
}

// pendingMemoryTombstone compares owners using the same normalization as the
// policy guard. Raw remote payloads stay immutable in the journal, so equality
// on their unnormalized JSON owner would let whitespace undo a tombstone.
func pendingMemoryTombstone(tx *sql.Tx, id, owner string, before int64) (bool, error) {
	rows, err := tx.Query(`SELECT payload FROM events WHERE seq < ? AND kind = ? AND json_extract(payload, '$.id') = ?`, before, EventMemoryForget, id)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return false, err
		}
		var tombstone memoryForgetPayload
		if err := json.Unmarshal([]byte(encoded), &tombstone); err != nil {
			return false, err
		}
		if normalizeOwner(tombstone.Owner) == owner {
			return true, nil
		}
	}
	return false, rows.Err()
}
