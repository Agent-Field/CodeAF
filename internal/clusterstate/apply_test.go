package clusterstate_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftsim/model"
	"github.com/anishathalye/porcupine"
)

type application struct {
	t    *testing.T
	rows map[cs.RecordKey][]byte
	meta cs.ApplicationMeta
	seq  map[string]uint64
}

func actorKey(id string) ed25519.PrivateKey {
	h := sha256.Sum256([]byte(id))
	return ed25519.NewKeyFromSeed(h[:])
}
func newApplication(t *testing.T) *application {
	a := &application{t: t, rows: map[cs.RecordKey][]byte{}, meta: cs.ApplicationMeta{Cluster: "test", Ceiling: cs.DefaultApplicationBytes}, seq: map[string]uint64{}}
	for _, id := range []string{"a", "b"} {
		a.put(cs.AuthorityKey(id), cs.Authority{Cluster: "test", Key: actorKey(id).Public().(ed25519.PublicKey), Personal: true})
		a.put(cs.DeviceKey(id), cs.Device{V: 1})
	}
	a.put(cs.IdentityKey(), cs.IdentityRec{V: 1, Identity: cs.IdentityID(actorKey("root").Public().(ed25519.PublicKey)), SigningKey: actorKey("root").Public().(ed25519.PublicKey)})
	return a
}
func (a *application) put(k cs.RecordKey, v any) {
	b, e := cs.Encode(v)
	if e != nil {
		a.t.Fatal(e)
	}
	a.rows[k] = b
}
func (a *application) prepare(actor string, now int64, m cs.Mutation) cs.Command {
	// The independent ownership campaign assumes completed uploads. Supply the
	// corresponding committed receipt fixture; missing uploads have separate R21 vectors.
	if m.Create != nil && m.Create.Receipt == "" {
		r := cs.UploadReceipt{Device: actor, Head: m.Create.Init.Head, HeadRID: "fixture", Frames: m.Create.Init.Frames}
		m.Create.Receipt = cs.UploadID(r)
		a.put(cs.UploadKey(m.Create.Receipt), r)
	}
	if m.Publish != nil && m.Publish.Receipt == "" {
		r := cs.UploadReceipt{Device: actor, Head: m.Publish.Publish.Head, HeadRID: "fixture", Frames: m.Publish.Publish.Frames}
		m.Publish.Receipt = cs.UploadID(r)
		a.put(cs.UploadKey(m.Publish.Receipt), r)
	}
	return a.prepareBare(actor, now, m)
}
func (a *application) prepareBare(actor string, now int64, m cs.Mutation) cs.Command {
	a.seq[actor]++
	seq := a.seq[actor]
	c, e := cs.Sign(cs.Command{Schema: 1, Cluster: "test", Actor: actor, Session: "test", Sequence: seq, Ack: seq - 1, Now: now, Change: m}, actorKey(actor))
	if e != nil {
		a.t.Fatal(e)
	}
	return c
}
func (a *application) evaluate(c cs.Command) cs.Evaluation {
	a.meta.Bytes = 0
	for k, b := range a.rows {
		a.meta.Bytes += cs.RecordBytes(k, b)
	}
	raw, e := cs.EncodeCommand(c)
	if e != nil {
		a.t.Fatal(e)
	}
	out, e := cs.Evaluate(raw, a.meta, func(k cs.RecordKey) ([]byte, error) { return a.rows[k], nil })
	if e != nil {
		a.t.Fatal(e)
	}
	for _, w := range out.Writes {
		a.rows[w.RecordKey] = w.Data
	}
	a.meta.Now = out.Now
	a.meta.Version = out.Version
	return out
}
func (a *application) run(actor string, now int64, m cs.Mutation) cs.Result {
	return a.evaluate(a.prepare(actor, now, m)).Result
}
func (a *application) cell() cs.Cell {
	var c cs.Cell
	if b := a.rows[cs.CellKey("chat")]; b != nil {
		if e := cs.Decode(b, &c); e != nil {
			a.t.Fatal(e)
		}
	}
	return c
}
func small(c cs.Cell) model.Cell {
	return model.Cell{Holder: c.Lease.Device, Fence: c.Lease.Fence, Until: c.Lease.Expires, Head: c.Head, Pending: c.Lease.Pending, ReleasedAt: c.Lease.ReleasedAt}
}
func seedCount(t *testing.T) int {
	t.Helper()
	if s, ok := os.LookupEnv("RAFTSIM_SEEDS"); ok {
		n, e := strconv.Atoi(s)
		if e != nil || n < 1 || n > 10000 {
			t.Fatal("invalid RAFTSIM_SEEDS")
		}
		return n
	}
	return 500
}

