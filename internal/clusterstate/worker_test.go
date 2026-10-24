package clusterstate_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

type workerFixture struct {
	a      *application
	id     string
	key    ed25519.PrivateKey
	cert   cs.WorkerCertificate
	serial int
}

func sealedWorker(t *testing.T, text string) []byte {
	t.Helper()
	b, e := aes.NewCipher(bytes.Repeat([]byte{3}, 32))
	if e != nil {
		t.Fatal(e)
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		t.Fatal(e)
	}
	nonce := bytes.Repeat([]byte{7}, g.NonceSize())
	return append(nonce, g.Seal(nil, nonce, []byte(text), nil)...)
}
func workerFixtureFor(t *testing.T, learner bool) *workerFixture {
	a := membershipApp(t, cs.MaxMemberRecords)
	k := actorKey("worker-key")
	cert := cs.WorkerCertificate{V: 1, Identity: cs.IdentityID(actorKey("root").Public().(ed25519.PublicKey)), Cluster: "test", Key: k.Public().(ed25519.PublicKey), Made: 1000, Expires: 1000000, Cells: []string{"chat"}, Learner: learner}
	raw, e := cs.WorkerCertificateBytes(cert)
	if e != nil {
		t.Fatal(e)
	}
	cert.Signature = ed25519.Sign(actorKey("root"), raw)
	f := &workerFixture{a: a, id: cert.DeviceID(), key: k, cert: cert}
	r := f.owner(1000, cs.Mutation{EnrollWorker: &cs.EnrollWorker{Certificate: cert, Grants: map[string][]byte{"chat": sealedWorker(t, "selected-chat-key-and-credential")}}})
	if r.Code != cs.OK {
		t.Fatal("R16 enrollment", r)
	}
	return f
}
func (f *workerFixture) command(actor string, k ed25519.PrivateKey, inc uint64, now int64, m cs.Mutation) cs.Command {
	f.serial++
	c, e := cs.Sign(cs.Command{Schema: 1, Cluster: "test", Actor: actor, Session: fmt.Sprint("request-", f.serial), Sequence: 1, Now: now, WorkerIncarnation: inc, Change: m}, k)
	if e != nil {
		f.a.t.Fatal(e)
	}
	return c
}
func (f *workerFixture) eval(c cs.Command) cs.Result { f.a.meta.Index++; return f.a.evaluate(c).Result }
func (f *workerFixture) owner(now int64, m cs.Mutation) cs.Result {
	return f.eval(f.command("a", actorKey("a"), 0, now, m))
}
func (f *workerFixture) run(inc uint64, now int64, m cs.Mutation) cs.Result {
	return f.eval(f.command(f.id, f.key, inc, now, m))
}
func (f *workerFixture) record() cs.WorkerRecord {
	var w cs.WorkerRecord
	if e := cs.Decode(f.a.rows[cs.WorkerKey(f.id)], &w); e != nil {
		f.a.t.Fatal(e)
	}
	return w
}
func (f *workerFixture) authority() cs.Authority {
	var a cs.Authority
	_ = cs.Decode(f.a.rows[cs.AuthorityKey(f.id)], &a)
	return a
}
func (f *workerFixture) identity() cs.IdentityRec {
	var i cs.IdentityRec
	_ = cs.Decode(f.a.rows[cs.IdentityKey()], &i)
	return i
}
func (f *workerFixture) register(previous uint64, now int64) cs.Result {
	return f.run(previous, now, cs.Mutation{RegisterWorker: &cs.RegisterWorker{Previous: previous, Incarnation: previous + 1, Caps: cs.Caps{OS: "linux", Arch: "arm64", Cow: "none"}, SealedLabels: sealedWorker(f.a.t, "secret-worker-label requirement /private/repo")}})
}
func (f *workerFixture) proof(inc, seq uint64, at int64, work ...cs.HoldFence) cs.WorkerHeartbeat {
	h := cs.WorkerHeartbeat{Worker: f.id, Incarnation: inc, Sequence: seq, SeenAt: at, Work: work}
	b, e := cs.WorkerHeartbeatBytes("test", h)
	if e != nil {
		f.a.t.Fatal(e)
	}
	h.Signature = ed25519.Sign(f.key, b)
	return h
}
func (f *workerFixture) beat(h cs.WorkerHeartbeat, now int64) cs.Result {
	return f.owner(now, cs.Mutation{WorkerBeatBatch: &cs.WorkerBeatBatch{Items: []cs.WorkerHeartbeat{h}}})
}
func expectWorker(t *testing.T, r cs.Result, code cs.Code) {
	t.Helper()
	if r.Code != code {
		t.Fatalf("want %s: %+v", code, r)
	}
}

