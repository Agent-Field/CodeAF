package relayserve_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterapi"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
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

type personalSigner struct {
	root ed25519.PublicKey
	key  ed25519.PrivateKey
	cert identity.Cert
}

func (s personalSigner) IdentityKey() ed25519.PublicKey { return s.root }
func (s personalSigner) Cert() identity.Cert            { return s.cert }
func (s personalSigner) Sign(b []byte) []byte           { return ed25519.Sign(s.key, b) }
func personalDevice(t *testing.T, id identity.Identity) personalSigner {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return personalSigner{id.PublicKey(), key, identity.IssueCert(id, pub, time.UnixMilli(1000))}
}
func personalToken(t *testing.T) string {
	t.Helper()
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}
func admission(t *testing.T, r *rig, s reqsign.Signer, store string, protocol uint16) (raftdirectory.PersonalEnrollment, int, string) {
	t.Helper()
	raw, _ := json.Marshal(cs.PersonalAdmission{Store: store, Protocol: protocol})
	req, err := http.NewRequest(http.MethodPost, r.srv.URL+cs.PersonalAdmissionPath, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req, raw, s, r.clock.Now())
	response, err := r.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out raftdirectory.PersonalEnrollment
	if response.StatusCode == 200 && json.Unmarshal(body, &out) != nil {
		t.Fatal("invalid enrollment reply")
	}
	return out, response.StatusCode, string(body)
}
func personalStatus(t *testing.T, r *rig, s reqsign.Signer) clusterctl.Status {
	t.Helper()
	out, err := (clusterctl.Client{Base: r.srv.URL, Sign: reqsign.SignFor(s, r.clock.Now), HTTP: r.srv.Client()}).Status(bg)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func joined(t *testing.T, r *rig, s reqsign.Signer, store string) raftdirectory.PersonalEnrollment {
	t.Helper()
	out, status, body := admission(t, r, s, store, 1)
	if status != 200 {
		t.Fatalf("enrollment %d: %s", status, body)
	}
	if out.Barrier == 0 {
		r.clock.Advance(time.Duration(cs.TickEveryMillis) * time.Millisecond)
		out, status, body = admission(t, r, s, store, 1)
		if status != 200 {
			t.Fatalf("retry %d: %s", status, body)
		}
	}
	if out.Node <= 3 || out.Barrier == 0 {
		t.Fatalf("no durable learner grant: %+v", out)
	}
	return out
}

// The test learner has its own real core and FULL SQLite store. Only its
// outbound Client reaches the relay. It completes Persist/Apply before signing
// durable progress, and retains an incoming snapshot until every chunk is saved.
type personalLearner struct {
	t              *testing.T
	signer         personalSigner
	grant          raftdirectory.PersonalEnrollment
	store          *raftstore.SQLite
	core           *raftcore.Core
	path           string
	status         raftcore.Status
	pending        []rafttransport.Envelope
	sequence       uint64
	ack            [3]uint64
	snapshot       *pb.Message
	chunks         map[uint64]bool
	receivedChunks int
	maxChunk       int
	snapshots      int
}

func learner(t *testing.T, s personalSigner, grant raftdirectory.PersonalEnrollment) *personalLearner {
	t.Helper()
	l := &personalLearner{t: t, signer: s, grant: grant, path: filepath.Join(t.TempDir(), "learner.db")}
	l.open()
	t.Cleanup(func() { l.store.Close() })
	return l
}
func (l *personalLearner) open() {
	l.t.Helper()
	var err error
	l.store, err = raftstore.Open(l.path, raftstore.Identity{ClusterUUID: l.grant.Scope.Cluster, NodeID: l.grant.Node, Protocol: l.grant.Scope.Protocol})
	if err != nil {
		l.t.Fatal(err)
	}
	recovered, err := l.store.Recover(bg)
	if err != nil {
		l.t.Fatal(err)
	}
	l.core, err = raftcore.New(raftcore.Config{ClusterUUID: l.grant.Scope.Cluster, NodeID: l.grant.Node, Protocol: l.grant.Scope.Protocol, Seed: l.grant.Node}, recovered.Core)
	if err != nil {
		l.t.Fatal(err)
	}
	l.step(raftcore.Poll{})
}
func (l *personalLearner) step(input raftcore.Input) {
	l.t.Helper()
	for n := 0; n < 10000; n++ {
		effects, err := l.core.Step(input)
		if err != nil {
			l.t.Fatal(err)
		}
		l.status = effects.Status
		for _, m := range effects.Send {
			var destination uint64
			for _, member := range l.grant.Members {
				if member.Node == m.To {
					destination = member.Incarnation
				}
			}
			l.sequence++
			e := rafttransport.Envelope{Scope: l.grant.Scope, From: l.grant.Node, To: m.To, Incarnation: l.grant.Incarnation, Destination: destination, Sequence: l.sequence, Payload: m.Bytes}
			if err = e.Sign(l.signer.key); err != nil {
				l.t.Fatal(err)
			}
			l.pending = append(l.pending, e)
		}
		if effects.Persist != nil {
			if err = l.store.Persist(bg, *effects.Persist); err != nil {
				l.t.Fatal(err)
			}
			input = raftcore.Persisted{BatchID: effects.Persist.ID}
			continue
		}
		if effects.Apply != nil {
			if _, err = raftdirectory.Apply(bg, l.store, *effects.Apply, l.grant.Scope.Cluster, 0); err != nil {
				l.t.Fatal(err)
			}
			input = raftcore.Applied{BatchID: effects.Apply.ID}
			continue
		}
		return
	}
	l.t.Fatal("learner exceeded bounded completion budget")
}
func (l *personalLearner) request() rafttransport.Request {
	l.t.Helper()
	app, _, err := l.store.ApplicationView(bg)
	if err != nil {
		l.t.Fatal(err)
	}
	req := rafttransport.Request{Scope: l.grant.Scope, Node: l.grant.Node, Incarnation: l.grant.Incarnation, Generation: l.grant.Generation, Ack: l.ack, Applied: app.Applied}
	if len(l.pending) > 0 {
		req.Messages = l.pending[:1]
	}
	if err = req.Sign(l.signer.key); err != nil {
		l.t.Fatal(err)
	}
	return req
}
func (l *personalLearner) exchange(r *rig) (rafttransport.Response, error) {
	l.t.Helper()
	req := l.request()
	response, err := (rafttransport.Client{URL: r.srv.URL, HTTP: r.srv.Client()}).Exchange(bg, req)
	if err != nil {
		return response, err
	}
	if len(req.Messages) > 0 {
		l.pending = l.pending[1:]
	}
	l.receive(response)
	return response, nil
}
func (l *personalLearner) receive(response rafttransport.Response) {
	l.t.Helper()
	for _, delivery := range response.Messages {
		if delivery.Cursor <= l.ack[delivery.Lane] {
			continue
		}
		e := delivery.Envelope
		var source ed25519.PublicKey
		for _, m := range l.grant.Members {
			if m.Node == e.From && m.Incarnation == e.Incarnation && !m.Revoked {
				source = m.Key
			}
		}
		if e.Verify(source) != nil || e.Scope != l.grant.Scope || e.To != l.grant.Node || e.Destination != l.grant.Incarnation {
			l.t.Fatal("unauthorized peer delivery")
		}
		if e.Chunk != nil {
			if l.snapshot == nil || e.Chunk.Generation != raftsnapshot.ID(l.snapshot.GetSnapshot().GetData()) {
				l.t.Fatal("chunk without its signed manifest")
			}
			if err := l.store.PutSnapshotChunk(bg, e.Chunk.Generation, e.Chunk.Offset, e.Payload, e.Chunk.Hash); err != nil {
				l.t.Fatal(err)
			}
			l.chunks[e.Chunk.Offset] = true
			l.receivedChunks++
			l.maxChunk = max(l.maxChunk, len(e.Payload))
			manifest, err := raftsnapshot.Decode(l.snapshot.GetSnapshot().GetData())
			if err != nil {
				l.t.Fatal(err)
			}
			if uint64(len(l.chunks)) == manifest.Chunks {
				pointer, err := l.store.CompleteSnapshot(bg, e.Chunk.Generation)
				if err != nil || !proto.Equal(pointer, l.snapshot.GetSnapshot()) {
					l.t.Fatalf("invalid completed snapshot: %v", err)
				}
				raw, err := proto.Marshal(l.snapshot)
				if err != nil {
					l.t.Fatal(err)
				}
				l.step(raftcore.PeerMessage{Bytes: raw})
				l.snapshot = nil
				l.snapshots++
			}
		} else {
			m := new(pb.Message)
			if proto.Unmarshal(e.Payload, m) != nil {
				l.t.Fatal("invalid raft message")
			}
			if m.GetType() == pb.MsgSnap && m.GetSnapshot().GetMetadata().GetIndex() > l.status.Applied {
				if l.snapshot == nil || !proto.Equal(l.snapshot.GetSnapshot(), m.GetSnapshot()) {
					if _, err := l.store.BeginSnapshot(bg, m.GetSnapshot().GetData()); err != nil {
						l.t.Fatal(err)
					}
					l.snapshot = m
					l.chunks = map[uint64]bool{}
				}
			} else {
				l.step(raftcore.PeerMessage{Bytes: e.Payload})
			}
		}
		l.ack[delivery.Lane] = delivery.Cursor
	}
}
func (l *personalLearner) catchUp(r *rig, index uint64) {
	l.t.Helper()
	for round := 0; round < 120; round++ {
		if _, err := l.exchange(r); err != nil {
			l.t.Fatalf("catch-up round %d: %v", round, err)
		}
		if l.status.Applied >= index && len(l.pending) == 0 {
			if _, err := l.exchange(r); err != nil {
				l.t.Fatal(err)
			}
			return
		}
		if round%10 == 9 {
			r.clock.Advance(time.Duration(cs.TickEveryMillis) * time.Millisecond)
		}
	}
	l.t.Fatalf("R9 learner did not apply advertised barrier %d: %+v", index, l.status)
}

// E1/R9/R12 prove allocation at the authenticated production door, including
// exact-store retries, wipe retirement, protocol and personal-only authority.
func TestR9R12PersonalEnrollmentDoor(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	s := personalDevice(t, id)
	token := personalToken(t)
	first := joined(t, r, s, token)
	again := joined(t, r, s, token)
	if first.Node != again.Node || first.Barrier != again.Barrier {
		t.Fatal("same store allocated twice")
	}
	replacement := joined(t, r, s, personalToken(t))
	if replacement.Node <= first.Node {
		t.Fatal("wiped store reused a node ID")
	}
	if _, status, _ := admission(t, r, s, token, 1); status == 200 {
		t.Fatal("retired store retired its replacement")
	}
	status := personalStatus(t, r, s)
	if status.Self != replacement.Node || !reflect.DeepEqual(status.Configuration.Voters, []uint64{1, 2, 3}) || !reflect.DeepEqual(status.Configuration.Learners, []uint64{replacement.Node}) {
		t.Fatalf("wrong topology: %+v", status)
	}
	if _, code, body := admission(t, r, s, personalToken(t), 2); code == 200 || !strings.Contains(body, cs.EnrollmentMessage(cs.UpdateRequired)) {
		t.Fatalf("protocol refusal %d %s", code, body)
	}
	// The old transport generation and node remain fenced after replacement.
	old := learner(t, s, first)
	if _, err := old.exchange(r); !errors.Is(err, rafttransport.ErrAuth) {
		t.Fatalf("retired peer accepted: %v", err)
	}
	if err := directory.NewHTTP(r.srv.URL, reqsign.SignFor(s, r.clock.Now), r.srv.Client()).PutDevice(bg, s.cert.DeviceID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	if err := r.dir(a).Revoke(bg, s.cert.DeviceID()); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := admission(t, r, s, personalToken(t), 1); code == 200 {
		t.Fatal("revoked device enrolled")
	}
	unsigned, err := http.Post(r.srv.URL+cs.PersonalAdmissionPath, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	unsigned.Body.Close()
	if unsigned.StatusCode != 403 {
		t.Fatal("unpaired admission accepted")
	}
}

// E1/R9/R27 count live learners at the real admission boundary; the 33rd
// request uses the existing manual sentence and leaves the voters unchanged.
func TestR9R27PersonalEnrollmentCapacity(t *testing.T) {
	r := newRig(t)
	id, _ := newIdentity(t)
	for n := 0; n < cs.MaxLearners; n++ {
		joined(t, r, personalDevice(t, id), personalToken(t))
	}
	s := personalDevice(t, id)
	if _, status, body := admission(t, r, s, personalToken(t), 1); status != 429 || !strings.Contains(body, cs.EnrollmentMessage(cs.Capacity)) {
		t.Fatalf("capacity: %d %s", status, body)
	}
	state := personalStatus(t, r, s)
	if len(state.Configuration.Learners) != cs.MaxLearners || !reflect.DeepEqual(state.Configuration.Voters, []uint64{1, 2, 3}) {
		t.Fatalf("capacity changed quorum: %+v", state)
	}
}

// E2/R12 cuts the relay after its enrollment reply, while the committed intent
// is still outside the configuration. Activation, with NO second admission,
// must complete that same node's learner configuration and advertised barrier.
func TestR12PersonalEnrollmentCrashBetweenStages(t *testing.T) {
	r := newRig(t)
	id, _ := newIdentity(t)
	s := personalDevice(t, id)
	token := personalToken(t)
	admitted, code, body := admission(t, r, s, token, 1)
	if code != 200 || admitted.Barrier != 0 {
		t.Fatalf("expected separate enrollment stage: %+v %d %s", admitted, code, body)
	}
	before := personalStatus(t, r, s)
	if before.Membership.Pending == nil || len(before.Configuration.Learners) != 0 {
		t.Fatal("fixture did not cut between stages")
	}
	r.restart()
	after := personalStatus(t, r, s)
	if after.Self != admitted.Node || after.Membership.Pending != nil || !reflect.DeepEqual(after.Configuration.Learners, []uint64{admitted.Node}) {
		t.Fatalf("activation did not finish intent: %+v", after)
	}
	resumed := joined(t, r, s, token)
	if resumed.Node != admitted.Node || resumed.Barrier == 0 || resumed.Generation <= admitted.Generation {
		t.Fatal("restart reused transport generation or allocated again")
	}
	stale := learner(t, s, admitted)
	if _, err := stale.exchange(r); !errors.Is(err, rafttransport.ErrGeneration) {
		t.Fatalf("old generation accepted: %v", err)
	}
	l := learner(t, s, resumed)
	l.catchUp(r, resumed.Barrier)
}

// E3/R23 verifies HTTP and the outbound push socket on the ordinary native
// listener. Registered keys alone may send consensus, and revocation fences an
// already-open socket as well as future polls and cross-identity addressing.
func TestR16R23PersonalExchangeAndSocket(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	s := personalDevice(t, id)
	grant := joined(t, r, s, personalToken(t))
	l := learner(t, s, grant)
	bad := l.request()
	_, wrong, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bad.Sign(wrong)
	if _, err = (rafttransport.Client{URL: r.srv.URL}).Exchange(bg, bad); !errors.Is(err, rafttransport.ErrAuth) {
		t.Fatalf("unregistered key accepted: %v", err)
	}
	foreign := l.request()
	foreign.Scope.Identity = "id_00000000000000000000000000000000"
	foreign.Sign(s.key)
	if _, err = (rafttransport.Client{URL: r.srv.URL}).Exchange(bg, foreign); !errors.Is(err, rafttransport.ErrAuth) {
		t.Fatalf("cross-identity peer accepted: %v", err)
	}
	for _, kind := range []pb.MessageType{pb.MsgProp, pb.MsgHup, pb.MsgStorageAppend} {
		raw, _ := proto.Marshal(&pb.Message{Type: kind.Enum(), From: proto.Uint64(grant.Node), To: proto.Uint64(1), Term: proto.Uint64(1)})
		e := rafttransport.Envelope{Scope: grant.Scope, From: grant.Node, To: 1, Incarnation: 1, Destination: 1, Sequence: 1, Payload: raw}
		if e.Sign(s.key) == nil {
			t.Fatalf("peer %s was offered", kind)
		}
		// A hostile sender can bypass its local Sign validator. The actual
		// relay still refuses the signed envelope before any core input.
		digest := sha256.Sum256(raw)
		e.Digest = hex.EncodeToString(digest[:])
		unsigned, _ := json.Marshal(e)
		e.Signature = ed25519.Sign(s.key, append([]byte("codeaf/raft/envelope/1\n"), unsigned...))
		request := l.request()
		request.Messages = []rafttransport.Envelope{e}
		if err = request.Sign(s.key); err != nil {
			t.Fatal(err)
		}
		if _, err = (rafttransport.Client{URL: r.srv.URL}).Exchange(bg, request); err == nil {
			t.Fatalf("relay accepted peer %s", kind)
		}
	}
	req := l.request()
	raw, _ := rafttransport.Encode(req)
	headers := http.Header{rafttransport.SocketAuthHeader: []string{base64.RawURLEncoding.EncodeToString(raw)}}
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(r.srv.URL, "http")+rafttransport.SocketPath, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(rafttransport.MaxExchangeBytes)
	kind, raw, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageBinary {
		t.Fatalf("socket initial delivery: %v", err)
	}
	var response rafttransport.Response
	if rafttransport.Decode(raw, &response) != nil {
		t.Fatal("invalid pushed response")
	}
	l.receive(response)
	l.catchUp(r, grant.Barrier)
	if err = directory.NewHTTP(r.srv.URL, reqsign.SignFor(s, r.clock.Now), r.srv.Client()).PutDevice(bg, s.cert.DeviceID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	if err = r.dir(a).Revoke(bg, s.cert.DeviceID()); err != nil {
		t.Fatal(err)
	}
	raw, _ = rafttransport.Encode(l.request())
	_ = conn.Write(ctx, websocket.MessageBinary, raw)
	for n := 0; n < 8; n++ {
		if _, _, err = conn.Read(ctx); err != nil {
			break
		}
	}
	if err == nil {
		t.Fatal("revoked socket kept serving")
	}
	if _, err = l.exchange(r); !errors.Is(err, rafttransport.ErrAuth) {
		t.Fatalf("revoked poll accepted: %v", err)
	}
}

// E4/R9 requires evidence from each learner's durable application, not just a
// configuration row or uploaded snapshot. Both learners remain nonvoting.
func TestR9PersonalLearnersSnapshotLogAndReady(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	// Multiple bounded sealed rows produce full-size snapshot chunks while
	// keeping every public command below the existing 64 KiB command cap.
	for n := 0; n < 18; n++ {
		d := personalDevice(t, id)
		if err := directory.NewHTTP(r.srv.URL, reqsign.SignFor(d, r.clock.Now), r.srv.Client()).PutDevice(bg, d.cert.DeviceID(), directory.Device{V: 1, Name: strings.Repeat("e", 20000)}); err != nil {
			t.Fatal(err)
		}
	}
	s := personalDevice(t, id)
	grant := joined(t, r, s, personalToken(t))
	l := learner(t, s, grant)
	initial := personalStatus(t, r, s)
	if len(initial.Ready) != 0 {
		t.Fatal("ready before learner applied")
	}
	l.catchUp(r, grant.Barrier)
	if l.snapshots == 0 || l.receivedChunks < 2 || l.maxChunk != raftsnapshot.ChunkBytes {
		t.Fatalf("no streamed snapshot: snapshots=%d chunks=%d max=%d", l.snapshots, l.receivedChunks, l.maxChunk)
	}
	if err := r.dir(a).PutDevice(bg, a.dev.ID(), directory.Device{V: 1, Name: "after snapshot"}); err != nil {
		t.Fatal(err)
	}
	target := personalStatus(t, r, s).Membership.Index
	l.catchUp(r, max(target, l.status.Applied+1))
	s2 := personalDevice(t, id)
	g2 := joined(t, r, s2, personalToken(t))
	l2 := learner(t, s2, g2)
	l2.catchUp(r, g2.Barrier)
	// Refresh routing metadata from committed admission before accepting the
	// second learner's node as a possible peer source.
	l.grant.Members = g2.Members
	l.catchUp(r, g2.Barrier)
	state := personalStatus(t, r, s)
	if !reflect.DeepEqual(state.Ready, []uint64{grant.Node, g2.Node}) || !reflect.DeepEqual(state.Configuration.Voters, []uint64{1, 2, 3}) || !reflect.DeepEqual(state.Configuration.Learners, []uint64{grant.Node, g2.Node}) {
		t.Fatalf("wrong ready learners: %+v", state)
	}
	if err := l.store.Close(); err != nil {
		t.Fatal(err)
	}
	l.open()
	l.catchUp(r, g2.Barrier)
	if l.status.Applied < g2.Barrier {
		t.Fatal("same learner store lost durable catch-up")
	}
}

// E5/R9/R27 proves a blocked or killed exchange cannot gate public operations
// or compaction. The resumed learner catches up from a later snapshot.
func TestR9R27BlockedLearnerCannotDelayVotersOrCompaction(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	s := personalDevice(t, id)
	grant := joined(t, r, s, personalToken(t))
	l := learner(t, s, grant)
	l.catchUp(r, grant.Barrier)
	// Hold an authenticated exchange at its response boundary. Public work
	// must finish while this learner is blocked, then survive its cancellation.
	entered := make(chan struct{})
	release := make(chan struct{})
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		r.svc.Handler.ServeHTTP(&blockedPeerWriter{ResponseWriter: w, entered: entered, release: release}, request)
	}))
	ctx, cancel := context.WithCancel(bg)
	finished := make(chan error, 1)
	go func() {
		_, err := (rafttransport.Client{URL: blocked.URL, HTTP: blocked.Client()}).Exchange(ctx, l.request())
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		cancel()
		close(release)
		blocked.Close()
		t.Fatal("learner exchange did not reach response boundary")
	}
	done := make(chan error, 1)
	go func() {
		done <- r.dir(a).PutDevice(bg, a.dev.ID(), directory.Device{V: 1, Name: "while learner blocked"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(10 * time.Second):
		t.Error("public write waited on blocked learner")
	}
	if _, err := r.dir(a).List(bg); err != nil {
		t.Error(err)
	}
	cancel()
	close(release)
	<-finished
	blocked.Close()
	var err error
	// Bounded real signed writes cross the existing 4 MiB log threshold
	// while the learner is absent. No compaction threshold is changed.
	for n := 0; n < 120; n++ {
		if err = r.dir(a).PutDevice(bg, a.dev.ID(), directory.Device{V: 1, Name: strings.Repeat("s", 20000) + fmt.Sprint(n)}); err != nil {
			t.Fatal(err)
		}
	}
	before := l.snapshots
	r.clock.Advance(time.Duration(cs.TickEveryMillis) * time.Millisecond)
	l.catchUp(r, l.status.Applied+1)
	if l.snapshots <= before {
		t.Fatal("dead learner pinned compaction instead of getting a new snapshot")
	}
}

// E5/E6/R26/R27 use the production fake-clock scheduler with a real learner.
// All voters sleep without learner acks; no peer frames recur for ten minutes,
// and first read then write wake before the learner receives the new log.
func TestR9R26R27PersonalLearnerSleepAndWake(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	s := personalDevice(t, id)
	grant := joined(t, r, s, personalToken(t))
	l := learner(t, s, grant)
	// This learner stalls before catch-up. Neither its progress nor its
	// final acknowledgment may be required for the voters to sleep.
	for n := 0; n < 20; n++ {
		r.clock.Advance(5 * time.Second)
	}
	inspect := func() (raftstore.Application, cs.Quiescence) {
		t.Helper()
		store, err := raftstore.Open(filepath.Join(r.store, "raft", id.ID(), "1.db"), raftstore.Identity{ClusterUUID: grant.Scope.Cluster, NodeID: 1, Protocol: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		app, _, err := store.ApplicationView(bg)
		if err != nil {
			t.Fatal(err)
		}
		key := raftstore.Key{Namespace: raftstore.Membership, Key: cs.QuiescenceKey().Key}
		rows, _, err := store.Read(bg, app.Applied, []raftstore.Key{key})
		if err != nil {
			t.Fatal(err)
		}
		var marker cs.Quiescence
		if cs.Decode(rows[key], &marker) != nil {
			t.Fatal("no durable sleep marker")
		}
		return app, marker
	}
	asleep, marker := inspect()
	if marker.Index != asleep.Applied {
		t.Fatal("learner prevented all-voter sleep")
	}
	for n := 0; n < 120; n++ {
		r.clock.Advance(5 * time.Second)
	}
	after, _ := inspect()
	if after.Applied != asleep.Applied {
		t.Fatal("idle learner caused recurring application work")
	}
	response, err := l.exchange(r)
	if err != nil || len(response.Messages) != 0 {
		t.Fatalf("sleeping relay sent learner traffic: %+v %v", response, err)
	}
	if _, err = r.dir(a).List(bg); err != nil {
		t.Fatal("first quorum read after sleep:", err)
	}
	read, _ := inspect()
	if read.Applied <= after.Applied {
		t.Fatal("first read reused sleep authority")
	}
	if err = r.dir(a).PutDevice(bg, a.dev.ID(), directory.Device{V: 1, Name: "after wake"}); err != nil {
		t.Fatal(err)
	}
	written, _ := inspect()
	if written.Applied <= read.Applied {
		t.Fatal("first write did not durably wake")
	}
	l.catchUp(r, written.Applied)
	t.Logf("R9/R26/R27 ten-minute idle applied=%d peer deliveries=0; fresh read=%d write=%d learner=%d", asleep.Applied, read.Applied, written.Applied, l.status.Applied)
}

// E1/R9/R16 keeps scoped credentials out of personal admission, even if the
// shared root also issued a personal certificate for that retained worker key.
func TestR9R16PersonalAdmissionRefusesWorkerAuthority(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1}); err != nil {
		t.Fatal(err)
	}
	state := personalStatus(t, r, a)
	s := personalDevice(t, id)
	workerCert, err := identity.IssueWorkerCert(id, s.key.Public().(ed25519.PublicKey), state.Cluster, []string{cell}, r.clock.Now(), r.clock.Now().Add(time.Hour), true)
	if err != nil {
		t.Fatal(err)
	}
	certRaw, _ := json.Marshal(a.Cert())
	cert := base64.RawURLEncoding.EncodeToString(certRaw)
	command := cs.Command{Schema: 1, Cluster: state.Cluster, Actor: a.dev.ID(), Certificate: cert, Session: "enroll-worker-test", Sequence: 1, Now: r.clock.Now().UnixMilli(), Change: cs.Mutation{EnrollWorker: &cs.EnrollWorker{Certificate: workerCert, Grants: map[string][]byte{cell: {1}}}}}
	command.RequestDigest, err = cs.Digest(command)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := cs.SigningBytes(command)
	if err != nil {
		t.Fatal(err)
	}
	command.Signature = a.Sign(raw)
	client := clusterapi.Client{Base: r.srv.URL, HTTP: r.srv.Client(), Now: r.clock.Now, Allowed: func() bool { return true }, Sign: a.Sign, Request: func() cs.ClusterRequest {
		return cs.ClusterRequest{IdentityKey: id.PublicKey(), Cluster: state.Cluster, Certificate: cert}
	}}
	result, err := client.Submit(bg, command)
	if err != nil || result.Code != cs.OK {
		t.Fatalf("worker setup: %+v %v", result, err)
	}
	if _, status, _ := admission(t, r, s, personalToken(t), 1); status != 403 {
		t.Fatalf("retained worker role enrolled as personal: %d", status)
	}
	body, _ := json.Marshal(cs.PersonalAdmission{Store: personalToken(t), Protocol: 1})
	req, err := http.NewRequest(http.MethodPost, r.srv.URL+cs.PersonalAdmissionPath, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req, body, s, r.clock.Now())
	raw, _ = json.Marshal(workerCert)
	req.Header.Set(reqsign.HeaderCert, base64.RawURLEncoding.EncodeToString(raw))
	response, err := r.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("worker certificate enrolled as personal")
	}
	if after := personalStatus(t, r, a); len(after.Configuration.Learners) != 0 || after.Membership.Live != 0 {
		t.Fatal("refused worker changed membership")
	}
}

// blockedPeerWriter holds the response only after the production peer handler
// has authorized and serviced its input. Voter work must not share this wait.
type blockedPeerWriter struct {
	http.ResponseWriter
	entered chan struct{}
	release <-chan struct{}
}

func (w *blockedPeerWriter) Write(raw []byte) (int, error) {
	close(w.entered)
	<-w.release
	return w.ResponseWriter.Write(raw)
}

// R12/R23 keep generation fencing durable without mutating the original
// provision. Damaged counter evidence is retained and cannot reset authority.
func TestR12R23PersonalTransportRetainsCounterAndImmutableProvision(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	s := personalDevice(t, id)
	token := personalToken(t)
	grant := joined(t, r, s, token)
	live := learner(t, s, grant)
	live.catchUp(r, grant.Barrier)
	base := filepath.Join(r.store, "raft", id.ID())
	provision, err := os.ReadFile(filepath.Join(base, "provision.json"))
	if err != nil {
		t.Fatal(err)
	}
	counterPath := filepath.Join(base, "transport-generation")
	counter, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatal(err)
	}
	r.stop()
	damaged := append([]byte(nil), counter...)
	damaged[len(damaged)-1] ^= 1
	if err = os.WriteFile(counterPath, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	r.start()
	_, err = (clusterctl.Client{Base: r.srv.URL, Sign: reqsign.SignFor(a, r.clock.Now), HTTP: r.srv.Client()}).Status(bg)
	if err == nil {
		t.Fatal("damaged generation reset its authority")
	}
	retained, err := os.ReadFile(counterPath)
	if err != nil || !bytes.Equal(retained, damaged) {
		t.Fatal("damaged counter evidence was overwritten")
	}
	r.stop()
	if err = os.WriteFile(counterPath, counter, 0600); err != nil {
		t.Fatal(err)
	}
	r.start()
	_ = personalStatus(t, r, a)
	after, err := os.ReadFile(filepath.Join(base, "provision.json"))
	if err != nil || !bytes.Equal(after, provision) {
		t.Fatal("restart rewrote immutable provision")
	}
	stale := learner(t, s, grant)
	if _, err = stale.exchange(r); !errors.Is(err, rafttransport.ErrGeneration) {
		t.Fatalf("restored counter reused an advertised generation: %v", err)
	}
	current := joined(t, r, s, token)
	if current.Node != grant.Node || current.Generation <= grant.Generation {
		t.Fatal("cold restart changed durable member identity")
	}
	live.grant = current
	live.ack = [3]uint64{}
	if err := r.dir(a).PutDevice(bg, a.dev.ID(), directory.Device{V: 1, Name: "after router restart"}); err != nil {
		t.Fatal(err)
	}
	live.catchUp(r, live.status.Applied+1)

}

// R9/R27 require every admitted public command to remain usable with a
// learner. Protobuf framing does not fit inside a maximum-sized command's
// own byte budget; transport must catch up without changing either cap.
func TestR9R27PersonalLearnerCannotBreakMaximumCommand(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	s := personalDevice(t, id)
	grant := joined(t, r, s, personalToken(t))
	l := learner(t, s, grant)
	l.catchUp(r, grant.Barrier)
	encodedSize := func(n int) int {
		t.Helper()
		body, _ := json.Marshal(directory.Device{V: 1, Name: strings.Repeat("x", n)})
		request, err := http.NewRequest(http.MethodPut, r.srv.URL+"/v1/dir/devices/"+a.dev.ID(), bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		reqsign.Sign(request, body, a, r.clock.Now())
		proof, err := reqsign.Capture(request, body)
		if err != nil {
			t.Fatal(err)
		}
		command, err := cs.V1Command(grant.Scope.Cluster, r.clock.Now().UnixMilli(), proof)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := cs.EncodeCommand(command)
		if err != nil {
			t.Fatal(err)
		}
		return len(raw)
	}
	low, high := 0, cs.MaxCommandBytes
	for low < high {
		middle := (low + high + 1) / 2
		if encodedSize(middle) <= cs.MaxCommandBytes {
			low = middle
		} else {
			high = middle - 1
		}
	}
	if encodedSize(low) > cs.MaxCommandBytes || encodedSize(low) < cs.MaxCommandBytes-4 {
		t.Fatal("fixture missed the legal command boundary")
	}
	if err := r.dir(a).PutDevice(bg, a.dev.ID(), directory.Device{V: 1, Name: strings.Repeat("x", low)}); err != nil {
		t.Fatal("learner broke a legal public write:", err)
	}
	if _, err := r.dir(a).List(bg); err != nil {
		t.Fatal("learner stopped quorum reads:", err)
	}
	l.catchUp(r, l.status.Applied+1)
	app, _, err := l.store.ApplicationView(bg)
	if err != nil {
		t.Fatal(err)
	}
	record := raftstore.Key{Namespace: raftstore.State, Key: cs.DeviceKey(a.dev.ID()).Key}
	rows, _, err := l.store.Read(bg, app.Applied, []raftstore.Key{record})
	if err != nil {
		t.Fatal(err)
	}
	var saved cs.Device
	if cs.Decode(rows[record], &saved) != nil || saved.Name != strings.Repeat("x", low) {
		t.Fatal("learner did not retain the maximum legal command")
	}
}