// R1/R2/R16/R17 compare the actual signed production evaluator with the
// independent sequential ownership/vault/revocation model, including errors.
func TestR1R2R16R17AuthoritativeApplyIndependentCampaign(t *testing.T) {
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%04d", seed), func(t *testing.T) {
			a := newApplication(t)
			rng := rand.New(rand.NewSource(int64(seed)))
			state := model.State{}
			history := []model.Operation{}
			now := int64(1000000)
			for step := 0; step < 24; step++ {
				actor := []string{"a", "b"}[rng.Intn(2)]
				op := model.Call{Actor: actor, Cell: "chat", At: now}
				m := cs.Mutation{}
				cell := a.cell()
				f := cell.Lease.Fence
				if rng.Intn(4) == 0 && f > 0 {
					f--
				}
				switch {
				case step == 0:
					op.Kind = "create"
					op.Head = "sealed-one"
					m.Create = &cs.CreateCell{Cell: "chat", Init: cs.CellInit{Head: op.Head}}
				case step == 23:
					op.Kind = "revoke"
					op.Target = "b"
					m.Revoke = &cs.DeviceID{Device: op.Target}
				default:
					switch rng.Intn(6) {
					case 0:
						op.Kind = "acquire"
						op.Force = rng.Intn(2) == 0
						op.Expected = model.Cell{Holder: cell.Lease.Device, Fence: f}
						m.Acquire = &cs.AcquireCell{Cell: "chat", Force: op.Force, Expect: cs.LeaseExpectation{Device: op.Expected.Holder, Fence: f}}
					case 1:
						op.Kind = "publish"
						op.Fence = f
						op.OldHead = cell.Head
						if rng.Intn(4) == 0 {
							op.OldHead = "old"
						}
						op.Head = fmt.Sprint("sealed-", step)
						m.Publish = &cs.PublishCell{Cell: "chat", Publish: cs.Publish{Fence: f, OldHead: op.OldHead, Head: op.Head}}
					case 2:
						op.Kind = "beat"
						op.Fence = f
						op.Pending = uint32(rng.Intn(4))
						m.Beat = &cs.BeatCell{Cell: "chat", Beat: cs.Beat{Fence: f, Pending: op.Pending}}
					case 3:
						op.Kind = "release"
						op.Fence = f
						op.Quiet = rng.Intn(2) == 0
						m.Release = &cs.ReleaseCell{Cell: "chat", Fence: f, Quiet: op.Quiet}
					case 4:
						op.Kind = "set_vault"
						op.OldVault = state.Vault
						if rng.Intn(3) == 0 {
							op.OldVault = "old"
						}
						op.NewVault = fmt.Sprint("sealed-vault-", step)
						m.SetVault = &cs.VaultCAS{Old: op.OldVault, New: op.NewVault}
					case 5:
						op.Kind = "advance_time"
						m.AdvanceTime = &cs.AdvanceTime{}
					}
				}
				next, want := model.Step(state, op)
				got := a.run(actor, now, m)
				if string(got.Code) != want.Code {
					t.Fatalf("step %d %s: got %s want %s", step, op.Kind, got.Code, want.Code)
				}
				if want.Code == "ok" && got.View != nil && small(got.View.Cell) != want.Cell {
					t.Fatalf("step %d result differs: %+v %+v", step, small(got.View.Cell), want.Cell)
				}
				if c, ok := next.Cells["chat"]; ok && small(a.cell()) != c {
					t.Fatalf("step %d persisted cell differs", step)
				}
				var identity cs.IdentityRec
				if e := cs.Decode(a.rows[cs.IdentityKey()], &identity); e != nil {
					t.Fatal(e)
				}
				if identity.Vault != next.Vault {
					t.Fatalf("step %d vault differs", step)
				}
				observed := model.Outcome{Code: string(got.Code)}
				if got.View != nil {
					observed.Cell = small(got.View.Cell)
				}
				history = append(history, model.Operation{ID: step, Call: int64(step * 2), Return: int64(step*2 + 1), Input: op, Output: observed})
				state = next
				now += int64(rng.Intn(150000) - 30000)
			}
			if result := model.Check(history); result != porcupine.Ok {
				t.Fatalf("independent history check: %v", result)
			}
		})
	}
}
func TestR16R17SignedAuthorizationAndExactRetry(t *testing.T) {
	a := newApplication(t)
	c := a.prepare("a", 1000, cs.Mutation{Create: &cs.CreateCell{Cell: "chat", Init: cs.CellInit{Head: "sealed"}}})
	first := a.evaluate(c)
	retry := c
	retry.Now = 9000
	retry, e := cs.Sign(retry, actorKey("a"))
	if e != nil {
		t.Fatal(e)
	}
	again := a.evaluate(retry)
	x, _ := cs.EncodeResult(first.Result)
	y, _ := cs.EncodeResult(again.Result)
	if !bytes.Equal(x, y) || len(again.Writes) != 0 {
		t.Fatal("retry changed its committed result")
	}
	bad := c
	bad.Change = cs.Mutation{Archive: &cs.CellID{Cell: "chat"}}
	bad, e = cs.Sign(bad, actorKey("a"))
	if e != nil {
		t.Fatal(e)
	}
	if r := a.evaluate(bad); r.Result.Code != cs.Conflict {
		t.Fatal(r)
	}
	old := a.prepare("b", 1001, cs.Mutation{Acquire: &cs.AcquireCell{Cell: "chat", Force: true, Expect: cs.LeaseExpectation{Device: "a", Fence: 1}}})
	if r := a.run("a", 1002, cs.Mutation{Revoke: &cs.DeviceID{Device: "b"}}); r.Code != cs.OK {
		t.Fatal(r)
	}
	if r := a.evaluate(old); r.Result.Code != cs.Revoked {
		t.Fatal("already authenticated request escaped revocation", r)
	}
	tampered := a.prepare("a", 1003, cs.Mutation{Archive: &cs.CellID{Cell: "chat"}})
	tampered.Now++
	if r := a.evaluate(tampered); r.Result.Code != cs.Unauthorized || len(r.Writes) != 0 {
		t.Fatal(r)
	}
}
func TestR17SessionCloseRetainsTombstoneAndAcknowledgedAdvance(t *testing.T) {
	a := newApplication(t)
	first := a.prepare("a", 1000, cs.Mutation{AdvanceTime: &cs.AdvanceTime{}})
	a.evaluate(first)
	next := a.prepare("a", 1001, cs.Mutation{CloseSession: &cs.CloseSession{}})
	bad := next
	bad.Ack = 0
	bad, _ = cs.Sign(bad, actorKey("a"))
	if a.evaluate(bad).Result.Code != cs.Conflict {
		t.Fatal("missing acknowledgement accepted")
	}
	if a.evaluate(next).Result.Code != cs.OK {
		t.Fatal("close failed")
	}
	if a.evaluate(first).Result.Code != cs.Conflict {
		t.Fatal("closed session resurrected")
	}
}
func TestR1R2R22BatchItemResultsAndVersions(t *testing.T) {
	a := newApplication(t)
	a.run("a", 1000, cs.Mutation{Create: &cs.CreateCell{Cell: "chat", Init: cs.CellInit{Head: "sealed"}}})
	version := a.meta.Version
	r := a.run("a", 2000, cs.Mutation{BeatBatch: &cs.BeatBatch{Items: []cs.BeatCell{{Cell: "chat", Beat: cs.Beat{Fence: 1, Pending: 3}}, {Cell: "missing", Beat: cs.Beat{Fence: 1}}, {Cell: "chat", Beat: cs.Beat{Fence: 0}}}}})
	if len(r.Items) != 3 || r.Items[0].Code != cs.OK || r.Items[1].Code != cs.NotFound || r.Items[2].Code != cs.FenceStale || a.cell().Lease.Pending != 3 || a.meta.Version != version+1 {
		t.Fatal(r, a.meta)
	}
	version = a.meta.Version
	a.run("a", 3000, cs.Mutation{Beat: &cs.BeatCell{Cell: "chat", Beat: cs.Beat{Fence: 1, Pending: 3}}})
	if a.meta.Version != version {
		t.Fatal("expiry-only renewal moved visible version")
	}
}
func TestR27CapacityRefusalIsDurableAndDoesNotDeleteState(t *testing.T) {
	a := newApplication(t)
	a.run("a", 1000, cs.Mutation{Create: &cs.CreateCell{Cell: "chat", Init: cs.CellInit{Head: "sealed"}}})
	a.meta.Bytes = 0
	for k, b := range a.rows {
		a.meta.Bytes += cs.RecordBytes(k, b)
	}
	a.meta.Ceiling = a.meta.Bytes + 1000
	r := a.run("a", 2000, cs.Mutation{Publish: &cs.PublishCell{Cell: "chat", Publish: cs.Publish{Fence: 1, OldHead: "sealed", Head: "new", Title: string(bytes.Repeat([]byte("x"), 4000))}}})
	if r.Code != cs.Capacity || a.cell().Head != "sealed" {
		t.Fatal(r)
	}
}

