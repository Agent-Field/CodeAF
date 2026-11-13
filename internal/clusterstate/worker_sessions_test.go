package clusterstate_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"reflect"
	"sort"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
)

type retryCounts struct {
	Live           uint32 `json:"live"`
	Retained       uint32 `json:"retained"`
	Bookkeeping    uint64 `json:"bookkeeping,omitempty"`
	Initialization uint64 `json:"initialization,omitempty"`
}

type assignmentRetries struct {
	f    *workerFixture
	v    cs.Assign
	base retryCounts
}

func newAssignmentRetries(t *testing.T) *assignmentRetries {
	f, v := assignmentFixture(t)
	expectWorker(t, f.a.run("a", 1003, cs.Mutation{AdvanceTime: &cs.AdvanceTime{}}), cs.OK)
	return &assignmentRetries{f: f, v: v, base: readRetryCounts(t, f.a)}
}

func readRetryCounts(t *testing.T, a *application) retryCounts {
	t.Helper()
	var counts retryCounts
	if err := cs.Decode(a.rows[cs.SessionsKey()], &counts); err != nil {
		t.Fatal(err)
	}
	var retained, credit uint64
	for k, b := range a.rows {
		if k.Namespace == "sessions" && b != nil {
			credit += min(cs.RecordBytes(k, b), 512)
			if k != cs.SessionsKey() {
				retained++
			}
		}
	}
	if uint64(counts.Retained) != retained || counts.Bookkeeping != credit {
		t.Fatalf("retry accounting disagrees with rows: %+v, rows=%d credit=%d", counts, retained, credit)
	}
	if _, err := cs.ApplicationBookkeeping(a.rows[cs.SessionsKey()]); err != nil {
		t.Fatal(err)
	}
	return counts
}

func (r *assignmentRetries) owner(m cs.Mutation) cs.Result {
	return r.f.a.run("a", 1004, m)
}

func (r *assignmentRetries) command(kind string, v cs.Assign, m cs.Mutation) cs.Command {
	b, err := cs.Encode(ref(v))
	if err != nil {
		r.f.a.t.Fatal(err)
	}
	h := sha256.Sum256(b)
	c, err := cs.Sign(cs.Command{Schema: 1, Cluster: "test", Actor: r.f.id, WorkerIncarnation: v.Incarnation, Session: "worker-" + kind + "-" + hex.EncodeToString(h[:]), Sequence: 1, Now: 1004, Change: m}, r.f.key)
	if err != nil {
		r.f.a.t.Fatal(err)
	}
	return c
}

func (r *assignmentRetries) start(id string) (cs.Assign, cs.Command, cs.Result) {
	v := r.v
	v.ID = id
	cell := r.f.a.cell()
	v.Expect = cs.LeaseExpectation{Device: cell.Lease.Device, Fence: cell.Lease.Fence}
	a := r.owner(cs.Mutation{Assign: &v})
	if a.Code != cs.OK {
		return v, cs.Command{}, a
	}
	c := r.command("claim", v, cs.Mutation{ClaimAssignment: &cs.ClaimAssignment{Ref: ref(v), Expect: v.Expect}})
	return v, c, r.f.eval(c)
}

func (r *assignmentRetries) finish(v cs.Assign, claimed cs.Result) cs.Command {
	return r.command("finish", v, cs.Mutation{FinishAssignment: &cs.FinishAssignment{Ref: ref(v), Fence: claimed.View.Cell.Lease.Fence, Head: claimed.View.Cell.Head}})
}

func (r *assignmentRetries) complete(n int) int {
	for i := 0; i < n; i++ {
		v, _, claimed := r.start(fmt.Sprintf("run-%d", i))
		if claimed.Code != cs.OK {
			r.f.a.t.Errorf("assignment %d claim: %s", i+1, claimed.Code)
			return i
		}
		finished := r.f.eval(r.finish(v, claimed))
		if finished.Code != cs.OK {
			r.f.a.t.Errorf("assignment %d finish: %s", i+1, finished.Code)
			return i
		}
	}
	return n
}

