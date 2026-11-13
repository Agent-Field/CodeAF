package clusterstate

import (
	"crypto/sha256"
	"encoding/hex"
)

// Assignment routing is public; instructions, credentials and task descriptors
// stay inside SealedPayload. A target is always selected by an authorized client.
type Assign struct {
	ID            string           `json:"id"`
	Revision      uint64           `json:"revision"`
	Cell          string           `json:"cell"`
	ParentCell    string           `json:"parent_cell"`
	Expect        LeaseExpectation `json:"expect"`
	Target        string           `json:"target"`
	Incarnation   uint64           `json:"incarnation"`
	Expires       int64            `json:"expires"`
	SealedPayload []byte           `json:"sealed_payload"`
}

// AssignmentRef repeats routing dependencies so evaluation never discovers an
// undeclared row after the host has fixed its immutable application read-set.
type AssignmentRef struct {
	ID          string `json:"id"`
	Revision    uint64 `json:"revision"`
	Cell        string `json:"cell"`
	Target      string `json:"target"`
	Incarnation uint64 `json:"incarnation"`
}
type ClaimAssignment struct {
	Ref    AssignmentRef    `json:"ref"`
	Expect LeaseExpectation `json:"expect"`
}
type FinishAssignment struct {
	Ref   AssignmentRef `json:"ref"`
	Fence uint64        `json:"fence"`
	Head  string        `json:"head"`
}
type CancelAssignment struct {
	Ref AssignmentRef `json:"ref"`
}

type Assignment struct {
	Spec   Assign                  `json:"spec"`
	Status string                  `json:"status"`
	Fence  uint64                  `json:"fence"`
	Head   string                  `json:"head"`
	Claim  *AssignmentClaimReceipt `json:"claim,omitempty"`
}

// AssignmentClaimReceipt preserves a completed claim's reply without retaining
// a live session or duplicating the assignment's immutable specification.
type AssignmentClaimReceipt struct {
	Digest  string   `json:"digest"`
	View    CellView `json:"view"`
	Now     int64    `json:"now"`
	Version uint64   `json:"version"`
}

func (a Assignment) claimResult() Result {
	r := a.Claim
	a.Claim = nil
	a.Status = "claimed"
	a.Fence = r.View.Cell.Lease.Fence
	a.Head = r.View.Cell.Head
	return Result{Schema: SchemaVersion, Code: OK, Now: r.Now, Version: r.Version, View: &r.View, Assignment: &a}
}

func (a Assignment) Ref() AssignmentRef {
	v := a.Spec
	return AssignmentRef{ID: v.ID, Revision: v.Revision, Cell: v.Cell, Target: v.Target, Incarnation: v.Incarnation}
}

// The per-cell slot bounds active scheduling independently of retained runs.
// A completed/cancelled slot is reusable, but its original run is never deleted.
type AssignmentSlot struct {
	Ref    AssignmentRef `json:"ref"`
	Active bool          `json:"active"`
}
type WorkerHold struct {
	Incarnation uint64 `json:"incarnation"`
	Fence       uint64 `json:"fence"`
}

func AssignmentKey(id string) RecordKey { return RecordKey{"state", "assignment/" + segment(id)} }

// WorkerAssignmentSession binds a saved request to one assignment attempt.
func WorkerAssignmentSession(kind string, ref AssignmentRef) string {
	b, _ := Encode(ref)
	h := sha256.Sum256(b)
	return "worker-" + kind + "-" + hex.EncodeToString(h[:])
}

// Assignment-owned retry rows let replacement and cancellation release the
// previous claimant without discovering its process or session name.
func assignmentSessionKey(id string) RecordKey {
	return RecordKey{"sessions", "assignment/" + segment(id)}
}

