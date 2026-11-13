package raftsim

import (
	"container/heap"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsim/model"
	"go.etcd.io/raft/v3"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

const clusterUUID = "raftsim-contract"
const MaxEvents = 2000

type Config struct {
	TimingVersion uint16
	PaddingBytes  int
	CutSnapshot   bool
	RecordDisk    bool
	Seed          uint64
	Nodes         int
	MaxEvents     int
	// Fault rates are per thousand and apply only to network messages.
	Drop          int
	Duplicate     int
	Delay         int64
	CutNode       int
	CutPhase      string
	CutOccurrence int
	Broken        string
}
type Action struct {
	Schedule   *raftcore.ScheduleInput      `json:",omitempty"`
	Leadership *clusterstate.Membership     `json:",omitempty"`
	Evidence   *raftcore.LeadershipEvidence `json:",omitempty"`
	Kind       string
	Node       int
	Other      int
	Count      int
	Value      int64
	Request    model.Call
}
type Trace struct {
	Order      int64
	Time       int64
	Node       int
	Generation uint64
	Phase      string
	Batch      uint64
	Digest     string
}
type client struct {
	History model.Operation
	Node    int
	Bytes   string
	Done    bool
	Lost    bool
}
type member struct {
	SchedulePlan   raftcore.SchedulePlan
	SnapshotSource *origin

	Core         *raftcore.Core
	Stable       image
	Written      *image
	Torn         []byte
	Status       raftcore.Status
	Generation   uint64
	Pending      uint64
	Persisted    uint64
	ApplyFlushed uint64
	Alive        bool
}
type origin struct {
	Node       int
	Generation uint64
}
type event struct {
	Target int

	Source origin

	At         int64
	Serial     uint64
	Node       int
	Generation uint64
	Kind       string
	Input      raftcore.Input
	Persist    *raftcore.PersistBatch
	Apply      *raftcore.ApplyBatch
	Results    map[string]model.Outcome
	Client     int
	Result     model.Outcome
	Snapshot   *generation
	Chunk      int
}
type queue []event

func (q queue) Len() int { return len(q) }
func (q queue) Less(i, j int) bool {
	if q[i].At == q[j].At {
		return q[i].Serial < q[j].Serial
	}
	return q[i].At < q[j].At
}
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)   { *q = append(*q, x.(event)) }
func (q *queue) Pop() any     { old := *q; e := old[len(old)-1]; *q = old[:len(old)-1]; return e }

// Simulator owns one serial event queue. No wall time, sleep or background
// goroutine changes simulated state. Checker deadlines are outside this loop.
type diskRecord struct {
	Node    int
	Persist *raftcore.PersistBatch
	Apply   *raftcore.ApplyBatch
	App     ProbeApplication
}
type Simulator struct {
	ReadAckDelay map[int]int64

	journal []diskRecord

	initial       Config
	Config        Config
	Nodes         []*member
	Time          int64
	Events        int
	Trace         []Trace
	Actions       []Action
	Clients       []client
	Err           error
	CutHit        bool
	FailNext      map[int]bool
	Blocked       map[[2]int]bool
	Stalled       map[int]int64
	catalog       map[string]model.Call
	queue         queue
	serial        uint64
	order         int64
	rng           uint64
	leaders       map[uint64]int
	commits       map[uint64]string
	applications  map[uint64]string
	maxCommit     []uint64
	maxApplied    []uint64
	appGeneration []uint64
	maxFence      map[string]uint64
	cutCount      int
}

