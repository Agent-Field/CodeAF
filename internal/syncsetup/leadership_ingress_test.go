package syncsetup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/clusterapi"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// T2/R6/R15/R18 cover original signed v2/scoped ingress and committed watches
// while P leads. P never requests or stores the independent content blob.
func TestR6R15R18PersonalLeaderOriginalWorkerIngressAndThinStore(t *testing.T) {
	r := newMemberRig(t)
	p := r.home(nil)
	var blobRequests atomic.Uint64
	signed, _ := http.NewRequest(http.MethodGet, r.server.URL, nil)
	reqsign.Sign(signed, nil, deviceSigner{p.Identity, p.Device}, r.clock.Now())
	certP := signed.Header.Get(reqsign.HeaderCert)
	originalHandler := r.server.Config.Handler
	r.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if strings.HasPrefix(q.URL.Path, "/v1/store/") && q.Header.Get(reqsign.HeaderCert) == certP {
			blobRequests.Add(1)
		}
		originalHandler.ServeHTTP(w, q)
	})
	personalLeader(t, r, p)
	id := p.Identity
	b := r.home(&id)
	const cell = "01J0000000000000000000000A"
	head := strings.Repeat("1", 64)
	_, err := b.Dir.Create(t.Context(), cell, directory.CellInit{Head: head})
	mustNil(t, err)
	mustNil(t, b.Dir.Release(t.Context(), cell, 1))
	status, err := p.clusterClient().Status(t.Context())
	mustNil(t, err)
	owner := b.ClusterAPI(status.Cluster)
	owner.Now = r.clock.Now
	sequence := uint64(0)
	signOwner := func(change cs.Mutation) cs.Command {
		sequence++
		c, err := b.SignClusterCommand(cs.Command{Schema: cs.SchemaVersion, Cluster: status.Cluster, Session: "external-ingress", Sequence: sequence, Ack: sequence - 1, Now: r.clock.Now().UnixMilli(), Change: change})
		mustNil(t, err)
		return c
	}
	submit := func(client clusterapi.Client, c cs.Command, want cs.Code) cs.Result {
		t.Helper()
		result, err := client.Submit(t.Context(), c)
		mustNil(t, err)
		if result.Code != want {
			t.Fatalf("R6 original proposal: want %s got %+v", want, result)
		}
		return result
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	mustNil(t, err)
	cert, err := identity.IssueWorkerCert(id, key.Public().(ed25519.PublicKey), status.Cluster, []string{cell}, r.clock.Now(), r.clock.Now().Add(time.Hour), false)
	mustNil(t, err)
	worker := cert.DeviceID()
	incarnation := uint64(0)
	wire := clusterapi.Client{Base: r.server.URL, HTTP: r.server.Client(), Now: r.clock.Now, Allowed: func() bool { return true }, Sign: func(b []byte) []byte { return ed25519.Sign(key, b) }, Request: func() cs.ClusterRequest {
		return cs.ClusterRequest{IdentityKey: id.PublicKey(), Cluster: status.Cluster, Worker: &cert, Incarnation: incarnation}
	}}
	n := uint64(0)
	signWorker := func(change cs.Mutation) cs.Command {
		n++
		c, err := cs.Sign(cs.Command{Schema: cs.SchemaVersion, Cluster: status.Cluster, Actor: worker, Session: "external-worker", Sequence: n, Ack: n - 1, WorkerIncarnation: incarnation, Now: r.clock.Now().UnixMilli(), Change: change}, key)
		mustNil(t, err)
		return c
	}
	original := signOwner(cs.Mutation{EnrollWorker: &cs.EnrollWorker{Certificate: cert, Grants: map[string][]byte{cell: {0x81, 0x82}}}})
	raw, err := cs.EncodeCommand(original)
	mustNil(t, err)
	submit(owner, original, cs.OK)
	submit(wire, signWorker(cs.Mutation{RegisterWorker: &cs.RegisterWorker{Incarnation: 1}}), cs.OK)
	incarnation = 1
	view, grant, err := wire.Cell(t.Context(), cell)
	mustNil(t, err)
	if view.Cell.Head != head || !bytes.Equal(grant, []byte{0x81, 0x82}) {
		t.Fatal("R18 scoped read lost grant", view, grant)
	}
	if _, _, err = wire.Cell(t.Context(), "outside"); err == nil {
		t.Fatal("R18 worker escaped its grant")
	}
	hb := cs.WorkerHeartbeat{Worker: worker, Incarnation: 1, Sequence: 1, SeenAt: r.clock.Now().UnixMilli()}
	hraw, err := cs.WorkerHeartbeatBytes(status.Cluster, hb)
	mustNil(t, err)
	hb.Signature = ed25519.Sign(key, hraw)
	submit(wire, signWorker(cs.Mutation{WorkerBeatBatch: &cs.WorkerBeatBatch{Items: []cs.WorkerHeartbeat{hb}}}), cs.OK)
	assignment := cs.Assign{ID: "external-run", Revision: 1, Cell: cell, Expect: cs.LeaseExpectation{Device: b.Device.ID(), Fence: 1}, Target: worker, Incarnation: 1, Expires: r.clock.Now().Add(time.Minute).UnixMilli(), SealedPayload: []byte{0x91}}
	submit(owner, signOwner(cs.Mutation{Assign: &assignment}), cs.OK)
	ref := (cs.Assignment{Spec: assignment}).Ref()
	current, _, err := wire.Current(t.Context(), ref)
	mustNil(t, err)
	if current.Status != "pending" {
		t.Fatal(current)
	}
	claim := signWorker(cs.Mutation{ClaimAssignment: &cs.ClaimAssignment{Ref: ref, Expect: assignment.Expect}})
	result := submit(wire, claim, cs.OK)
	if result.View == nil || result.View.Cell.Lease.Fence != 2 {
		t.Fatal(result)
	}
	submit(wire, claim, cs.OK)
	_, err = b.Dir.Acquire(t.Context(), cell, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: worker, Fence: 2}})
	mustNil(t, err)
	current, view, err = wire.Current(t.Context(), ref)
	mustNil(t, err)
	if current.Fence != 2 || view.Cell.Lease.Fence != 3 {
		t.Fatal("R18 cached claim became current authority", current, view)
	}
	watch, err := b.Dir.(directory.HoldWatcher).WatchHolding(t.Context(), []directory.Hold{{Cell: cell, Fence: 3}})
	mustNil(t, err)
	defer watch.Close()
	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	defer cancel()
	_, err = watch.Next(ctx)
	mustNil(t, err)
	r.clock.Advance(time.Duration(cs.RenewEveryMillis) * time.Millisecond)
	mustNil(t, watch.Ping(ctx))
	receipt := false
	for !receipt {
		frame, err := watch.Next(ctx)
		mustNil(t, err)
		for _, hold := range frame.Holds {
			if hold.Cell == cell && hold.Fence == 3 && hold.Index > 0 {
				receipt = true
			}
		}
	}
	// C1/C3 decisions use the same original public write/read path while P
	// leads. Pairing itself never adds a vote or a peer on the new device.
	requests := directory.NewLinkHTTP(r.server.URL, r.server.Client())
	for _, approve := range []bool{true, false} {
		seed, e := identity.NewSeed()
		mustNil(t, e)
		opened, e := requests.CreateRequest(t.Context(), directory.NewRequest{Pubkey: base64.RawURLEncoding.EncodeToString(seed.Public()), X25519: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x37}, 32)), NameSealed: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x81}, 48)), Platform: "linux"})
		mustNil(t, e)
		if approve {
			certRaw, e := json.Marshal(identity.IssueCert(id, seed.Public(), r.clock.Now()))
			mustNil(t, e)
			approval := directory.Approval{Device: directory.Device{V: 1, Name: "c2VhbGVk", Platform: "linux"}, Cert: base64.RawURLEncoding.EncodeToString(certRaw), Grant: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x87}, 64))}
			mustNil(t, b.Dir.ApproveRequest(t.Context(), opened.Code, approval))
			mustNil(t, b.Dir.ApproveRequest(t.Context(), opened.Code, approval))
		} else {
			mustNil(t, b.Dir.DenyRequest(t.Context(), opened.Code))
		}
		decision, e := requests.GetRequest(t.Context(), opened.Code, 0)
		mustNil(t, e)
		if approve && (decision.State != directory.RequestApproved || decision.Grant == nil) || !approve && decision.State != directory.RequestDenied {
			t.Fatal("R6 pairing decision did not commit", decision)
		}
	}
	// A content object exists at the relay; only its uploader may request it.

	sentinel := []byte("P-MUST-NOT-STORE-THIS-CONTENT-5f99")
	frame, err := blobstore.Encode(strings.Repeat("0", 32), []blobstore.Object{{RID: strings.Repeat("a", 64), Bytes: append([]byte("AGEO\x01"), sentinel...)}})
	mustNil(t, err)
	_, err = b.Store.PutFrame(t.Context(), frame)
	mustNil(t, err)
	_, err = b.Dir.List(t.Context())
	mustNil(t, err)
	waitMember(t, p, func(m *personalMember) bool { return m.idle })
	p.member.mu.Lock()
	recovered, err := p.member.store.Recover(t.Context())
	p.member.mu.Unlock()
	mustNil(t, err)
	found := false
	for _, entry := range recovered.Core.Entries {
		found = found || bytes.Equal(entry.GetData(), raw)
	}
	if !found {
		t.Fatal("R6 relay replaced the original signed proposal")
	}
	err = filepath.WalkDir(p.memberPath(), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(raw, sentinel) {
				t.Error("R15 P stored content", path)
			}
		}
		return nil
	})
	mustNil(t, err)
	if blobRequests.Load() != 0 {
		t.Fatal("R15 P fetched content")
	}
}