// R15/R16/R18 allow only scoped held-work mutations. A worker cannot acquire
// through the personal door, edit membership, pair, rotate or obtain the vault.
func TestR15R16R18WorkerScopeAndAuthorityDoors(t *testing.T) {
	f := workerFixtureFor(t, false)
	expectWorker(t, f.register(0, 1001), cs.OK)
	denied := []cs.Mutation{
		{EnrollWorker: &cs.EnrollWorker{Certificate: f.cert}}, {DecideJoin: &cs.JoinDecision{}}, {AdmitJoin: &cs.AdmitJoin{}}, {Rotate: &cs.RotateRequest{Op: "freeze"}}, {SetVault: &cs.VaultCAS{}}, {PutDevice: &cs.PutDevice{ID: f.id}}, {Revoke: &cs.DeviceID{Device: "b"}}, {EnrollMember: &cs.EnrollMember{Device: f.id}}, {RequestLeadership: &cs.RequestLeadership{Device: f.id}}, {InitMembership: &cs.InitMembership{}}, {AdvanceTime: &cs.AdvanceTime{}}, {Create: &cs.CreateCell{Cell: "chat"}}, {Acquire: &cs.AcquireCell{Cell: "chat"}}, {Archive: &cs.CellID{Cell: "chat"}}, {DeviceSeen: &cs.DeviceSeen{Device: f.id}},
		{Publish: &cs.PublishCell{Cell: "outside"}}, {Beat: &cs.BeatCell{Cell: "outside"}}, {Release: &cs.ReleaseCell{Cell: "outside"}}, {BeatBatch: &cs.BeatBatch{Items: []cs.BeatCell{{Cell: "chat"}, {Cell: "outside"}}}}, {DrainWorker: &cs.DrainWorker{Worker: "b", Incarnation: 1}},
	}
	for i, m := range denied {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			before, _ := cs.Encode(f.record())
			expectWorker(t, f.run(1, 1002, m), cs.Unauthorized)
			after, _ := cs.Encode(f.record())
			if !bytes.Equal(before, after) {
				t.Fatal("refusal changed worker")
			}
		})
	}
	// The worker may save/release a fence obtained through P16's future claim.
	f.a.put(cs.CellKey("chat"), cs.Created(cs.CellInit{Head: "old"}, f.id, 1000))
	receipt := cs.UploadReceipt{Device: f.id, Cell: "chat", WorkerIncarnation: 1, Head: "new", HeadRID: "opaque-rid"}
	f.a.put(cs.UploadKey(cs.UploadID(receipt)), receipt)
	expectWorker(t, f.run(1, 1003, cs.Mutation{Publish: &cs.PublishCell{Cell: "chat", Receipt: cs.UploadID(receipt), Publish: cs.Publish{Fence: 1, OldHead: "old", Head: "new"}}}), cs.OK)
	expectWorker(t, f.run(1, 1004, cs.Mutation{Release: &cs.ReleaseCell{Cell: "chat", Fence: 1}}), cs.OK)
	// Reclassifying an enrollment cannot smuggle in a second role.
	a := f.authority()
	a.Personal = true
	f.a.put(cs.AuthorityKey(f.id), a)
	expectWorker(t, f.run(1, 1005, cs.Mutation{Rotate: &cs.RotateRequest{Op: "freeze"}}), cs.Unauthorized)
}