// r3.1
func TestR31WorkerSessionsStayBoundedAcrossFiveThousandAssignments(t *testing.T) {
	r := newAssignmentRetries(t)
	if n := r.complete(5000); n != 5000 {
		t.Fatalf("completed %d assignments", n)
	}
	got := readRetryCounts(t, r.f.a)
	if got.Live != r.base.Live || got.Retained != r.base.Retained || got.Bookkeeping > r.base.Bookkeeping+512 {
		t.Fatalf("completed assignments retained retry sessions: before=%+v after=%+v", r.base, got)
	}
}

// r3.2
func TestR32WorkerHistoryLeavesRoomForPersonalLeadershipSession(t *testing.T) {
	r := newAssignmentRetries(t)
	r.f.a.meta.Term = 1
	expectWorker(t, enroll(r.f.a, "b"), cs.OK)
	expectWorker(t, settle(r.f.a), cs.OK)
	r.complete(5000)
	h := memberHeader(r.f.a)
	c, err := cs.Sign(cs.Command{Schema: 1, Cluster: "test", Actor: "b", Session: "leadership", Sequence: 1, Now: 1004, Change: cs.Mutation{RequestLeadership: &cs.RequestLeadership{Revision: h.Revision, Node: 4, Device: "b"}}}, actorKey("b"))
	if err != nil {
		t.Fatal(err)
	}
	expectWorker(t, r.f.eval(c), cs.OK)
}

// r3.3
func TestR33WorkerLostRepliesAndReleasedSessionReplays(t *testing.T) {
	r := newAssignmentRetries(t)
	v, claim, claimed := r.start("original")
	expectWorker(t, claimed, cs.OK)
	if retry := r.f.eval(claim); !reflect.DeepEqual(retry, claimed) {
		t.Fatal("lost claim reply changed result or fence", retry, claimed)
	}
	finish := r.finish(v, claimed)
	expectWorker(t, r.f.eval(finish), cs.OK)
	before := r.f.a.cell()
	expectWorker(t, r.f.eval(finish), cs.OK)
	if !reflect.DeepEqual(before, r.f.a.cell()) {
		t.Fatal("finish retry changed the chat")
	}
	got := readRetryCounts(t, r.f.a)
	if got.Live != r.base.Live || got.Retained != r.base.Retained {
		t.Errorf("finished assignment retained worker sessions: before=%+v after=%+v", r.base, got)
	}
	_, _, next := r.start("replacement")
	expectWorker(t, next, cs.OK)
	before = r.f.a.cell()
	for _, c := range []cs.Command{claim, finish} {
		r.f.eval(c)
		if !reflect.DeepEqual(before, r.f.a.cell()) {
			t.Fatal("released request changed a replacement's fence or head")
		}
	}
}

