package raftsim

import (
	"crypto/ed25519"
	"fmt"
	"math/rand"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsim/model"
	"github.com/anishathalye/porcupine"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

type assignmentNode struct {
	core *raftcore.Core
	disk raftcore.RecoveredState
	rows map[cs.RecordKey][]byte
	meta cs.ApplicationMeta
}
type assignmentGroup struct {
	responders map[string]int
	trace      []string
	t          *testing.T
	seed       uint64
	nodes      [3]assignmentNode
	queue      []raftcore.Outbound
	rng        *rand.Rand
	replies    map[string]cs.Result
	events     int
	cut        string
	cutDone    bool
	worker     string
	keys       map[string]ed25519.PrivateKey
	spec       cs.Assign
}

func assignmentKey(id string) ed25519.PrivateKey {
	var seed [32]byte
	copy(seed[:], id)
	return ed25519.NewKeyFromSeed(seed[:])
}
func newAssignmentGroup(t *testing.T, seed uint64) *assignmentGroup {
	g := &assignmentGroup{t: t, seed: seed, rng: rand.New(rand.NewSource(int64(seed))), replies: map[string]cs.Result{}, responders: map[string]int{}, keys: map[string]ed25519.PrivateKey{}}
	for _, id := range []string{"root", "owner", "worker", "other"} {
		g.keys[id] = assignmentKey(id)
	}
	g.worker = cs.DeviceIDOfKey(g.keys["worker"].Public().(ed25519.PublicKey))
	cert := cs.WorkerCertificate{V: 1, Identity: cs.IdentityID(g.keys["root"].Public().(ed25519.PublicKey)), Cluster: "assignment-sim", Key: g.keys["worker"].Public().(ed25519.PublicKey), Made: 1, Expires: 100000, Cells: []string{"child"}}
	b, _ := cs.WorkerCertificateBytes(cert)
	cert.Signature = ed25519.Sign(g.keys["root"], b)
	g.spec = cs.Assign{ID: "opaque-run", Revision: 1, Cell: "child", ParentCell: "parent", Expect: cs.LeaseExpectation{Device: "owner", Fence: 1}, Target: g.worker, Incarnation: 1, Expires: 90000, SealedPayload: []byte{0x81, 0x54}}
	for i := range g.nodes {
		n := &g.nodes[i]
		n.rows = map[cs.RecordKey][]byte{}
		n.meta = cs.ApplicationMeta{Cluster: "assignment-sim", Now: 1000}
		put := func(k cs.RecordKey, v any) { n.rows[k], _ = cs.Encode(v) }
		put(cs.IdentityKey(), cs.IdentityRec{V: 1, Identity: cert.Identity, SigningKey: g.keys["root"].Public().(ed25519.PublicKey)})
		for _, id := range []string{"owner", "other"} {
			put(cs.AuthorityKey(id), cs.Authority{Cluster: "assignment-sim", Key: g.keys[id].Public().(ed25519.PublicKey), Personal: true})
			put(cs.DeviceKey(id), cs.Device{V: 1})
		}
		put(cs.AuthorityKey(g.worker), cs.Authority{Cluster: "assignment-sim", Key: cert.Key, Worker: &cert})
		put(cs.DeviceKey(g.worker), cs.Device{V: 1})
		put(cs.WorkerKey(g.worker), cs.WorkerRecord{Incarnation: 1, AvailableUntil: 91000, Grants: map[string][]byte{"child": {0x80}}})
		child := cs.Created(cs.CellInit{Head: "head"}, "owner", 1000)
		child.ParentCell = "parent"
		put(cs.CellKey("child"), child)
		put(cs.CellKey("parent"), cs.Created(cs.CellInit{Head: "parent-head"}, "owner", 1000))
		put(cs.AssignmentKey(g.spec.ID), cs.Assignment{Spec: g.spec, Status: "pending"})
		put(cs.AssignmentSlotKey("child"), cs.AssignmentSlot{Ref: (cs.Assignment{Spec: g.spec}).Ref(), Active: true})
		var err error
		n.core, err = raftcore.New(g.config(i, true), n.disk)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := range g.nodes {
		g.step(i, raftcore.Poll{})
	}
	g.pump()
	for k := 0; k < 96 && g.leader() < 0; k++ {
		for i := range g.nodes {
			g.step(i, raftcore.Tick{})
		}
		g.pump()
	}
	if g.leader() < 0 {
		t.Fatal("R18 no elected leader", seed)
	}
	return g
}
func (g *assignmentGroup) config(i int, bootstrap bool) raftcore.Config {
	c := raftcore.Config{ClusterUUID: "assignment-sim", NodeID: uint64(i + 1), Seed: g.seed*7 + uint64(i+1), Protocol: 1}
	if bootstrap {
		c.Bootstrap = []uint64{1, 2, 3}
	}
	return c
}
func (g *assignmentGroup) leader() int {
	for i := range g.nodes {
		e, err := g.nodes[i].core.Step(raftcore.Poll{})
		if err != nil {
			g.t.Fatal(err)
		}
		g.accept(i, e)
		if e.Status.Role == raft.StateLeader {
			return i
		}
	}
	return -1
}
func (g *assignmentGroup) step(i int, in raftcore.Input) {
	g.events++
	g.trace = append(g.trace, fmt.Sprintf("%d node=%d input=%T", g.events, i, in))
	if g.events > 2000 {
		g.t.Fatal("R18 event budget", g.seed)
	}
	e, err := g.nodes[i].core.Step(in)
	if err != nil {
		g.t.Fatal(g.seed, err)
	}
	g.accept(i, e)
}
func (g *assignmentGroup) restart(i int) {
	n := &g.nodes[i]
	var err error
	n.core, err = raftcore.New(g.config(i, false), n.disk)
	if err != nil {
		g.t.Fatal(err)
	}
	g.step(i, raftcore.Poll{})
}
func (g *assignmentGroup) accept(i int, e raftcore.Effects) {
	n := &g.nodes[i]
	g.queue = append(g.queue, e.Send...)
	if e.Persist != nil {
		p := e.Persist
		if !g.cutDone && g.cut == "before-persist" {
			g.cutDone = true
			g.restart(i)
			return
		}
		if !raft.IsEmptyHardState(p.HardState) {
			n.disk.HardState = proto.Clone(p.HardState).(*pb.HardState)
		}
		if len(p.Entries) > 0 {
			first := p.Entries[0].GetIndex()
			kept := 0
			for _, v := range n.disk.Entries {
				if v.GetIndex() < first {
					kept++
				}
			}
			n.disk.Entries = n.disk.Entries[:kept]
			for _, v := range p.Entries {
				n.disk.Entries = append(n.disk.Entries, proto.Clone(v).(*pb.Entry))
			}
		}
		if !g.cutDone && g.cut == "after-persist" {
			g.cutDone = true
			g.restart(i)
			return
		}
		g.step(i, raftcore.Persisted{BatchID: p.ID})
	}
	if e.Apply != nil {
		a := e.Apply
		if !g.cutDone && g.cut == "before-apply" {
			g.cutDone = true
			g.restart(i)
			return
		}
		results := map[string]cs.Result{}
		for _, entry := range a.Entries {
			if entry.GetType() != pb.EntryNormal || len(entry.Data) == 0 {
				continue
			}
			n.meta.Index = entry.GetIndex()
			n.meta.Bytes = 0
			for k, b := range n.rows {
				n.meta.Bytes += cs.RecordBytes(k, b)
			}
			out, err := cs.Evaluate(entry.Data, n.meta, func(k cs.RecordKey) ([]byte, error) { return n.rows[k], nil })
			if err != nil {
				g.t.Fatal(g.seed, err)
			}
			for _, w := range out.Writes {
				n.rows[w.RecordKey] = w.Data
			}
			n.meta.Now = out.Now
			n.meta.Version = out.Version
			c, _ := cs.DecodeCommand(entry.Data)
			results[c.Session] = out.Result
		}
		n.disk.Applied = a.Index
		n.disk.ConfState = proto.Clone(a.ConfState).(*pb.ConfState)
		if !g.cutDone && (g.cut == "after-apply" || g.cut == "before-reply") {
			g.cutDone = true
			g.restart(i)
			return
		}
		g.step(i, raftcore.Applied{BatchID: a.ID})
		for id, r := range results {
			if g.responders[id] == i {
				g.replies[id] = r
			}
		}
	}
}
func (g *assignmentGroup) pump() {
	for len(g.queue) > 0 {
		j := g.rng.Intn(len(g.queue))
		m := g.queue[j]
		g.queue = append(g.queue[:j], g.queue[j+1:]...)
		g.step(int(m.To)-1, raftcore.PeerMessage{Bytes: m.Bytes})
	}
}
func (g *assignmentGroup) command(kind string) cs.Command {
	ref := (cs.Assignment{Spec: g.spec}).Ref()
	m := cs.Mutation{}
	actor := "owner"
	inc := uint64(0)
	switch kind {
	case "claim":
		actor = "worker"
		inc = 1
		m.ClaimAssignment = &cs.ClaimAssignment{Ref: ref, Expect: g.spec.Expect}
	case "acquire":
		actor = "other"
		m.Acquire = &cs.AcquireCell{Cell: "child", Force: true, Expect: g.spec.Expect}
	case "cancel":
		m.CancelAssignment = &cs.CancelAssignment{Ref: ref}
	case "reassign":
		v := g.spec
		v.Revision = 2
		m.Reassign = &v
	case "incarnation":
		actor = "worker"
		inc = 1
		m.RegisterWorker = &cs.RegisterWorker{Previous: 1, Incarnation: 2}
	case "drain":
		m.DrainWorker = &cs.DrainWorker{Worker: g.worker, Incarnation: 1}
	case "revoke":
		m.Revoke = &cs.DeviceID{Device: g.worker}
	case "parent_take":
		actor = "other"
		m.Acquire = &cs.AcquireCell{Cell: "parent", Force: true, Expect: cs.LeaseExpectation{Device: "owner", Fence: 1}}
	case "finish":
		actor = "worker"
		inc = 1
		m.FinishAssignment = &cs.FinishAssignment{Ref: ref, Fence: 2, Head: "head"}
	}
	id := actor
	if actor == "worker" {
		id = g.worker
	}
	c, err := cs.Sign(cs.Command{Schema: 1, Cluster: "assignment-sim", Actor: id, WorkerIncarnation: inc, Session: kind, Sequence: 1, Now: 1001, Change: m}, g.keys[actor])
	if err != nil {
		g.t.Fatal(err)
	}
	return c
}
func (g *assignmentGroup) submit(c cs.Command) {
	b, err := cs.EncodeCommand(c)
	if err != nil {
		g.t.Fatal(err)
	}
	i := g.leader()
	if i < 0 {
		g.t.Fatal("no leader before proposal")
	}
	g.responders[c.Session] = i
	g.step(i, raftcore.Propose{Command: b})
}

// R18/R19 run the production signed evaluator behind three REAL cores and
// durable Ready/Apply completions. Seeded reordering and crash cuts preserve
// unknown operations in one independent Porcupine history, never dropping them.
func TestR18R19AssignmentCoreRaceAndCrashCampaign(t *testing.T) {
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%04d", seed), func(t *testing.T) {
			g := newAssignmentGroup(t, uint64(seed))
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("R18 replay seed=%d flush/event trace=%v", seed, g.trace)
				}
			})
			kinds := []string{"acquire", "cancel", "reassign", "incarnation", "drain", "revoke", "parent_take"}
			other := kinds[seed%len(kinds)]
			first, second := g.command("claim"), g.command(other)
			if g.rng.Intn(2) == 1 {
				first, second = second, first
			}
			cuts := []string{"", "before-persist", "after-persist", "before-apply", "after-apply", "before-reply"}
			g.cut = cuts[seed%len(cuts)]
			// Both invocations precede either reply. A cut may remove a proposal, so
			// bounded retransmission retries the SAME durable sequence after recovery.
			g.submit(first)
			g.pump()
			for n := 0; n < 96; n++ {
				if g.leader() >= 0 {
					break
				}
				for i := range g.nodes {
					g.step(i, raftcore.Tick{})
				}
				g.pump()
			}
			g.submit(second)
			g.pump()
			for _, c := range []cs.Command{first, second} {
				for n := 0; n < 96; n++ {
					if _, ok := g.replies[c.Session]; ok {
						break
					}
					for i := range g.nodes {
						g.step(i, raftcore.Tick{})
					}
					g.pump()
					if g.leader() >= 0 {
						g.submit(c)
						g.pump()
					}
				}
			}
			history := []model.AssignmentOperation{}
			for j, c := range []cs.Command{first, second} {
				r, ok := g.replies[c.Session]
				if !ok {
					t.Fatal("R18 retry never resolved", seed, c.Session)
				}
				history = append(history, model.AssignmentOperation{ID: j, Call: 1, Return: 3, Input: model.AssignmentCall{Kind: c.Session}, Code: string(r.Code)})
			}
			if other == "parent_take" {
				finish := g.command("finish")
				g.submit(finish)
				g.pump()
				r, ok := g.replies[finish.Session]
				if !ok {
					t.Fatal("R19 finish did not commit")
				}
				history = append(history, model.AssignmentOperation{ID: 2, Call: 4, Return: 5, Input: model.AssignmentCall{Kind: "finish"}, Code: string(r.Code)})
				for i := range g.nodes {
					var p cs.Cell
					_ = cs.Decode(g.nodes[i].rows[cs.CellKey("parent")], &p)
					if p.Lease.Fence != 2 || p.Head != "parent-head" {
						t.Fatal("R19 finish changed parent's head/fence")
					}
				}
			}
			initial := model.AssignmentState{Holder: "owner", Fence: 1, Status: "pending", Revision: 1, Incarnation: 1, Available: true, ParentFence: 1}
			if got := model.CheckAssignments(initial, history); got != porcupine.Ok {
				t.Fatalf("R18 seed %d history %s: %+v", seed, got, history)
			}
			// A lost successful reply remains unknown in a second checked history.
			unknown := append([]model.AssignmentOperation(nil), history...)
			unknown[0].Unknown = true
			if model.CheckAssignments(initial, unknown) != porcupine.Ok {
				t.Fatal("R18 unknown outcome dropped")
			}
			for i := range g.nodes {
				g.restart(i)
			}
			g.pump()
			for i := range g.nodes {
				var c cs.Cell
				if cs.Decode(g.nodes[i].rows[cs.CellKey("child")], &c) != nil {
					t.Fatal("lost child")
				}
				if c.Lease.Fence > 2 {
					t.Fatal("R18 duplicate claim advanced fence")
				}
			}
		})
	}
}
