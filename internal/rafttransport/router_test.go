package rafttransport

import (
	"bytes"
	"crypto/ed25519"
	"math"
	"testing"

	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

var testScope = Scope{Identity: "id_test", Cluster: "cluster-test", Epoch: 1, Protocol: Protocol}

func key(id byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{id}, ed25519.SeedSize))
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func view() View {
	v := View{Scope: testScope, Revision: 1, Generation: 9}
	for id := uint64(1); id <= 4; id++ {
		v.Members = append(v.Members, Member{Node: id, Incarnation: 1, Key: key(byte(id)).Public().(ed25519.PublicKey), Voter: id <= 3})
	}
	return v
}
func router(t *testing.T) *Router {
	t.Helper()
	r, err := NewRouter(DefaultLimits())
	must(t, err)
	must(t, r.Install(view()))
	return r
}
func message(t *testing.T, from, to, seq uint64, kind pb.MessageType, payload int) Envelope {
	t.Helper()
	m := &pb.Message{Type: kind.Enum(), From: proto.Uint64(from), To: proto.Uint64(to), Term: proto.Uint64(1), Context: bytes.Repeat([]byte("x"), payload)}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	must(t, err)
	e := Envelope{Scope: testScope, From: from, To: to, Incarnation: 1, Destination: 1, Sequence: seq, Payload: b}
	must(t, e.Sign(key(byte(from))))
	return e
}
func request(t *testing.T, id uint64, messages ...Envelope) Request {
	t.Helper()
	r := Request{Scope: testScope, Node: id, Incarnation: 1, Generation: 9, Messages: messages}
	must(t, r.Sign(key(byte(id))))
	return r
}
func exchange(t *testing.T, r *Router, req Request) Response {
	t.Helper()
	resp, err := r.Exchange(req, 1000)
	must(t, err)
	return resp
}

// R10/R14/R16 require wire identities to survive JavaScript and prevent a
// signed sender from impersonating another inner Raft member or local input.
func TestR10R14R16SignedEnvelopesAndCanonicalWire(t *testing.T) {
	e := message(t, 1, 2, math.MaxUint64, pb.MsgHeartbeat, 0)
	must(t, e.Verify(key(1).Public().(ed25519.PublicKey)))
	req := request(t, 1, e)
	req.Ack = [laneCount]uint64{math.MaxUint64, 1 << 53, 0}
	must(t, req.Sign(key(1)))
	b, err := Encode(req)
	must(t, err)
	var round Request
	must(t, Decode(b, &round))
	must(t, round.Verify(key(1).Public().(ed25519.PublicKey)))
	if !bytes.Contains(b, []byte(`"18446744073709551615"`)) || !bytes.Contains(b, []byte(`"9007199254740992"`)) {
		t.Fatal("R14: lossy integer encoding")
	}
	for _, bad := range [][]byte{append(b, ' '), bytes.Replace(b, []byte(`"node":"1"`), []byte(`"node":"01"`), 1), bytes.Replace(b, []byte(`"identity":"id_test"`), []byte(`"identity":"id_test","identity":"id_test"`), 1), bytes.Replace(b, []byte(`"ack":[`), []byte(`"ack":["0",`), 1)} {
		if Decode(bad, new(Request)) == nil {
			t.Fatal("R14: accepted ambiguous wire bytes")
		}
	}
	for _, mutate := range []func(*Envelope){func(e *Envelope) { e.Scope.Identity = "id_other" }, func(e *Envelope) { e.Scope.Cluster = "other" }, func(e *Envelope) { e.Scope.Epoch++ }, func(e *Envelope) { e.To = 3 }, func(e *Envelope) { e.Incarnation++ }, func(e *Envelope) { e.Destination++ }, func(e *Envelope) { e.Sequence++ }, func(e *Envelope) { e.Payload = append(bytes.Clone(e.Payload), 0) }} {
		bad := e
		mutate(&bad)
		if bad.Verify(key(1).Public().(ed25519.PublicKey)) == nil {
			t.Fatal("R16: altered envelope signature accepted")
		}
	}
	for _, kind := range []pb.MessageType{pb.MsgHup, pb.MsgBeat, pb.MsgProp, pb.MsgReadIndex, pb.MsgStorageAppend, pb.MsgStorageApply} {
		m := &pb.Message{Type: kind.Enum(), From: proto.Uint64(1), To: proto.Uint64(2), Term: proto.Uint64(1)}
		b, err := proto.Marshal(m)
		must(t, err)
		e.Payload = b
		if e.Sign(key(1)) != ErrWire {
			t.Fatalf("R16: accepted local/forwarded message %v", kind)
		}
	}
	e = message(t, 1, 2, 1, pb.MsgApp, 0)
	e.From = 3
	if e.Sign(key(3)) != ErrWire {
		t.Fatal("R16: signed inner sender spoof accepted")
	}
}

