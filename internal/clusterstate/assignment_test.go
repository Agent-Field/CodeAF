package clusterstate_test

import (
	"bytes"
	"fmt"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"math/rand"
	"reflect"
	"testing"
)

func assignmentFixture(t *testing.T) (*workerFixture, cs.Assign) {
	f := workerFixtureFor(t, false)
	expectWorker(t, f.register(0, 1001), cs.OK)
	expectWorker(t, f.beat(f.proof(1, 1, 1002), 1002), cs.OK)
	f.a.put(cs.CellKey("chat"), cs.Created(cs.CellInit{Head: "head"}, "a", 1000))
	return f, cs.Assign{ID: "opaque-run", Revision: 1, Cell: "chat", Expect: cs.LeaseExpectation{Device: "a", Fence: 1}, Target: f.id, Incarnation: 1, Expires: 10000, SealedPayload: sealedWorker(t, "private task /repo model-secret shell body")}
}
func ref(v cs.Assign) cs.AssignmentRef { return (cs.Assignment{Spec: v}).Ref() }
func assign(t *testing.T, f *workerFixture, v cs.Assign) cs.Assignment {
	t.Helper()
	r := f.owner(1003, cs.Mutation{Assign: &v})
	expectWorker(t, r, cs.OK)
	if r.Assignment == nil {
		t.Fatal(r)
	}
	return *r.Assignment
}
func claim(f *workerFixture, v cs.Assign) cs.Command {
	return f.command(f.id, f.key, v.Incarnation, 1004, cs.Mutation{ClaimAssignment: &cs.ClaimAssignment{Ref: ref(v), Expect: v.Expect}})
}