func workerAssignmentSession(c Command) bool {
	kind := "claim"
	if c.Change.FinishAssignment != nil {
		kind = "finish"
	} else if c.Change.ClaimAssignment == nil {
		return false
	}
	return c.Session == WorkerAssignmentSession(kind, assignmentRef(c.Change))
}
func AssignmentSlotKey(cell string) RecordKey {
	return RecordKey{"state", "assignment-slot/" + segment(cell)}
}
func WorkerHoldKey(worker, cell string) RecordKey {
	return RecordKey{"state", "worker-hold/" + segment(worker) + "/" + segment(cell)}
}
func assignmentMutation(m Mutation) bool {
	return m.Assign != nil || m.Reassign != nil || m.ClaimAssignment != nil || m.FinishAssignment != nil || m.CancelAssignment != nil
}
func assignmentRef(m Mutation) AssignmentRef {
	if m.Assign != nil {
		return (Assignment{Spec: *m.Assign}).Ref()
	}
	if m.Reassign != nil {
		return (Assignment{Spec: *m.Reassign}).Ref()
	}
	if m.ClaimAssignment != nil {
		return m.ClaimAssignment.Ref
	}
	if m.FinishAssignment != nil {
		return m.FinishAssignment.Ref
	}
	if m.CancelAssignment != nil {
		return m.CancelAssignment.Ref
	}
	return AssignmentRef{}
}
func assignmentDependencies(c Command) []RecordKey {
	var keys []RecordKey
	if assignmentMutation(c.Change) {
		r := assignmentRef(c.Change)
		keys = append(keys, AssignmentKey(r.ID), AssignmentSlotKey(r.Cell), CellKey(r.Cell), WorkerKey(r.Target), AuthorityKey(r.Target), DeviceKey(r.Target), WorkerHoldKey(r.Target, r.Cell), assignmentSessionKey(r.ID))
		v := c.Change.Assign
		if v == nil {
			v = c.Change.Reassign
		}
		if v != nil && v.ParentCell != "" {
			keys = append(keys, CellKey(v.ParentCell))
		}
	}
	for _, id := range cellIDs(c.Change) {
		keys = append(keys, WorkerHoldKey(c.Actor, id))
	}
	return keys
}

func applyAssignment(c Command, now int64, identity IdentityRec, rows map[RecordKey][]byte, put func(RecordKey, any), result *Result) Code {
	m := c.Change
	r := assignmentRef(m)
	if r.ID == "" || r.Cell == "" || r.Target == "" || r.Revision == 0 || r.Incarnation == 0 {
		return BadRequest
	}
	var a Assignment
	var slot AssignmentSlot
	var cell Cell
	if rows[CellKey(r.Cell)] == nil {
		return NotFound
	}
	if Decode(rows[CellKey(r.Cell)], &cell) != nil {
		return BadRequest
	}
	if b := rows[AssignmentKey(r.ID)]; b != nil {
		if Decode(b, &a) != nil {
			return BadRequest
		}
	}
	if b := rows[AssignmentSlotKey(r.Cell)]; b != nil {
		if Decode(b, &slot) != nil {
			return BadRequest
		}
	}
	v := m.Assign
	if v == nil {
		v = m.Reassign
	}
	if v != nil {
		if m.Assign != nil {
			if rows[AssignmentKey(r.ID)] != nil {
				return Exists
			}
			if r.Revision != 1 {
				return Conflict
			}
		} else {
			if rows[AssignmentKey(r.ID)] == nil {
				return NotFound
			}
			if a.Spec.Revision == ^uint64(0) || r.Revision != a.Spec.Revision+1 || a.Spec.Cell != r.Cell || a.Spec.ParentCell != v.ParentCell || a.Status == "finished" || a.Status == "cancelled" {
				return Conflict
			}
		}
		if slot.Active && (m.Assign != nil || slot.Ref.ID != r.ID || slot.Ref.Revision != a.Spec.Revision) {
			return Conflict
		}
		if v.ParentCell == r.Cell || cell.ParentCell != v.ParentCell {
			return BadRequest
		}
		if v.ParentCell != "" && rows[CellKey(v.ParentCell)] == nil {
			return NotFound
		}
		if v.Expires <= now || v.Expires > int64(MaxSafeInteger) || len(v.SealedPayload) == 0 || len(v.SealedPayload) > MaxWorkerSealedBytes {
			return BadRequest
		}
		if cell.Lease.Device != v.Expect.Device || cell.Lease.Fence != v.Expect.Fence {
			return LeaseHeld
		}
		auth, w, code := workerFromRows(c.Cluster, r.Target, now, identity, rows)
		if code != OK {
			return code
		}
		var d Device
		if b := rows[DeviceKey(r.Target)]; b != nil {
			_ = Decode(b, &d)
		}
		if code = WorkerCanClaim(c.Cluster, r.Target, r.Cell, r.Incarnation, now, identity, auth, d, w); code != OK {
			return code
		}
		a = Assignment{Spec: *v, Status: "pending"}
		put(AssignmentKey(r.ID), a)
		put(AssignmentSlotKey(r.Cell), AssignmentSlot{Ref: r, Active: true})
		result.Assignment = &a
		return OK
	}
	if rows[AssignmentKey(r.ID)] == nil {
		return NotFound
	}
	if a.Ref() != r || slot.Ref != r {
		return Conflict
	}
	if m.CancelAssignment != nil {
		if a.Status == "finished" {
			return Conflict
		}
		a.Status = "cancelled"
		slot.Active = false
	} else if m.ClaimAssignment != nil {
		if c.Actor != r.Target || c.WorkerIncarnation != r.Incarnation {
			return Unauthorized
		}
		if a.Status != "pending" || !slot.Active || a.Spec.Expires <= now {
			return Conflict
		}
		auth, w, code := workerFromRows(c.Cluster, r.Target, now, identity, rows)
		if code != OK {
			return code
		}
		var d Device
		if b := rows[DeviceKey(r.Target)]; b != nil {
			_ = Decode(b, &d)
		}
		if code = WorkerCanClaim(c.Cluster, r.Target, r.Cell, r.Incarnation, now, identity, auth, d, w); code != OK {
			return code
		}
		if m.ClaimAssignment.Expect != a.Spec.Expect {
			return LeaseHeld
		}
		if cell.Lease.Fence == ^uint64(0) {
			return Capacity
		}
		next, err := Acquire(cell, c.Actor, now, true, &a.Spec.Expect)
		if err != nil {
			return LeaseHeld
		}
		cell = next
		a.Status = "claimed"
		a.Fence = cell.Lease.Fence
		a.Head = cell.Head
		put(CellKey(r.Cell), cell)
		put(WorkerHoldKey(r.Target, r.Cell), WorkerHold{Incarnation: r.Incarnation, Fence: a.Fence})
		result.View = &CellView{Now: now, Cell: cell}
	} else if m.FinishAssignment != nil {
		v := m.FinishAssignment
		if c.Actor != r.Target || c.WorkerIncarnation != r.Incarnation {
			return Unauthorized
		}
		// Completion is retained on the assignment after its retry row is freed.
		if workerAssignmentSession(c) && a.Status == "finished" && a.Fence == v.Fence && a.Head == v.Head {
			result.Assignment = &a
			return OK
		}
		if a.Status != "claimed" || !slot.Active {
			return Conflict
		}
		if cell.Lease.Device != r.Target || cell.Lease.Fence != v.Fence || a.Fence != v.Fence {
			return FenceStale
		}
		if cell.Head != v.Head || v.Head == "" {
			return HeadMoved
		}
		a.Status = "finished"
		a.Head = v.Head
		slot.Active = false
	} else {
		return BadRequest
	}
	put(AssignmentKey(r.ID), a)
	put(AssignmentSlotKey(r.Cell), slot)
	result.Assignment = &a
	return OK
}