func New(config Config) (*Simulator, error) {
	if config.TimingVersion == 0 {
		config.TimingVersion = raftcore.ElectionTimingVersion
	}
	if config.Nodes == 0 {
		config.Nodes = 3
	}
	if config.MaxEvents == 0 {
		config.MaxEvents = MaxEvents
	}
	if config.PaddingBytes < 0 || config.PaddingBytes > 640<<10 || config.Nodes < 3 || config.Nodes > 3+clusterstate.MaxLearners || config.MaxEvents > MaxEvents || config.MaxEvents < 1 || config.Delay < 0 || config.Delay > 20 || config.Drop < 0 || config.Drop > 1000 || config.Duplicate < 0 || config.Duplicate > 1000 {
		return nil, errors.New("raftsim: invalid bounded configuration")
	}
	s := &Simulator{initial: config, Config: config, ReadAckDelay: map[int]int64{}, rng: config.Seed + 1, FailNext: map[int]bool{}, Blocked: map[[2]int]bool{}, Stalled: map[int]int64{}, catalog: map[string]model.Call{}, leaders: map[uint64]int{}, commits: map[uint64]string{}, applications: map[uint64]string{}, maxFence: map[string]uint64{}, maxCommit: make([]uint64, config.Nodes), maxApplied: make([]uint64, config.Nodes), appGeneration: make([]uint64, config.Nodes)}
	for i := 0; i < config.Nodes; i++ {
		d := freshImage()
		d.App.Padding = make([]byte, config.PaddingBytes)
		s.Nodes = append(s.Nodes, &member{Stable: d})
	}
	for i := range s.Nodes {
		s.start(i)
	}
	return s, s.Err
}
func (s *Simulator) random(n uint64) uint64 {
	s.rng = s.rng*6364136223846793005 + 1442695040888963407
	return s.rng % n
}
func (s *Simulator) stamp() int64 { s.order++; return s.order }
func (s *Simulator) fail(format string, args ...any) {
	if s.Err == nil {
		s.Err = fmt.Errorf(format, args...)
	}
}
func (s *Simulator) schedule(e event) { s.serial++; e.Serial = s.serial; heap.Push(&s.queue, e) }
func (s *Simulator) later(node int, kind string) eBuilder {
	return eBuilder{s: s, e: event{Node: node, Kind: kind, Generation: s.Nodes[node].Generation, At: s.Time + 1}}
}

type eBuilder struct {
	s *Simulator
	e event
}

