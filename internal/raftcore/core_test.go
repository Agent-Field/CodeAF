package raftcore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

type testGroup struct {
	t        *testing.T
	nodes    []*Core
	messages []Outbound
	trace    bytes.Buffer
	reads    []ReadBarrier
	applied  [][]*pb.Entry
	durable  []RecoveredState
	drop     func(from, to uint64) bool
}

func newGroup(t *testing.T) *testGroup { return newTimingGroup(t, ElectionTimingVersion) }

func newTimingGroup(t *testing.T, version uint16) *testGroup {
	t.Helper()
	g := &testGroup{t: t, applied: make([][]*pb.Entry, 3), durable: make([]RecoveredState, 3)}
	for id := uint64(1); id <= 3; id++ {
		c, err := New(Config{TimingVersion: version, ClusterUUID: "test-cluster", NodeID: id, Seed: 100 + id, Protocol: ProtocolVersion, Bootstrap: []uint64{3, 1, 2}}, RecoveredState{})
		if err != nil {
			t.Fatal(err)
		}
		g.nodes = append(g.nodes, c)
	}
	for id := uint64(1); id <= 3; id++ {
		g.call(id, Poll{})
	}
	g.pump()
	return g
}

func (g *testGroup) call(id uint64, in Input) {
	g.t.Helper()
	e, err := g.nodes[id-1].Step(in)
	if err != nil {
		g.t.Fatalf("node %d input %T: %v", id, in, err)
	}
	g.accept(id, e, 0)
}
func (g *testGroup) accept(id uint64, e Effects, depth int) {
	g.t.Helper()
	if depth > 100 {
		g.t.Fatal("unbounded Ready completions")
	}
	b, err := json.Marshal(e)
	if err != nil {
		g.t.Fatal(err)
	}
	g.trace.Write(b)
	g.trace.WriteByte('\n')
	g.messages = append(g.messages, e.Send...)
	g.reads = append(g.reads, e.Reads...)
	var in Input
	if e.Persist != nil {
		p := e.Persist
		d := &g.durable[id-1]
		if !raft.IsEmptyHardState(p.HardState) {
			d.HardState = clone(p.HardState)
		}
		if !raft.IsEmptySnap(p.Snapshot) {
			d.Snapshot = clone(p.Snapshot)
			d.Entries = nil
		}
		if len(p.Entries) != 0 {
			first := p.Entries[0].GetIndex()
			kept := 0
			for _, entry := range d.Entries {
				if entry.GetIndex() < first {
					kept++
				}
			}
			d.Entries = append(d.Entries[:kept], copyEntries(p.Entries)...)
		}
		in = Persisted{p.ID}
	}
	if e.Apply != nil {
		g.durable[id-1].Applied = e.Apply.Index
		g.durable[id-1].ConfState = clone(e.Apply.ConfState)
		for _, entry := range e.Apply.Entries {
			g.applied[id-1] = append(g.applied[id-1], clone(entry))
		}
		in = Applied{e.Apply.ID}
	}
	if in != nil {
		next, err := g.nodes[id-1].Step(in)
		if err != nil {
			g.t.Fatalf("node %d completion %T: %v", id, in, err)
		}
		g.accept(id, next, depth+1)
	}
}
func (g *testGroup) pump() {
	g.t.Helper()
	for count := 0; len(g.messages) > 0; count++ {
		if count > 10000 {
			g.t.Fatal("unbounded network effects")
		}
		out := g.messages[0]
		g.messages = g.messages[1:]
		m := &pb.Message{}
		if err := proto.Unmarshal(out.Bytes, m); err != nil {
			g.t.Fatal(err)
		}
		if g.drop != nil && g.drop(m.GetFrom(), out.To) {
			continue
		}
		g.call(out.To, PeerMessage{out.Bytes})
	}
}
func (g *testGroup) tick(n int) {
	for i := 0; i < n; i++ {
		for id := uint64(1); id <= 3; id++ {
			g.call(id, Tick{})
		}
		g.pump()
	}
}
func (g *testGroup) leader() uint64 {
	g.t.Helper()
	var leader uint64
	for _, c := range g.nodes {
		if c.status().Role == raft.StateLeader {
			if leader != 0 {
				g.t.Fatal("multiple leaders")
			}
			leader = c.cfg.NodeID
		}
	}
	return leader
}
func (g *testGroup) elect() uint64 {
	g.t.Helper()
	for i := 0; i < 30; i++ {
		g.tick(1)
		if leader := g.leader(); leader != 0 {
			return leader
		}
	}
	g.t.Fatal("no leader within seeded election budget")
	return 0
}
func command(t *testing.T, sequence uint64) []byte {
	t.Helper()
	b, err := clusterstate.EncodeCommand(clusterstate.Command{Schema: clusterstate.SchemaVersion, Cluster: "test-cluster", Actor: "device", Session: "session", Sequence: sequence, Now: 1234, RequestDigest: "opaque-digest", Change: clusterstate.Mutation{AdvanceTime: &clusterstate.AdvanceTime{}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// R14 requires elections and emitted bytes to be reproducible, including the
// exact count of PRNG draws. R6's wrapper mechanism must elect the target and
// pass a NEW read barrier before a host can report a completed transfer.
func TestR14SeededReplayAndR6Transfer(t *testing.T) {
	run := func() []byte {
		g := newTimingGroup(t, 1)
		leader := g.elect()
		g.call(leader, Propose{command(t, 1)})
		g.pump()
		g.call(leader, Read{"before-transfer"})
		g.pump()
		if len(g.reads) != 1 {
			t.Fatal("no quorum read barrier")
		}
		target := leader%3 + 1
		g.call(leader, Transfer{target})
		g.pump()
		if g.leader() != target {
			t.Fatalf("transfer did not elect %d", target)
		}
		g.call(target, Read{"after-transfer"})
		g.pump()
		if len(g.reads) != 2 || g.reads[1].Index < g.reads[0].Index {
			t.Fatal("new leader did not pass a barrier")
		}
		for i := range g.applied {
			found := false
			for _, e := range g.applied[i] {
				if bytes.Equal(e.Data, command(t, 1)) {
					found = true
				}
			}
			if !found {
				t.Fatalf("member %d did not apply command", i+1)
			}
		}
		return bytes.Clone(g.trace.Bytes())
	}
	first, second := run(), run()
	if !bytes.Equal(first, second) {
		t.Fatal("same seeds/inputs produced different effects")
	}
	digest := sha256.Sum256(first)
	golden, err := os.ReadFile("testdata/replay.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(golden)) != hex.EncodeToString(digest[:]) {
		t.Fatalf("R14 trace changed: got %x (review core/wire/RNG version before changing golden)", digest)
	}
}

// R4 forbids fresh read success and mutation application in a minority, even
// while it still remembers itself as leader. CheckQuorum must then step down.
func TestR4MinorityReadAndCheckQuorum(t *testing.T) {
	g := newGroup(t)
	leader := g.elect()
	g.drop = func(from, to uint64) bool { return from == leader || to == leader }
	before := len(g.applied[leader-1])
	g.call(leader, Read{"isolated"})
	g.pump()
	g.call(leader, Propose{command(t, 2)})
	g.pump()
	if len(g.reads) != 0 || len(g.applied[leader-1]) != before {
		t.Fatal("minority exposed success")
	}
	g.tick(ElectionTicks * 3)
	if g.nodes[leader-1].status().Role == raft.StateLeader {
		t.Fatal("isolated leader did not step down")
	}
	if len(g.reads) != 0 || len(g.applied[leader-1]) != before {
		t.Fatal("minority applied a proposal or read")
	}
	if _, err := g.nodes[leader-1].Step(Read{"after-stepdown"}); !errors.Is(err, ErrLeader) {
		t.Fatalf("follower read: %v", err)
	}
}

// R5 requires repeated isolated/rejoin attempts not to depose a stable healthy
// majority. This smoke uses the production PreVote path and explicit ticks.
func TestR5PreVoteFlappingMember(t *testing.T) {
	g := newGroup(t)
	leader := g.elect()
	isolated := leader%3 + 1
	term := g.nodes[leader-1].status().Term
	for attempt := 0; attempt < 100; attempt++ {
		g.drop = func(from, to uint64) bool { return from == isolated || to == isolated }
		g.tick(ElectionTicks * 3)
		if g.nodes[isolated-1].status().Term != term {
			t.Fatal("isolated member raised term without quorum")
		}
		g.drop = nil
		// Deliver a healthy leader heartbeat before any new election deadline.
		g.call(leader, Tick{})
		g.pump()
		if g.leader() != leader || g.nodes[leader-1].status().Term != term {
			t.Fatalf("attempt %d disrupted stable leadership", attempt)
		}
	}
}

// R3's persistence protocol must withhold every dependent message, and R4's
// read visibility must additionally wait for the application's durable index.
func TestR3DurableCompletionOrderAndR4AppliedRead(t *testing.T) {
	c, err := New(Config{ClusterUUID: "test-cluster", NodeID: 1, Seed: 9, Protocol: 1, Bootstrap: []uint64{1, 2, 3}}, RecoveredState{})
	if err != nil {
		t.Fatal(err)
	}
	e, err := c.Step(Poll{})
	if err != nil || e.Persist == nil || e.Apply != nil || len(e.Send) != 0 {
		t.Fatalf("initial Ready: %+v %v", e, err)
	}
	id := e.Persist.ID
	if _, err = c.Step(Applied{id}); !errors.Is(err, ErrCompletion) {
		t.Fatal("application accepted before persistence")
	}
	if _, err = c.Step(Tick{}); !errors.Is(err, ErrBusy) {
		t.Fatal("input silently queued during outstanding batch")
	}
	if _, err = c.Step(Persisted{id + 1}); !errors.Is(err, ErrCompletion) {
		t.Fatal("wrong batch completion accepted")
	}
	// Mutating a host's effect cannot mutate the core's pending Ready.
	e.Persist.Entries[0].Data[0] ^= 255
	e, err = c.Step(Persisted{id})
	if err != nil || e.Apply == nil || e.Status.Applied != 0 {
		t.Fatalf("persisted: %+v %v", e, err)
	}
	if _, err = c.Step(Persisted{id}); !errors.Is(err, ErrCompletion) {
		t.Fatal("duplicate persistence accepted")
	}
	e, err = c.Step(Applied{id})
	if err != nil || e.Status.Applied != 3 {
		t.Fatalf("applied: %+v %v", e, err)
	}
	g := newGroup(t)
	leader := g.elect()
	e, err = g.nodes[leader-1].Step(Read{"delayed"})
	if err != nil {
		t.Fatal(err)
	}
	// Withhold persistence of the leader's ReadIndex heartbeat Ready.
	if e.Persist == nil || len(e.Send) != 0 || len(e.Reads) != 0 {
		t.Fatal("read escaped persistence boundary")
	}
	g.accept(leader, e, 0)
	g.pump()
	if len(g.reads) != 1 {
		t.Fatal("durable quorum did not release read")
	}
}

// R14 pins a published SplitMix64 vector independently of a replay generated by
// this wrapper, so accidental algorithm or consumption-order changes are visible.
func TestR14RandomGoldenAndNodeIsolation(t *testing.T) {
	r := splitMix64{}
	for _, want := range []uint64{0xe220a8397b1dcdaf, 0x6e789e6aa1b965f4, 0x06c45d188009454f, 0xf88bb8a8724c81ec} {
		if got := r.next(); got != want {
			t.Fatalf("got %x want %x", got, want)
		}
	}
	a, b := splitMix64{state: 7}, splitMix64{state: 7}
	other := splitMix64{state: 8}
	for i := 0; i < 100; i++ {
		if a.bounded(6) != b.bounded(6) {
			t.Fatal("interleaved node influenced another seed")
		}
		other.bounded(7)
	}
	if a.draws != 100 {
		t.Fatal("RNG consumption changed")
	}
}

// R14's deterministic boundary may not acquire hidden time, random, file or
// goroutine inputs. Runtime internals in Go/protobuf are outside the wrapper;
// they must not drive the election PRNG or semantic state.
func TestR14NoAmbientInputs(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range f.Imports {
			p := strings.Trim(im.Path.Value, "\"")
			if p == "time" || p == "os" || p == "syscall" || p == "unsafe" || strings.HasPrefix(p, "net") || strings.Contains(p, "rand") {
				t.Fatalf("ambient import in %s: %s", path, p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if _, ok := n.(*ast.GoStmt); ok {
				t.Fatalf("goroutine in %s", path)
			}
			return true
		})
	}
}

// R3's restart boundary consumes host-recorded durable bytes, not the old
// leader's live memory. R12's applied ConfState must retain a learner on restart.
func TestR3RestartAndR12Configuration(t *testing.T) {
	g := newGroup(t)
	leader := g.elect()
	g.call(leader, Propose{command(t, 3)})
	g.pump()
	cc := &pb.ConfChangeV2{Changes: []*pb.ConfChangeSingle{{Type: pb.ConfChangeAddLearnerNode.Enum(), NodeId: new(uint64(4))}}}
	data, err := wire.Marshal(cc)
	if err != nil {
		t.Fatal(err)
	}
	// Learner four is deliberately absent: it must not block majority progress.
	g.drop = func(_, to uint64) bool { return to == 4 }
	g.call(leader, Configure{data})
	g.pump()
	saved := g.durable[leader-1]
	if len(saved.ConfState.GetLearners()) != 1 || saved.ConfState.GetLearners()[0] != 4 {
		t.Fatal("learner configuration not durably handed to host")
	}
	replacement, err := New(Config{ClusterUUID: "test-cluster", NodeID: leader, Seed: 999, Protocol: ProtocolVersion}, saved)
	if err != nil {
		t.Fatal(err)
	}
	g.nodes[leader-1] = replacement
	if replacement.status().Role != raft.StateFollower || replacement.status().Applied != saved.Applied {
		t.Fatal("recovery resumed stale leadership or lost application")
	}
	g.call(leader, Poll{})
	g.pump()
	g.tick(20)
	newLeader := g.leader()
	if newLeader == 0 {
		t.Fatal("recovered group did not elect")
	}
	g.call(newLeader, Read{"after-restart"})
	g.pump()
	if len(g.reads) != 1 || g.reads[0].Index < saved.Applied {
		t.Fatal("restart lost committed progress")
	}
}

// R4 forbids peer-injected local triggers, forwarded unsigned proposals, reads,
// transfers and fake storage completions. R14 requires invalid inputs to leave
// the deterministic state untouched.
func TestR4PeerControlRejection(t *testing.T) {
	g := newGroup(t)
	leader := g.elect()
	c := g.nodes[leader-1]
	before := c.status()
	for _, typ := range []pb.MessageType{pb.MsgHup, pb.MsgBeat, pb.MsgProp, pb.MsgReadIndex, pb.MsgReadIndexResp, pb.MsgTransferLeader, pb.MsgStorageAppend, pb.MsgStorageAppendResp, pb.MsgStorageApply, pb.MsgStorageApplyResp, pb.MessageType(999)} {
		data, err := wire.Marshal(&pb.Message{Type: typ.Enum(), From: new(leader%3 + 1), To: new(leader), Term: new(before.Term)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.Step(PeerMessage{data}); !errors.Is(err, ErrPeer) {
			t.Fatalf("accepted %s: %v", typ, err)
		}
		if c.status() != before {
			t.Fatalf("rejected %s changed state", typ)
		}
	}
	data, _ := wire.Marshal(&pb.Message{Type: pb.MsgHeartbeat.Enum(), From: new(raft.LocalAppendThread), To: new(leader), Term: new(before.Term)})
	if _, err := c.Step(PeerMessage{data}); !errors.Is(err, ErrPeer) {
		t.Fatalf("fake local sender: %v", err)
	}
	follower := leader%3 + 1
	if _, err := g.nodes[follower-1].Step(Propose{command(t, 1)}); !errors.Is(err, ErrLeader) {
		t.Fatal("follower forwarded a proposal")
	}
}

// R3 requires storage errors to stop participation. R14 requires unknown
// committed schemas to stop application, never be mistaken for an empty command.
func TestR3StorageFailureAndR14UnknownSchemaStops(t *testing.T) {
	c, err := New(Config{ClusterUUID: "test-cluster", NodeID: 1, Seed: 1, Protocol: 1, Bootstrap: []uint64{1, 2, 3}}, RecoveredState{})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := c.Step(Poll{})
	e, err = c.Step(StorageFailed{e.Persist.ID})
	if !errors.Is(err, ErrStopped) || !e.Status.Stopped || e.NeedNextTick || len(e.Send) != 0 {
		t.Fatal("storage failure did not stop member")
	}
	if _, err = c.Step(Tick{}); !errors.Is(err, ErrStopped) {
		t.Fatal("stopped member accepted input")
	}
	g := newGroup(t)
	leader := g.elect()
	follower := leader%3 + 1
	status := g.nodes[follower-1].status()
	next := status.Commit + 1
	future := bytes.Replace(command(t, 1), []byte(`"schema":1`), []byte(`"schema":999`), 1)
	m := &pb.Message{Type: pb.MsgApp.Enum(), From: new(leader), To: new(follower), Term: new(status.Term), Index: new(status.Commit), LogTerm: new(status.Term), Commit: new(next), Entries: []*pb.Entry{{Type: pb.EntryNormal.Enum(), Term: new(status.Term), Index: new(next), Data: future}}}
	data, _ := wire.Marshal(m)
	e, err = g.nodes[follower-1].Step(PeerMessage{data})
	if err != nil || e.Persist == nil || len(e.Send) != 0 {
		t.Fatalf("future entry receipt: %v %+v", err, e)
	}
	e, err = g.nodes[follower-1].Step(Persisted{e.Persist.ID})
	if !errors.Is(err, ErrCommand) || !e.Status.Stopped || e.Apply != nil || len(e.Send) != 0 {
		t.Fatalf("unknown entry became a no-op: %v %+v", err, e)
	}
}

// R4 requires a read to wait for durable application even if its ReadIndex
// result arrives with committed entries. Dropping a size-delayed barrier would
// strand a valid read; exposing it early would authorize stale state.
func TestR4ReadWaitsForApplication(t *testing.T) {
	g := newGroup(t)
	leader := g.elect()
	c := g.nodes[leader-1]
	g.call(leader, Propose{command(t, 5)})
	// First deliver the append to one follower and then its durable append ack.
	var response Outbound
	for _, out := range g.messages {
		if out.To == leader%3+1 {
			g.messages = nil
			g.call(out.To, PeerMessage{out.Bytes})
			for _, reply := range g.messages {
				if reply.To == leader {
					response = reply
					break
				}
			}
			break
		}
	}
	if response.Bytes == nil {
		t.Fatal("missing durable append response")
	}
	g.messages = nil
	e, err := c.Step(PeerMessage{response.Bytes})
	if err != nil || e.Persist == nil {
		t.Fatalf("commit Ready: %v", err)
	}
	e, err = c.Step(Persisted{e.Persist.ID})
	if err != nil || e.Apply == nil {
		t.Fatal("committed application batch missing")
	}
	before := e.Status.Applied
	if before >= e.Apply.Index || len(e.Reads) != 0 {
		t.Fatal("application marked durable early")
	}
	// Host cannot start another authority read while the transaction is pending.
	if _, err = c.Step(Read{"while-applying"}); !errors.Is(err, ErrBusy) {
		t.Fatal("read crossed pending transaction")
	}
	g.accept(leader, e, 0)
	g.pump()
	g.call(leader, Read{"after-application"})
	g.pump()
	if len(g.reads) != 1 || g.reads[0].Index <= before {
		t.Fatal("read did not cover durable command")
	}
}

// R3 forbids fabricating a recoverable log when durable metadata contradicts
// it. R14 requires version mismatches to fail before the member participates.
func TestR3RecoveryRefusalAndR14Protocol(t *testing.T) {
	cfg := Config{ClusterUUID: "test-cluster", NodeID: 1, Seed: 1, Protocol: ProtocolVersion}
	cases := []RecoveredState{
		{HardState: &pb.HardState{Term: new(uint64(1)), Commit: new(uint64(1))}},
		{Applied: 1},
		{HardState: &pb.HardState{Term: new(uint64(1))}, Entries: []*pb.Entry{{Index: new(uint64(2)), Term: new(uint64(1))}}},
		{HardState: &pb.HardState{Term: new(uint64(1))}, Entries: []*pb.Entry{{Index: new(uint64(1)), Term: new(uint64(2))}}},
		{HardState: &pb.HardState{Term: new(uint64(2))}, Entries: []*pb.Entry{{Index: new(uint64(1)), Term: new(uint64(2))}, {Index: new(uint64(2)), Term: new(uint64(1))}}},
	}
	for i, state := range cases {
		if _, err := New(cfg, state); !errors.Is(err, ErrConfig) {
			t.Fatalf("recovery %d: %v", i, err)
		}
	}
	cfg.Protocol++
	if _, err := New(cfg, RecoveredState{}); !errors.Is(err, ErrProtocol) {
		t.Fatalf("protocol mismatch: %v", err)
	}
}

// R4 requires a safe current-term quorum even for the first read after an
// election. R14's read backlog is explicitly bounded and cancellable.
func TestR4FirstReadNeedsCurrentTermCommit(t *testing.T) {
	g := newGroup(t)
	// Keep the future leader's term-start entry uncommitted by withholding both
	// followers' append acknowledgments, while still delivering election votes.
	g.drop = func(from, to uint64) bool {
		for _, c := range g.nodes {
			if c.status().Role == raft.StateLeader && to == c.cfg.NodeID && from != to {
				return true
			}
		}
		return false
	}
	var leader uint64
	for i := 0; i < 20 && leader == 0; i++ {
		g.tick(1)
		leader = g.leader()
	}
	if leader == 0 {
		t.Fatal("election did not finish")
	}
	c := g.nodes[leader-1]
	g.call(leader, Read{"first"})
	g.pump()
	if len(g.reads) != 0 {
		t.Fatal("first read succeeded without current-term commit")
	}
	for i := 1; i < MaxReadRequests; i++ {
		g.call(leader, Read{string(rune(1000 + i))})
		g.pump()
	}
	if _, err := c.Step(Read{"excess"}); !errors.Is(err, ErrRead) {
		t.Fatal("read backlog unbounded")
	}
	g.call(leader, CancelRead{"first"})
	g.call(leader, Read{"replacement"})
	g.pump()
	g.drop = nil
	g.tick(1)
	if len(g.reads) == 0 {
		t.Fatal("current-term commit did not release queued read")
	}
	for _, r := range g.reads {
		if r.RequestID == "first" {
			t.Fatal("cancelled read was released")
		}
	}
}

// R4/R27 require cancelled minority reads to retain a bounded admission charge
// while RawNode still holds their contexts. Timeouts cannot grow hidden queues.
func TestR4CancelledReadsStayBoundedR27(t *testing.T) {
	g := newGroup(t)
	leader := g.elect()
	c := g.nodes[leader-1]
	g.drop = func(from, to uint64) bool { return from == leader || to == leader }
	for i := 0; i < MaxReadRequests; i++ {
		id := string(rune(1000 + i))
		g.call(leader, Read{id})
		g.pump()
		g.call(leader, CancelRead{id})
	}
	if _, err := c.Step(Read{"overflow"}); !errors.Is(err, ErrRead) {
		t.Fatalf("cancelled contexts escaped admission: %v", err)
	}
	g.drop = nil
	g.tick(1)
	if len(g.reads) != 0 {
		t.Fatal("cancelled reads returned success")
	}
	g.call(leader, Read{"fresh"})
	g.pump()
	if len(g.reads) != 1 {
		t.Fatal("completed cancellation did not release admission")
	}
}