// T3/R6 consumes a committed refusal before issuing the next owner request.
func TestR6PersonalLeadershipCommittedRefusalAcknowledged(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	readyMember(t, s)
	status, err := s.clusterClient().Status(t.Context())
	mustNil(t, err)
	c, err := s.SignClusterCommand(cs.Command{Schema: cs.SchemaVersion, Cluster: status.Cluster, Session: "leadership", Sequence: 1, Now: r.clock.Now().UnixMilli(), Change: cs.Mutation{RequestLeadership: &cs.RequestLeadership{Revision: 0, Node: status.Self, Device: s.Device.ID()}}})
	mustNil(t, err)
	path := filepath.Join(s.Home, "cluster-leadership-"+s.Device.ID()+".json")
	mustNil(t, saveLeadership(path, leadershipSession{Pending: &c}))
	_, err = s.LeadCluster(t.Context(), 0)
	var refused clusterctl.CommittedRefusal
	if !errors.As(err, &refused) || refused.Sequence != 1 {
		t.Fatal("R6 refusal was not committed", err)
	}
	raw, err := os.ReadFile(path)
	mustNil(t, err)
	var saved leadershipSession
	mustNil(t, json.Unmarshal(raw, &saved))
	if saved.Pending != nil || saved.Sequence != 1 {
		t.Fatal("R6 refusal was not acknowledged", saved)
	}
	receipt, err := s.LeadCluster(t.Context(), 0)
	mustNil(t, err)
	if receipt.Node != status.Self {
		t.Fatal(receipt)
	}
}

