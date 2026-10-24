package syncsetup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/coder/websocket"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

var errMemberWaiting = errors.New("this computer is still joining the cluster; retry shortly; your saved chats are still here")

// memberRecord belongs to the store, rather than the device's pairing. Losing
// an initialized store must retire its old authority instead of bootstrapping
// another log under the same node. Pending admission keeps its token on retry.
type memberRecord struct {
	Store       string
	Grant       raftdirectory.PersonalEnrollment
	Initialized bool
	Seed        uint64
}

type personalMember struct {
	closed   bool
	changed  chan struct{}
	idle     bool
	mu       sync.Mutex
	owner    *Sync
	record   memberRecord
	path     string
	lock     *os.File
	store    *raftstore.SQLite
	core     *raftcore.Core
	status   raftcore.Status
	pending  []rafttransport.Envelope
	sequence uint64
	ack      [3]uint64
	snapshot *pb.Message
	chunks   map[uint64]bool
	conn     *websocket.Conn
	cancel   context.CancelFunc
	done     chan struct{}
	failure  error
}

func (s *Sync) memberPath() string {
	sum := sha256.Sum256([]byte(s.Ledger + "/" + s.Device.ID()))
	return filepath.Join(s.Home, "cluster-member", hex.EncodeToString(sum[:]))
}

// memberAllowed fences delayed work as well as new dials. A re-pair can leave
// the old handle paired but bound to a different identity or relay.
func (s *Sync) memberAllowed() bool {
	if !MayTalk(s.Home) {
		return false
	}
	id, err := identity.Load(s.Home)
	base, e := relayBase(Resolve(s.Home).URL)
	return err == nil && e == nil && id.ID() == s.Identity.ID() && base == s.Relay
}

func (s *Sync) enrollMember(ctx context.Context, token string) (raftdirectory.PersonalEnrollment, error) {
	var grant raftdirectory.PersonalEnrollment
	if !s.memberAllowed() {
		return grant, ErrQuiet
	}
	raw, _ := json.Marshal(cs.PersonalAdmission{Store: token, Protocol: rafttransport.Protocol})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Relay+cs.PersonalAdmissionPath, bytes.NewReader(raw))
	if err != nil {
		return grant, err
	}
	reqsign.Sign(req, raw, deviceSigner{s.Identity, s.Device}, s.now())
	req.Header.Set("Content-Type", "application/json")
	client := s.ClusterHTTP(&http.Client{Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
	resp, err := client.Do(req)
	if err != nil {
		return grant, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, cs.MaxEvaluationBytes+1))
	if err != nil {
		return grant, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return grant, clusterctl.ErrUnsupported
	}
	if resp.StatusCode != http.StatusOK {
		return grant, errors.New(string(bytes.TrimSpace(b)))
	}
	if len(b) > cs.MaxEvaluationBytes || json.Unmarshal(b, &grant) != nil || grant.Scope.Identity != s.Identity.ID() || grant.Scope.Protocol != rafttransport.Protocol || grant.Node == 0 || grant.Incarnation == 0 || grant.Generation == 0 {
		return grant, rafttransport.ErrWire
	}
	return grant, nil
}