// r3.4
func TestR34WorkerSessionReleaseOnWithdrawalDrainAndReassignment(t *testing.T) {
	for _, event := range []string{"withdraw", "drain-withdraw", "reassign", "new-incarnation"} {
		t.Run(event, func(t *testing.T) {
			r := newAssignmentRetries(t)
			v, oldClaim, claimed := r.start("original")
			expectWorker(t, claimed, cs.OK)
			oldFinish := r.finish(v, claimed)
			if event == "drain-withdraw" {
				expectWorker(t, r.owner(cs.Mutation{DrainWorker: &cs.DrainWorker{Worker: r.f.id, Incarnation: 1}}), cs.OK)
			}
			if event == "withdraw" || event == "drain-withdraw" {
				expectWorker(t, r.owner(cs.Mutation{CancelAssignment: &cs.CancelAssignment{Ref: ref(v)}}), cs.OK)
			} else {
				next := v
				next.Revision++
				next.Expect = cs.LeaseExpectation{Device: r.f.id, Fence: claimed.View.Cell.Lease.Fence}
				if event == "new-incarnation" {
					expectWorker(t, r.f.register(1, 1004), cs.OK)
					expectWorker(t, r.f.beat(r.f.proof(2, 1, 1004), 1004), cs.OK)
					next.Incarnation = 2
				}
				before := readRetryCounts(t, r.f.a)
				expectWorker(t, r.owner(cs.Mutation{Reassign: &next}), cs.OK)
				after := readRetryCounts(t, r.f.a)
				if after.Live >= before.Live || after.Retained >= before.Retained {
					t.Errorf("reassignment retained previous worker sessions: before=%+v after=%+v", before, after)
				}
				c := r.command("claim", next, cs.Mutation{ClaimAssignment: &cs.ClaimAssignment{Ref: ref(next), Expect: next.Expect}})
				current := r.f.eval(c)
				expectWorker(t, current, cs.OK)
				beforeCell := r.f.a.cell()
				r.f.eval(oldClaim)
				r.f.eval(oldFinish)
				if !reflect.DeepEqual(beforeCell, r.f.a.cell()) || !reflect.DeepEqual(current, r.f.eval(c)) {
					t.Fatal("released requests interfered with the new assignment")
				}
				expectWorker(t, r.f.eval(r.finish(next, current)), cs.OK)
			}
			got := readRetryCounts(t, r.f.a)
			allowance := uint32(0)
			if event == "new-incarnation" {
				allowance = 2
			}
			if got.Live != r.base.Live+allowance || got.Retained != r.base.Retained+allowance {
				t.Fatalf("released assignment left worker sessions: before=%+v after=%+v", r.base, got)
			}
		})
	}
}

// r3.5
func TestR35WorkerSessionReleaseIsDeterministicAndSnapshotPortable(t *testing.T) {
	r := newAssignmentRetries(t)
	v, _, claimed := r.start("original")
	expectWorker(t, claimed, cs.OK)
	finish := r.finish(v, claimed)
	raw, err := cs.EncodeCommand(finish)
	if err != nil {
		t.Fatal(err)
	}
	r.f.a.meta.Bytes = 0
	for k, b := range r.f.a.rows {
		r.f.a.meta.Bytes += cs.RecordBytes(k, b)
	}
	var first cs.Evaluation
	for i := 0; i < 3; i++ {
		got, err := cs.Evaluate(raw, r.f.a.meta, func(k cs.RecordKey) ([]byte, error) { return bytes.Clone(r.f.a.rows[k]), nil })
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && !reflect.DeepEqual(first, got) {
			t.Fatal("members disagree on release result or write order")
		}
		first = got
	}
	expectWorker(t, r.f.eval(finish), cs.OK)
	keys := make([]cs.RecordKey, 0, len(r.f.a.rows))
	for k, b := range r.f.a.rows {
		if b != nil {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Namespace+"/"+keys[i].Key < keys[j].Namespace+"/"+keys[j].Key })
	var stream bytes.Buffer
	for _, k := range keys {
		if err := raftsnapshot.WriteRow(&stream, raftsnapshot.Row{Namespace: k.Namespace, Key: k.Key, Value: r.f.a.rows[k]}); err != nil {
			t.Fatal(err)
		}
	}
	restored := map[cs.RecordKey][]byte{}
	for {
		row, err := raftsnapshot.ReadRow(&stream)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		restored[cs.RecordKey{Namespace: row.Namespace, Key: row.Key}] = row.Value
	}
	r.f.a.rows = restored
	got := readRetryCounts(t, r.f.a)
	if got.Live != r.base.Live || got.Retained != r.base.Retained || got.Bookkeeping > r.base.Bookkeeping+512 {
		t.Fatalf("restored state retained completed worker sessions: before=%+v after=%+v", r.base, got)
	}
	expectWorker(t, r.f.eval(finish), cs.OK)
}