// R10/R16 permit duplicate/reordered transport while denying cross-identity,
// unknown, revoked or retired-generation authority, including delayed requests.
func TestR10R16DuplicateReorderAndRevocation(t *testing.T) {
	r := router(t)
	a := message(t, 1, 2, 2, pb.MsgApp, 0)
	b := message(t, 1, 2, 1, pb.MsgVote, 0)
	req := request(t, 1, a, b, a)
	exchange(t, r, req)
	exchange(t, r, req)
	got := exchange(t, r, request(t, 2))
	if len(got.Messages) != 2 || got.Messages[0].Envelope.Sequence != 2 || got.Messages[1].Envelope.Sequence != 1 {
		t.Fatal("R10: duplicate/reorder changed message delivery")
	}
	got.Messages[0].Envelope.Payload[0] ^= 1
	if next := exchange(t, r, request(t, 2)); next.Messages[0].Envelope.Verify(key(1).Public().(ed25519.PublicKey)) != nil {
		t.Fatal("R16: receiver mutated retained queue")
	}
	ack := request(t, 2)
	ack.Ack[Voter] = got.Messages[1].Cursor
	must(t, ack.Sign(key(2)))
	exchange(t, r, ack)
	if s := r.Stats(testScope, 1000); s.Queued != 0 {
		t.Fatal("R11: acknowledgment retained messages")
	}
	exchange(t, r, req)
	bad := request(t, 2)
	bad.Ack[Voter] = 99
	must(t, bad.Sign(key(2)))
	if _, err := r.Exchange(bad, 1000); err != ErrCursor {
		t.Fatal("R16: unsigned/unissued cursor drained traffic", err)
	}
	bad = request(t, 1, a)
	bad.Scope.Identity = "id_other"
	must(t, bad.Sign(key(1)))
	if _, err := r.Exchange(bad, 1000); err != ErrAuth {
		t.Fatal("R16: cross-identity admitted", err)
	}
	other := view()
	other.Scope.Identity = "id_other"
	must(t, r.Install(other))
	if _, err := r.Exchange(bad, 1000); err != ErrAuth {
		t.Fatal("R16: cross-identity payload admitted into registered identity", err)
	}
	bad = request(t, 1, a)
	bad.Signature[0] ^= 1
	if _, err := r.Exchange(bad, 1000); err != ErrAuth {
		t.Fatal("R16: forged exchange accepted", err)
	}
	// The signed request already exists when the durable registry changes.
	v := view()
	v.Revision++
	v.Members[0].Revoked = true
	must(t, r.Install(v))
	if _, err := r.Exchange(req, 1000); err != ErrAuth {
		t.Fatal("R16: delayed authenticated request outlived committed revocation", err)
	}
	if got := exchange(t, r, request(t, 2)); len(got.Messages) != 0 {
		t.Fatal("R16: revoked sender retained queued authority")
	}
	v.Revision++
	v.Members[0].Revoked = false
	if r.Install(v) != ErrAuth {
		t.Fatal("R16: retired node regained authority")
	}
	v = view()
	v.Revision = 3
	v.Members[0].Revoked = true
	v.Members[1].Incarnation = 2
	must(t, r.Install(v))
	if _, err := r.Exchange(request(t, 2), 1000); err != ErrAuth {
		t.Fatal("R16: old member incarnation admitted", err)
	}
	e := message(t, 3, 2, 1, pb.MsgHeartbeat, 0)
	if _, err := r.Exchange(request(t, 3, e), 1000); err != ErrAuth {
		t.Fatal("R16: old destination generation admitted", err)
	}
	v.Revision++
	v.Generation++
	must(t, r.Install(v))
	if _, err := r.Exchange(request(t, 3), 1000); err != ErrGeneration {
		t.Fatal("R16: old restart cursor admitted", err)
	}
}