func (s *Sync) openMember(ctx context.Context) (_ *personalMember, err error) {
	if !s.memberAllowed() {
		return nil, ErrQuiet
	}
	path := s.memberPath()
	if err = os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(path, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	m := &personalMember{owner: s, path: path, lock: lock, changed: make(chan struct{})}
	defer func() {
		if err != nil {
			m.close()
		}
	}()
	if err = filelock.Lock(lock, true, true); err != nil {
		return nil, err
	}
	b, e := os.ReadFile(filepath.Join(path, "identity.json"))
	if e == nil {
		err = json.Unmarshal(b, &m.record)
	} else if !errors.Is(e, os.ErrNotExist) {
		err = e
	} else if _, exists := os.Stat(filepath.Join(path, "member.db")); !errors.Is(exists, os.ErrNotExist) {
		// Missing authority beside an existing database is damage, not permission
		// to retire the old member and try that database under newly minted bytes.
		err = raftstore.ErrIdentity
	}
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(path, "member.db")
	_, e = os.Stat(dbPath)
	if m.record.Initialized && errors.Is(e, os.ErrNotExist) {
		m.record = memberRecord{}
	}
	if m.record.Store == "" {
		var token [32]byte
		if _, err = rand.Read(token[:]); err != nil {
			return nil, err
		}
		m.record.Store = hex.EncodeToString(token[:])
		if err = saveLeadership(filepath.Join(path, "identity.json"), m.record); err != nil {
			return nil, err
		}
	}
	old := m.record.Grant
	pendingStage := old.Node != 0 && old.Barrier == 0
	// Admission is resumable from committed intent. The same store token is
	// saved before the first request, including a lost response before any DB.
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		m.record.Grant, err = s.enrollMember(ctx, m.record.Store)
		if err != nil {
			return nil, err
		}
		g := m.record.Grant
		if old.Node != 0 && (old.Node != g.Node || old.Scope != g.Scope || old.Incarnation != g.Incarnation) {
			return nil, raftstore.ErrIdentity
		}
		if err = saveLeadership(filepath.Join(path, "identity.json"), m.record); err != nil {
			return nil, err
		}
		if g.Barrier != 0 {
			break
		}
		if pendingStage {
			return nil, errMemberWaiting
		}
		pendingStage = true
		old = g
	}
	if !s.memberAllowed() {
		return nil, ErrQuiet
	}
	g := m.record.Grant
	m.store, err = raftstore.Open(dbPath, raftstore.Identity{ClusterUUID: g.Scope.Cluster, NodeID: g.Node, Protocol: g.Scope.Protocol})
	if err != nil {
		return nil, err
	}
	m.record.Initialized = true
	recovered, err := m.store.Recover(ctx)
	if err != nil {
		return nil, err
	}
	var entropy [8]byte
	if _, err = rand.Read(entropy[:]); err != nil {
		return nil, err
	}
	m.record.Seed = binary.LittleEndian.Uint64(entropy[:])
	if err = saveLeadership(filepath.Join(path, "identity.json"), m.record); err != nil {
		return nil, err
	}
	m.core, err = raftcore.New(raftcore.Config{ClusterUUID: g.Scope.Cluster, NodeID: g.Node, Protocol: g.Scope.Protocol, Seed: m.record.Seed}, recovered.Core)
	if err != nil {
		return nil, err
	}
	if err = m.step(ctx, raftcore.Poll{}); err != nil {
		return nil, err
	}
	for {
		beforeApplied, beforeAck := m.status.Applied, m.ack
		sent := len(m.pending) != 0
		if err = m.exchange(ctx); err != nil {
			return nil, err
		}
		if m.status.Applied >= g.Barrier && len(m.pending) == 0 {
			// The extra round reports the durable application, not transport receipt.
			if err = m.exchange(ctx); err != nil {
				return nil, err
			}
			break
		}
		// An unavailable leader must not turn activation into an empty hot poll.
		// Retain the same durable store and resume at the owner's next retry round.
		if !sent && beforeApplied == m.status.Applied && beforeAck == m.ack && len(m.pending) == 0 {
			return nil, errMemberWaiting
		}
	}
	return m, nil
}

func (m *personalMember) step(ctx context.Context, input raftcore.Input) error {
	for {
		if !m.owner.memberAllowed() {
			return ErrQuiet
		}
		effects, err := m.core.Step(input)
		if err != nil {
			return err
		}
		m.status = effects.Status
		for _, out := range effects.Send {
			var destination uint64
			for _, peer := range m.record.Grant.Members {
				if peer.Node == out.To && !peer.Revoked {
					destination = peer.Incarnation
				}
			}
			m.sequence++
			e := rafttransport.Envelope{Scope: m.record.Grant.Scope, From: m.record.Grant.Node, To: out.To, Incarnation: m.record.Grant.Incarnation, Destination: destination, Sequence: m.sequence, Payload: out.Bytes}
			if err = e.SignWith(m.owner.Device.Sign); err != nil {
				return err
			}
			m.pending = append(m.pending, e)
		}
		if effects.Persist != nil {
			if err = m.store.Persist(ctx, *effects.Persist); err != nil {
				return err
			}
			input = raftcore.Persisted{BatchID: effects.Persist.ID}
			continue
		}
		if effects.Apply != nil {
			if _, err = raftdirectory.Apply(ctx, m.store, *effects.Apply, m.record.Grant.Scope.Cluster, 0); err != nil {
				return err
			}
			input = raftcore.Applied{BatchID: effects.Apply.ID}
			continue
		}
		if effects.SnapshotNeeded != nil {
			p, err := m.store.CreateSnapshot(ctx)
			if err != nil {
				return err
			}
			if err = m.store.Compact(ctx, p.GetMetadata().GetIndex()); err != nil {
				return err
			}
			input = raftcore.CompactSnapshot{Snapshot: p}
			continue
		}
		return nil
	}
}