func TestR14R25FullWidthFencesAndV1Refusal(t *testing.T) {
	a := newApplication(t)
	a.put(cs.CellKey("chat"), cs.Cell{V: 1, Head: "sealed", Lease: cs.Lease{Device: "a", Fence: cs.MaxSafeInteger, Expires: 90000}})
	r := a.run("a", 1000, cs.Mutation{Acquire: &cs.AcquireCell{Cell: "chat", V1: true, Expect: cs.LeaseExpectation{Device: "a", Fence: cs.MaxSafeInteger}}})
	if r.Code != cs.Capacity {
		t.Fatal(r)
	}
	r = a.run("a", 1001, cs.Mutation{Acquire: &cs.AcquireCell{Cell: "chat", Expect: cs.LeaseExpectation{Device: "a", Fence: cs.MaxSafeInteger}}})
	if r.Code != cs.OK || r.View.Cell.Lease.Fence != cs.MaxSafeInteger+1 {
		t.Fatal(r)
	}
	a.put(cs.CellKey("chat"), cs.Cell{V: 1, Head: "sealed", Lease: cs.Lease{Device: "a", Fence: ^uint64(0), Expires: 90000}})
	r = a.run("a", 1002, cs.Mutation{Acquire: &cs.AcquireCell{Cell: "chat", Expect: cs.LeaseExpectation{Device: "a", Fence: ^uint64(0)}}})
	if r.Code != cs.Capacity || a.cell().Lease.Fence != ^uint64(0) {
		t.Fatal("overflow changed fence", r)
	}
}