// R11/R27 require voter reservation even when an absent learner or snapshot
// receiver fills its budget, with bounded batches, cursors and retention.
func TestR11R27VoterReservationsAndBackpressure(t *testing.T) {
	r := router(t)
	for i := uint64(1); i <= 2; i++ {
		exchange(t, r, request(t, 1, message(t, 1, 4, i, pb.MsgApp, 40<<10)))
	}
	if _, err := r.Exchange(request(t, 1, message(t, 1, 4, 3, pb.MsgApp, 40<<10)), 1000); err != ErrCapacity {
		t.Fatal("R11: unbounded learner queue", err)
	}
	for i := uint64(1); i <= uint64(laneSlots[Voter]); i++ {
		exchange(t, r, request(t, 1, message(t, 1, 2, i, pb.MsgHeartbeat, 0)))
	}
	if _, err := r.Exchange(request(t, 1, message(t, 1, 2, 9, pb.MsgHeartbeat, 0)), 1000); err != ErrCapacity {
		t.Fatal("R27: inflight cap absent", err)
	}
	// A full-size chunk has a separate reservation and cannot consume votes.
	chunk := Envelope{Scope: testScope, From: 2, To: 3, Incarnation: 1, Destination: 1, Sequence: 1, Payload: bytes.Repeat([]byte{9}, MaxChunkBytes), Chunk: &Chunk{Generation: digest([]byte("snapshot"))}}
	chunk.Chunk.Hash = digest(chunk.Payload)
	must(t, chunk.Sign(key(2)))
	exchange(t, r, request(t, 2, chunk))
	if _, err := r.Exchange(request(t, 2, func() Envelope { x := chunk; x.Sequence++; must(t, x.Sign(key(2))); return x }()), 1000); err != ErrCapacity {
		t.Fatal("R27: unbounded snapshot queue", err)
	}
	exchange(t, r, request(t, 3, message(t, 3, 1, 1, pb.MsgVote, 0)))
	got := exchange(t, r, request(t, 3))
	if len(got.Messages) != 1 || got.Messages[0].Lane != Snapshot {
		t.Fatal("R11: snapshot did not use its own lane")
	}
	got = exchange(t, r, request(t, 2))
	if len(got.Messages) != laneSlots[Voter] {
		t.Fatal("R11: voter traffic was displaced")
	}
	s := r.Stats(testScope, 1000)
	total := 0
	for lane, n := range s.Bytes {
		if n > laneBudget[lane] {
			t.Fatal("R27: lane budget exceeded")
		}
		total += n
	}
	if total > MaxQueuedBytes {
		t.Fatal("R27: identity byte bound exceeded")
	}
	if s = r.Stats(testScope, 1000+RetentionMillis); s.Queued != 0 || s.Bytes != [laneCount]int{} {
		t.Fatal("R27: transient buffers did not expire")
	}
	// Expired cursors are never reused, including under a backward host clock.
	again := exchange(t, r, request(t, 1, message(t, 1, 2, 10, pb.MsgApp, 0)))
	_ = again
	got = exchange(t, r, request(t, 2))
	if len(got.Messages) != 1 || got.Messages[0].Cursor <= uint64(laneSlots[Voter]) {
		t.Fatal("R16: expired cursor reused")
	}
}