// R16/R18 require replacement, drain and revoke to beat delayed already-signed
// work. Drain is permanent for this delegation, while held work can still save.
func TestR16R17R18WorkerIncarnationDrainAndRevoke(t *testing.T) {
	f := workerFixtureFor(t, true)
	expectWorker(t, f.register(0, 1001), cs.OK)
	h := f.proof(1, 1, 1002)
	r := f.beat(h, 1002)
	expectWorker(t, r, cs.OK)
	if r.Workers[0].AvailableUntil != 91002 {
		t.Fatal(r)
	}
	if code := cs.WorkerCanClaim("test", f.id, "chat", 1, 1003, f.identity(), f.authority(), cs.Device{}, f.record()); code != cs.OK {
		t.Fatal("fresh worker unavailable", code)
	}
	old := f.command(f.id, f.key, 1, 1003, cs.Mutation{Beat: &cs.BeatCell{Cell: "chat", Beat: cs.Beat{Fence: 1}}})
	f.a.put(cs.CellKey("chat"), cs.Created(cs.CellInit{Head: "old"}, f.id, 1000))
	saved := f.eval(old)
	expectWorker(t, saved, cs.OK)
	expectWorker(t, f.register(1, 1004), cs.OK)
	expectWorker(t, f.eval(old), cs.Conflict)
	if r := f.beat(f.proof(1, 2, 1004), 1004); r.Workers[0].Code != cs.Conflict {
		t.Fatal("old incarnation refreshed availability", r)
	}
	if f.record().AvailableUntil != 0 {
		t.Fatal("restart inherited availability")
	}
	expectWorker(t, f.owner(1005, cs.Mutation{DrainWorker: &cs.DrainWorker{Worker: f.id, Incarnation: 1}}), cs.Conflict)
	expectWorker(t, f.owner(1005, cs.Mutation{DrainWorker: &cs.DrainWorker{Worker: f.id, Incarnation: 2}}), cs.OK)
	expectWorker(t, f.register(2, 1006), cs.OK)
	if !f.record().Draining {
		t.Fatal("restart cleared drain")
	}
	expectWorker(t, f.beat(f.proof(3, 1, 1007), 1007), cs.OK)
	if code := cs.WorkerCanClaim("test", f.id, "chat", 3, 1007, f.identity(), f.authority(), cs.Device{}, f.record()); code != cs.Conflict {
		t.Fatal("draining worker could claim", code)
	}
	expectWorker(t, f.run(3, 1008, cs.Mutation{Release: &cs.ReleaseCell{Cell: "chat", Fence: 1}}), cs.OK)
	pending := f.command(f.id, f.key, 3, 1008, cs.Mutation{Beat: &cs.BeatCell{Cell: "chat", Beat: cs.Beat{Fence: 1}}})
	expectWorker(t, f.owner(1009, cs.Mutation{Revoke: &cs.DeviceID{Device: f.id}}), cs.OK)
	expectWorker(t, f.eval(pending), cs.Revoked)
	if r := f.beat(f.proof(3, 2, 1009), 1009); r.Workers[0].Code != cs.Revoked {
		t.Fatal(r)
	}
	expectWorker(t, f.owner(1010, cs.Mutation{EnrollWorker: &cs.EnrollWorker{Certificate: f.cert, Grants: f.record().Grants}}), cs.Revoked)
}