func (m *personalMember) request() (rafttransport.Request, error) {
	if !m.owner.memberAllowed() {
		return rafttransport.Request{}, ErrQuiet
	}
	g := m.record.Grant
	r := rafttransport.Request{Scope: g.Scope, Node: g.Node, Incarnation: g.Incarnation, Generation: g.Generation, Ack: m.ack, Applied: m.status.Applied}
	if len(m.pending) > 0 {
		r.Messages = m.pending[:1]
	}
	err := r.SignWith(m.owner.Device.Sign)
	return r, err
}
func (m *personalMember) exchange(ctx context.Context) error {
	req, err := m.request()
	if err != nil {
		return err
	}
	response, err := (rafttransport.Client{URL: m.owner.Relay, HTTP: m.owner.ClusterHTTP(nil)}).Exchange(ctx, req)
	if err != nil {
		return err
	}
	if len(req.Messages) > 0 {
		m.pending = m.pending[1:]
	}
	return m.receive(ctx, response)
}

func (m *personalMember) receive(ctx context.Context, response rafttransport.Response) error {
	if !m.owner.memberAllowed() {
		return ErrQuiet
	}
	if response.Generation != m.record.Grant.Generation {
		return rafttransport.ErrGeneration
	}
	if len(response.Messages) > rafttransport.MaxMessages {
		return rafttransport.ErrWire
	}
	for _, delivery := range response.Messages {
		if delivery.Cursor == 0 {
			return rafttransport.ErrCursor
		}
		if int(delivery.Lane) >= len(m.ack) {
			return rafttransport.ErrWire
		}
		if delivery.Cursor <= m.ack[delivery.Lane] {
			continue
		}
		e := delivery.Envelope
		peers := m.record.Grant.Members
		// Once the registry is applied, it replaces the initial authenticated grant.
		// A retained grant cannot authorize a subsequently revoked member.
		app, _, err := m.store.ApplicationView(ctx)
		if err != nil {
			return err
		}
		if app.Applied >= m.record.Grant.Barrier {
			view, err := rafttransport.CommittedViewAt(m.record.Grant.Scope, m.record.Grant.Generation, app.Applied, raftcore.Configuration(app.ConfState), app.LogicalTime, func(k cs.RecordKey) ([]byte, error) {
				key := raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}
				rows, _, e := m.store.Read(ctx, app.Applied, []raftstore.Key{key})
				return rows[key], e
			})
			if err != nil {
				return err
			}
			peers = view.Members
			m.record.Grant.Members = peers
		}
		var source ed25519.PublicKey
		for _, peer := range peers {
			if peer.Node == e.From && peer.Incarnation == e.Incarnation && !peer.Revoked {
				source = peer.Key
			}
		}
		if e.Verify(source) != nil || e.Scope != m.record.Grant.Scope || e.To != m.record.Grant.Node || e.Destination != m.record.Grant.Incarnation {
			return rafttransport.ErrAuth
		}
		if e.Chunk != nil {
			if m.snapshot == nil || e.Chunk.Generation != raftsnapshot.ID(m.snapshot.GetSnapshot().GetData()) {
				return rafttransport.ErrWire
			}
			if err = m.store.PutSnapshotChunk(ctx, e.Chunk.Generation, e.Chunk.Offset, e.Payload, e.Chunk.Hash); err != nil {
				return err
			}
			m.chunks[e.Chunk.Offset] = true
			manifest, err := raftsnapshot.Decode(m.snapshot.GetSnapshot().GetData())
			if err != nil {
				return err
			}
			if uint64(len(m.chunks)) == manifest.Chunks {
				pointer, err := m.store.CompleteSnapshot(ctx, e.Chunk.Generation)
				if err != nil {
					return err
				}
				if !proto.Equal(pointer, m.snapshot.GetSnapshot()) {
					return raftstore.ErrSnapshot
				}
				raw, err := proto.Marshal(m.snapshot)
				if err != nil {
					return err
				}
				if err = m.step(ctx, raftcore.PeerMessage{Bytes: raw}); err != nil {
					return err
				}
				m.snapshot = nil
			}
		} else {
			message := new(pb.Message)
			if proto.Unmarshal(e.Payload, message) != nil {
				return rafttransport.ErrWire
			}
			if message.GetType() == pb.MsgSnap && message.GetSnapshot().GetMetadata().GetIndex() > m.status.Applied {
				if m.snapshot == nil || !proto.Equal(m.snapshot.GetSnapshot(), message.GetSnapshot()) {
					if _, err = m.store.BeginSnapshot(ctx, message.GetSnapshot().GetData()); err != nil {
						return err
					}
					m.snapshot = message
					m.chunks = map[uint64]bool{}
				}
			} else if err = m.step(ctx, raftcore.PeerMessage{Bytes: e.Payload}); err != nil {
				return err
			}
		}
		m.ack[delivery.Lane] = delivery.Cursor
	}
	return nil
}