func (b eBuilder) push() { b.s.schedule(b.e) }
func (s *Simulator) record(node int, phase string, batch uint64, digest string) {
	s.Trace = append(s.Trace, Trace{s.stamp(), s.Time, node, s.Nodes[node].Generation, phase, batch, digest})
}
func (s *Simulator) start(node int) {
	n := s.Nodes[node]
	recovered, err := n.Stable.recovered()
	if err != nil {
		s.fail("recovery: %v", err)
		return
	}
	cfg := raftcore.Config{TimingVersion: s.initial.TimingVersion, Anchors: []uint64{1, 2, 3}, ClusterUUID: clusterUUID, NodeID: uint64(node + 1), Protocol: raftcore.ProtocolVersion, Seed: s.random(^uint64(0))}
	if n.Stable.Hard == nil && len(n.Stable.Entries) == 0 && n.Stable.Snapshot == nil && node < 3 {
		cfg.Bootstrap = []uint64{1, 2, 3}
	}
	n.Core, err = raftcore.New(cfg, recovered)
	if err != nil {
		s.fail("node %d recovery: %v", node, err)
		return
	}
	n.Generation++
	n.Alive = true
	n.Pending = 0
	n.Persisted = 0
	n.ApplyFlushed = 0
	n.Written = nil
	n.Torn = nil
	s.record(node, "restart", 0, fmt.Sprint(cfg.Seed))
	s.step(node, raftcore.Poll{})
}
func (s *Simulator) crash(node int, power bool) {
	n := s.Nodes[node]
	sources := map[int]uint64{}
	if n.SnapshotSource != nil {
		sources[n.SnapshotSource.Node] = n.SnapshotSource.Generation
		n.SnapshotSource = nil
	}
	for _, event := range s.queue {
		if event.Node == node && event.Snapshot != nil && event.Kind != "snapshot-timeout" {
			sources[event.Source.Node] = event.Source.Generation
		}
	}
	ids := make([]int, 0, len(sources))
	for id := range sources {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		s.reportSnapshot(origin{id, sources[id]}, node, false)
	}
	if !power && n.Written != nil {
		n.Stable = n.Written.copy()
	}
	n.Alive = false
	n.Core = nil
	n.Written = nil
	n.Torn = nil
	s.record(node, "crash", n.Pending, fmt.Sprint(power))
}
func (s *Simulator) step(node int, input raftcore.Input) {
	if s.Err != nil || !s.Nodes[node].Alive {
		return
	}
	n := s.Nodes[node]
	e, err := n.Core.Step(input)
	if errors.Is(err, raftcore.ErrBusy) {
		b := s.later(node, "input")
		b.e.Input = input
		b.push()
		return
	}
	if errors.Is(err, raftcore.ErrLeader) || errors.Is(err, raftcore.ErrRead) || errors.Is(err, raftcore.ErrTransfer) {
		return
	}
	if err != nil {
		s.fail("node %d %T: %v", node, input, err)
		return
	}
	n.Status = e.Status
	stateBytes, _ := json.Marshal(e.Status)
	s.record(node, "step-"+fmt.Sprintf("%T", input), n.Pending, raftMessageDigest(stateBytes))
	if n.Status.Role == raft.StateLeader {
		if old, ok := s.leaders[n.Status.Term]; ok && old != node {
			s.fail("R4: two leaders in term %d", n.Status.Term)
		}
		s.leaders[n.Status.Term] = node
	}
	s.effects(node, e)
}
func (s *Simulator) effects(node int, e raftcore.Effects) {
	n := s.Nodes[node]
	if e.Persist != nil {
		if len(e.Send) > 0 || e.Apply != nil || len(e.Reads) > 0 {
			s.fail("R3: effects before flush")
		}
		n.Pending = e.Persist.ID
		n.Persisted = 0
		n.ApplyFlushed = 0
		b := s.later(node, "persist-write")
		if e.Persist.HardState == nil && len(e.Persist.Entries) == 0 && e.Persist.Snapshot.GetMetadata().GetIndex() == 0 {
			// Empty Ready barriers have no bytes to flush. Their explicit
			// completion still releases messages on the next scheduled event.
			n.Persisted = e.Persist.ID
			b.e.Kind = "persist-ack"
			s.record(node, "persist-empty", e.Persist.ID, "")
		}
		b.e.Persist = e.Persist
		b.push()
	}
	for _, out := range e.Send {
		if n.Pending != 0 && n.Persisted != n.Pending {
			s.fail("R3: send before flush")
		}
		m := &pb.Message{}
		if err := proto.Unmarshal(out.Bytes, m); err != nil {
			s.fail("message decode: %v", err)
			return
		}
		s.record(node, "send-"+m.GetType().String(), n.Pending, raftMessageDigest(out.Bytes))
		target := int(out.To) - 1
		if target < 0 || target >= len(s.Nodes) {
			s.fail("unknown destination")
			return
		}
		if m.GetType() == pb.MsgSnap {
			s.schedule(event{At: s.Time + 32, Node: node, Generation: n.Generation, Kind: "snapshot-timeout", Target: target, Snapshot: &generation{Pointer: clonePB(m.Snapshot)}})
		}
		if s.Blocked[[2]int{node, target}] || int(s.random(1000)) < s.Config.Drop {
			continue
		}
		copies := 1
		if int(s.random(1000)) < s.Config.Duplicate {
			copies++
		}
		for j := 0; j < copies; j++ {
			b := s.later(target, "input")
			b.e.At += int64(s.random(uint64(s.Config.Delay + 1)))
			if m.GetType() == pb.MsgHeartbeatResp && len(m.Context) > 0 {
				b.e.At += s.ReadAckDelay[target]
			}
			b.e.Input = raftcore.PeerMessage{Bytes: slices.Clone(out.Bytes)}
			if m.GetType() == pb.MsgSnap {
				g, ok := s.snapshotFor(node, m.Snapshot)
				if !ok {
					s.fail("missing durable snapshot source=%d pointer=%v generations=%d", node, m.Snapshot, len(n.Stable.Generations))
					continue
				}
				b.e.Snapshot = &g
				b.e.Source = origin{node, n.Generation}
			}
			b.push()
		}
	}
	if e.Apply != nil {
		if n.Persisted != e.Apply.ID {
			s.fail("R3: apply before flush")
		}
		b := s.later(node, "apply-write")
		b.e.Apply = e.Apply
		b.push()
	}
	for _, barrier := range e.Reads {
		id := -1
		if _, err := fmt.Sscanf(barrier.RequestID, "read-%d", &id); err != nil || id < 0 || id >= len(s.Clients) {
			s.fail("unknown read context")
			return
		}
		if n.Stable.Applied < barrier.Index {
			s.fail("R4: read before applied barrier")
		}
		s.reply(node, id, n.Stable.App.read(s.Clients[id].History.Input))
	}
	if e.Persist == nil && e.Apply == nil {
		n.Pending = 0
	}
}
func raftMessageDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (s *Simulator) reply(node, id int, out model.Outcome) {
	b := s.later(node, "reply")
	b.e.Client = id
	b.e.Result = out
	b.push()
}
func (s *Simulator) apply(node int, batch *raftcore.ApplyBatch) (image, map[string]model.Outcome) {
	n := s.Nodes[node]
	d := n.Stable.copy()
	results := map[string]model.Outcome{}
	for _, entry := range batch.Entries {
		if entry.GetType() != pb.EntryNormal || len(entry.Data) == 0 {
			continue
		}
		cmd, err := clusterstate.DecodeCommand(entry.Data)
		if err != nil {
			s.fail("command decode: %v", err)
			return d, results
		}
		if cmd.Change.PrepareQuiescence != nil {
			if d.App.Coordination == nil {
				d.App.Coordination = map[string][]byte{}
			}
			read := func(k clusterstate.RecordKey) ([]byte, error) {
				if k == clusterstate.AuthorityKey("anchor") {
					b, _ := clusterstate.Encode(clusterstate.Authority{Cluster: clusterUUID, Key: quiescenceKey().Public().(ed25519.PublicKey), Anchor: true})
					return b, nil
				}
				return d.App.Coordination[k.Namespace+"/"+k.Key], nil
			}
			meta := clusterstate.ApplicationMeta{Cluster: clusterUUID, Index: entry.GetIndex(), Term: entry.GetTerm(), Configuration: raftcore.Configuration(batch.ConfState), Now: d.App.Now, Ceiling: clusterstate.DefaultApplicationBytes}
			out, err := clusterstate.Evaluate(entry.Data, meta, read)
			if err != nil || out.Result.Code != clusterstate.OK {
				s.fail("quiescence apply: %v %v", out.Result.Code, err)
				return d, results
			}
			for _, w := range out.Writes {
				d.App.Coordination[w.Namespace+"/"+w.Key] = w.Data
			}
			continue
		}
		if d.App.Coordination != nil {
			b, _ := clusterstate.Encode(clusterstate.Quiescence{})
			d.App.Coordination["membership/quiescence"] = b
		}
		call, ok := s.catalog[string(entry.Data)]
		if !ok {
			s.fail("uncatalogued committed command")
			return d, results
		}
		results[string(entry.Data)] = d.App.evaluate(call, s.Config.Broken == "fence")
	}
	d.Applied = batch.Index
	d.Conf = clonePB(batch.ConfState)
	return d, results
}
func (s *Simulator) process(e event) {
	n := s.Nodes[e.Node]
	if !n.Alive || e.Generation != n.Generation {
		return
	}
	if until := s.Stalled[e.Node]; until > s.Time {
		e.At = until
		s.schedule(e)
		return
	}
	batch := uint64(0)
	if e.Persist != nil {
		batch = e.Persist.ID
	}
	if e.Apply != nil {
		batch = e.Apply.ID
	}
	s.record(e.Node, e.Kind, batch, "")
	if e.Snapshot != nil && e.Kind != "snapshot-timeout" {
		source := e.Source
		n.SnapshotSource = &source
	}
	isSnapshot := e.Snapshot != nil || e.Persist != nil && e.Persist.Snapshot.GetMetadata().GetIndex() > 0 || e.Apply != nil && e.Apply.Snapshot.GetMetadata().GetIndex() > 0
	if !s.CutHit && e.Node == s.Config.CutNode && e.Kind == s.Config.CutPhase && (!s.Config.CutSnapshot || isSnapshot) {
		s.cutCount++
		if s.cutCount >= max(1, s.Config.CutOccurrence) {
			s.CutHit = true
			s.crash(e.Node, true)
			return
		}
	}
	switch e.Kind {
	case "snapshot-timeout":
		if s.Nodes[e.Target].Stable.Applied < e.Snapshot.Pointer.GetMetadata().GetIndex() {
			s.step(e.Node, raftcore.TransportResult{Peer: uint64(e.Target + 1), Snapshot: true, Success: false})
		}
	case "input":
		if e.Snapshot != nil && !s.hasComplete(e.Node, e.Snapshot.Pointer) {
			if n.Written != nil {
				e.At = s.Time + 1
				s.schedule(e)
				return
			}
			e.Kind = "snapshot-begin"
			e.At = s.Time + 1
			s.schedule(e)
			return
		}
		if e.Snapshot != nil {
			source := e.Source
			n.SnapshotSource = &source
		}
		s.step(e.Node, e.Input)
	case "snapshot-begin", "snapshot-chunk", "snapshot-complete":
		s.snapshotEvent(e)
	case "persist-write":
		if s.FailNext[e.Node] {
			delete(s.FailNext, e.Node)
			_, err := n.Core.Step(raftcore.StorageFailed{BatchID: e.Persist.ID})
			if !errors.Is(err, raftcore.ErrStopped) {
				s.fail("disk error did not stop core")
			}
			s.crash(e.Node, true)
			return
		}
		d := n.Stable.copy()
		if err := d.persist(e.Persist); err != nil {
			s.fail("persist: %v", err)
			return
		}
		n.Written = &d
		if len(e.Persist.Entries) > 0 {
			b, _ := wire.Marshal(e.Persist.Entries[0])
			n.Torn = slices.Clone(b[:len(b)/2])
		}
		e.Kind = "persist-flush"
		e.At = s.Time + 1
		s.schedule(e)
		if s.Config.Broken == "flush" {
			s.step(e.Node, raftcore.Persisted{BatchID: e.Persist.ID})
		}
	case "persist-flush":
		if n.Written == nil {
			s.fail("flush without write")
			return
		}
		n.Stable = n.Written.copy()
		n.Written = nil
		n.Torn = nil
		n.Persisted = e.Persist.ID
		if s.Config.RecordDisk {
			s.journal = append(s.journal, diskRecord{Node: e.Node, Persist: e.Persist})
		}
		if e.Persist.Snapshot.GetMetadata().GetIndex() > 0 && n.SnapshotSource != nil {
			s.reportSnapshot(*n.SnapshotSource, e.Node, true)
			n.SnapshotSource = nil
		}
		e.Kind = "persist-ack"
		e.At = s.Time + 1
		s.schedule(e)
	case "persist-ack":
		s.step(e.Node, raftcore.Persisted{BatchID: e.Persist.ID})
	case "apply-write":
		d, results := s.apply(e.Node, e.Apply)
		n.Written = &d
		e.Results = results
		e.Kind = "apply-flush"
		e.At = s.Time + 1
		s.schedule(e)
	case "apply-flush":
		if n.Written == nil {
			s.fail("application flush without write")
			return
		}
		n.Stable = n.Written.copy()
		n.Written = nil
		n.ApplyFlushed = e.Apply.ID
		if s.Config.RecordDisk {
			s.journal = append(s.journal, diskRecord{Node: e.Node, Apply: e.Apply, App: n.Stable.App.copy()})
		}
		e.Kind = "apply-ack"
		e.At = s.Time + 1
		s.schedule(e)
	case "apply-ack":
		if n.ApplyFlushed != e.Apply.ID {
			s.fail("R3: application completion before flush")
		}
		s.step(e.Node, raftcore.Applied{BatchID: e.Apply.ID})
		for id := range s.Clients {
			c := &s.Clients[id]
			if c.Node != e.Node || c.Done || c.Lost {
				continue
			}
			if out, ok := e.Results[c.Bytes]; ok {
				s.reply(e.Node, id, out)
			}
		}
	case "reply":
		c := &s.Clients[e.Client]
		if !c.Lost && !c.Done {
			c.History.Output = e.Result
			c.History.Return = s.stamp()
			c.Done = true
		}
	default:
		s.fail("unknown event %s", e.Kind)
	}
	s.invariants()
}
func (s *Simulator) invariants() {
	for i, n := range s.Nodes {
		d := n.Stable
		commit := d.Hard.GetCommit()
		if commit < s.maxCommit[i] || d.Applied < s.maxApplied[i] {
			s.fail("R3: durable index regressed on node %d", i)
		}
		applicationChanged := s.maxApplied[i] != d.Applied || s.appGeneration[i] != n.Generation
		s.maxCommit[i] = commit
		s.maxApplied[i] = d.Applied
		s.appGeneration[i] = n.Generation
		for _, e := range d.Entries {
			if e.GetIndex() > commit {
				break
			}
			data, _ := wire.Marshal(e)
			digest := raftMessageDigest(data)
			if old, ok := s.commits[e.GetIndex()]; ok && old != digest {
				s.fail("R3: committed log disagreement at %d", e.GetIndex())
			}
			s.commits[e.GetIndex()] = digest
		}
		if d.Applied > 0 && applicationChanged {
			b, _ := json.Marshal(d.App)
			conf, _ := wire.Marshal(d.Conf)
			digest := raftMessageDigest(append(b, conf...))
			if old, ok := s.applications[d.Applied]; ok && old != digest {
				s.fail("R13: application disagreement at %d", d.Applied)
			}
			s.applications[d.Applied] = digest
		}
		for key, c := range d.App.Cells {
			k := fmt.Sprintf("%d/%s", i, key)
			if c.Lease.Fence < s.maxFence[k] {
				s.fail("R2: fence regression")
			}
			s.maxFence[k] = c.Lease.Fence
		}
	}
}
func (s *Simulator) run(count int, until int64) {
	for len(s.queue) > 0 && s.Err == nil && (count < 0 || count > 0) {
		if s.queue[0].At > until {
			return
		}
		if s.Events == s.Config.MaxEvents {
			s.fail("event budget exhausted (%d)", s.Events)
			return
		}
		e := heap.Pop(&s.queue).(event)
		s.Time = max(s.Time, e.At)
		s.Events++
		s.process(e)
		if count > 0 {
			count--
		}
	}
}