// R15/R16/R27 keep secret labels sealed, reject free-text capability fields and
// retain all delegations when capacity fills or certificates expire.
func TestR15R16R27WorkerSealedMetadataAndLimits(t *testing.T) {
	f := workerFixtureFor(t, false)
	expectWorker(t, f.register(0, 1001), cs.OK)
	for _, caps := range []cs.Caps{{OS: "private-host-name"}, {Arch: "/private/repo"}, {GPU: func() *string { s := "secret GPU model"; return &s }()}} {
		expectWorker(t, f.run(1, 1002, cs.Mutation{RegisterWorker: &cs.RegisterWorker{Previous: 1, Incarnation: 2, Caps: caps}}), cs.BadRequest)
	}
	for _, b := range f.a.rows {
		for _, sentinel := range []string{"secret-worker-label", "/private/repo", "selected-chat-key-and-credential"} {
			if bytes.Contains(b, []byte(sentinel)) {
				t.Fatal("R15 plaintext leaked", sentinel)
			}
		}
	}
	before := append([]byte(nil), f.a.rows[cs.WorkerKey(f.id)]...)
	expectWorker(t, f.run(1, f.cert.Expires, cs.Mutation{Release: &cs.ReleaseCell{Cell: "chat", Fence: 1}}), cs.Unauthorized)
	if !bytes.Equal(before, f.a.rows[cs.WorkerKey(f.id)]) {
		t.Fatal("expiry deleted record")
	}
	f.a.put(cs.WorkerRegistryKey(), cs.WorkerRegistry{Retained: cs.MaxWorkerRecords})
	other := f.cert
	other.Key = actorKey("another-worker").Public().(ed25519.PublicKey)
	b, _ := cs.WorkerCertificateBytes(other)
	other.Signature = ed25519.Sign(actorKey("root"), b)
	expectWorker(t, f.owner(1003, cs.Mutation{EnrollWorker: &cs.EnrollWorker{Certificate: other, Grants: map[string][]byte{"chat": {1}}}}), cs.Capacity)
	if !bytes.Equal(before, f.a.rows[cs.WorkerKey(f.id)]) {
		t.Fatal("admission evicted retained record")
	}
}

// R9/R12/R16 forbid learner-by-default delegation and worker promotion. Exact
// incarnation and committed expiry also fence already-open protocol traffic.
func TestR9R12R16WorkerLearnerConsentAndPeerRevocation(t *testing.T) {
	for _, learner := range []bool{false, true} {
		t.Run(fmt.Sprint(learner), func(t *testing.T) {
			f := workerFixtureFor(t, learner)
			expectWorker(t, f.register(0, 1001), cs.OK)
			r := enroll(f.a, f.id)
			if !learner {
				expectWorker(t, r, cs.Unauthorized)
				return
			}
			expectWorker(t, r, cs.OK)
			expectWorker(t, settle(f.a), cs.OK)
			read := func(k cs.RecordKey) ([]byte, error) { return f.a.rows[k], nil }
			peer := func(now int64) cs.PeerEnrollment {
				_, p, e := cs.MembershipViewAt(read, f.a.meta.Configuration, f.a.meta.Index, now)
				if e != nil {
					t.Fatal(e)
				}
				return p[3]
			}
			if p := peer(1002); p.Revoked || p.Voter {
				t.Fatal("worker is not an authorized learner", p)
			}
			if !peer(f.cert.Expires).Revoked {
				t.Fatal("expired peer authorized")
			}
			f.a.meta.Term = 2
			expectWorker(t, f.owner(1003, cs.Mutation{RequestLeadership: &cs.RequestLeadership{Node: 4, Device: f.id, Revision: memberHeader(f.a).Revision}}), cs.Unauthorized)
			expectWorker(t, f.register(1, 1004), cs.OK)
			if !peer(1004).Revoked {
				t.Fatal("old worker socket survived incarnation change")
			}
		})
	}
}