func (m *personalMember) start(ctx context.Context) error {
	if !m.owner.memberAllowed() {
		return ErrQuiet
	}
	req, err := m.request()
	if err != nil {
		return err
	}
	// Upgrade authentication carries no outbound messages. Catch-up flushed them.
	req.Messages = nil
	if err = req.SignWith(m.owner.Device.Sign); err != nil {
		return err
	}
	raw, _ := rafttransport.Encode(req)
	header := http.Header{}
	header.Set(rafttransport.SocketAuthHeader, base64.RawURLEncoding.EncodeToString(raw))
	ctx, m.cancel = context.WithCancel(ctx)
	conn, resp, err := websocket.Dial(ctx, m.owner.Relay+rafttransport.SocketPath, &websocket.DialOptions{HTTPClient: m.owner.ClusterHTTP(&http.Client{Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}), HTTPHeader: header, CompressionMode: websocket.CompressionDisabled})
	if resp != nil && err != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		m.cancel()
		return err
	}
	conn.SetReadLimit(rafttransport.MaxExchangeBytes)
	m.conn = conn
	m.done = make(chan struct{})
	go func() {
		defer close(m.done)
		for {
			kind, raw, err := conn.Read(ctx)
			m.mu.Lock()
			if err == nil && (!m.owner.memberAllowed() || ctx.Err() != nil) {
				err = ErrQuiet
			}
			var response rafttransport.Response
			if err == nil && (kind != websocket.MessageBinary || rafttransport.Decode(raw, &response) != nil) {
				err = rafttransport.ErrWire
			}
			if err == nil {
				err = m.receive(ctx, response)
			}
			// An empty push is only a subscription acknowledgment. Answering it would
			// create an endless empty frame loop and wake an otherwise idle group.
			if err == nil && (len(response.Messages) > 0 || len(m.pending) > 0) {
				for {
					req, e := m.request()
					if e != nil {
						err = e
						break
					}
					data, _ := rafttransport.Encode(req)
					if !m.owner.memberAllowed() {
						err = ErrQuiet
						break
					}
					writeCtx, cancel := context.WithTimeout(ctx, requestTimeout)
					m.owner.clusterFrames.Add(1)
					err = conn.Write(writeCtx, websocket.MessageBinary, data)
					cancel()
					if err != nil {
						break
					}
					if len(req.Messages) == 0 {
						break
					}
					m.pending = m.pending[1:]
					if len(m.pending) == 0 {
						break
					}
				}
			}
			if err != nil {
				m.failure = err
			}
			m.idle = err == nil && len(response.Messages) == 0 && len(m.pending) == 0
			close(m.changed)
			m.changed = make(chan struct{})
			m.mu.Unlock()
			if err != nil {
				conn.CloseNow()
				return
			}
		}
	}()
	return nil
}