// Do records every external input for exact replay. Network deliveries and disk
// completions are derived from the recorded seed and these ordered actions.
func (s *Simulator) Do(a Action) int {
	// Replay owns immutable input bytes. A host updating the next leadership
	// stage must not rewrite the intent that drove an earlier configuration.
	if a.Leadership != nil {
		h := *a.Leadership
		h.Anchors = slices.Clone(h.Anchors)
		h.Nodes = slices.Clone(h.Nodes)
		if h.Leadership != nil {
			l := *h.Leadership
			h.Leadership = &l
		}
		if h.Pending != nil {
			p := *h.Pending
			h.Pending = &p
		}
		a.Leadership = &h
	}
	if a.Evidence != nil {
		e := *a.Evidence
		a.Evidence = &e
	}
	if a.Schedule != nil {
		v := *a.Schedule
		v.Acks = slices.Clone(v.Acks)
		v.Marker.Voters = slices.Clone(v.Marker.Voters)
		a.Schedule = &v
	}
	s.Actions = append(s.Actions, a)
	s.stamp()
	if s.Err != nil {
		return -1
	}
	switch a.Kind {
	case "delay-read-acks":
		s.ReadAckDelay[a.Node] = a.Value
	case "arm-cut":
		s.Config.CutSnapshot = a.Count != 0
		s.Config.CutNode = a.Node
		s.Config.CutPhase = a.Request.Kind
		s.Config.CutOccurrence = a.Other
		s.cutCount = 0
		s.CutHit = false
	case "drain":
		s.run(-1, s.Time+max(1, a.Value))
	case "events":
		s.run(a.Count, 1<<62)
	case "node-ticks":
		for j := 0; j < a.Count; j++ {
			b := s.later(a.Node, "input")
			b.e.Input = raftcore.Tick{}
			b.push()
			s.run(-1, s.Time+100)
		}
	case "ticks":
		for j := 0; j < a.Count; j++ {
			for i := range s.Nodes {
				if s.Nodes[i].Alive {
					b := s.later(i, "input")
					b.e.Input = raftcore.Tick{}
					b.push()
				}
			}
			s.run(-1, s.Time+100)
		}
	case "crash":
		s.crash(a.Node, a.Value != 0)
	case "restart":
		s.start(a.Node)
	case "schedule":
		if a.Schedule == nil {
			s.fail("missing schedule input")
			return -1
		}
		p, err := s.Nodes[a.Node].Core.Schedule(*a.Schedule)
		if err != nil {
			s.fail("schedule: %v", err)
			return -1
		}
		s.Nodes[a.Node].SchedulePlan = p
		b, _ := json.Marshal(p)
		s.record(a.Node, "schedule", 0, raftMessageDigest(b))
		if p.Tick {
			s.step(a.Node, raftcore.Tick{})
		}
	case "quiesce":
		n := s.Nodes[a.Node]
		voters := raftcore.Configuration(n.Stable.Conf).Voters
		c, err := clusterstate.Sign(clusterstate.Command{Schema: 1, Cluster: clusterUUID, Actor: "anchor", Session: fmt.Sprintf("idle-%d", n.Stable.Applied), Sequence: 1, Now: a.Value, Change: clusterstate.Mutation{PrepareQuiescence: &clusterstate.PrepareQuiescence{Barrier: n.Stable.Applied, Term: n.Status.Term, Voters: voters}}}, quiescenceKey())
		if err != nil {
			s.fail("sleep command: %v", err)
			return -1
		}
		raw, err := clusterstate.EncodeCommand(c)
		if err != nil {
			s.fail("sleep encode: %v", err)
			return -1
		}
		s.step(a.Node, raftcore.Propose{Command: raw})
	case "block":
		s.Blocked[[2]int{a.Node, a.Other}] = a.Value != 0
	case "stall":
		s.Stalled[a.Node] = s.Time + a.Value
	case "disk-fail":
		s.FailNext[a.Node] = true
	case "network":
		s.Config.Drop = a.Count
		s.Config.Duplicate = a.Other
		s.Config.Delay = a.Value
	case "snapshot":
		s.checkpoint(a.Node)
	case "leadership":
		if a.Leadership == nil || a.Evidence == nil {
			s.fail("missing leadership action evidence")
			return -1
		}
		plan, err := s.Nodes[a.Node].Core.PlanLeadership(*a.Leadership, *a.Evidence)
		if err != nil {
			s.fail("leadership plan: %v", err)
			return -1
		}
		if plan.Configure {
			s.step(a.Node, raftcore.Configure{Change: plan.Change})
		}
		if plan.Transfer != 0 {
			s.step(a.Node, raftcore.Transfer{Target: plan.Transfer})
		}
	case "configure":
		var change *pb.ConfChangeV2
		switch a.Count {
		case 1:
			change = &pb.ConfChangeV2{Changes: []*pb.ConfChangeSingle{{Type: pb.ConfChangeAddLearnerNode.Enum(), NodeId: proto.Uint64(uint64(a.Other + 1))}}}
		case 2:
			change = &pb.ConfChangeV2{Transition: pb.ConfChangeTransitionJointExplicit.Enum(), Changes: []*pb.ConfChangeSingle{{Type: pb.ConfChangeAddLearnerNode.Enum(), NodeId: proto.Uint64(3)}, {Type: pb.ConfChangeAddNode.Enum(), NodeId: proto.Uint64(4)}}}
		case 6:
			change = &pb.ConfChangeV2{}
			for node := 3; node < len(s.Nodes); node++ {
				change.Changes = append(change.Changes, &pb.ConfChangeSingle{Type: pb.ConfChangeRemoveNode.Enum(), NodeId: proto.Uint64(uint64(node + 1))})
			}
		case 5:
			change = &pb.ConfChangeV2{}
			for node := 3; node < len(s.Nodes); node++ {
				change.Changes = append(change.Changes, &pb.ConfChangeSingle{Type: pb.ConfChangeAddLearnerNode.Enum(), NodeId: proto.Uint64(uint64(node + 1))})
			}
		case 4:
			change = &pb.ConfChangeV2{Changes: []*pb.ConfChangeSingle{{Type: pb.ConfChangeRemoveNode.Enum(), NodeId: proto.Uint64(uint64(a.Other + 1))}}}
		case 3:
			change = &pb.ConfChangeV2{}
		default:
			s.fail("unknown configuration action")
			return -1
		}
		data, _ := wire.Marshal(change)
		s.step(a.Node, raftcore.Configure{Change: data})
	case "timeout":
		c := &s.Clients[a.Node]
		if !c.Done {
			c.Lost = true
			if c.History.Input.Kind == "read" || c.History.Input.Kind == "read_vault" {
				s.step(c.Node, raftcore.CancelRead{RequestID: fmt.Sprintf("read-%d", a.Node)})
			}
			c.History.Unknown = true
			c.History.Return = s.order
		}
	case "submit":
		id := len(s.Clients)
		if id == 100 {
			s.fail("history operation budget exceeded")
			return -1
		}
		call := a.Request
		c := client{Node: a.Node, History: model.Operation{ID: id, Call: s.order, Input: call}}
		if call.Kind == "read" || call.Kind == "read_vault" {
			s.Clients = append(s.Clients, c)
			if s.Config.Broken == "read" {
				s.reply(a.Node, id, s.Nodes[a.Node].Stable.App.read(call))
			} else {
				s.step(a.Node, raftcore.Read{RequestID: fmt.Sprintf("read-%d", id)})
			}
		} else {
			data, err := command(call, id)
			if err != nil {
				s.fail("command: %v", err)
				return -1
			}
			c.Bytes = string(data)
			if previous, exists := s.catalog[c.Bytes]; exists && previous != call {
				s.fail("ambiguous probe catalog command")
				return -1
			}
			s.catalog[c.Bytes] = call
			s.Clients = append(s.Clients, c)
			s.step(a.Node, raftcore.Propose{Command: data})
		}
		return id
	default:
		s.fail("unknown action %s", a.Kind)
	}
	return -1
}
func (s *Simulator) Drain()          { s.Do(Action{Kind: "drain", Value: 1000}) }
func (s *Simulator) Ticks(count int) { s.Do(Action{Kind: "ticks", Count: count}) }
func (s *Simulator) Submit(node int, op model.Call) int {
	return s.Do(Action{Kind: "submit", Node: node, Request: op})
}
func (s *Simulator) Leader() int {
	winner := -1
	var term uint64
	for i, n := range s.Nodes {
		if n.Alive && n.Status.Role == raft.StateLeader && n.Status.Term >= term {
			winner = i
			term = n.Status.Term
		}
	}
	return winner
}
func (s *Simulator) History() []model.Operation {
	out := make([]model.Operation, len(s.Clients))
	for i, c := range s.Clients {
		out[i] = c.History
		if !c.Done {
			out[i].Unknown = true
		}
	}
	return out
}
func (s *Simulator) Digest() string {
	b, _ := json.Marshal(struct {
		Trace   []Trace
		History []model.Operation
	}{s.Trace, s.History()})
	return raftMessageDigest(b)
}