// T2/R6/R9/R13 keep admission and streamed catch-up working when the actual
// leader is outside the relay and has compacted its coordination log.
func TestR6R9R13PersonalLeaderAdmitsAndSnapshotsAnotherHome(t *testing.T) {
	r := newMemberRig(t)
	p := r.home(nil)
	receipt := personalLeader(t, r, p)
	mustNil(t, p.Dir.PutDevice(t.Context(), p.Device.ID(), directory.Device{V: 1, Name: "c2VhbGVk"}))
	frames := make([]string, 400)
	for i := range frames {
		frames[i] = fmt.Sprintf("%064x", i+1)
	}
	for i := 0; i < 12; i++ {
		_, err := p.Dir.Create(t.Context(), fmt.Sprintf("%026d", i+1), directory.CellInit{Head: strings.Repeat("1", 64), Frames: frames})
		mustNil(t, err)
	}
	p.member.mu.Lock()
	err := p.member.checkpoint(t.Context())
	recovered, recoverErr := p.member.store.Recover(t.Context())
	p.member.mu.Unlock()
	mustNil(t, err)
	mustNil(t, recoverErr)
	manifest, err := raftsnapshot.Decode(recovered.Core.Snapshot.GetData())
	mustNil(t, err)
	if manifest.Chunks < 2 {
		t.Fatal("R13 fixture did not require multiple snapshot chunks", manifest.Chunks)
	}
	id := p.Identity
	joined := r.home(&id)
	// Retry only when the actual leader reports local progress, preserving the
	// same admission token. There is no real-time polling or fixed retry budget.
	deadline := time.NewTimer(requestTimeout)
	defer deadline.Stop()
	for {
		p.member.mu.Lock()
		changed := p.member.changed
		p.member.mu.Unlock()
		err = joined.memberRound(t.Context())
		if err == nil && joined.member != nil {
			break
		}
		if err != nil && !errors.Is(err, errMemberWaiting) {
			t.Fatal(err)
		}
		joined.memberRetry = time.Time{}
		select {
		case <-changed:
		case <-deadline.C:
			t.Fatal("R9 learner did not join while P led", err)
		}
	}
	waitMember(t, joined, func(m *personalMember) bool { return m.idle && m.status.Applied >= m.record.Grant.Barrier })
	status, err := joined.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader != receipt.Node || !slices.Contains(status.Ready, nodeOf(joined)) || !slices.Contains(status.Configuration.Learners, nodeOf(joined)) {
		t.Fatal("R6/R9 external leader did not serve catch-up", status)
	}
}