func (m *personalMember) close() {
	m.withdrawDisplay()
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		close(m.changed)
		m.changed = make(chan struct{})
	}
	m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
	if m.conn != nil {
		m.conn.CloseNow()
	}
	if m.done != nil {
		<-m.done
	}
	m.mu.Lock()
	if m.store != nil {
		m.store.Close()
	}
	m.mu.Unlock()
	if m.lock != nil {
		filelock.Unlock(m.lock)
		m.lock.Close()
	}
}

func (s *Sync) stopMember() {
	s.memberMu.Lock()
	defer s.memberMu.Unlock()
	if s.member != nil {
		s.member.close()
		s.member = nil
	}
}

// memberRound is called by the existing terminal lifetime, never by Open.
// Unsupported hosted relays keep their original state-sync path and are not
// repeatedly rediscovered. Failed native activation retries with saved identity.
func (s *Sync) memberRound(ctx context.Context) error {
	s.memberMu.Lock()
	defer s.memberMu.Unlock()
	if !s.memberAllowed() {
		if s.member != nil {
			s.member.close()
			s.member = nil
		}
		return nil
	}
	if s.member != nil {
		s.member.mu.Lock()
		err := s.member.failure
		s.member.mu.Unlock()
		if err == nil {
			return nil
		}
		s.member.close()
		s.member = nil
	}
	if s.memberUnsupported || s.now().Before(s.memberRetry) {
		return nil
	}
	status, err := s.clusterClient().Status(ctx)
	if errors.Is(err, clusterctl.ErrUnsupported) {
		s.memberUnsupported = true
		return nil
	}
	if err == nil && status.Cluster != "" {
		s.member, err = s.openMember(ctx)
		if err == nil {
			err = s.member.start(ctx)
		}
		if err == nil {
			s.member.publishDisplay()
		}
		if err != nil && s.member != nil {
			s.member.close()
			s.member = nil
		}
	}
	if err != nil {
		if errors.Is(err, clusterctl.ErrUnsupported) {
			s.memberUnsupported = true
		}
		if s.memberBackoff == 0 {
			s.memberBackoff = s.Interval
		} else {
			s.memberBackoff *= 2
		}
		s.memberBackoff = min(s.memberBackoff, time.Minute)
		// Seeded jitter separates homes that all wake after the same outage.
		// The exponential ceiling still bounds the complete retry delay.
		s.memberRetries++
		seed := sha256.Sum256([]byte(s.Home + s.Device.ID()))
		sample := binary.LittleEndian.Uint64(seed[:8]) ^ (s.memberRetries * 0x9e3779b97f4a7c15)
		sample ^= sample >> 12
		sample ^= sample << 25
		sample ^= sample >> 27
		half := s.memberBackoff / 2
		delay := half + time.Duration(sample%uint64(s.memberBackoff-half+1))
		s.memberRetry = s.now().Add(delay)
	} else {
		s.memberBackoff = 0
	}
	return err
}

// memberVault is a local change hint, never authority to perform a public read
// or take. Network operations still use their ordinary fresh quorum barriers.
func (s *Sync) memberVault(ctx context.Context) (string, bool, error) {
	s.memberMu.Lock()
	defer s.memberMu.Unlock()
	if s.member == nil {
		return "", false, nil
	}
	m := s.member
	m.mu.Lock()
	defer m.mu.Unlock()
	if !s.memberAllowed() {
		return "", false, ErrQuiet
	}
	if m.failure != nil {
		return "", false, m.failure
	}
	app, _, err := m.store.ApplicationView(ctx)
	if err != nil {
		return "", false, err
	}
	if app.Applied < m.record.Grant.Barrier {
		return "", false, nil
	}
	key := raftstore.Key{Namespace: raftstore.State, Key: cs.IdentityKey().Key}
	rows, _, err := m.store.Read(ctx, app.Applied, []raftstore.Key{key})
	if err != nil {
		return "", false, err
	}
	var id cs.IdentityRec
	if err = cs.Decode(rows[key], &id); err != nil {
		return "", false, err
	}
	return id.Vault, true, nil
}