// R16/R18 use an independent small registry oracle for reordered register,
// drain, signed-heartbeat and revoke events. Every seed honors RAFTSIM_SEEDS.
func TestR16R18WorkerRegistryOrderingCampaign(t *testing.T) {
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%04d", seed), func(t *testing.T) {
			f := workerFixtureFor(t, false)
			rng := rand.New(rand.NewSource(int64(seed)))
			var inc, beat uint64
			var draining, revoked bool
			now := int64(1000)
			for step := 0; step < 18; step++ {
				now += int64(1 + rng.Intn(100))
				switch rng.Intn(4) {
				case 0:
					prev := inc
					if rng.Intn(3) == 0 && prev > 0 {
						prev--
					}
					want := cs.OK
					if revoked {
						want = cs.Revoked
					} else if prev != inc {
						want = cs.Conflict
					}
					expectWorker(t, f.register(prev, now), want)
					if want == cs.OK {
						inc++
						beat = 0
					}
				case 1:
					target := inc
					if rng.Intn(3) == 0 && target > 0 {
						target--
					}
					want := cs.OK
					if revoked {
						want = cs.Revoked
					} else if target == 0 || target != inc {
						want = cs.Conflict
					}
					expectWorker(t, f.owner(now, cs.Mutation{DrainWorker: &cs.DrainWorker{Worker: f.id, Incarnation: target}}), want)
					if want == cs.OK {
						draining = true
					}
				case 2:
					target := inc
					if rng.Intn(3) == 0 && target > 0 {
						target--
					}
					n := beat + 1
					if rng.Intn(4) == 0 {
						n = beat
					}
					want := cs.OK
					if revoked {
						want = cs.Revoked
					} else if inc == 0 || target != inc || n <= beat {
						want = cs.Conflict
					}
					r := f.beat(f.proof(target, n, now), now)
					expectWorker(t, r, cs.OK)
					if r.Workers[0].Code != want {
						t.Fatal("independent heartbeat oracle", want, r)
					}
					if want == cs.OK {
						beat = n
					}
				case 3:
					if step > 10 {
						expectWorker(t, f.owner(now, cs.Mutation{Revoke: &cs.DeviceID{Device: f.id}}), cs.OK)
						revoked = true
					}
				}
				w := f.record()
				if w.Incarnation != inc || w.Heartbeat != beat || w.Draining != draining {
					t.Fatal("independent registry oracle differs", w, inc, beat, draining)
				}
			}
		})
	}
}

// R17 retries keep the exact committed response and the worker-incarnation
// binding is part of the durable request digest, even across process restart.
func TestR17WorkerExactResultAndDigestBinding(t *testing.T) {
	f := workerFixtureFor(t, false)
	c := f.command(f.id, f.key, 0, 1001, cs.Mutation{RegisterWorker: &cs.RegisterWorker{Incarnation: 1}})
	r := f.eval(c)
	expectWorker(t, r, cs.OK)
	// Register is an operation by the predecessor. Exact retry must survive the
	// incarnation change caused by that SAME registration.
	got := f.eval(c)
	if !reflect.DeepEqual(got, r) {
		t.Fatal("R17 lost registration result", r, got)
	}
	c.WorkerIncarnation = 1
	digest, e := cs.Digest(c)
	if e != nil || digest == c.RequestDigest {
		t.Fatal("incarnation not bound to digest")
	}
}