func TestR18R17AssignmentConditionalClaimAndExactRetry(t *testing.T) {
	f, v := assignmentFixture(t)
	assign(t, f, v)
	before := f.a.cell()
	other := v
	other.ID = "second"
	expectWorker(t, f.owner(1003, cs.Mutation{Assign: &other}), cs.Conflict)
	c := claim(f, v)
	r := f.eval(c)
	expectWorker(t, r, cs.OK)
	if r.Assignment.Status != "claimed" || r.View.Cell.Lease.Fence != 2 || r.View.Cell.Head != "head" || r.View.Cell.Lease.Device != f.id {
		t.Fatal(r)
	}
	if retry := f.eval(c); !reflect.DeepEqual(r, retry) {
		t.Fatal("claim retry changed exact result", r, retry)
	}
	if f.a.cell().Lease.Fence != 2 || before.Head != f.a.cell().Head {
		t.Fatal("claim changed head or double fenced")
	}
	expectWorker(t, f.owner(1005, cs.Mutation{Acquire: &cs.AcquireCell{Cell: "chat", Force: true, Expect: v.Expect}}), cs.LeaseHeld)
	expectWorker(t, f.run(1, 1005, cs.Mutation{FinishAssignment: &cs.FinishAssignment{Ref: ref(v), Fence: 2, Head: "unpublished"}}), cs.HeadMoved)
	expectWorker(t, f.run(1, 1005, cs.Mutation{FinishAssignment: &cs.FinishAssignment{Ref: ref(v), Fence: 2, Head: "head"}}), cs.OK)
	other.Expect = cs.LeaseExpectation{Device: f.id, Fence: 2}
	expectWorker(t, f.owner(1006, cs.Mutation{Assign: &other}), cs.OK)
	for _, b := range f.a.rows {
		for _, needle := range []string{"private task", "/repo", "model-secret", "shell body"} {
			if bytes.Contains(b, []byte(needle)) {
				t.Fatal("R15 assignment plaintext")
			}
		}
	}
}
func TestR18ClaimLosesToContinueCancelReassignIncarnationDrainRevokeExpiry(t *testing.T) {
	for _, event := range []string{"continue", "cancel", "reassign", "incarnation", "drain", "revoke", "expiry"} {
		t.Run(event, func(t *testing.T) {
			f, v := assignmentFixture(t)
			assign(t, f, v)
			c := claim(f, v)
			want := cs.Conflict
			switch event {
			case "continue":
				expectWorker(t, f.owner(1004, cs.Mutation{Acquire: &cs.AcquireCell{Cell: "chat", Force: true, Expect: v.Expect}}), cs.OK)
				want = cs.LeaseHeld
			case "cancel":
				expectWorker(t, f.owner(1004, cs.Mutation{CancelAssignment: &cs.CancelAssignment{Ref: ref(v)}}), cs.OK)
			case "reassign":
				next := v
				next.Revision++
				expectWorker(t, f.owner(1004, cs.Mutation{Reassign: &next}), cs.OK)
			case "incarnation":
				expectWorker(t, f.register(1, 1004), cs.OK)
			case "drain":
				expectWorker(t, f.owner(1004, cs.Mutation{DrainWorker: &cs.DrainWorker{Worker: f.id, Incarnation: 1}}), cs.OK)
			case "revoke":
				expectWorker(t, f.owner(1004, cs.Mutation{Revoke: &cs.DeviceID{Device: f.id}}), cs.OK)
				want = cs.Revoked
			case "expiry":
				expectWorker(t, f.owner(10000, cs.Mutation{AdvanceTime: &cs.AdvanceTime{}}), cs.OK)
			}
			before := f.a.cell()
			expectWorker(t, f.eval(c), want)
			if !reflect.DeepEqual(before, f.a.cell()) {
				t.Fatal("losing claim changed chat")
			}
		})
	}
}
func TestR18R19R21ChildAssignmentsAndParentFence(t *testing.T) {
	f, v := assignmentFixture(t)
	// Issue a separately scoped child delegation: parallel tasks cannot alias the
	// parent's lease or obtain authority by inventing a parent in the payload.
	f.a.put(cs.CellKey("parent"), cs.Created(cs.CellInit{Head: "parent-head"}, "a", 1000))
	child := f.a.cell()
	child.ParentCell = "parent"
	f.a.put(cs.CellKey("chat"), child)
	expectWorker(t, f.owner(1003, cs.Mutation{Assign: &v}), cs.BadRequest)
	v.ParentCell = "parent"
	assign(t, f, v)
	r := f.eval(claim(f, v))
	expectWorker(t, r, cs.OK)
	expectWorker(t, f.run(1, 1004, cs.Mutation{Publish: &cs.PublishCell{Cell: "chat", Publish: cs.Publish{Fence: 2, OldHead: "head", Head: "result"}}}), cs.BadRequest)
	receipt := cs.UploadReceipt{Device: f.id, Cell: "chat", WorkerIncarnation: 1, Head: "result", HeadRID: "rid", Frames: []string{"frame"}}
	f.a.put(cs.UploadKey(cs.UploadID(receipt)), receipt)
	expectWorker(t, f.run(1, 1005, cs.Mutation{Publish: &cs.PublishCell{Cell: "chat", Receipt: cs.UploadID(receipt), Publish: cs.Publish{Fence: 2, OldHead: "head", Head: "result", Frames: receipt.Frames}}}), cs.OK)
	expectWorker(t, f.owner(1006, cs.Mutation{Acquire: &cs.AcquireCell{Cell: "parent", Force: true, Expect: cs.LeaseExpectation{Device: "a", Fence: 1}}}), cs.OK)
	parent := append([]byte(nil), f.a.rows[cs.CellKey("parent")]...)
	expectWorker(t, f.run(1, 1007, cs.Mutation{FinishAssignment: &cs.FinishAssignment{Ref: ref(v), Fence: 2, Head: "result"}}), cs.OK)
	if !bytes.Equal(parent, f.a.rows[cs.CellKey("parent")]) {
		t.Fatal("finish wrote parent")
	}
	expectWorker(t, f.owner(1008, cs.Mutation{Publish: &cs.PublishCell{Cell: "parent", Publish: cs.Publish{Fence: 1, OldHead: "parent-head", Head: "child-result"}}}), cs.FenceStale)
	expectWorker(t, f.register(1, 1009), cs.OK)
	expectWorker(t, f.run(2, 1010, cs.Mutation{Release: &cs.ReleaseCell{Cell: "chat", Fence: 2}}), cs.Conflict)
}
func TestR18R27AssignmentActivityAndRetention(t *testing.T) {
	f, v := assignmentFixture(t)
	a := assign(t, f, v)
	b, _ := cs.Encode(a)
	busy, e := cs.RecordActive(cs.AssignmentKey(v.ID), b, 1004)
	if e != nil || !busy {
		t.Fatal(busy, e)
	}
	busy, e = cs.RecordActive(cs.AssignmentKey(v.ID), b, v.Expires)
	if e != nil || busy {
		t.Fatal(busy, e)
	}
	expectWorker(t, f.owner(1005, cs.Mutation{CancelAssignment: &cs.CancelAssignment{Ref: ref(v)}}), cs.OK)
	retained := append([]byte(nil), f.a.rows[cs.AssignmentKey(v.ID)]...)
	var size uint64
	for k, b := range f.a.rows {
		size += cs.RecordBytes(k, b)
	}
	f.a.meta.Ceiling = size + 2048
	v.ID = "too-many"
	v.SealedPayload = bytes.Repeat([]byte{7}, cs.MaxWorkerSealedBytes)
	expectWorker(t, f.owner(1006, cs.Mutation{Assign: &v}), cs.Capacity)
	b = retained
	v.ID = "opaque-run"
	if !bytes.Equal(b, f.a.rows[cs.AssignmentKey(v.ID)]) {
		t.Fatal("admission deleted retained assignment")
	}
}
func TestR18SeededAssignmentOrdering(t *testing.T) {
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%04d", seed), func(t *testing.T) {
			f, v := assignmentFixture(t)
			assign(t, f, v)
			rng := rand.New(rand.NewSource(int64(seed)))
			c := claim(f, v)
			take := f.command("b", actorKey("b"), 0, 1004, cs.Mutation{Acquire: &cs.AcquireCell{Cell: "chat", Force: true, Expect: v.Expect}})
			if rng.Intn(2) == 0 {
				expectWorker(t, f.eval(c), cs.OK)
				expectWorker(t, f.eval(take), cs.LeaseHeld)
			} else {
				expectWorker(t, f.eval(take), cs.OK)
				expectWorker(t, f.eval(c), cs.LeaseHeld)
			}
			if f.a.cell().Lease.Fence != 2 {
				t.Fatal("R18 multiple authoritative winners")
			}
		})
	}
}