// R11/R16 require atomic batch admission and safe retry of a queue-full send;
// valid acks must make progress even when the same request hits backpressure.
func TestR11R16AtomicBatchAndAckUnderPressure(t *testing.T) {
	r := router(t)
	first := message(t, 1, 2, 1, pb.MsgApp, 0)
	bad := message(t, 1, 3, 2, pb.MsgApp, 0)
	bad.Signature[0] ^= 1
	if _, err := r.Exchange(request(t, 1, first, bad), 1000); err != ErrAuth || r.Stats(testScope, 1000).Queued != 0 {
		t.Fatal("R16: partial invalid batch admitted", err)
	}
	for i := uint64(1); i <= uint64(laneSlots[Voter]); i++ {
		exchange(t, r, request(t, 1, message(t, 1, 2, i, pb.MsgApp, 0)))
	}
	exchange(t, r, request(t, 2, message(t, 2, 1, 1, pb.MsgApp, 0)))
	got := exchange(t, r, request(t, 1))
	req := request(t, 1, message(t, 1, 2, 9, pb.MsgApp, 0))
	req.Ack[Voter] = got.Messages[0].Cursor
	must(t, req.Sign(key(1)))
	if _, err := r.Exchange(req, 1000); err != ErrCapacity {
		t.Fatal("R11: expected outbound backpressure", err)
	}
	if got = exchange(t, r, request(t, 1)); len(got.Messages) != 0 {
		t.Fatal("R11: full outbound blocked incoming ack")
	}
	// A conflicting sequence while pending cannot replace an authenticated packet.
	conflict := message(t, 1, 2, 1, pb.MsgVote, 0)
	if _, err := r.Exchange(request(t, 1, conflict), 1000); err != ErrAuth {
		t.Fatal("R16: conflicting transport sequence replaced pending message", err)
	}
}

// R11/R27 bound live learners and operator registration without turning a
// signed poll into enrollment, or permitting history deletion to regain space.
func TestR11R27RegistryAndOperatorBounds(t *testing.T) {
	r, err := NewRouter(Limits{1, 40, 6})
	must(t, err)
	v := view()
	for id := uint64(5); id <= 35; id++ {
		v.Members = append(v.Members, Member{Node: id, Incarnation: 1, Key: key(byte(id)).Public().(ed25519.PublicKey)})
	}
	must(t, r.Install(v))
	v.Revision++
	v.Members = append(v.Members, Member{Node: 36, Incarnation: 1, Key: key(36).Public().(ed25519.PublicKey)})
	if r.Install(v) != ErrCapacity {
		t.Fatal("R11: learner admission unbounded")
	}
	second := view()
	second.Scope.Cluster = "second"
	if r.Install(second) != ErrCapacity {
		t.Fatal("R27: operator identity cap ignored")
	}
	v = view()
	v.Revision = 3
	if r.Install(v) != ErrAuth {
		t.Fatal("R16: node tombstones silently deleted")
	}
	if _, err := r.Exchange(request(t, 99), 1000); err != ErrAuth {
		t.Fatal("R16: signed unknown key enrolled itself", err)
	}
}