// R15/R16/R22 prove signed availability is not ownership, and freshness,
// per-item refusals and exact incarnation bind both uploads and hold renewals.
func TestR15R16R22WorkerEvidenceAndEffectsScopes(t *testing.T) {
	f := workerFixtureFor(t, false)
	expectWorker(t, f.register(0, 1001), cs.OK)
	f.a.put(cs.AuthorityKey("effects"), cs.Authority{Cluster: "test", Key: actorKey("effects").Public().(ed25519.PublicKey), Effects: true})
	f.a.put(cs.CellKey("chat"), cs.Created(cs.CellInit{Head: "old"}, f.id, 1000))
	effect := func(now int64, m cs.Mutation) cs.Result {
		return f.eval(f.command("effects", actorKey("effects"), 0, now, m))
	}
	before := append([]byte(nil), f.a.rows[cs.CellKey("chat")]...)
	h := f.proof(1, 1, 1002, cs.HoldFence{Cell: "chat", Fence: 1})
	r := f.beat(h, 1002)
	if r.Workers[0].Code != cs.OK {
		t.Fatal(r)
	}
	if !bytes.Equal(before, f.a.rows[cs.CellKey("chat")]) {
		t.Fatal("availability changed ownership")
	}
	for _, change := range []func(*cs.WorkerHeartbeat){
		func(h *cs.WorkerHeartbeat) { h.Signature[0] ^= 1 },
		func(h *cs.WorkerHeartbeat) { h.Work = []cs.HoldFence{{Cell: "outside", Fence: 1}} },
		func(h *cs.WorkerHeartbeat) { h.SeenAt = 1004 },
		func(h *cs.WorkerHeartbeat) { h.Sequence = 1 },
	} {
		bad := f.proof(1, 2, 1003)
		change(&bad)
		r := f.beat(bad, 1003)
		if r.Workers[0].Code == cs.OK {
			t.Fatal("bad evidence accepted", r)
		}
		if f.record().AvailableUntil != 91002 {
			t.Fatal("replayed evidence moved availability")
		}
	}
	stale := f.proof(1, 2, 1003)
	r = f.beat(stale, 1003+cs.EvidenceWindowMillis+1)
	if r.Workers[0].Code != cs.BadRequest {
		t.Fatal("old signed evidence accepted", r)
	}
	r = f.owner(1004, cs.Mutation{WorkerBeatBatch: &cs.WorkerBeatBatch{Items: []cs.WorkerHeartbeat{f.proof(1, 2, 30000), {Worker: "unknown", Incarnation: 1}}}})
	if r.Workers[0].Code == cs.OK || r.Workers[1].Code != cs.Unauthorized {
		t.Fatal("batch per-worker results", r)
	}
	// Use committed time after the deliberate stale-evidence advance.
	at := int64(30010)
	receipt := cs.UploadReceipt{Device: f.id, Cell: "outside", WorkerIncarnation: 1, Head: "new", HeadRID: "rid"}
	expectWorker(t, effect(at, cs.Mutation{RecordUpload: &receipt}), cs.Unauthorized)
	receipt.Cell = "chat"
	receipt.WorkerIncarnation = 2
	expectWorker(t, effect(at, cs.Mutation{RecordUpload: &receipt}), cs.Conflict)
	receipt.WorkerIncarnation = 1
	expectWorker(t, effect(at, cs.Mutation{RecordUpload: &receipt}), cs.OK)
	expectWorker(t, effect(at, cs.Mutation{RenewHolds: &cs.RenewHolds{Device: f.id, WorkerIncarnation: 1, SeenAt: at, Items: []cs.HoldFence{{Cell: "outside", Fence: 1}}}}), cs.Unauthorized)
	renew := cs.RenewHolds{Device: f.id, WorkerIncarnation: 1, SeenAt: at, Items: []cs.HoldFence{{Cell: "chat", Fence: 1}}}
	expectWorker(t, effect(at, cs.Mutation{RenewHolds: &renew}), cs.OK)
	expectWorker(t, f.run(1, at+1, cs.Mutation{Release: &cs.ReleaseCell{Cell: "chat", Fence: 1}}), cs.OK)
	r = effect(at+2, cs.Mutation{RenewHolds: &renew})
	if r.Items[0].Code != cs.FenceStale {
		t.Fatal("delayed renew undid release", r)
	}
}

// R27 idle registry availability does not pin timers. Active work does, while
// its proof lasts; the registry itself never deletes or expires saved records.
func TestR27WorkerActivityAndScopedRead(t *testing.T) {
	f := workerFixtureFor(t, false)
	expectWorker(t, f.register(0, 1001), cs.OK)
	expectWorker(t, f.beat(f.proof(1, 1, 1002), 1002), cs.OK)
	active, e := cs.RecordActive(cs.WorkerKey(f.id), f.a.rows[cs.WorkerKey(f.id)], 1003)
	if e != nil || active {
		t.Fatal("idle worker kept timer active", e)
	}
	f.a.put(cs.CellKey("chat"), cs.Created(cs.CellInit{Head: "head"}, f.id, 1000))
	expectWorker(t, f.beat(f.proof(1, 2, 1003, cs.HoldFence{Cell: "chat", Fence: 1}), 1003), cs.OK)
	active, e = cs.RecordActive(cs.WorkerKey(f.id), f.a.rows[cs.WorkerKey(f.id)], 1004)
	if e != nil || !active {
		t.Fatal("active worker did not keep timer", e)
	}
	active, e = cs.RecordActive(cs.WorkerKey(f.id), f.a.rows[cs.WorkerKey(f.id)], 91003)
	if e != nil || active {
		t.Fatal("availability never went stale", e)
	}
	read := func(k cs.RecordKey) ([]byte, error) { return f.a.rows[k], nil }
	r, e := cs.ReadWorkerCell("test", f.id, "chat", 1, 1004, read)
	if e != nil || r.Code != cs.OK || r.View.Cell.Head != "head" || len(r.Grant) == 0 {
		t.Fatal(r, e)
	}
	for _, cell := range []string{"outside", ""} {
		r, e = cs.ReadWorkerCell("test", f.id, cell, 1, 1004, read)
		if e != nil || r.Code != cs.Unauthorized || r.Grant != nil || r.View != nil {
			t.Fatal("scope response disclosed data", r, e)
		}
	}
	r, e = cs.ReadWorkerCell("test", f.id, "chat", 1, f.cert.Expires, read)
	if e != nil || r.Code != cs.Unauthorized {
		t.Fatal("expired certificate read", r, e)
	}
}

