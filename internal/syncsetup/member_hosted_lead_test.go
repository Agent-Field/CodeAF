package syncsetup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
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
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"go.etcd.io/raft/v3"
)

type hostedLeadRig struct {
	t      *testing.T
	server *httptest.Server
}

func newHostedLeadRig(t *testing.T) *hostedLeadRig {
	t.Helper()
	endpoint := hostedLearnerEndpoint(t)
	target, e := url.Parse(endpoint)
	mustNil(t, e)
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	proxy.Transport = transport
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	t.Cleanup(transport.CloseIdleConnections)
	return &hostedLeadRig{t, server}
}
func (r *hostedLeadRig) home(id *identity.Identity) *Sync {
	r.t.Helper()
	home := machine(r.t, r.server.URL)
	if id != nil {
		_, e := identity.Adopt(home, *id, true)
		mustNil(r.t, e)
	}
	s, ok, e := Open(home)
	mustNil(r.t, e)
	if !ok {
		r.t.Fatal("open")
	}
	hostedOwner(r.t, s)
	return s
}
func hostedPersonalLeader(t *testing.T, p *Sync) cs.LeadershipReceipt {
	t.Helper()
	receipt, e := p.LeadCluster(t.Context(), 0)
	mustNil(t, e)
	waitMember(t, p, func(m *personalMember) bool {
		return m.status.Role == raft.StateLeader && m.status.Term == receipt.Term && m.status.Applied >= receipt.Barrier
	})
	status, e := p.ClusterStatus(t.Context())
	mustNil(t, e)
	if status.Leader != receipt.Node || status.LeadershipUnavailable || status.Configuration.Joint() || !slices.Equal(status.Configuration.Voters, []uint64{1, 2, receipt.Node}) || !slices.Contains(status.Configuration.Learners, 3) || status.Membership.Leadership == nil || !status.Membership.Leadership.Completed || receipt.Revision != status.Membership.Revision {
		t.Fatalf("R6 incomplete leadership receipt/status: %+v %+v", receipt, status)
	}
	return receipt
}