// R11 preserves voter ordering and read-barrier contexts even when older
// learner/chunk traffic was queued first; lane acknowledgments stay separate.
func TestR11VoterPriorityPreservesReadContexts(t *testing.T) {
	r := router(t)
	exchange(t, r, request(t, 4, message(t, 4, 2, 1, pb.MsgApp, 0)))
	chunk := Envelope{Scope: testScope, From: 3, To: 2, Incarnation: 1, Destination: 1, Sequence: 1, Payload: []byte("opaque-chunk"), Chunk: &Chunk{Generation: digest([]byte("generation")), Hash: digest([]byte("opaque-chunk"))}}
	must(t, chunk.Sign(key(3)))
	exchange(t, r, request(t, 3, chunk))
	first := message(t, 1, 2, 1, pb.MsgHeartbeat, 11)
	second := message(t, 1, 2, 2, pb.MsgHeartbeat, 12)
	exchange(t, r, request(t, 1, first, second))
	got := exchange(t, r, request(t, 2))
	if len(got.Messages) != 3 || got.Messages[0].Lane != Voter || got.Messages[1].Lane != Voter || got.Messages[2].Lane != Learner || !sameEnvelope(got.Messages[0].Envelope, first) || !sameEnvelope(got.Messages[1].Envelope, second) {
		t.Fatal("R11: learner backlog overtook votes or coalesced read barriers")
	}
	ack := request(t, 2)
	ack.Ack[Voter] = got.Messages[1].Cursor
	must(t, ack.Sign(key(2)))
	got = exchange(t, r, ack)
	if len(got.Messages) != 1 || got.Messages[0].Lane != Learner {
		t.Fatal("R11: voter ack consumed another lane")
	}
	ack.Ack[Learner] = got.Messages[0].Cursor
	must(t, ack.Sign(key(2)))
	got = exchange(t, r, ack)
	if len(got.Messages) != 1 || got.Messages[0].Lane != Snapshot || !sameEnvelope(got.Messages[0].Envelope, chunk) {
		t.Fatal("R11: drained normal traffic did not release snapshot delivery")
	}
}

// R11/R27 refuse oversized normal batches and chunks before admission, while
// a maximum-size chunk remains deliverable with its hash/offset/generation.
func TestR11R27BatchAndChunkBoundaries(t *testing.T) {
	r := router(t)
	req := Request{Scope: testScope, Node: 1, Incarnation: 1, Generation: 9, Messages: []Envelope{message(t, 1, 2, 1, pb.MsgApp, 40<<10), message(t, 1, 2, 2, pb.MsgApp, 40<<10)}}
	if req.Sign(key(1)) != ErrWire {
		t.Fatal("R27: normal batch byte cap missing")
	}
	chunk := Envelope{Scope: testScope, From: 1, To: 2, Incarnation: 1, Destination: 1, Sequence: 1, Payload: bytes.Repeat([]byte{3}, MaxChunkBytes), Chunk: &Chunk{Generation: digest([]byte("generation")), Offset: math.MaxUint64}}
	chunk.Chunk.Hash = digest(chunk.Payload)
	must(t, chunk.Sign(key(1)))
	exchange(t, r, request(t, 1, chunk))
	got := exchange(t, r, request(t, 2))
	if len(got.Messages) != 1 || !sameEnvelope(got.Messages[0].Envelope, chunk) {
		t.Fatal("R14/R27: maximum chunk lost bytes or full-width offset")
	}
	chunk.Payload = append(chunk.Payload, 0)
	chunk.Chunk.Hash = digest(chunk.Payload)
	if chunk.Sign(key(1)) != ErrWire {
		t.Fatal("R27: oversized chunk admitted")
	}
}

// R16 prevents a pending transport sequence from being replaced under a
// different queue class, and preserves its original signed bytes on refusal.
func TestR16PendingSequenceCannotChangeLane(t *testing.T) {
	r := router(t)
	original := message(t, 1, 2, 1, pb.MsgApp, 0)
	exchange(t, r, request(t, 1, original))
	e := Envelope{Scope: testScope, From: 1, To: 2, Incarnation: 1, Destination: 1, Sequence: 1, Payload: []byte("sealed-chunk"), Chunk: &Chunk{Generation: digest([]byte("generation")), Hash: digest([]byte("sealed-chunk"))}}
	must(t, e.Sign(key(1)))
	if _, err := r.Exchange(request(t, 1, e), 1000); err != ErrAuth {
		t.Fatal("R16: pending sequence changed class", err)
	}
	got := exchange(t, r, request(t, 2))
	if len(got.Messages) != 1 || !sameEnvelope(got.Messages[0].Envelope, original) {
		t.Fatal("R16: refused replacement damaged original packet")
	}
}
