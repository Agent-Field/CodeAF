package rafttransport

import (
	"bytes"
	"crypto/ed25519"
	"math"
	"slices"
	"sync"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
)

// Lane reservations are charged by encoded envelope bytes, including metadata.
// Learners and snapshots cannot spend the normal voter reservation.
type Lane uint8

const (
	Voter Lane = iota
	Learner
	Snapshot
	laneCount
	RetentionMillis int64 = clusterstate.PeerRetentionMillis
)

// Slot reservations bound the whole member inbox to eight outstanding
// deliveries while leaving space for voter traffic beside stalled learners.
var laneSlots = [laneCount]int{MaxInflight / 2, MaxInflight / 4, MaxInflight / 4}

var laneBudget = [laneCount]int{3 * MaxQueuedBytes / 8, MaxQueuedBytes / 8, MaxQueuedBytes / 2}

// Member comes ONLY from committed enrollment. Revoked records are retained as
// tombstones, and their node IDs can never be silently admitted again.
type Member struct {
	Node        uint64
	Incarnation uint64
	Key         ed25519.PublicKey
	Voter       bool
	Revoked     bool
}

// View is installed after durable application and before acknowledging a
// registry change. Generation is durable host state incremented on router
// restart, not a volatile connection number. Queued messages are expendable.
type View struct {
	Scope      Scope
	Revision   uint64
	Generation uint64
	Members    []Member
}

// Limits cap operator memory independently of per-identity byte reservations.
// Voters includes both sides of joint consensus, rather than only incoming IDs.
type Limits struct{ Identities, Records, Voters int }

func DefaultLimits() Limits { return Limits{Identities: 1024, Records: 4096, Voters: 6} }

type queued struct {
	Delivery
	size    int
	expires int64
}
type inbox struct {
	queues              [laneCount][]queued
	next, issued, acked [laneCount]uint64
	sockets             int
	changed             chan struct{}
	chunks              map[uint64]uint64
}
type group struct {
	view    View
	members map[uint64]Member
	inboxes map[uint64]*inbox
	bytes   [laneCount]int
	now     int64
}
type Router struct {
	// Pump services locally hosted members after routing, outside the router lock.
	// It must never wait for an external member or perform network I/O.
	Pump func(Request, int64) error

	mu     sync.Mutex
	limits Limits
	groups map[Scope]*group
}

func NewRouter(limits Limits) (*Router, error) {
	if limits.Identities <= 0 || limits.Records <= 0 || limits.Voters <= 0 {
		return nil, ErrCapacity
	}
	return &Router{limits: limits, groups: make(map[Scope]*group)}, nil
}