// AssignmentRead exposes only the selected run and its selected chat grant.
type AssignmentRead struct {
	Code       Code        `json:"code"`
	Assignment *Assignment `json:"assignment,omitempty"`
	View       *CellView   `json:"view,omitempty"`
	Grant      []byte      `json:"grant,omitempty"`
}

func AssignmentReadKeys(worker string, ref AssignmentRef) []RecordKey {
	return append(WorkerReadKeys(worker, ref.Cell), AssignmentKey(ref.ID))
}

// ReadWorkerAssignment runs at the host's current committed time and fresh
// quorum barrier. Revoked/expired targets expose no saved grant or execution
// permission, even when a raw pending row is retained for recovery.
func ReadWorkerAssignment(cluster, worker string, incarnation uint64, ref AssignmentRef, now int64, read ReadRecord) (AssignmentRead, error) {
	size := 0
	bounded := func(k RecordKey) ([]byte, error) {
		b, e := read(k)
		size += len(k.Namespace) + len(k.Key) + len(b)
		if size > MaxEvaluationBytes {
			return nil, ErrDependency
		}
		return b, e
	}
	selected, err := ReadWorkerCell(cluster, worker, ref.Cell, incarnation, now, bounded)
	if err != nil {
		return AssignmentRead{}, err
	}
	if selected.Code != OK {
		return AssignmentRead{Code: selected.Code}, nil
	}
	raw, err := bounded(AssignmentKey(ref.ID))
	if err != nil {
		return AssignmentRead{}, err
	}
	if raw == nil {
		return AssignmentRead{Code: NotFound}, nil
	}
	var a Assignment
	if Decode(raw, &a) != nil {
		return AssignmentRead{}, ErrDependency
	}
	if a.Ref() != ref || ref.Target != worker || ref.Incarnation != incarnation {
		return AssignmentRead{Code: Conflict}, nil
	}
	if a.Status == "pending" && a.Spec.Expires <= now {
		a.Status = "expired"
	}
	return AssignmentRead{Code: OK, Assignment: &a, View: selected.View, Grant: selected.Grant}, nil
}