// R16 checks a CURRENT worker peer, independently of incarnation retirement.
// An already signed request loses to the committed device revoke everywhere.
func TestR16WorkerActivePeerRevoke(t *testing.T) {
	f := workerFixtureFor(t, true)
	expectWorker(t, f.register(0, 1001), cs.OK)
	expectWorker(t, enroll(f.a, f.id), cs.OK)
	expectWorker(t, settle(f.a), cs.OK)
	read := func(k cs.RecordKey) ([]byte, error) { return f.a.rows[k], nil }
	_, before, e := cs.MembershipViewAt(read, f.a.meta.Configuration, f.a.meta.Index, 1002)
	if e != nil || before[3].Revoked {
		t.Fatal(before, e)
	}
	pending := f.command(f.id, f.key, 1, 1002, cs.Mutation{RegisterWorker: &cs.RegisterWorker{Previous: 1, Incarnation: 2}})
	expectWorker(t, f.owner(1003, cs.Mutation{Revoke: &cs.DeviceID{Device: f.id}}), cs.OK)
	expectWorker(t, f.eval(pending), cs.Revoked)
	_, after, e := cs.MembershipViewAt(read, f.a.meta.Configuration, f.a.meta.Index, 1003)
	if e != nil || !after[3].Revoked {
		t.Fatal("revoked active peer admitted", after, e)
	}
}

// R16/R17 allow the restricted ingress service to batch original worker proofs
// without root authority. Replaying one proof cannot extend availability.
func TestR16R17WorkerHeartbeatCollectorAndReplay(t *testing.T) {
	f := workerFixtureFor(t, false)
	expectWorker(t, f.register(0, 1001), cs.OK)
	f.a.put(cs.AuthorityKey("effects"), cs.Authority{Cluster: "test", Key: actorKey("effects").Public().(ed25519.PublicKey), Effects: true})
	h := f.proof(1, 1, 1002)
	command := f.command("effects", actorKey("effects"), 0, 1002, cs.Mutation{WorkerBeatBatch: &cs.WorkerBeatBatch{Items: []cs.WorkerHeartbeat{h}}})
	r := f.eval(command)
	expectWorker(t, r, cs.OK)
	if r.Workers[0].Code != cs.OK {
		t.Fatal(r)
	}
	command.Now = 1002 + cs.EvidenceWindowMillis + 1
	var e error
	command, e = cs.Sign(command, actorKey("effects"))
	if e != nil {
		t.Fatal(e)
	}
	r = f.eval(command)
	expectWorker(t, r, cs.OK)
	if r.Workers[0].Code != cs.OK || r.Workers[0].AvailableUntil != 91002 || f.record().AvailableUntil != 91002 {
		t.Fatal("exact proof replay moved availability", r)
	}
	// The collector cannot alter the proof while retaining its sequence.
	h.SeenAt = command.Now
	b, _ := cs.WorkerHeartbeatBytes("test", h)
	h.Signature = ed25519.Sign(f.key, b)
	r = f.eval(f.command("effects", actorKey("effects"), 0, command.Now, cs.Mutation{WorkerBeatBatch: &cs.WorkerBeatBatch{Items: []cs.WorkerHeartbeat{h}}}))
	if r.Workers[0].Code != cs.Conflict {
		t.Fatal("same sequence accepted different proof", r)
	}
}
