package raftabi

import (
	"bytes"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
)

func request(op uint32) *encoder { e := &encoder{}; e.u32(Version); e.u32(op); return e }
func answer(t *testing.T, b []byte) *decoder {
	t.Helper()
	d := &decoder{b: b}
	if d.u32() != Version {
		t.Fatal("R14 response version")
	}
	if err := d.str(); err != "" {
		t.Fatal(err)
	}
	return d
}
func create(t *testing.T, r *Runtime) uint32 {
	t.Helper()
	e := request(1)
	e.str("R14")
	e.u64(1)
	e.u64(42)
	e.u32(1)
	e.u32(1)
	e.u64(1)
	e.blob(nil)
	e.blob(nil)
	e.blob(nil)
	e.u32(0)
	e.u64(0)
	d := answer(t, r.Call(e.b))
	id := d.u32()
	if d.end() != nil {
		t.Fatal("trailing response")
	}
	return id
}

// R14 requires exact byte replay through the production binary ABI, not a
// second JSON interpretation of diagnostic structs.
func TestR14BinaryReplayAndMalformedInputDoesNotMutate(t *testing.T) {
	a, b := New(), New()
	id := create(t, a)
	other := create(t, b)
	for _, tag := range []uint32{0, 1, 1, 1, 1, 1, 1, 1} {
		x, y := request(3), request(3)
		x.u32(id)
		x.u32(tag)
		y.u32(other)
		y.u32(tag)
		bad := append(bytes.Clone(x.b), 1)
		d := &decoder{b: a.Call(bad)}
		d.u32()
		if d.str() == "" {
			t.Fatal("R14 trailing input accepted")
		}
		if !bytes.Equal(a.Call(x.b), b.Call(y.b)) {
			t.Fatal("R14 malformed call changed subsequent effect bytes")
		}
	}
}

// R3 completion generations cannot cross handle lifetimes after recovery.
func TestR3DroppedHandleNeverTargetsReplacement(t *testing.T) {
	r := New()
	old := create(t, r)
	e := request(2)
	e.u32(old)
	answer(t, r.Call(e.b))
	next := create(t, r)
	if next == old {
		t.Fatal("R3 handle ID reused")
	}
	e = request(3)
	e.u32(old)
	e.u32(5)
	e.u64(1)
	d := &decoder{b: r.Call(e.b)}
	d.u32()
	if d.str() == "" {
		t.Fatal("R3 stale completion accepted")
	}
}

// R27 bounds framing before touching the core, including pathological count
// fields that could otherwise request a whole-identity allocation.
func TestR27FrameAndCountAdmission(t *testing.T) {
	r := New()
	for _, b := range [][]byte{nil, {1}, make([]byte, MaxBuffer+1)} {
		d := &decoder{b: r.Call(b)}
		d.u32()
		if d.str() == "" {
			t.Fatal("R27 invalid frame accepted")
		}
	}
	e := request(1)
	e.str("R27")
	e.u64(1)
	e.u64(0)
	e.u32(1)
	e.u32(^uint32(0))
	d := &decoder{b: r.Call(e.b)}
	d.u32()
	if d.str() == "" {
		t.Fatal("R27 unbounded bootstrap accepted")
	}
	create(t, r)
}

// R14/R28 preserve full-width fences and every sealed row byte while paging
// an identity. A round trip cannot truncate an opaque frame plan or title.
func TestR14R28SingleRowKeepsOpaqueDataAndUint64(t *testing.T) {
	cell := clusterstate.Created(clusterstate.CellInit{Head: "opaque", Title: string(bytes.Repeat([]byte("A"), 768<<10)), Frames: []string{"f1", "f2"}}, "device", 1000)
	cell.Lease.Fence = ^uint64(0)
	row, err := clusterstate.EncodeRow(clusterstate.Row{Schema: 1, Key: "cell", Cell: &cell})
	if err != nil {
		t.Fatal(err)
	}
	e := request(5)
	e.blob(row)
	d := answer(t, New().Call(e.b))
	if !bytes.Equal(d.blob(), row) || d.end() != nil {
		t.Fatal("R28 row changed")
	}
}

// R3/R14 prove the feasibility command is the real Go rule and produces the
// same canonical durable row that a native host reads after commitment.
func TestR3R14LeaseProbeUsesSharedRule(t *testing.T) {
	c := clusterstate.Command{Schema: 1, Cluster: "R14", Actor: "device", Session: "s", Sequence: 1, RequestDigest: "opaque-digest", Now: 1000, Change: clusterstate.Mutation{Create: &clusterstate.CreateCell{Cell: "cell", Init: clusterstate.CellInit{Head: "opaque-head"}}}}
	b, err := clusterstate.EncodeCommand(c)
	if err != nil {
		t.Fatal(err)
	}
	e := request(4)
	e.blob(b)
	e.blob(nil)
	d := answer(t, New().Call(e.b))
	row, err := clusterstate.DecodeRow(d.blob())
	if err != nil {
		t.Fatal(err)
	}
	if row.Cell.Head != "opaque-head" || row.Cell.Lease.Fence != 1 || row.Cell.Lease.Expires != 91000 {
		t.Fatal("R14 wrong rule result")
	}
}