// R6/R15/R18 keep every public ingress usable while the elected personal core leads.
func TestR6R15R18HostedLeaderOriginalWorkerIngressAndThinStore(t *testing.T) {
	r := newHostedLeadRig(t)
	p := r.home(nil)
	var blobRequests atomic.Uint64
	signed, _ := http.NewRequest(http.MethodGet, r.server.URL, nil)
	reqsign.Sign(signed, nil, deviceSigner{p.Identity, p.Device}, time.Now())
	certP := signed.Header.Get(reqsign.HeaderCert)
	originalHandler := r.server.Config.Handler
	r.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if strings.HasPrefix(q.URL.Path, "/v1/store/") && q.Header.Get(reqsign.HeaderCert) == certP {
			blobRequests.Add(1)
		}
		originalHandler.ServeHTTP(w, q)
	})
	hostedPersonalLeader(t, p)
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
	owner.Now = time.Now
	sequence := uint64(0)
	signOwner := func(change cs.Mutation) cs.Command {
		sequence++
		c, err := b.SignClusterCommand(cs.Command{Schema: cs.SchemaVersion, Cluster: status.Cluster, Session: "external-ingress", Sequence: sequence, Ack: sequence - 1, Now: time.Now().UnixMilli(), Change: change})
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
	cert, err := identity.IssueWorkerCert(id, key.Public().(ed25519.PublicKey), status.Cluster, []string{cell}, time.Now(), time.Now().Add(time.Hour), false)
	mustNil(t, err)
	worker := cert.DeviceID()
	incarnation := uint64(0)
	wire := clusterapi.Client{Base: r.server.URL, HTTP: r.server.Client(), Now: time.Now, Allowed: func() bool { return true }, Sign: func(b []byte) []byte { return ed25519.Sign(key, b) }, Request: func() cs.ClusterRequest {
		return cs.ClusterRequest{IdentityKey: id.PublicKey(), Cluster: status.Cluster, Worker: &cert, Incarnation: incarnation}
	}}
	n := uint64(0)
	signWorker := func(change cs.Mutation) cs.Command {
		n++
		c, err := cs.Sign(cs.Command{Schema: cs.SchemaVersion, Cluster: status.Cluster, Actor: worker, Session: "external-worker", Sequence: n, Ack: n - 1, WorkerIncarnation: incarnation, Now: time.Now().UnixMilli(), Change: change}, key)
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
	hb := cs.WorkerHeartbeat{Worker: worker, Incarnation: 1, Sequence: 1, SeenAt: time.Now().UnixMilli()}
	hraw, err := cs.WorkerHeartbeatBytes(status.Cluster, hb)
	mustNil(t, err)
	hb.Signature = ed25519.Sign(key, hraw)
	submit(wire, signWorker(cs.Mutation{WorkerBeatBatch: &cs.WorkerBeatBatch{Items: []cs.WorkerHeartbeat{hb}}}), cs.OK)
	assignment := cs.Assign{ID: "external-run", Revision: 1, Cell: cell, Expect: cs.LeaseExpectation{Device: b.Device.ID(), Fence: 1}, Target: worker, Incarnation: 1, Expires: time.Now().Add(time.Minute).UnixMilli(), SealedPayload: []byte{0x91}}
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
	cancel()
	renewAfter := time.Now().Add(time.Duration(cs.RenewEveryMillis-cs.PresenceEveryMillis) * time.Millisecond).UnixMilli()
	for elapsed := int64(0); elapsed < cs.RenewEveryMillis; elapsed += cs.PresenceEveryMillis {
		time.Sleep(time.Duration(cs.PresenceEveryMillis) * time.Millisecond)
		ping, stop := context.WithTimeout(t.Context(), requestTimeout)
		mustNil(t, watch.Ping(ping))
		stop()
	}
	ctx, cancel = context.WithTimeout(t.Context(), requestTimeout)
	defer cancel()
	receipt := false
	for !receipt {
		frame, err := watch.Next(ctx)
		mustNil(t, err)
		for _, hold := range frame.Holds {
			if hold.Cell == cell && hold.Fence == 3 && hold.Index > 0 && hold.At >= renewAfter {
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
			certRaw, e := json.Marshal(identity.IssueCert(id, seed.Public(), time.Now()))
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
	waitMember(t, p, func(m *personalMember) bool { return m.status.Applied > 0 })
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

// R6 requires both homes to observe the elected personal voter after fresh reads.
func TestR6HostedPersonalLeadershipBothHomesStatus(t *testing.T) {
	r := newHostedLeadRig(t)
	p := r.home(nil)
	id := p.Identity
	b := r.home(&id)
	receipt := hostedPersonalLeader(t, p)
	status, e := b.ClusterStatus(t.Context())
	mustNil(t, e)
	if status.Leader != receipt.Node || status.LeadershipUnavailable || !slices.Equal(status.Configuration.Voters, receipt.Voters) || status.Self == receipt.Node {
		t.Fatalf("R6 second home status: %+v", status)
	}
}

// R6 retains one saved signed request across a lost reply and client restart.
func TestR6HostedPersonalLeadershipLostReplyResumes(t *testing.T) {
	r := newHostedLeadRig(t)
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
		raw, e := io.ReadAll(q.Body)
		if e != nil {
			panic(e)
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
	if _, e := s.LeadCluster(t.Context(), 0); e == nil {
		t.Fatal("R6 lost reply claimed success")
	}
	path := filepath.Join(s.Home, "cluster-leadership-"+s.Device.ID()+".json")
	raw, e := os.ReadFile(path)
	mustNil(t, e)
	var saved leadershipSession
	mustNil(t, json.Unmarshal(raw, &saved))
	if saved.Pending == nil {
		t.Fatal("R6 unknown outcome discarded request")
	}
	again := buildAt(s.Home, s.Relay, s.Identity, s.Device, s.Interval, time.Now)
	if _, e = again.LeadCluster(t.Context(), saved.Pending.Change.RequestLeadership.Node+1); e == nil {
		t.Fatal("R6 pending target changed")
	}
	receipt, e := again.LeadCluster(t.Context(), 0)
	mustNil(t, e)
	mu.Lock()
	same := lost && len(requests) == 2 && bytes.Equal(requests[0], requests[1])
	mu.Unlock()
	if !same || receipt.Node != saved.Pending.Change.RequestLeadership.Node {
		t.Fatal("R6 retry allocated a different request")
	}
	raw, e = os.ReadFile(path)
	mustNil(t, e)
	var acknowledged leadershipSession
	mustNil(t, json.Unmarshal(raw, &acknowledged))
	if acknowledged.Pending != nil || acknowledged.Sequence != saved.Pending.Sequence {
		t.Fatal("R6 completion was not acknowledged")
	}
}

// R6 acknowledges a committed refusal before allocating the next signed request.
func TestR6HostedPersonalLeadershipCommittedRefusal(t *testing.T) {
	r := newHostedLeadRig(t)
	s := r.home(nil)
	status, e := s.clusterClient().Status(t.Context())
	mustNil(t, e)
	c, e := s.SignClusterCommand(cs.Command{Schema: cs.SchemaVersion, Cluster: status.Cluster, Session: "leadership", Sequence: 1, Now: time.Now().UnixMilli(), Change: cs.Mutation{RequestLeadership: &cs.RequestLeadership{Revision: status.Membership.Revision + 1, Node: status.Self, Device: s.Device.ID()}}})
	mustNil(t, e)
	path := filepath.Join(s.Home, "cluster-leadership-"+s.Device.ID()+".json")
	mustNil(t, saveLeadership(path, leadershipSession{Pending: &c}))
	_, e = s.LeadCluster(t.Context(), 0)
	var refusal clusterctl.CommittedRefusal
	if !errors.As(e, &refusal) || refusal.Sequence != 1 {
		t.Fatal("R6 committed refusal not returned", e)
	}
	raw, e := os.ReadFile(path)
	mustNil(t, e)
	var saved leadershipSession
	mustNil(t, json.Unmarshal(raw, &saved))
	if saved.Pending != nil || saved.Sequence != 1 {
		t.Fatal("R6 refusal not acknowledged")
	}
	receipt, e := s.LeadCluster(context.Background(), 0)
	mustNil(t, e)
	if receipt.Node != status.Self {
		t.Fatal("R6 next request did not complete")
	}
}

// R6 resumes the saved operation when the member home closes before admission.
func TestR6HostedPersonalLeadershipHomeRestartBeforeAdmission(t *testing.T) {
	r := newHostedLeadRig(t)
	home := machine(t, r.server.URL)
	s, ok, e := Open(home)
	mustNil(t, e)
	if !ok {
		t.Fatal("open")
	}
	stop := hostedOwner(t, s)
	original := r.server.Config.Handler
	var requests [][]byte
	lost := false
	var mu sync.Mutex
	r.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.Method == http.MethodPost && q.URL.Path == clusterctl.Path {
			raw, e := io.ReadAll(q.Body)
			if e != nil {
				panic(e)
			}
			q.Body = io.NopCloser(bytes.NewReader(raw))
			mu.Lock()
			requests = append(requests, raw)
			first := !lost
			lost = true
			mu.Unlock()
			if first {
				http.Error(w, "request interrupted before admission", 503)
				return
			}
		}
		original.ServeHTTP(w, q)
	})
	if _, e = s.LeadCluster(t.Context(), 0); e == nil {
		t.Fatal("R6 interrupted request claimed success")
	}
	path := filepath.Join(home, "cluster-leadership-"+s.Device.ID()+".json")
	raw, e := os.ReadFile(path)
	mustNil(t, e)
	var pending leadershipSession
	mustNil(t, json.Unmarshal(raw, &pending))
	if pending.Pending == nil {
		t.Fatal("R6 interrupted request was discarded")
	}
	stop()
	again, ok, e := Open(home)
	mustNil(t, e)
	if !ok {
		t.Fatal("reopen")
	}
	receipt, e := again.LeadCluster(t.Context(), 0)
	mustNil(t, e)
	mu.Lock()
	same := len(requests) == 2 && bytes.Equal(requests[0], requests[1])
	mu.Unlock()
	if !same || receipt.Node != pending.Pending.Change.RequestLeadership.Node {
		t.Fatal("R6 restarted home replaced its saved operation")
	}
}

// R6 keeps ordinary directory mutations, reads and rotation on the forwarded path.
func TestR6HostedLeaderPublicDirectoryVerbs(t *testing.T) {
	r := newHostedLeadRig(t)
	p := r.home(nil)
	hostedPersonalLeader(t, p)
	id := p.Identity
	b := r.home(&id)
	ctx := t.Context()
	mustNil(t, b.Dir.PutDevice(ctx, b.Device.ID(), directory.Device{V: 1, Name: "c2VhbGVk", Platform: "linux"}))
	const cell = "01J0000000000000000000000B"
	head := strings.Repeat("2", 64)
	view, e := b.Dir.Create(ctx, cell, directory.CellInit{Head: head})
	mustNil(t, e)
	view, e = b.Dir.Heartbeat(ctx, cell, directory.Beat{Fence: view.Cell.Lease.Fence, Pending: 1})
	mustNil(t, e)
	view, e = b.Dir.Publish(ctx, cell, directory.Publish{Fence: view.Cell.Lease.Fence, OldHead: head, Head: head, Size: 1})
	mustNil(t, e)
	mustNil(t, b.Dir.Release(ctx, cell, view.Cell.Lease.Fence))
	view, e = b.Dir.Acquire(ctx, cell, directory.AcquireOpts{})
	mustNil(t, e)
	mustNil(t, b.Dir.Release(ctx, cell, view.Cell.Lease.Fence))
	mustNil(t, b.Dir.Archive(ctx, cell))
	got, e := b.Dir.Cell(ctx, cell)
	mustNil(t, e)
	if !got.Cell.Archived {
		t.Fatal("R6 forwarded archive was not applied")
	}
	listing, e := b.Dir.List(ctx)
	mustNil(t, e)
	vault := strings.Repeat("3", 64)
	mustNil(t, b.Dir.SetVault(ctx, listing.Identity.Vault, vault))
	listing, e = b.Dir.List(ctx)
	mustNil(t, e)
	if listing.Identity.Vault != vault {
		t.Fatal("R6 forwarded vault CAS was not applied")
	}
	_, e = b.Dir.Presence(ctx)
	mustNil(t, e)
	_, e = b.Dir.Rotation(ctx)
	mustNil(t, e)
	_, e = b.Dir.Rotate(ctx, directory.RotationReq{Op: directory.OpFreeze})
	mustNil(t, e)
	_, e = b.Dir.Rotate(ctx, directory.RotationReq{Op: directory.OpThaw})
	mustNil(t, e)
	seed, e := identity.NewSeed()
	mustNil(t, e)
	cert := identity.IssueCert(id, seed.Public(), time.Now())
	revoked := cert.DeviceID()
	dev, e := seed.Join(cert)
	mustNil(t, e)
	caller := directory.NewHTTP(r.server.URL, reqsign.SignFor(deviceSigner{id, dev}, time.Now), r.server.Client())
	mustNil(t, caller.PutDevice(ctx, revoked, directory.Device{V: 1, Name: "c2VhbGVk", Platform: "linux"}))
	mustNil(t, b.Dir.Revoke(ctx, revoked))
	if _, e = caller.List(ctx); e == nil {
		t.Fatal("R6 revoked forwarded caller retained authority")
	}
}

// R6/R13/R27 retain the largest legal command through the existing chunk lane.
func TestR6R13R27HostedLeaderKeepsMaximumPublicCommand(t *testing.T) {
	r := newHostedLeadRig(t)
	p := r.home(nil)
	hostedPersonalLeader(t, p)
	status, err := p.clusterClient().Status(t.Context())
	mustNil(t, err)
	encodedSize := func(n int) int {
		t.Helper()
		body, _ := json.Marshal(directory.Device{V: 1, Name: strings.Repeat("x", n)})
		request, err := http.NewRequest(http.MethodPut, r.server.URL+"/v1/dir/devices/"+p.Device.ID(), bytes.NewReader(body))
		mustNil(t, err)
		reqsign.Sign(request, body, deviceSigner{p.Identity, p.Device}, time.Now())
		proof, err := reqsign.Capture(request, body)
		mustNil(t, err)
		command, err := cs.V1Command(status.Cluster, time.Now().UnixMilli(), proof)
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