// T3/R6 resumes the saved bytes after the member and its Sync owner restart.
// A durable fallback supersedes that request; it must be acknowledged BEFORE
// a later explicit request can choose leadership again.
func TestR6PersonalLeadershipRestartResumesSavedRequest(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	original := r.server.Config.Handler
	var mu sync.Mutex
	var requests [][]byte
	lost := false
	r.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path != clusterctl.Path || q.Method != http.MethodPost {
			original.ServeHTTP(w, q)
			return
		}
		raw, err := io.ReadAll(q.Body)
		if err != nil {
			panic(err)
		}
		q.Body = io.NopCloser(bytes.NewReader(raw))
		record := httptest.NewRecorder()
		original.ServeHTTP(record, q)
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, raw)
		if record.Code == 200 && !lost {
			lost = true
			http.Error(w, "reply lost", 503)
			return
		}
		for key, values := range record.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(record.Code)
		w.Write(record.Body.Bytes())
	})
	readyMember(t, s)
	if _, err := s.LeadCluster(t.Context(), 0); err == nil {
		t.Fatal("R6 lost reply claimed success")
	}
	path := filepath.Join(s.Home, "cluster-leadership-"+s.Device.ID()+".json")
	raw, err := os.ReadFile(path)
	mustNil(t, err)
	var pending leadershipSession
	mustNil(t, json.Unmarshal(raw, &pending))
	if pending.Pending == nil {
		t.Fatal("R6 unknown outcome discarded request")
	}
	s.stopMember()
	for tick := 0; tick < 22; tick++ {
		r.clock.Advance(DefaultInterval)
	}
	again := buildAt(s.Home, s.Relay, s.Identity, s.Device, s.Interval, r.clock.Now)
	_, err = again.LeadCluster(t.Context(), 0)
	var refused clusterctl.CommittedRefusal
	if !errors.As(err, &refused) || refused.Sequence != pending.Pending.Sequence {
		t.Fatal("R6 restart did not acknowledge superseded intent", err)
	}
	mu.Lock()
	same := len(requests) == 2 && bytes.Equal(requests[0], requests[1])
	mu.Unlock()
	if !same {
		t.Fatal("R6 restart allocated or changed its saved request")
	}
	raw, err = os.ReadFile(path)
	mustNil(t, err)
	var saved leadershipSession
	mustNil(t, json.Unmarshal(raw, &saved))
	if saved.Pending != nil || saved.Sequence != pending.Pending.Sequence {
		t.Fatal("R6 refusal not acknowledged", saved)
	}
	status, err := again.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Membership.Leadership != nil || !slices.Equal(status.Configuration.Voters, []uint64{1, 2, 3}) {
		t.Fatal("R6 saved retry automatically chose leadership", status)
	}
}

// R6/R13/R27 keep the largest admitted public command usable while P leads.
// Protobuf framing must consume the existing bounded chunk lane, not a larger cap.
func TestR6R13R27PersonalLeaderKeepsMaximumPublicCommand(t *testing.T) {
	r := newMemberRig(t)
	p := r.home(nil)
	personalLeader(t, r, p)
	status, err := p.clusterClient().Status(t.Context())
	mustNil(t, err)
	encodedSize := func(n int) int {
		t.Helper()
		body, _ := json.Marshal(directory.Device{V: 1, Name: strings.Repeat("x", n)})
		request, err := http.NewRequest(http.MethodPut, r.server.URL+"/v1/dir/devices/"+p.Device.ID(), bytes.NewReader(body))
		mustNil(t, err)
		reqsign.Sign(request, body, deviceSigner{p.Identity, p.Device}, r.clock.Now())
		proof, err := reqsign.Capture(request, body)
		mustNil(t, err)
		command, err := cs.V1Command(status.Cluster, r.clock.Now().UnixMilli(), proof)
		mustNil(t, err)
		raw, err := cs.EncodeCommand(command)
		mustNil(t, err)
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
		t.Fatal("R27 fixture missed the legal command boundary")
	}
	mustNil(t, p.Dir.PutDevice(t.Context(), p.Device.ID(), directory.Device{V: 1, Name: strings.Repeat("x", low)}))
	_, err = p.Dir.List(t.Context())
	mustNil(t, err)
	p.member.mu.Lock()
	app, _, err := p.member.store.ApplicationView(t.Context())
	key := raftstore.Key{Namespace: raftstore.State, Key: cs.DeviceKey(p.Device.ID()).Key}
	var rows map[raftstore.Key][]byte
	if err == nil {
		rows, _, err = p.member.store.Read(t.Context(), app.Applied, []raftstore.Key{key})
	}
	p.member.mu.Unlock()
	mustNil(t, err)
	var saved cs.Device
	mustNil(t, cs.Decode(rows[key], &saved))
	if saved.Name != strings.Repeat("x", low) {
		t.Fatal("R6 maximum public command was not applied on P")
	}
}