func TestR16R17R27DependencyAndSessionAdmissionFailures(t *testing.T) {
	a := newApplication(t)
	raw, e := cs.EncodeCommand(a.prepare("a", 1000, cs.Mutation{AdvanceTime: &cs.AdvanceTime{}}))
	if e != nil {
		t.Fatal(e)
	}
	if _, e := cs.Evaluate(raw, a.meta, func(k cs.RecordKey) ([]byte, error) {
		if k == cs.SessionsKey() {
			return nil, cs.ErrDependency
		}
		return a.rows[k], nil
	}); e == nil {
		t.Fatal("unread dependency accepted")
	}
	a.rows[cs.DeviceKey("b")] = []byte("corrupt")
	raw, e = cs.EncodeCommand(a.prepare("a", 1000, cs.Mutation{Revoke: &cs.DeviceID{Device: "b"}}))
	if e != nil {
		t.Fatal(e)
	}
	if _, e := cs.Evaluate(raw, a.meta, func(k cs.RecordKey) ([]byte, error) { return a.rows[k], nil }); e == nil {
		t.Fatal("corrupt target became a policy refusal")
	}
}

func TestR16R30CommittedPairDecisionIsAtomicAndFirstWins(t *testing.T) {
	a := newApplication(t)
	public := actorKey("new").Public().(ed25519.PublicKey)
	newID := cs.DeviceIDOfKey(public)
	a.put(cs.JoinKey("request"), cs.JoinRequest{Device: newID, PublicKey: public, Expires: 10000})
	decision := cs.JoinDecision{Code: "request", Device: newID, Record: cs.Device{V: 1, Name: "sealed"}, Grant: "c2VhbGVkLWdyYW50", Cert: joinCert(t, "new")}
	if r := a.run("a", 1000, cs.Mutation{DecideJoin: &decision}); r.Code != cs.OK {
		t.Fatal(r)
	}
	var request cs.JoinRequest
	if e := cs.Decode(a.rows[cs.JoinKey("request")], &request); e != nil {
		t.Fatal(e)
	}
	var device cs.Device
	if e := cs.Decode(a.rows[cs.DeviceKey(newID)], &device); e != nil {
		t.Fatal(e)
	}
	if request.Decision == nil || request.Decision.Grant != "c2VhbGVkLWdyYW50" || device.Created != 1000 {
		t.Fatal(request, device)
	}
	version := a.meta.Version
	if r := a.run("a", 1001, cs.Mutation{DecideJoin: &decision}); r.Code != cs.OK || a.meta.Version != version {
		t.Fatal(r)
	}
	if r := a.run("b", 1002, cs.Mutation{DecideJoin: &cs.JoinDecision{Code: "request", Deny: true}}); r.Code != cs.AlreadyDecided {
		t.Fatal(r)
	}
	if a.rows[cs.AuthorityKey(newID)] == nil {
		t.Fatal("pair decision omitted committed certificate authorization")
	}
}