// Install is a host seam, never an HTTP enrollment endpoint. Updating under
// the exchange lock fences requests authenticated before a committed revoke.
// Hosts must reload durable views before accepting traffic after a restart.
func (r *Router) Install(v View) error {
	if !validScope(v.Scope) || v.Revision == 0 || v.Generation == 0 || len(v.Members) > r.limits.Records {
		return ErrWire
	}
	members := make(map[uint64]Member, len(v.Members))
	learners, voters := 0, 0
	for _, m := range v.Members {
		if !node(m.Node) || m.Incarnation == 0 || len(m.Key) != ed25519.PublicKeySize {
			return ErrWire
		}
		if _, ok := members[m.Node]; ok {
			return ErrWire
		}
		m.Key = bytes.Clone(m.Key)
		members[m.Node] = m
		if !m.Revoked {
			if m.Voter {
				voters++
			} else {
				learners++
			}
		}
	}
	if learners > MaxLearners || voters > r.limits.Voters {
		return ErrCapacity
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	old := r.groups[v.Scope]
	if old == nil && len(r.groups) >= r.limits.Identities {
		return ErrCapacity
	}
	if old != nil {
		if v.Revision <= old.view.Revision || v.Generation < old.view.Generation {
			return ErrGeneration
		}
		for id, before := range old.members {
			after, ok := members[id]
			if !ok || before.Revoked && !after.Revoked || after.Incarnation < before.Incarnation || after.Incarnation == before.Incarnation && !bytes.Equal(after.Key, before.Key) {
				return ErrAuth
			}
		}
	}
	g := &group{view: View{Scope: v.Scope, Revision: v.Revision, Generation: v.Generation}, members: members, inboxes: make(map[uint64]*inbox)}
	if old != nil {
		g.now = old.now
	}
	for id, m := range members {
		if m.Revoked {
			continue
		}
		box := &inbox{changed: make(chan struct{})}
		if old != nil {
			if prev := old.inboxes[id]; prev != nil {
				// Reusing the notification channel lets open sockets notice retirement.
				close(prev.changed)
				if v.Generation == old.view.Generation && m.Incarnation == old.members[id].Incarnation {
					box.next, box.issued, box.acked = prev.next, prev.issued, prev.acked
					box.sockets = prev.sockets
					box.chunks = map[uint64]uint64{}
					for destination, sequence := range prev.chunks {
						if peer, ok := members[destination]; ok && !peer.Revoked {
							box.chunks[destination] = sequence
						}
					}
				}
			}
		}
		g.inboxes[id] = box
	}
	if old != nil {
		for id, box := range old.inboxes {
			if m, ok := members[id]; !ok || m.Revoked {
				close(box.changed)
			}
		}
	}
	r.groups[v.Scope] = g
	return nil
}

func signal(box *inbox) { close(box.changed); box.changed = make(chan struct{}) }
func (g *group) expire(now int64) {
	g.now = max(g.now, min(max(now, 0), math.MaxInt64-RetentionMillis))
	for _, box := range g.inboxes {
		for lane := Lane(0); lane < laneCount; lane++ {
			keep := box.queues[lane][:0]
			for _, q := range box.queues[lane] {
				if q.expires > g.now {
					keep = append(keep, q)
				} else {
					g.bytes[lane] -= q.size
				}
			}
			clear(box.queues[lane][len(keep):])
			box.queues[lane] = keep
		}
	}
}
func (g *group) member(id, inc uint64) (Member, error) {
	m, ok := g.members[id]
	if !ok || m.Revoked || m.Incarnation != inc {
		return Member{}, ErrAuth
	}
	return m, nil
}
func (g *group) lane(e Envelope) Lane {
	if e.Chunk != nil {
		return Snapshot
	}
	if g.members[e.From].Voter && g.members[e.To].Voter {
		return Voter
	}
	return Learner
}

// Delivery.Cursor is scoped to one lane and durable router generation. An ack
// releases transport memory only; it never completes a Raft persistence batch.
type Delivery struct {
	Lane     Lane     `json:"lane"`
	Cursor   uint64   `json:"cursor,string"`
	Envelope Envelope `json:"envelope"`
}
type ChunkAck struct {
	Node     uint64 `json:"node,string"`
	Sequence uint64 `json:"sequence,string"`
}

type Response struct {
	Chunks []ChunkAck `json:"chunks,omitempty"`
	// Accepted/Rejected describe SOCKET admission only. Neither means a peer
	// persisted an append; a busy learner must not close its sender's voter path.
	Accepted   []string   `json:"accepted,omitempty"`
	Rejected   []string   `json:"rejected,omitempty"`
	Generation uint64     `json:"generation,string"`
	Messages   []Delivery `json:"messages"`
}

func (r *Router) authorize(req Request) (*group, *inbox, error) {
	g := r.groups[req.Scope]
	if g == nil {
		return nil, nil, ErrAuth
	}
	if req.Generation != g.view.Generation {
		return nil, nil, ErrGeneration
	}
	m, err := g.member(req.Node, req.Incarnation)
	if err != nil {
		return nil, nil, err
	}
	if err = req.Verify(m.Key); err != nil {
		return nil, nil, err
	}
	return g, g.inboxes[req.Node], nil
}

// Exchange validates the entire outbound batch before admitting any message.
// Valid delivery acks still release memory on backpressure, so a full sender
// cannot prevent its own incoming queue from draining. Retries retain bytes.
func (r *Router) Exchange(req Request, now int64) (Response, error) {
	return r.exchange(req, now, false)
}
func (r *Router) exchange(req Request, now int64, receiveOnly bool) (Response, error) {
	response, err := r.route(req, now, receiveOnly)
	if err != nil || r.Pump == nil {
		return response, err
	}
	if err = r.Pump(req, now); err != nil {
		return Response{}, err
	}
	return r.route(req, now, true)
}

// LocalExchange is the actor's internal side of the SAME authenticated router.
// It avoids recursively entering Pump while that actor holds its own queue.
func (r *Router) LocalExchange(req Request, now int64) (Response, error) {
	return r.route(req, now, false)
}

func (r *Router) route(req Request, now int64, receiveOnly bool) (Response, error) {
	if now < 0 || now > math.MaxInt64-RetentionMillis {
		return Response{}, ErrWire
	}
	if err := req.validate(); err != nil {
		return Response{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	g, box, err := r.authorize(req)
	if err != nil {
		return Response{}, err
	}
	for lane, ack := range req.Ack {
		if ack > box.issued[lane] {
			return Response{}, ErrCursor
		}
	}
	type pending struct {
		e    Envelope
		lane Lane
		size int
	}
	plan := make([]pending, 0, len(req.Messages))
	messages := req.Messages
	if receiveOnly {
		messages = nil
	}
	for _, e := range messages {
		if e.Scope != req.Scope || e.From != req.Node || e.Incarnation != req.Incarnation {
			return Response{}, ErrAuth
		}
		if _, err = g.member(e.To, e.Destination); err != nil {
			return Response{}, err
		}
		if err = e.Verify(g.members[e.From].Key); err != nil {
			return Response{}, err
		}
		encoded, _ := Encode(e)
		plan = append(plan, pending{e, g.lane(e), len(encoded)})
	}
	g.expire(now)
	for lane, ack := range req.Ack {
		if ack <= box.acked[lane] {
			continue
		}
		keep := box.queues[lane][:0]
		for _, q := range box.queues[lane] {
			if q.Cursor > ack {
				keep = append(keep, q)
			} else {
				g.bytes[lane] -= q.size
				if q.Envelope.Chunk != nil {
					if source := g.inboxes[q.Envelope.From]; source != nil {
						if source.chunks == nil {
							source.chunks = map[uint64]uint64{}
						}
						if q.Envelope.Sequence > source.chunks[req.Node] {
							source.chunks[req.Node] = q.Envelope.Sequence
							signal(source)
						}
					}
				}
			}
		}
		clear(box.queues[lane][len(keep):])
		box.queues[lane] = keep
		box.acked[lane] = ack
	}
	// Pending duplicates are idempotent. Previously acknowledged or expired
	// traffic can be redelivered; Raft, rather than an unbounded replay table,
	// resolves that duplication. Reordering never invalidates an older sequence.
	added := [laneCount]int{}
	counts := make(map[[2]uint64]int)
	accepted := plan[:0]
	for _, p := range plan {
		target := g.inboxes[p.e.To]
		duplicate := false
		for _, queue := range target.queues {
			for _, q := range queue {
				if q.Envelope.From == p.e.From && q.Envelope.Sequence == p.e.Sequence {
					if !sameEnvelope(q.Envelope, p.e) {
						return Response{}, ErrAuth
					}
					duplicate = true
					break
				}
			}
		}
		for _, q := range accepted {
			if q.e.To == p.e.To && q.e.Sequence == p.e.Sequence {
				if !sameEnvelope(q.e, p.e) {
					return Response{}, ErrAuth
				}
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		k := [2]uint64{p.e.To, uint64(p.lane)}
		counts[k]++
		added[p.lane] += p.size
		if len(target.queues[p.lane])+counts[k] > laneSlots[p.lane] || g.bytes[p.lane]+added[p.lane] > laneBudget[p.lane] || target.next[p.lane] > math.MaxUint64-uint64(counts[k]) {
			return Response{}, ErrCapacity
		}
		accepted = append(accepted, p)
	}
	for _, p := range accepted {
		target := g.inboxes[p.e.To]
		target.next[p.lane]++
		b, _ := Encode(p.e)
		var owned Envelope
		_ = Decode(b, &owned)
		target.queues[p.lane] = append(target.queues[p.lane], queued{Delivery: Delivery{p.lane, target.next[p.lane], owned}, size: p.size, expires: g.now + RetentionMillis})
		g.bytes[p.lane] += p.size
		signal(target)
	}
	response := Response{Generation: g.view.Generation, Messages: []Delivery{}}
	destinations := make([]uint64, 0, len(box.chunks))
	for id := range box.chunks {
		destinations = append(destinations, id)
	}
	slices.Sort(destinations)
	for _, id := range destinations {
		response.Chunks = append(response.Chunks, ChunkAck{Node: id, Sequence: box.chunks[id]})
	}
	size := 0
	normal := 0
	snapshot := false
	for lane := Lane(0); lane < laneCount; lane++ {
		for _, q := range box.queues[lane] {
			payload := len(q.Envelope.Payload)
			if q.Envelope.Chunk != nil {
				if snapshot || normal != 0 {
					break
				}
				snapshot = true
			} else {
				if snapshot || normal+payload > MaxBatchBytes {
					break
				}
				normal += payload
			}
			b, _ := Encode(q.Delivery)
			if size+len(b) > MaxExchangeBytes-1024 {
				break
			}
			var owned Delivery
			_ = Decode(b, &owned)
			response.Messages = append(response.Messages, owned)
			size += len(b)
			box.issued[lane] = max(box.issued[lane], q.Cursor)
		}
	}
	return response, nil
}
func sameEnvelope(a, b Envelope) bool { x, _ := Encode(a); y, _ := Encode(b); return bytes.Equal(x, y) }

// Stats exposes counters without payloads. Expiration uses explicit host time;
// inactive identities need no recurring cleanup alarm or disk write.
type Stats struct {
	Bytes           [laneCount]int
	Queued, Sockets int
}

func (r *Router) Stats(scope Scope, now int64) Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	g := r.groups[scope]
	if g == nil {
		return Stats{}
	}
	g.expire(now)
	s := Stats{Bytes: g.bytes}
	for _, box := range g.inboxes {
		s.Sockets += box.sockets
		for _, q := range box.queues {
			s.Queued += len(q)
		}
	}
	return s
}

// Quiesce discards only expendable deliveries to nonvoters at the all-voter
// sleep boundary. Learner queues cannot keep a group active or push old work
// after sleep. Raft sends fresh append or snapshot traffic after public wake.
func (r *Router) Quiesce(scope Scope) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g := r.groups[scope]
	if g == nil {
		return
	}
	for id, box := range g.inboxes {
		if g.members[id].Voter {
			continue
		}
		for lane, queue := range box.queues {
			for _, q := range queue {
				g.bytes[lane] -= q.size
			}
			box.queues[lane] = nil
		}
	}
}
