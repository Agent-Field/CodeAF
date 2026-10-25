package syncsetup

import (
	"bytes"
	"context"
	"encoding/base64"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
	"net/http"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	"github.com/coder/websocket"
)

// startForward opens application ingress separately from the authenticated
// peer stream. Its lifetime is the SAME terminal/scoped member owner.
func (m *personalMember) startForward(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.owner.memberAllowed() {
		return ErrQuiet
	}
	auth, err := m.request()
	if err != nil {
		return err
	}
	auth.Messages = nil
	if err = auth.SignWith(m.owner.Device.Sign); err != nil {
		return err
	}
	b, _ := rafttransport.Encode(auth)
	headers := http.Header{}
	headers.Set(rafttransport.SocketAuthHeader, base64.RawURLEncoding.EncodeToString(b))
	conn, resp, err := websocket.Dial(ctx, m.owner.Relay+clusterctl.ForwardPath, &websocket.DialOptions{HTTPClient: m.owner.ClusterHTTP(&http.Client{Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}), HTTPHeader: headers, CompressionMode: websocket.CompressionDisabled})
	if resp != nil && err != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return err
	}
	conn.SetReadLimit(rafttransport.MaxExchangeBytes)
	m.forwardConn = conn
	m.forwardDone = make(chan struct{})
	m.forwardReplies = make(chan clusterctl.Forward, rafttransport.MaxInflight)
	m.forwardPending = map[string]clusterctl.Forward{}
	m.forwardStarted = map[string]time.Time{}
	go func() {
		defer close(m.forwardDone)
		defer func() {
			m.mu.Lock()
			if !m.closed && m.failure == nil && m.owner.memberAllowed() {
				m.failure = rafttransport.ErrGeneration
			}
			close(m.changed)
			m.changed = make(chan struct{})
			m.mu.Unlock()
		}()
		writeCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		writerDone := make(chan struct{})
		go func() {
			defer close(writerDone)
			for {
				select {
				case <-writeCtx.Done():
					return
				case reply := <-m.forwardReplies:
					if !m.owner.memberAllowed() {
						conn.CloseNow()
						return
					}
					reply.Sign(m.owner.Device.Sign)
					b, _ := rafttransport.Encode(reply)
					deadline, c := context.WithTimeout(writeCtx, requestTimeout)
					m.owner.clusterFrames.Add(1)
					err := conn.Write(deadline, websocket.MessageBinary, b)
					c()
					if err != nil {
						conn.CloseNow()
						return
					}
				}
			}
		}()
		defer func() { cancel(); conn.CloseNow(); <-writerDone }()
		for {
			kind, b, err := conn.Read(ctx)
			if err != nil {
				return
			}
			m.mu.Lock()
			var work clusterctl.Forward
			if !m.owner.memberAllowed() || kind != websocket.MessageBinary || rafttransport.Decode(b, &work) != nil {
				m.mu.Unlock()
				return
			}
			valid := work.Scope == m.record.Grant.Scope && work.Generation == m.record.Grant.Generation && work.To == m.record.Grant.Node
			authorized := false
			// Only immutable provisioned anchor keys may route application ingress.
			h, e := raftdirectory.LeadershipState(ctx, m.store)
			if e == nil {
				for _, id := range h.Anchors {
					if id == work.From {
						for _, peer := range m.record.Grant.Members {
							authorized = authorized || peer.Node == id && !peer.Revoked && work.Verify(peer.Key)
						}
					}
				}
			}
			if !valid || !authorized {
				m.mu.Unlock()
				return
			}
			err = m.acceptForward(ctx, work)
			if err == nil && (len(m.pending) > 0 || m.hasSnapshotWork()) {
				err = m.flushPeer(ctx)
			}
			if err != nil {
				m.failure = err
			}
			close(m.changed)
			m.changed = make(chan struct{})
			m.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return nil
}
func (m *personalMember) answerForward(work clusterctl.Forward) {
	work.From, work.To = work.To, work.From
	work.Command = nil
	work.Schedule = nil
	work.Status = clusterctl.PeerStatus(m.status)
	if work.Applied == 0 {
		work.Applied = m.status.Applied
	}
	work.Signature = nil
	select {
	case m.forwardReplies <- work:
	default:
		m.failure = rafttransport.ErrCapacity
	}
}
func (m *personalMember) acceptForward(ctx context.Context, work clusterctl.Forward) error {
	for id, started := range m.forwardStarted {
		if time.Since(started) < clusterctl.ForwardTimeout {
			continue
		}
		pending, ok := m.forwardPending[id]
		delete(m.forwardStarted, id)
		delete(m.forwardPending, id)
		if ok && (pending.Action == "read" || pending.Action == "receipt" || pending.Action == "idle-read" || pending.Action == "poke") {
			if err := m.step(ctx, raftcore.CancelRead{RequestID: id}); err != nil {
				return err
			}
		}
	}
	if _, ok := m.forwardPending[work.ID]; ok {
		return nil
	}
	if len(m.forwardPending) >= rafttransport.MaxInflight {
		work.Error = "busy"
		m.answerForward(work)
		return nil
	}
	var err error
	switch work.Action {
	case "submit":
		m.sleeping = false
		if _, err = cs.DecodeCommand(work.Command); err != nil {
			break
		}
		m.forwardPending[work.ID] = work
		m.forwardStarted[work.ID] = time.Now()
		err = m.step(ctx, raftcore.Propose{Command: work.Command})
	case "read", "receipt", "idle-read", "poke":
		if work.Action == "read" || work.Action == "receipt" {
			m.sleeping = false
		}
		m.forwardPending[work.ID] = work
		m.forwardStarted[work.ID] = time.Now()
		if work.Action == "idle-read" || work.Action == "poke" {
			err = m.step(ctx, raftcore.IdleRead{RequestID: work.ID})
		} else {
			err = m.step(ctx, raftcore.Read{RequestID: work.ID})
		}
	case "membership":
		h, e := raftdirectory.LeadershipState(ctx, m.store)
		var plan raftcore.MembershipPlan
		if e == nil {
			plan, e = m.core.PlanMembership(h)
		}
		if e == nil && len(plan.Change) > 0 {
			e = m.step(ctx, raftcore.Configure{Change: plan.Change})
		}
		if e == nil && plan.Checkpoint {
			e = m.checkpoint(ctx)
		}
		work.Membership = &plan
		m.answerForwardResult(work, e)
		return nil
	case "evidence":
		var evidence raftcore.LeadershipEvidence
		evidence, err = raftdirectory.DurableLeadershipEvidence(ctx, m.store, m.record.Grant.Node, work.Revision, work.Barrier)
		if err == nil {
			work.Evidence = &evidence
		}
		m.answerForwardResult(work, err)
		return nil
	case "schedule":
		if work.Schedule != nil && len(work.Schedule.Acks) > 0 && (len(m.pending) > 0 || len(m.sent) > 0) {
			m.forwardPending[work.ID] = work
			m.forwardStarted[work.ID] = time.Now()
			return nil
		}
		if work.Schedule == nil {
			return rafttransport.ErrWire
		}
		var plan raftcore.SchedulePlan
		plan, err = raftdirectory.Schedule(ctx, m.store, m.core, work.Schedule.Now, work.Now, work.Schedule.Busy, work.Schedule.Acks)
		if err == nil && plan.Tick {
			err = m.step(ctx, raftcore.Tick{})
		}
		if err == nil {
			work.Plan = &plan
			m.sleeping = plan.Sleeping
			app, _, e := m.store.ApplicationView(ctx)
			if e != nil {
				err = e
			} else {
				key := raftstore.Key{Namespace: raftstore.Membership, Key: cs.QuiescenceKey().Key}
				rows, _, e := m.store.Read(ctx, app.Applied, []raftstore.Key{key})
				var marker cs.Quiescence
				if e == nil && cs.Decode(rows[key], &marker) == nil && marker.Index == app.Applied && marker.Term == m.status.Term {
					work.SleepAck = &raftcore.SleepAck{Node: m.record.Grant.Node, Index: marker.Index, Term: marker.Term}
				}
			}
		}
		m.answerForwardResult(work, err)
		return nil
	default:
		return rafttransport.ErrWire
	}
	if err != nil {
		delete(m.forwardPending, work.ID)
		delete(m.forwardStarted, work.ID)
		m.answerForwardResult(work, err)
	}
	return nil
}
func (m *personalMember) answerForwardResult(work clusterctl.Forward, err error) {
	if err != nil {
		work.Error = "retry"
	}
	m.answerForward(work)
}
func (m *personalMember) appliedForward(entries [][]byte, indices []uint64, results []cs.Result) {
	for id, work := range m.forwardPending {
		if work.Action != "submit" {
			continue
		}
		for i, raw := range entries {
			if bytes.Equal(raw, work.Command) && work.Barrier != 0 && indices[i] == work.Barrier {
				work.Result = &results[i]
				work.Applied = indices[i]
				m.answerForward(work)
				delete(m.forwardPending, id)
				delete(m.forwardStarted, id)
				break
			}
		}
	}
}
func (m *personalMember) readForward(ctx context.Context, id string, index uint64) {
	work, ok := m.forwardPending[id]
	if !ok {
		return
	}
	delete(m.forwardPending, id)
	delete(m.forwardStarted, id)
	work.Barrier = index
	var err error
	if work.Action == "receipt" {
		var h cs.Membership
		h, err = raftdirectory.LeadershipState(ctx, m.store)
		if err == nil {
			var r cs.LeadershipReceipt
			r, err = m.core.LeadershipReceipt(h)
			work.Receipt = &r
			work.Barrier = r.Barrier
		}
	}
	m.answerForwardResult(work, err)
}

// flushPeer shares the peer writer's member queue, so forwarded ingress can
// immediately replicate without waiting for an unrelated inbound frame.
func (m *personalMember) flushPeer(ctx context.Context) error {
	if err := m.nextSnapshot(ctx); err != nil {
		return err
	}
	if m.conn == nil {
		return nil
	}
	if m.sent == nil {
		m.sent = map[uint64]rafttransport.Envelope{}
	}
	for len(m.sent) < rafttransport.MaxInflight {
		req, err := m.request()
		if err != nil {
			return err
		}
		data, _ := rafttransport.Encode(req)
		if !m.owner.memberAllowed() {
			return ErrQuiet
		}
		deadline, cancel := context.WithTimeout(ctx, requestTimeout)
		m.owner.clusterFrames.Add(1)
		err = m.conn.Write(deadline, websocket.MessageBinary, data)
		cancel()
		if err != nil {
			return err
		}
		if len(req.Messages) == 0 {
			return nil
		}
		envelope := req.Messages[0]
		m.sent[envelope.Sequence] = envelope
		m.pending = m.pending[1:]
		if len(m.pending) == 0 {
			return nil
		}
	}
	return nil
}

// socketAdmission retires only transient sends. Backpressure reports actual
// transport failure to Raft and preserves the connection for other voters.
func (m *personalMember) socketAdmission(ctx context.Context, response rafttransport.Response) error {
	// Chunk credit means only that the target accepted that staged input. Actual
	// snapshot installation still needs its authenticated durable append reply.
	for _, ack := range response.Chunks {
		if sequence := m.chunkWaiting[ack.Node]; sequence != 0 && ack.Sequence >= sequence {
			delete(m.chunkWaiting, ack.Node)
		}
	}
	for _, text := range response.Accepted {
		sequence, err := strconv.ParseUint(text, 10, 64)
		if err != nil || strconv.FormatUint(sequence, 10) != text {
			return rafttransport.ErrWire
		}
		delete(m.sent, sequence)
	}
	for _, text := range response.Rejected {
		sequence, err := strconv.ParseUint(text, 10, 64)
		if err != nil || strconv.FormatUint(sequence, 10) != text {
			return rafttransport.ErrWire
		}
		envelope, ok := m.sent[sequence]
		if !ok {
			continue
		}
		delete(m.sent, sequence)
		snapshot := envelope.Chunk != nil && !envelope.AppendChunk()
		if !snapshot {
			message := new(pb.Message)
			if proto.Unmarshal(envelope.Payload, message) != nil {
				return rafttransport.ErrWire
			}
			snapshot = message.GetType() == pb.MsgSnap
		}
		if snapshot {
			delete(m.outSnapshots, envelope.To)
			delete(m.chunkWaiting, envelope.To)
		}
		if err = m.step(ctx, raftcore.TransportResult{Peer: envelope.To, Snapshot: snapshot}); err != nil {
			return err
		}
	}
	return nil
}

func (m *personalMember) refreshPeers(ctx context.Context) error {
	app, _, err := m.store.ApplicationView(ctx)
	if err != nil {
		return err
	}
	if app.Applied < m.record.Grant.Barrier {
		return nil
	}
	view, err := rafttransport.CommittedViewAt(m.record.Grant.Scope, m.record.Grant.Generation, app.Applied, raftcore.Configuration(app.ConfState), app.LogicalTime, func(k cs.RecordKey) ([]byte, error) {
		key := raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}
		rows, _, e := m.store.Read(ctx, app.Applied, []raftstore.Key{key})
		return rows[key], e
	})
	if err == nil {
		m.record.Grant.Members = view.Members
	}
	return err
}
func (m *personalMember) checkpoint(ctx context.Context) error {
	snapshot, err := m.store.CreateSnapshot(ctx)
	if err != nil {
		return err
	}
	if err = m.store.Compact(ctx, snapshot.GetMetadata().GetIndex()); err != nil {
		return err
	}
	return m.step(ctx, raftcore.CompactSnapshot{Snapshot: snapshot})
}

// Final sleep waits for this socket's bounded sends to be admitted, rather than
// leaving pre-sleep voter heartbeats in flight. Learner application is irrelevant.
func (m *personalMember) finishSchedules(ctx context.Context) error {
	if len(m.pending) > 0 || len(m.sent) > 0 {
		return nil
	}
	for id, work := range m.forwardPending {
		if work.Action == "schedule" {
			delete(m.forwardPending, id)
			delete(m.forwardStarted, id)
			if err := m.acceptForward(ctx, work); err != nil {
				return err
			}
		}
	}
	return nil
}
