// Package fabrictest holds HTTP conformance cases shared by in-process and live relays.
package fabrictest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func fabricCall(t *testing.T, r *Rig, d device, method, path string, body any, want int) []byte {
	t.Helper()
	out, _ := fabricCallHeader(t, r, d, method, path, body, want)
	return out
}

func fabricCallHeader(t *testing.T, r *Rig, d device, method, path string, body any, want int) ([]byte, http.Header) {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, r.URL()+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req, raw, d, r.Now())
	resp, err := r.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: %d %s, want %d", method, path, resp.StatusCode, out, want)
	}
	return out, resp.Header
}

// openCallPeer answers a fresh private address for one open call.
func openCallPeer(t *testing.T) string {
	t.Helper()
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("10.%d.%d.%d", b[0], b[1], b[2])
}

func openCall(t *testing.T, r *Rig, method, path string, body any, want int) []byte {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, r.URL()+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	// Every open call comes from a network of its own, so a live relay started with --trust-proxy does not charge
	// one case's link requests to the budget another case already spent from the tester's single address.
	req.Header.Set("X-Forwarded-For", openCallPeer(t))
	resp, err := r.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s: %d %s, want %d", method, path, resp.StatusCode, out, want)
	}
	return out
}

func pairClient(t *testing.T, r *Rig) (device, device, string) {
	t.Helper()
	id, full := newIdentity(t)
	client := newDevice(t, id)
	path := "/v1/clients/" + client.dev.ID()
	fabricCall(t, r, full, "PUT", path, map[string]any{"cert": client.Cert(), "methods": []string{"chat.read", "chat.send"}}, 204)
	return full, client, path
}

func testFabricClientScope(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	raw := fabricCall(t, r, full, "GET", "/v1/dir/list", nil, 200)
	var listing struct {
		Devices map[string]struct {
			Kind    string
			AddedBy string `json:"added_by"`
			Methods []string
		}
	}
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	rec := listing.Devices[client.dev.ID()]
	if rec.Kind != "client" || len(rec.Methods) != 2 || rec.AddedBy != reqsign.IDOf(full.IdentityKey()) {
		t.Fatalf("client record: %+v", rec)
	}
	for _, route := range []string{"/v1/dir/list", "/v1/store/stats", "/v1/identity/rotation"} {
		fabricCall(t, r, client, "GET", route, nil, 401)
	}
	fabricCall(t, r, client, "PUT", "/v1/dir/devices/"+client.dev.ID(), map[string]any{"V": 1}, 401)
	fabricCall(t, r, client, "POST", "/v1/dir/cells/chat", map[string]any{"head": "h"}, 401)
	fabricCall(t, r, client, "POST", "/v1/dir/cells/chat/acquire", map[string]any{}, 401)
	fabricCall(t, r, client, "POST", "/v1/dir/cells/chat/publish", map[string]any{"fence": 1, "old_head": "h", "head": "h2"}, 401)
	fabricCall(t, r, client, "POST", "/v1/dir/cells/chat/release", map[string]any{"fence": 1}, 401)
	fabricCall(t, r, client, "POST", "/v1/dir/vault", map[string]any{"old": "", "new": "v"}, 401)
	fabricCall(t, r, client, "POST", "/v1/dir/devices/"+full.dev.ID()+"/revoke", nil, 401)
	fabricCall(t, r, client, "POST", "/v1/store/has", map[string]any{"rids": []string{}}, 401)
	fabricCall(t, r, full, "PUT", "/v1/dir/devices/"+full.dev.ID(), map[string]any{"V": 1, "kind": "client"}, 400)
	fabricCall(t, r, full, "PUT", "/v1/dir/devices/"+full.dev.ID(), map[string]any{"V": 1, "methods": []string{"chat.read"}}, 400)
	fabricCall(t, r, full, "PUT", path, map[string]any{"cert": client.Cert(), "methods": []string{}}, 409)
	if r.Restart != nil {
		r.Restart()
	}
	fabricCall(t, r, client, "GET", "/v1/dir/list", nil, 401)
	fabricCall(t, r, full, "POST", "/v1/dir/devices/"+client.dev.ID()+"/revoke", nil, 204)
	fabricCall(t, r, client, "PUT", path, nil, 401)
}

func testFabricSlots(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	slot := path + "/slots/home"
	before := relayTime(t, r)
	fabricCall(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("first")}, 204)
	raw := fabricCall(t, r, client, "GET", slot, nil, 200)
	var got struct {
		Sealed []byte
		Writer string
		At     int64
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Sealed) != "first" || got.Writer != full.dev.ID() || (got.At < before || got.At > relayTime(t, r)) {
		t.Fatalf("slot: %+v", got)
	}
	fabricCall(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("replacement")}, 204)
	if r.Restart != nil {
		r.Restart()
	}
	raw = fabricCall(t, r, client, "GET", slot, nil, 200)
	if err := json.Unmarshal(raw, &got); err != nil || string(got.Sealed) != "replacement" {
		t.Fatalf("after restart: %s %v", raw, err)
	}
	fabricCall(t, r, client, "PUT", slot, map[string]any{"sealed": []byte("forged")}, 401)
	fabricCall(t, r, full, "GET", slot, nil, 401)
	_, other := newIdentity(t)
	fabricCall(t, r, other, "PUT", slot, map[string]any{"sealed": []byte("foreign")}, 404)
	fabricCall(t, r, full, "PUT", slot, map[string]any{"sealed": make([]byte, 65537)}, 413)
	if r.Advance == nil {
		return
	}
	r.Advance(24 * time.Hour)
	fabricCall(t, r, client, "GET", slot, nil, 404)
	fabricCall(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("fresh")}, 204)
	fabricCall(t, r, full, "POST", "/v1/dir/devices/"+client.dev.ID()+"/revoke", nil, 204)
	fabricCall(t, r, client, "GET", slot, nil, 401)
}

func testFabricSlotCapacityAndClientIsolation(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	other := newDevice(t, full.id)
	otherPath := "/v1/clients/" + other.dev.ID()
	fabricCall(t, r, full, "PUT", otherPath, map[string]any{"cert": other.Cert(), "methods": []string{}}, 204)
	for n := 0; n < clientproto.MaxClientSlots; n++ {
		fabricCall(t, r, full, "PUT", path+"/slots/"+strconv.Itoa(n), map[string]any{"sealed": []byte("sealed")}, 204)
	}
	fabricCall(t, r, full, "PUT", path+"/slots/extra", map[string]any{"sealed": []byte("sealed")}, 429)
	fabricCall(t, r, full, "PUT", path+"/slots/0", map[string]any{"sealed": []byte("replaced")}, 204)
	fabricCall(t, r, other, "GET", path+"/slots/0", nil, 401)
	fabricCall(t, r, client, "GET", otherPath+"/slots/0", nil, 401)
	if r.Advance == nil {
		return
	}
	r.Advance(24 * time.Hour)
	fabricCall(t, r, full, "PUT", path+"/slots/extra", map[string]any{"sealed": []byte("sealed")}, 204)
}

func testFabricQueueClaimsAndProof(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	second := newDevice(t, full.id)
	queue := path + "/queue"
	body := map[string]any{"number": 1, "at": r.Now().UnixMilli(), "method": "chat.send", "target": "any", "sealed": []byte("sealed call")}
	fabricCall(t, r, client, "POST", queue, body, 204)
	raw := fabricCall(t, r, full, "GET", queue, nil, 200)
	var got struct {
		Entries []struct {
			Number      int64
			Sealed      []byte
			Body        []byte
			Signature   string
			Cert        string
			Identity    string
			RequestTime string `json:"request_time"`
			RequestURI  string `json:"request_uri"`
		}
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].Number != 1 || string(got.Entries[0].Sealed) != "sealed call" {
		t.Fatalf("queue: %s", raw)
	}
	e := got.Entries[0]
	req, _ := http.NewRequest("POST", r.URL()+e.RequestURI, bytes.NewReader(e.Body))
	req.Header.Set(reqsign.HeaderIdentity, e.Identity)
	req.Header.Set(reqsign.HeaderCert, e.Cert)
	req.Header.Set(reqsign.HeaderTime, e.RequestTime)
	req.Header.Set(reqsign.HeaderSig, e.Signature)
	if _, dev, err := reqsign.Verify(req, e.Body, r.Now()); err != nil || dev != client.dev.ID() {
		t.Fatalf("entry proof: %s %v", dev, err)
	}
	fabricCall(t, r, full, "DELETE", queue+"/1", nil, 409)
	token := map[string]string{"token": strings.Repeat("a", 64)}
	fabricCall(t, r, full, "POST", queue+"/1/claim", token, 204)
	fabricCall(t, r, full, "POST", queue+"/1/claim", map[string]string{"token": strings.Repeat("b", 64)}, 409)
	raw = fabricCall(t, r, full, "GET", queue, nil, 200)
	if bytes.Contains(raw, []byte(strings.Repeat("a", 64))) {
		t.Fatal("queue disclosed its private ownership token")
	}
	if r.Restart != nil {
		r.Restart()
	}
	fabricCall(t, r, full, "POST", queue+"/1/claim", token, 204)
	fabricCall(t, r, full, "POST", queue+"/1/claim", nil, 409)
	fabricCall(t, r, second, "POST", queue+"/1/claim", nil, 409)
	fabricCall(t, r, second, "DELETE", queue+"/1", nil, 409)
	fabricCall(t, r, full, "DELETE", queue+"/1", nil, 204)
	raw = fabricCall(t, r, full, "GET", queue, nil, 200)
	if string(raw) != "{\"entries\":[]}\n" {
		t.Fatalf("acked queue: %s", raw)
	}
	fabricCall(t, r, client, "POST", queue, body, 409)
	body["number"] = 3
	fabricCall(t, r, client, "POST", queue, body, 409)
	body["number"] = 2
	fabricCall(t, r, client, "POST", queue, body, 204)
}

func testFabricQueueLimitsRoutingAndExpiry(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	queue := path + "/queue"
	in := map[string]any{"number": 1, "at": r.Now().UnixMilli(), "method": "chat.send", "target": "any", "sealed": []byte("sealed")}
	in["method"] = "shell.run"
	fabricCall(t, r, client, "POST", queue, in, 403)
	in["method"] = "chat.send"
	in["at"] = r.Now().Add(-24 * time.Hour).UnixMilli()
	fabricCall(t, r, client, "POST", queue, in, 400)
	in["at"] = r.Now().Add(6 * time.Minute).UnixMilli()
	fabricCall(t, r, client, "POST", queue, in, 400)
	in["at"] = r.Now().UnixMilli()
	in["sealed"] = make([]byte, 4097)
	fabricCall(t, r, client, "POST", queue, in, 413)
	in["sealed"] = []byte("sealed")
	in["target"] = full.dev.ID()
	fabricCall(t, r, client, "POST", queue, in, 400)
	fabricCall(t, r, full, "PUT", "/v1/dir/devices/"+full.dev.ID(), map[string]any{"V": 1}, 204)
	fabricCall(t, r, client, "POST", queue, in, 204)
	other := newDevice(t, full.id)
	raw := fabricCall(t, r, other, "GET", queue, nil, 200)
	if string(raw) != "{\"entries\":[]}\n" {
		t.Fatalf("target isolation: %s", raw)
	}
	fabricCall(t, r, other, "POST", queue+"/1/claim", nil, 401)
	fabricCall(t, r, full, "POST", queue+"/1/claim", nil, 204)
	fabricCall(t, r, full, "DELETE", queue+"/1", nil, 204)
	in["target"] = "any"
	for number := 2; number <= 129; number++ {
		in["number"] = number
		fabricCall(t, r, client, "POST", queue, in, 204)
	}
	in["number"] = 130
	fabricCall(t, r, client, "POST", queue, in, 429)
	if r.Advance == nil {
		return
	}
	r.Advance(24 * time.Hour)
	fabricCall(t, r, full, "POST", queue+"/2/claim", nil, 404)
	in["at"] = r.Now().UnixMilli()
	fabricCall(t, r, client, "POST", queue, in, 204)
	if r.Restart != nil {
		r.Restart()
	}
	in["number"] = 129
	fabricCall(t, r, client, "POST", queue, in, 409)
}

func testFabricQueueOneClaimer(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	other := newDevice(t, full.id)
	fabricCall(t, r, client, "POST", path+"/queue", map[string]any{"number": 1, "at": r.Now().UnixMilli(), "method": "chat.send", "target": "any", "sealed": []byte("sealed")}, 204)
	statuses := make(chan int, 2)
	for _, d := range []device{full, other} {
		go func(d device) {
			req, _ := http.NewRequest("POST", r.URL()+path+"/queue/1/claim", nil)
			reqsign.Sign(req, nil, d, r.Now())
			resp, err := r.Client.Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			resp.Body.Close()
			statuses <- resp.StatusCode
		}(d)
	}
	a, b := <-statuses, <-statuses
	if !((a == 204 && b == 409) || (a == 409 && b == 204)) {
		t.Fatalf("claim race: %d %d", a, b)
	}
}

// Rig drives the real signed HTTP seam. Live relays omit clock advancement and restart.
type Rig struct {
	URL     func() string
	Client  *http.Client
	Now     func() time.Time
	Advance func(time.Duration)
	Restart func()
}

type device struct {
	id  identity.Identity
	dev identity.Dev
}

func (d device) IdentityKey() ed25519.PublicKey { return d.id.PublicKey() }
func (d device) Cert() identity.Cert            { return d.dev.Cert }
func (d device) Sign(msg []byte) []byte         { return d.dev.Sign(msg) }

func newIdentity(t *testing.T) (identity.Identity, device) {
	t.Helper()
	id, err := identity.Ensure(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return id, newDevice(t, id)
}
func newDevice(t *testing.T, id identity.Identity) device {
	t.Helper()
	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	dev, err := seed.Join(identity.IssueCert(id, seed.Public(), time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return device{id, dev}
}

// Run holds every implementation to the same scoped-client, slot and queue contract.
func Run(t *testing.T, makeRig func(*testing.T) *Rig) {
	t.Helper()
	t.Run("AuthorizationAndBodies", func(t *testing.T) { testFabricAuthorizationAndBodies(t, makeRig(t)) })
	t.Run("Revocation", func(t *testing.T) { testFabricRevocation(t, makeRig(t)) })
	t.Run("ClientScope", func(t *testing.T) { testFabricClientScope(t, makeRig(t)) })
	t.Run("SlotWaitCancellation", func(t *testing.T) { testSlotWaitCancellation(t, makeRig(t)) })
	t.Run("SlotChanges", func(t *testing.T) { testSlotChanges(t, makeRig(t)) })
	t.Run("Slots", func(t *testing.T) { testFabricSlots(t, makeRig(t)) })
	t.Run("SlotCapacityAndClientIsolation", func(t *testing.T) { testFabricSlotCapacityAndClientIsolation(t, makeRig(t)) })
	t.Run("SlotCapacity128", func(t *testing.T) { testSlotCapacity128(t, makeRig(t)) })
	t.Run("SlotVersionsAndWait", func(t *testing.T) { testSlotVersionsAndWait(t, makeRig(t)) })
	t.Run("QueueClaimsAndProof", func(t *testing.T) { testFabricQueueClaimsAndProof(t, makeRig(t)) })
	t.Run("ClaimToken", func(t *testing.T) { testClaimToken(t, makeRig(t)) })
	t.Run("QueueLimitsRoutingAndExpiry", func(t *testing.T) { testFabricQueueLimitsRoutingAndExpiry(t, makeRig(t)) })
	t.Run("QueueOneClaimer", func(t *testing.T) { testFabricQueueOneClaimer(t, makeRig(t)) })
	t.Run("EnrollBeforeRegistration", func(t *testing.T) { testEnrollBeforeRegistration(t, makeRig(t)) })
	t.Run("EnrollRefusals", func(t *testing.T) { testEnrollRefusals(t, makeRig(t)) })
	t.Run("EnrollListIncludesRevoked", func(t *testing.T) { testEnrollListIncludesRevoked(t, makeRig(t)) })
	t.Run("LinkClientClass", func(t *testing.T) { testLinkClientClass(t, makeRig(t)) })
	t.Run("LinkDecideClient", func(t *testing.T) { testLinkDecideClient(t, makeRig(t)) })
	t.Run("ComputersServing", func(t *testing.T) { testComputersServing(t, makeRig(t)) })
	t.Run("ServingFlagIsExact", func(t *testing.T) { testServingFlagIsExact(t, makeRig(t)) })
	t.Run("WatchingHeader", func(t *testing.T) { testWatchingHeader(t, makeRig(t)) })
	t.Run("WatchingEvent", func(t *testing.T) { testWatchingEvent(t, makeRig(t)) })
}

func makeEnrollment(t *testing.T, id identity.Identity, client device, methods, reads []string, ver uint64) clientproto.Enrollment {
	t.Helper()
	pub, err := hex.DecodeString(client.dev.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	e := clientproto.Enrollment{
		V:       1,
		Device:  client.dev.ID(),
		SignPub: pub,
		BoxPub:  bytes.Repeat([]byte{3}, 32),
		Methods: methods,
		Reads:   reads,
		Ver:     ver,
	}
	if e.Methods == nil {
		e.Methods = []string{}
	}
	if e.Reads == nil {
		e.Reads = []string{}
	}
	e.Sig = id.Sign(e.SignText())
	return e
}

func assertFabricErr(t *testing.T, raw []byte, want string) {
	t.Helper()
	var body struct {
		Err string `json:"err"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Err != want {
		t.Fatalf("error body %s, want err=%q", raw, want)
	}
}

// testEnrollBeforeRegistration verifies that an enrollment is stored and listed before the client row exists (contract R1, P10).
func testEnrollBeforeRegistration(t *testing.T, r *Rig) {
	id, full := newIdentity(t)
	client := newDevice(t, id)
	raw := fabricCall(t, r, full, "GET", "/v1/clients", nil, 200)
	if strings.TrimSpace(string(raw)) != `{"clients":[]}` {
		t.Fatalf("empty enrollment list: %s", raw)
	}
	enroll := makeEnrollment(t, id, client, []string{"agents.send", "agents.create"}, []string{"agents.list"}, 1)
	path := "/v1/clients/" + client.dev.ID()
	fabricCall(t, r, full, "PUT", path+"/enroll", enroll, 204)
	raw = fabricCall(t, r, full, "GET", "/v1/clients", nil, 200)
	var list struct {
		Clients []struct {
			Device string                 `json:"device"`
			Enroll clientproto.Enrollment `json:"enroll"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Clients) != 1 || list.Clients[0].Device != client.dev.ID() || !list.Clients[0].Enroll.Same(enroll) {
		t.Fatalf("listed enrollment before registration: %+v", list)
	}
	fabricCall(t, r, full, "PUT", path, map[string]any{"cert": client.Cert(), "methods": enroll.Methods}, 204)
	enroll2 := makeEnrollment(t, id, client, []string{"agents.send"}, []string{"agents.list"}, 2)
	fabricCall(t, r, full, "PUT", path+"/enroll", enroll2, 204)
	if r.Restart != nil {
		r.Restart()
	}
	raw = fabricCall(t, r, full, "GET", "/v1/clients", nil, 200)
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Clients) != 1 || list.Clients[0].Enroll.Ver != 2 {
		t.Fatalf("listed enrollment after registration and restart: %s %v", raw, err)
	}
}

// testEnrollRefusals covers signature, device binding, version monotonicity, capacity and caller scope refusals (contract R1, R2, P6).
func testEnrollRefusals(t *testing.T, r *Rig) {
	id, full := newIdentity(t)
	client := newDevice(t, id)
	path := "/v1/clients/" + client.dev.ID() + "/enroll"
	enroll := makeEnrollment(t, id, client, []string{"agents.send", "agents.create"}, []string{"agents.list", "agents.entries"}, 2)

	// Bad signature under the identity key is refused with 400 bad_request.
	badSig := enroll
	badSig.Sig = append([]byte(nil), enroll.Sig...)
	badSig.Sig[0] ^= 0xff
	assertFabricErr(t, fabricCall(t, r, full, "PUT", path, badSig, 400), "bad_request")

	// Enrollment signed by another identity is refused with 400 bad_request.
	otherID, _ := newIdentity(t)
	foreignSig := makeEnrollment(t, otherID, client, enroll.Methods, enroll.Reads, 1)
	assertFabricErr(t, fabricCall(t, r, full, "PUT", path, foreignSig, 400), "bad_request")

	// Device ID not bound to sign_pub is refused with 400 bad_request.
	otherDev := newDevice(t, id)
	unbound := enroll
	unbound.Device = otherDev.dev.ID()
	unbound.Sig = id.Sign(unbound.SignText())
	assertFabricErr(t, fabricCall(t, r, full, "PUT", "/v1/clients/"+otherDev.dev.ID()+"/enroll", unbound, 400), "bad_request")

	// Path device mismatching body.device is refused with 400 bad_request.
	assertFabricErr(t, fabricCall(t, r, full, "PUT", "/v1/clients/"+otherDev.dev.ID()+"/enroll", enroll, 400), "bad_request")

	// Unknown or missing key in JSON body is refused with 400 bad_request.
	assertFabricErr(t, fabricCall(t, r, full, "PUT", path, map[string]any{
		"v": 1, "device": enroll.Device, "sign_pub": enroll.SignPub, "box_pub": enroll.BoxPub,
		"methods": enroll.Methods, "reads": enroll.Reads, "ver": 2, "sig": enroll.Sig, "extra": true,
	}, 400), "bad_request")
	assertFabricErr(t, fabricCall(t, r, full, "PUT", path, map[string]any{
		"v": 1, "device": enroll.Device, "sign_pub": enroll.SignPub, "box_pub": enroll.BoxPub,
		"methods": enroll.Methods, "ver": 2, "sig": enroll.Sig,
	}, 400), "bad_request")

	// Store ver 2, then verify identical re-send (including reordered sets) is 204.
	fabricCall(t, r, full, "PUT", path, enroll, 204)
	reordered := enroll
	reordered.Methods = []string{"agents.create", "agents.send"}
	reordered.Reads = []string{"agents.entries", "agents.list"}
	fabricCall(t, r, full, "PUT", path, reordered, 204)

	// Different statement at equal version and lower version are refused with 409 version.
	diffAtSameVer := makeEnrollment(t, id, client, []string{"agents.send"}, []string{"agents.list"}, 2)
	assertFabricErr(t, fabricCall(t, r, full, "PUT", path, diffAtSameVer, 409), "version")
	lowerVer := makeEnrollment(t, id, client, []string{"agents.send", "agents.create"}, []string{"agents.list", "agents.entries"}, 1)
	assertFabricErr(t, fabricCall(t, r, full, "PUT", path, lowerVer, 409), "version")

	// Existing full device row is refused with 409 exists.
	fabricCall(t, r, full, "PUT", "/v1/dir/devices/"+full.dev.ID(), map[string]any{"V": 1}, 204)
	fullEnroll := makeEnrollment(t, id, full, []string{}, []string{}, 1)
	assertFabricErr(t, fabricCall(t, r, full, "PUT", "/v1/clients/"+full.dev.ID()+"/enroll", fullEnroll, 409), "exists")

	// Register client and verify a client caller is refused with 401 on enroll and list.
	fabricCall(t, r, full, "PUT", "/v1/clients/"+client.dev.ID(), map[string]any{"cert": client.Cert(), "methods": enroll.Methods}, 204)
	fabricCall(t, r, client, "PUT", path, enroll, 401)
	fabricCall(t, r, client, "GET", "/v1/clients", nil, 401)

	// Fill up to 16 enrollments, then verify the 17th is refused with 429 full.
	for i := 1; i < clientproto.MaxEnrollments; i++ {
		d := newDevice(t, id)
		e := makeEnrollment(t, id, d, []string{"agents.send"}, []string{}, 1)
		fabricCall(t, r, full, "PUT", "/v1/clients/"+d.dev.ID()+"/enroll", e, 204)
	}
	seventeenth := newDevice(t, id)
	e17 := makeEnrollment(t, id, seventeenth, []string{"agents.send"}, []string{}, 1)
	assertFabricErr(t, fabricCall(t, r, full, "PUT", "/v1/clients/"+seventeenth.dev.ID()+"/enroll", e17, 429), "full")

	// Updating an already enrolled device still succeeds at capacity.
	higherVer := makeEnrollment(t, id, client, []string{"agents.send"}, []string{"agents.list"}, 3)
	fabricCall(t, r, full, "PUT", path, higherVer, 204)
}

// testEnrollListIncludesRevoked checks that revoking a client deletes its state but preserves its enrollment (contract R1, R2, V4).
func testEnrollListIncludesRevoked(t *testing.T, r *Rig) {
	id, full := newIdentity(t)
	c1 := newDevice(t, id)
	c2 := newDevice(t, id)
	e1 := makeEnrollment(t, id, c1, []string{"agents.send"}, []string{"agents.list"}, 1)
	e2 := makeEnrollment(t, id, c2, []string{"agents.send"}, []string{"agents.list"}, 1)
	fabricCall(t, r, full, "PUT", "/v1/clients/"+c1.dev.ID()+"/enroll", e1, 204)
	fabricCall(t, r, full, "PUT", "/v1/clients/"+c2.dev.ID()+"/enroll", e2, 204)
	fabricCall(t, r, full, "PUT", "/v1/clients/"+c1.dev.ID(), map[string]any{"cert": c1.Cert(), "methods": e1.Methods}, 204)
	fabricCall(t, r, full, "PUT", "/v1/clients/"+c1.dev.ID()+"/slots/home", map[string]any{"sealed": []byte("state")}, 204)
	fabricCall(t, r, full, "POST", "/v1/dir/devices/"+c1.dev.ID()+"/revoke", nil, 204)

	// Slots are gone, and re-enrolling a revoked device is 409 exists.
	fabricCall(t, r, full, "PUT", "/v1/clients/"+c1.dev.ID()+"/slots/home", map[string]any{"sealed": []byte("state")}, 404)
	e1Next := makeEnrollment(t, id, c1, []string{"agents.send"}, []string{"agents.list"}, 2)
	assertFabricErr(t, fabricCall(t, r, full, "PUT", "/v1/clients/"+c1.dev.ID()+"/enroll", e1Next, 409), "exists")

	// Both enrollments remain in GET /v1/clients sorted by device ID.
	raw := fabricCall(t, r, full, "GET", "/v1/clients", nil, 200)
	var list struct {
		Clients []struct {
			Device string                 `json:"device"`
			Enroll clientproto.Enrollment `json:"enroll"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{c1.dev.ID(), c2.dev.ID()}
	slices.Sort(wantOrder)
	if len(list.Clients) != 2 || list.Clients[0].Device != wantOrder[0] || list.Clients[1].Device != wantOrder[1] {
		t.Fatalf("enrollment list after revoke: %+v", list.Clients)
	}
}

// testSlotVersionsAndWait verifies slot versions, since filtering, timeout, wake on PUT, cap and wake on revoke (contract R1, V4).
func testSlotVersionsAndWait(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	fabricCall(t, r, full, "PUT", path+"/slots/b", map[string]any{"sealed": []byte("one")}, 204)
	var slot struct {
		Sealed  []byte `json:"sealed"`
		Writer  string `json:"writer"`
		At      int64  `json:"at"`
		Version uint64 `json:"version"`
	}
	if err := json.Unmarshal(fabricCall(t, r, client, "GET", path+"/slots/b", nil, 200), &slot); err != nil {
		t.Fatal(err)
	}
	if slot.Version != 1 {
		t.Fatalf("first slot version = %d, want 1", slot.Version)
	}
	fabricCall(t, r, full, "PUT", path+"/slots/a", map[string]any{"sealed": []byte("two")}, 204)
	if err := json.Unmarshal(fabricCall(t, r, client, "GET", path+"/slots/a", nil, 200), &slot); err != nil || slot.Version != 2 {
		t.Fatalf("second slot version = %d (%v), want 2", slot.Version, err)
	}

	// since=0 lists both sorted; since=1 lists only "a"; since=2 with wait=0 or short wait answers 204.
	var changes struct {
		Version uint64   `json:"version"`
		Slots   []string `json:"slots"`
	}
	if err := json.Unmarshal(fabricCall(t, r, client, "GET", path+"/slots?since=0&wait=0", nil, 200), &changes); err != nil {
		t.Fatal(err)
	}
	if changes.Version != 2 || !slices.Equal(changes.Slots, []string{"a", "b"}) {
		t.Fatalf("since=0 changes: %+v", changes)
	}
	if err := json.Unmarshal(fabricCall(t, r, client, "GET", path+"/slots?since=1&wait=0", nil, 200), &changes); err != nil {
		t.Fatal(err)
	}
	if changes.Version != 2 || !slices.Equal(changes.Slots, []string{"a"}) {
		t.Fatalf("since=1 changes: %+v", changes)
	}
	fabricCall(t, r, client, "GET", path+"/slots?since=2&wait=0", nil, 204)
	fabricCall(t, r, client, "GET", path+"/slots?since=2&wait=0.02", nil, 204)

	// Invalid wait > 25 or body on GET is 400 bad_request.
	assertFabricErr(t, fabricCall(t, r, client, "GET", path+"/slots?since=2&wait=26", nil, 400), "bad_request")
	assertFabricErr(t, fabricCall(t, r, client, "GET", path+"/slots?since=2&wait=0", map[string]any{}, 400), "bad_request")

	// Park two waiters, verify third gets 429 full, and wake both with a PUT.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan slotPollResult, 3)
	for range 3 {
		go func() { done <- pollSlots(ctx, r, client, path+"/slots?since=2&wait=25") }()
	}
	select {
	case res := <-done:
		if res.status != 429 {
			t.Fatalf("third waiter status = %d (%s), want 429", res.status, res.raw)
		}
		assertFabricErr(t, res.raw, "full")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for third waiter refusal")
	}
	fabricCall(t, r, full, "PUT", path+"/slots/b", map[string]any{"sealed": []byte("three")}, 204)
	for range 2 {
		select {
		case res := <-done:
			if res.err != nil || res.status != 200 {
				t.Fatalf("woken waiter: %+v", res)
			}
			if err := json.Unmarshal(res.raw, &changes); err != nil || changes.Version != 3 || !slices.Equal(changes.Slots, []string{"b"}) {
				t.Fatalf("woken changes: %s %v", res.raw, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for PUT wake")
		}
	}

	// Park a waiter and revoke the client: the parked wait must end with 401 revoked.
	revokedWait := make(chan slotPollResult, 1)
	readyWait := make(chan slotPollResult, 2)
	for range 2 {
		go func() { revokedWait <- pollSlots(ctx, r, client, path+"/slots?since=3&wait=25") }()
	}
	go func() { readyWait <- pollSlots(ctx, r, client, path+"/slots?since=3&wait=25") }()
	var got429 bool
	for !got429 {
		select {
		case res := <-readyWait:
			if res.status == 429 {
				got429 = true
			} else {
				revokedWait <- res
			}
		case res := <-revokedWait:
			if res.status == 429 {
				got429 = true
			} else {
				t.Fatalf("unexpected early wake before revoke: %+v", res)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out parking waiters before revoke")
		}
	}
	fabricCall(t, r, full, "POST", "/v1/dir/devices/"+client.dev.ID()+"/revoke", nil, 204)
	for range 2 {
		select {
		case res := <-revokedWait:
			if res.err != nil || res.status != 401 {
				t.Fatalf("revoked waiter: %+v", res)
			}
			assertFabricErr(t, res.raw, "revoked")
		case res := <-readyWait:
			if res.err != nil || res.status != 401 {
				t.Fatalf("revoked waiter: %+v", res)
			}
			assertFabricErr(t, res.raw, "revoked")
		case <-time.After(3 * time.Second):
			t.Fatal("revoke did not wake parked waiter with 401")
		}
	}
}

// testClaimToken verifies optional 64-hex claim tokens, privacy in listings, re-claim rules and bodyless ack (contract R1).
func testClaimToken(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	other := newDevice(t, full.id)
	queue := path + "/queue"
	fabricCall(t, r, client, "POST", queue, map[string]any{"number": 1, "at": r.Now().UnixMilli(), "method": "chat.send", "target": "any", "sealed": []byte("one")}, 204)
	fabricCall(t, r, client, "POST", queue, map[string]any{"number": 2, "at": r.Now().UnixMilli(), "method": "chat.send", "target": "any", "sealed": []byte("two")}, 204)

	// Malformed tokens are refused with 400 bad_request.
	for _, bad := range []any{
		map[string]any{"token": "short"},
		map[string]any{"token": strings.Repeat("A", 64)},
		map[string]any{"token": strings.Repeat("g", 64)},
		map[string]any{"token": strings.Repeat("a", 64), "extra": 1},
		map[string]any{},
	} {
		assertFabricErr(t, fabricCall(t, r, full, "POST", queue+"/1/claim", bad, 400), "bad_request")
	}

	tokA := strings.Repeat("a1", 32)
	tokB := strings.Repeat("b2", 32)
	fabricCall(t, r, full, "POST", queue+"/1/claim", map[string]string{"token": tokA}, 204)
	// Same caller and same token can re-claim (204).
	fabricCall(t, r, full, "POST", queue+"/1/claim", map[string]string{"token": tokA}, 204)
	// Same caller with a different token or without a token is refused with 409 claimed.
	assertFabricErr(t, fabricCall(t, r, full, "POST", queue+"/1/claim", map[string]string{"token": tokB}, 409), "claimed")
	assertFabricErr(t, fabricCall(t, r, full, "POST", queue+"/1/claim", nil, 409), "claimed")
	// Another caller with the same token is refused with 409 claimed.
	assertFabricErr(t, fabricCall(t, r, other, "POST", queue+"/1/claim", map[string]string{"token": tokA}, 409), "claimed")

	// The claimer still lists its entry, and the listing never discloses the token. The contract does not promise a
	// claimed_by field, so one that a relay does show must name the device and nothing more.
	raw := fabricCall(t, r, full, "GET", queue, nil, 200)
	if bytes.Contains(raw, []byte(tokA)) {
		t.Fatalf("listing leaked claim token: %s", raw)
	}
	var listed struct {
		Entries []struct {
			Number    int64  `json:"number"`
			ClaimedBy string `json:"claimed_by"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &listed); err != nil || len(listed.Entries) != 2 || listed.Entries[0].Number != 1 {
		t.Fatalf("listed entries: %s %v", raw, err)
	}
	if by := listed.Entries[0].ClaimedBy; by != "" && by != full.dev.ID() {
		t.Fatalf("listing shows claimed_by %q, want the device or nothing: %s", by, raw)
	}

	// DELETE (ack) refuses a body with 400 bad_request and succeeds with no body.
	assertFabricErr(t, fabricCall(t, r, full, "DELETE", queue+"/1", map[string]string{"token": tokA}, 400), "bad_request")
	fabricCall(t, r, full, "DELETE", queue+"/1", nil, 204)

	// Tokenless claim on an unclaimed entry succeeds, and a second tokenless claim is 409 claimed.
	fabricCall(t, r, full, "POST", queue+"/2/claim", nil, 204)
	assertFabricErr(t, fabricCall(t, r, full, "POST", queue+"/2/claim", nil, 409), "claimed")
	fabricCall(t, r, full, "DELETE", queue+"/2", nil, 204)
}

// testSlotCapacity128 verifies the 128-slot per-client limit (contract R1).
func testSlotCapacity128(t *testing.T, r *Rig) {
	full, _, path := pairClient(t, r)
	for n := 0; n < clientproto.MaxClientSlots; n++ {
		fabricCall(t, r, full, "PUT", path+"/slots/s"+strconv.Itoa(n), map[string]any{"sealed": []byte("v")}, 204)
	}
	assertFabricErr(t, fabricCall(t, r, full, "PUT", path+"/slots/s128", map[string]any{"sealed": []byte("v")}, 429), "full")
	fabricCall(t, r, full, "PUT", path+"/slots/s0", map[string]any{"sealed": []byte("updated")}, 204)
}

func newClientLinkReq(t *testing.T, client device, bindB64 string) map[string]any {
	t.Helper()
	pub, err := hex.DecodeString(client.dev.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	box := bytes.Repeat([]byte{7}, 32)
	if bindB64 == "" {
		mac := clientproto.BindMAC(bytes.Repeat([]byte{5}, 16), pub, box)
		bindB64 = base64.RawURLEncoding.EncodeToString(clientproto.Bind(mac))
	}
	return map[string]any{
		"pubkey":      base64.RawURLEncoding.EncodeToString(pub),
		"x25519":      base64.RawURLEncoding.EncodeToString(box),
		"name_sealed": base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{9}, 48)),
		"platform":    "ios",
		"class":       clientproto.ClientLinkClass,
		"bind":        bindB64,
	}
}

// testLinkClientClass verifies client-class link creation, 10-char code, opaque bind storage and validation (contract R1, P7, P9).
func testLinkClientClass(t *testing.T, r *Rig) {
	id, _ := newIdentity(t)
	client := newDevice(t, id)
	reqBody := newClientLinkReq(t, client, "")
	wantBind := reqBody["bind"].(string)
	raw := openCall(t, r, "POST", "/v1/link/requests", reqBody, 201)
	var opened struct {
		Code   string `json:"code"`
		Device string `json:"device"`
		Class  string `json:"class"`
		Bind   string `json:"bind"`
	}
	if err := json.Unmarshal(raw, &opened); err != nil {
		t.Fatal(err)
	}
	if len(opened.Code) != clientproto.ClientLinkCodeLen || opened.Class != clientproto.ClientLinkClass || opened.Device != client.dev.ID() {
		t.Fatalf("opened client link: %+v (%s)", opened, raw)
	}

	// Case-insensitive GET returns class and bind verbatim.
	raw = openCall(t, r, "GET", "/v1/link/requests/"+strings.ToUpper(opened.Code), nil, 200)
	var got struct {
		Code   string `json:"code"`
		Device string `json:"device"`
		Class  string `json:"class"`
		Bind   string `json:"bind"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Code != opened.Code || got.Class != clientproto.ClientLinkClass || got.Bind != wantBind || got.State != "pending" {
		t.Fatalf("read client link: %+v", got)
	}

	// A relay-substituted or arbitrary 16-byte bind is stored and returned verbatim as opaque bytes (contract P9).
	stubBind := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, clientproto.ClientLinkBindLen))
	raw = openCall(t, r, "POST", "/v1/link/requests", newClientLinkReq(t, client, stubBind), 201)
	if err := json.Unmarshal(raw, &opened); err != nil {
		t.Fatal(err)
	}
	raw = openCall(t, r, "GET", "/v1/link/requests/"+opened.Code, nil, 200)
	if err := json.Unmarshal(raw, &got); err != nil || got.Bind != stubBind {
		t.Fatalf("opaque bind round-trip: got %q, want %q", got.Bind, stubBind)
	}

	// Bad class/bind combinations are refused with 400 bad_request.
	for _, tc := range []struct {
		name  string
		class any
		bind  any
	}{
		{"client without bind", "client", nil},
		{"client with empty bind", "client", ""},
		{"client with short bind", "client", base64.RawURLEncoding.EncodeToString(make([]byte, 15))},
		{"client with long bind", "client", base64.RawURLEncoding.EncodeToString(make([]byte, 17))},
		{"client with padded base64 bind", "client", base64.URLEncoding.EncodeToString(make([]byte, 16))},
		{"client with non-base64url bind", "client", "not!valid!base64url!!"},
		{"bind without class", nil, wantBind},
		{"unknown class without bind", "computer", nil},
		{"unknown class with bind", "phone", wantBind},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newClientLinkReq(t, client, "")
			if tc.class == nil {
				delete(b, "class")
			} else {
				b["class"] = tc.class
			}
			if tc.bind == nil {
				delete(b, "bind")
			} else {
				b["bind"] = tc.bind
			}
			assertFabricErr(t, openCall(t, r, "POST", "/v1/link/requests", b, 400), "bad_request")
		})
	}
}

// testLinkDecideClient verifies the R3 client-class approval matrix and full/client separation (contract R1, R3, P6, P7, P10).
func testLinkDecideClient(t *testing.T, r *Rig) {
	id, full := newIdentity(t)
	fabricCall(t, r, full, "PUT", "/v1/dir/devices/"+full.dev.ID(), map[string]any{"V": 1}, 204)

	openClientCode := func(c device) string {
		var opened struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(openCall(t, r, "POST", "/v1/link/requests", newClientLinkReq(t, c, ""), 201), &opened); err != nil {
			t.Fatal(err)
		}
		return opened.Code
	}
	assertPending := func(code string) {
		t.Helper()
		var req struct {
			State string  `json:"state"`
			Grant *string `json:"grant"`
		}
		if err := json.Unmarshal(openCall(t, r, "GET", "/v1/link/requests/"+code, nil, 200), &req); err != nil {
			t.Fatal(err)
		}
		if req.State != "pending" || req.Grant != nil {
			t.Fatalf("request %s state=%q grant=%v, want pending", code, req.State, req.Grant)
		}
	}

	phone1 := newDevice(t, id)
	code1 := openClientCode(phone1)
	grant := base64.RawURLEncoding.EncodeToString([]byte("sealed-client-grant-v2"))
	clientApprove := map[string]any{"class": "client", "grant": grant}

	// 1. Neither registered nor enrolled -> 409 not_enrolled, stays pending.
	assertFabricErr(t, fabricCall(t, r, full, "POST", "/v1/dir/requests/"+code1+"/approve", clientApprove, 409), "not_enrolled")
	assertPending(code1)

	// 2. Enrolled but not registered -> 409 not_enrolled, stays pending.
	enroll1 := makeEnrollment(t, id, phone1, []string{"agents.send", "agents.create"}, []string{"agents.list"}, 1)
	fabricCall(t, r, full, "PUT", "/v1/clients/"+phone1.dev.ID()+"/enroll", enroll1, 204)
	assertFabricErr(t, fabricCall(t, r, full, "POST", "/v1/dir/requests/"+code1+"/approve", clientApprove, 409), "not_enrolled")
	assertPending(code1)

	// 3. Registered but no enrollment (on phone2) -> 409 not_enrolled, stays pending.
	phone2 := newDevice(t, id)
	code2 := openClientCode(phone2)
	fabricCall(t, r, full, "PUT", "/v1/clients/"+phone2.dev.ID(), map[string]any{"cert": phone2.Cert(), "methods": []string{"agents.send"}}, 204)
	assertFabricErr(t, fabricCall(t, r, full, "POST", "/v1/dir/requests/"+code2+"/approve", clientApprove, 409), "not_enrolled")
	assertPending(code2)

	// 4. Full-device body for a client-class request -> 400 bad_request, stays pending (phone never gets an identity).
	certJSON, _ := json.Marshal(phone1.Cert())
	fullApprove := map[string]any{
		"device": map[string]any{"V": 1},
		"cert":   base64.RawURLEncoding.EncodeToString(certJSON),
		"grant":  grant,
	}
	assertFabricErr(t, fabricCall(t, r, full, "POST", "/v1/dir/requests/"+code1+"/approve", fullApprove, 400), "bad_request")
	assertPending(code1)

	// 5. Client body with extra keys, oversized grant or non-base64url grant -> 400 bad_request, stays pending.
	for _, badBody := range []any{
		map[string]any{"class": "client", "grant": grant, "cert": base64.RawURLEncoding.EncodeToString(certJSON)},
		map[string]any{"class": "client", "grant": grant, "device": map[string]any{"V": 1}},
		map[string]any{"class": "client", "grant": grant, "extra": true},
		map[string]any{"class": "client", "grant": base64.RawURLEncoding.EncodeToString(make([]byte, 4097))},
		map[string]any{"class": "client", "grant": "not!base64url!"},
		map[string]any{"class": "client", "grant": ""},
	} {
		assertFabricErr(t, fabricCall(t, r, full, "POST", "/v1/dir/requests/"+code1+"/approve", badBody, 400), "bad_request")
		assertPending(code1)
	}

	// 6. Client body for a normal computer request -> 400 bad_request, stays pending.
	comp := newDevice(t, id)
	compReq := newClientLinkReq(t, comp, "")
	delete(compReq, "class")
	delete(compReq, "bind")
	var compOpened struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(openCall(t, r, "POST", "/v1/link/requests", compReq, 201), &compOpened); err != nil {
		t.Fatal(err)
	}
	if len(compOpened.Code) != 8 {
		t.Fatalf("computer code len = %d, want 8", len(compOpened.Code))
	}
	assertFabricErr(t, fabricCall(t, r, full, "POST", "/v1/dir/requests/"+compOpened.Code+"/approve", clientApprove, 400), "bad_request")
	assertPending(compOpened.Code)

	// 7. Register phone1 (now both enrolled and registered) -> approve succeeds (204) and writes no directory row.
	fabricCall(t, r, full, "PUT", "/v1/clients/"+phone1.dev.ID(), map[string]any{"cert": phone1.Cert(), "methods": enroll1.Methods}, 204)
	beforeList := fabricCall(t, r, full, "GET", "/v1/dir/list", nil, 200)
	fabricCall(t, r, full, "POST", "/v1/dir/requests/"+code1+"/approve", clientApprove, 204)
	// Repeat of identical decision is 204.
	fabricCall(t, r, full, "POST", "/v1/dir/requests/"+code1+"/approve", clientApprove, 204)
	// Different grant or deny after approval is 409 already_decided.
	otherGrant := base64.RawURLEncoding.EncodeToString([]byte("different-grant"))
	assertFabricErr(t, fabricCall(t, r, full, "POST", "/v1/dir/requests/"+code1+"/approve", map[string]any{"class": "client", "grant": otherGrant}, 409), "already_decided")
	assertFabricErr(t, fabricCall(t, r, full, "POST", "/v1/dir/requests/"+code1+"/deny", nil, 409), "already_decided")

	// Phone's GET shows state approved with the exact grant.
	var decided struct {
		State string  `json:"state"`
		Grant *string `json:"grant"`
		Class string  `json:"class"`
	}
	if err := json.Unmarshal(openCall(t, r, "GET", "/v1/link/requests/"+code1, nil, 200), &decided); err != nil {
		t.Fatal(err)
	}
	if decided.State != "approved" || decided.Grant == nil || *decided.Grant != grant || decided.Class != "client" {
		t.Fatalf("decided client request: %+v", decided)
	}

	// Directory listing has the exact same devices before and after, and phone1 is still kind:client with its methods.
	afterList := fabricCall(t, r, full, "GET", "/v1/dir/list", nil, 200)
	var listBefore, listAfter struct {
		Devices map[string]struct {
			Kind    string   `json:"kind"`
			Methods []string `json:"methods"`
			Revoked bool     `json:"revoked"`
		} `json:"devices"`
	}
	if json.Unmarshal(beforeList, &listBefore) != nil || json.Unmarshal(afterList, &listAfter) != nil {
		t.Fatal("unmarshal listings")
	}
	if len(listBefore.Devices) != len(listAfter.Devices) {
		t.Fatalf("device count changed on client approve: %d -> %d", len(listBefore.Devices), len(listAfter.Devices))
	}
	p1Rec := listAfter.Devices[phone1.dev.ID()]
	if p1Rec.Kind != "client" || !slices.Equal(p1Rec.Methods, enroll1.Methods) || p1Rec.Revoked {
		t.Fatalf("phone1 record after approve: %+v", p1Rec)
	}
}

type watchSocket struct {
	conn     *websocket.Conn
	watching chan watchingRawFrame
	pongs    chan struct{}
}

type watchingRawFrame struct {
	frame watchingFrame
	raw   []byte
}

func dialWatch(t *testing.T, ctx context.Context, r *Rig, d device, query string) *watchSocket {
	t.Helper()
	target := r.URL() + "/v1/dir/watch"
	if query != "" {
		target += "?" + query
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req, nil, d, r.Now())
	conn, resp, err := websocket.Dial(ctx, req.URL.String(), &websocket.DialOptions{HTTPHeader: req.Header})
	if err != nil {
		var status string
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("watch dial %q: %v (%s)", query, err, status)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, _, err := conn.Read(readCtx); err != nil {
		t.Fatalf("watch initial version frame (%q): %v", query, err)
	}
	ws := &watchSocket{
		conn:     conn,
		watching: make(chan watchingRawFrame, 32),
		pongs:    make(chan struct{}, 8),
	}
	go func() {
		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if string(raw) == "pong" {
				ws.pongs <- struct{}{}
				continue
			}
			var f watchingFrame
			if json.Unmarshal(raw, &f) == nil && f.T == clientproto.WatchingEventType {
				ws.watching <- watchingRawFrame{frame: f, raw: raw}
			}
		}
	}()
	return ws
}

func (ws *watchSocket) ping(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := ws.conn.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ws.pongs:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watch pong")
	}
}

type watchingFrame struct {
	T      string `json:"t"`
	Device string `json:"device"`
	On     *bool  `json:"on"`
	At     int64  `json:"at"`
}

func readWatchingFrame(t *testing.T, ws *watchSocket, wantDevice string, timeout time.Duration) (watchingFrame, []byte, bool) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case item := <-ws.watching:
			if wantDevice == "" || item.frame.Device == wantDevice {
				return item.frame, item.raw, true
			}
		case <-timer.C:
			return watchingFrame{}, nil, false
		}
	}
}

// testComputersServing verifies GET /v1/clients/{device}/computers and serving=1 liveness (contract R1, L8).
func testComputersServing(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	second := newDevice(t, full.id)
	fabricCall(t, r, full, "PUT", "/v1/dir/devices/"+full.dev.ID(), map[string]any{"V": 1}, 204)
	fabricCall(t, r, second, "PUT", "/v1/dir/devices/"+second.dev.ID(), map[string]any{"V": 1}, 204)

	// Full devices and other clients are refused with 401.
	fabricCall(t, r, full, "GET", path+"/computers", nil, 401)
	otherClient := newDevice(t, full.id)
	fabricCall(t, r, full, "PUT", "/v1/clients/"+otherClient.dev.ID(), map[string]any{"cert": otherClient.Cert(), "methods": []string{}}, 204)
	fabricCall(t, r, otherClient, "GET", path+"/computers", nil, 401)

	type compRow struct {
		Device  string `json:"device"`
		Serving bool   `json:"serving"`
		At      int64  `json:"at"`
	}
	readComputers := func() (int64, map[string]compRow) {
		t.Helper()
		raw := fabricCall(t, r, client, "GET", path+"/computers", nil, 200)
		var out struct {
			Now       int64     `json:"now"`
			Computers []compRow `json:"computers"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		for i := 1; i < len(out.Computers); i++ {
			if out.Computers[i-1].Device >= out.Computers[i].Device {
				t.Fatalf("computers not sorted: %+v", out.Computers)
			}
		}
		byID := map[string]compRow{}
		for _, c := range out.Computers {
			byID[c.Device] = c
		}
		return out.Now, byID
	}

	_, byID := readComputers()
	if len(byID) != 2 || byID[full.dev.ID()].Serving || byID[second.dev.ID()].Serving {
		t.Fatalf("initial computers: %+v", byID)
	}

	// Opening a watch socket with serving=1 marks that computer serving with at == now.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := dialWatch(t, ctx, r, full, "beat=10&serving=1")
	now, byID := readComputers()
	if !byID[full.dev.ID()].Serving || byID[full.dev.ID()].At != now || byID[second.dev.ID()].Serving {
		t.Fatalf("computers while full is serving (now=%d): %+v", now, byID)
	}

	// Closing the serving socket clears serving.
	_ = conn.conn.Close(websocket.StatusNormalClosure, "")
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, byID = readComputers()
		if !byID[full.dev.ID()].Serving {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if byID[full.dev.ID()].Serving {
		t.Fatalf("full stayed serving after socket close: %+v", byID)
	}

	// On a fake clock, missing beats also clears serving and Revoke removes the computer from the list.
	if r.Advance != nil {
		ctx2, cancel2 := context.WithCancel(context.Background())
		defer cancel2()
		_ = dialWatch(t, ctx2, r, second, "beat=10&serving=1")
		_, byID = readComputers()
		if !byID[second.dev.ID()].Serving {
			t.Fatalf("second not serving after dial: %+v", byID)
		}
		r.Advance(30 * time.Second)
		_, byID = readComputers()
		if byID[second.dev.ID()].Serving {
			t.Fatalf("second still serving after missed beats: %+v", byID)
		}
	}
	fabricCall(t, r, full, "POST", "/v1/dir/devices/"+second.dev.ID()+"/revoke", nil, 204)
	_, byID = readComputers()
	if len(byID) != 1 || byID[full.dev.ID()].Device != full.dev.ID() {
		t.Fatalf("computers after revoking second: %+v", byID)
	}
}

// testServingFlagIsExact verifies that only the exact query parameter serving=1 marks a computer serving (contract R1, L8).
func testServingFlagIsExact(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	fabricCall(t, r, full, "PUT", "/v1/dir/devices/"+full.dev.ID(), map[string]any{"V": 1}, 204)

	isServing := func() bool {
		t.Helper()
		var out struct {
			Computers []struct {
				Device  string `json:"device"`
				Serving bool   `json:"serving"`
			} `json:"computers"`
		}
		if err := json.Unmarshal(fabricCall(t, r, client, "GET", path+"/computers", nil, 200), &out); err != nil {
			t.Fatal(err)
		}
		for _, c := range out.Computers {
			if c.Device == full.dev.ID() {
				return c.Serving
			}
		}
		t.Fatal("full computer not listed")
		return false
	}

	for _, query := range []string{"", "serving=0", "serving=true", "serving=yes", "serving=10"} {
		ctx, cancel := context.WithCancel(context.Background())
		conn := dialWatch(t, ctx, r, full, query)
		if isServing() {
			t.Fatalf("query %q unexpectedly marked computer serving", query)
		}
		_ = conn.conn.CloseNow()
		cancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = dialWatch(t, ctx, r, full, "serving=1")
	if !isServing() {
		t.Fatal("query serving=1 did not mark computer serving")
	}
}

// testWatchingHeader verifies the Codeaf-Client-Watching: 1 header on slot PUT replies across the 30 s window (contract R1, L4).
func testWatchingHeader(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	slot := path + "/slots/home"

	// Absent before any client read or wait.
	_, hdr := fabricCallHeader(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("one")}, 204)
	if got := hdr.Get(clientproto.WatchingHeader); got != "" {
		t.Fatalf("header before read = %q, want empty", got)
	}

	// Present immediately after a slot read.
	fabricCall(t, r, client, "GET", slot, nil, 200)
	_, hdr = fabricCallHeader(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("two")}, 204)
	if got := hdr.Get(clientproto.WatchingHeader); got != "1" {
		t.Fatalf("header after slot read = %q, want \"1\"", got)
	}

	if r.Advance == nil {
		return
	}
	// Still present at 29 s, absent once 30 s have elapsed.
	r.Advance(29 * time.Second)
	_, hdr = fabricCallHeader(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("three")}, 204)
	if got := hdr.Get(clientproto.WatchingHeader); got != "1" {
		t.Fatalf("header at 29s = %q, want \"1\"", got)
	}
	r.Advance(time.Second)
	_, hdr = fabricCallHeader(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("four")}, 204)
	if got := hdr.Get(clientproto.WatchingHeader); got != "" {
		t.Fatalf("header at 30s = %q, want empty", got)
	}

	// A slot wait poll also activates the 30 s header window.
	fabricCall(t, r, client, "GET", path+"/slots?since=99&wait=0", nil, 204)
	_, hdr = fabricCallHeader(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("five")}, 204)
	if got := hdr.Get(clientproto.WatchingHeader); got != "1" {
		t.Fatalf("header after slot wait = %q, want \"1\"", got)
	}
	r.Advance(clientproto.WatchingHeaderFor)
	_, hdr = fabricCallHeader(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("six")}, 204)
	if got := hdr.Get(clientproto.WatchingHeader); got != "" {
		t.Fatalf("header 30s after slot wait = %q, want empty", got)
	}
}

// testWatchingEvent verifies watching on/off frames on event sockets, replay on connect, 2 s dampening and 45 s expiry (contract R1, L4, L7).
func testWatchingEvent(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	second := newDevice(t, full.id)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn1 := dialWatch(t, ctx, r, full, "events=1&beat=10")

	// Slot read triggers an immediate watching on:true event frame.
	fabricCall(t, r, client, "GET", path+"/slots?since=0&wait=0", nil, 204)
	f, raw, ok := readWatchingFrame(t, conn1, client.dev.ID(), 3*time.Second)
	if !ok || f.On == nil || !*f.On || len(raw) > 512 {
		t.Fatalf("initial watching event: ok=%v frame=%+v raw=%s", ok, f, raw)
	}
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil || len(keys) != 4 {
		t.Fatalf("watching frame keys: %s", raw)
	}

	// A newly connected event socket receives the current watching state replayed at connect.
	ctx2, cancel2 := context.WithCancel(ctx)
	conn2 := dialWatch(t, ctx2, r, second, "events=1&beat=10")
	replayed, _, ok := readWatchingFrame(t, conn2, client.dev.ID(), 3*time.Second)
	if !ok || replayed.On == nil || !*replayed.On {
		t.Fatalf("replayed watching event on connect: ok=%v frame=%+v", ok, replayed)
	}
	_ = conn2.conn.CloseNow()
	cancel2()

	if r.Advance == nil {
		return
	}

	// After 45 s without client activity (while conn1 keeps its 10 s beat alive), watching turns off (on:false).
	for range 2 {
		r.Advance(20 * time.Second)
		conn1.ping(t, ctx)
	}
	r.Advance(5 * time.Second)
	offFrame, _, ok := readWatchingFrame(t, conn1, client.dev.ID(), 3*time.Second)
	if !ok || offFrame.On == nil || *offFrame.On {
		t.Fatalf("watching off after 45s: ok=%v frame=%+v", ok, offFrame)
	}

	// A flip back to watching inside the 2 s dampen window is delayed until 2 s have passed since the previous frame.
	r.Advance(500 * time.Millisecond)
	fabricCall(t, r, client, "GET", path+"/slots?since=0&wait=0", nil, 204)
	r.Advance(time.Second) // 1.5 s since offFrame: still inside the 2 s dampen window
	if early, _, gotEarly := readWatchingFrame(t, conn1, client.dev.ID(), 40*time.Millisecond); gotEarly {
		t.Fatalf("unexpected undampened watching frame at 1.5s: %+v", early)
	}
	r.Advance(500 * time.Millisecond) // 2.0 s since offFrame: dampen window ends
	delayed, _, ok := readWatchingFrame(t, conn1, client.dev.ID(), 3*time.Second)
	if !ok || delayed.On == nil || !*delayed.On {
		t.Fatalf("delayed watching frame after 2s dampen: ok=%v frame=%+v", ok, delayed)
	}

	// Revoking the client 500 ms later (inside the new 2 s dampen window) holds the on:false flip until 2 s pass.
	r.Advance(500 * time.Millisecond)
	fabricCall(t, r, full, "POST", "/v1/dir/devices/"+client.dev.ID()+"/revoke", nil, 204)
	r.Advance(time.Second)
	if early, _, gotEarly := readWatchingFrame(t, conn1, client.dev.ID(), 40*time.Millisecond); gotEarly {
		t.Fatalf("unexpected undampened watching off frame at 1.5s: %+v", early)
	}
	r.Advance(500 * time.Millisecond)
	revokedOff, _, ok := readWatchingFrame(t, conn1, client.dev.ID(), 3*time.Second)
	if !ok || revokedOff.On == nil || *revokedOff.On {
		t.Fatalf("delayed watching off frame after revoke: ok=%v frame=%+v", ok, revokedOff)
	}
}

func testFabricAuthorizationAndBodies(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	other := newDevice(t, full.id)
	otherPath := "/v1/clients/" + other.dev.ID()
	badCert := identity.IssueCert(full.id, ed25519.PublicKey{1}, r.Now())
	fabricCall(t, r, full, "PUT", "/v1/clients/"+badCert.DeviceID(), map[string]any{"cert": badCert, "methods": []string{}}, 400)
	fabricCall(t, r, client, "PUT", otherPath, map[string]any{"cert": other.Cert(), "methods": []string{}}, 401)
	fabricCall(t, r, full, "PUT", otherPath, map[string]any{"cert": client.Cert(), "methods": []string{}}, 400)
	_, foreign := newIdentity(t)
	fabricCall(t, r, full, "PUT", "/v1/clients/"+foreign.dev.ID(), map[string]any{"cert": foreign.Cert(), "methods": []string{}}, 400)
	fabricCall(t, r, full, "PUT", otherPath, map[string]any{"cert": other.Cert(), "methods": []string{}, "grant": "forbidden"}, 400)
	fabricCall(t, r, full, "PUT", otherPath, map[string]any{"cert": other.Cert(), "methods": []string{"chat.send", "chat.send"}}, 400)
	fabricCall(t, r, full, "PUT", otherPath, map[string]any{"cert": other.Cert(), "methods": []string{}}, 204)
	fabricCall(t, r, other, "POST", otherPath+"/queue", map[string]any{"number": 1, "at": r.Now().UnixMilli(), "method": "chat.send", "target": "any", "sealed": []byte("sealed")}, 403)
	fabricCall(t, r, client, "GET", path+"/queue", nil, 401)
	fabricCall(t, r, full, "POST", path+"/queue", map[string]any{"number": 1}, 401)
	fabricCall(t, r, full, "PUT", path+"/slots/max", map[string]any{"sealed": make([]byte, 65536)}, 204)
	fabricCall(t, r, client, "GET", path+"/slots/max", nil, 200)
	fabricCall(t, r, full, "PUT", path+"/slots/empty", map[string]any{"sealed": []byte{}}, 400)
	fabricCall(t, r, client, "GET", path+"/slots/max", map[string]any{}, 400)
	fabricCall(t, r, full, "PUT", path+"/slots/huge", map[string]any{"sealed": make([]byte, 100000)}, 413)
}

func testFabricRevocation(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	fabricCall(t, r, full, "PUT", path+"/slots/home", map[string]any{"sealed": []byte("snapshot")}, 204)
	fabricCall(t, r, client, "POST", path+"/queue", map[string]any{"number": 1, "at": r.Now().UnixMilli(), "method": "chat.send", "target": "any", "sealed": []byte("call")}, 204)
	fabricCall(t, r, full, "POST", path+"/queue/1/claim", nil, 204)
	fabricCall(t, r, full, "POST", "/v1/dir/devices/"+client.dev.ID()+"/revoke", nil, 204)
	check := func() {
		fabricCall(t, r, client, "GET", path+"/slots/home", nil, 401)
		fabricCall(t, r, client, "POST", path+"/queue", map[string]any{"number": 2}, 401)
		fabricCall(t, r, full, "PUT", path+"/slots/home", map[string]any{"sealed": []byte("snapshot")}, 404)
		fabricCall(t, r, full, "GET", path+"/queue", nil, 404)
		fabricCall(t, r, full, "POST", path+"/queue/1/claim", nil, 404)
		fabricCall(t, r, full, "DELETE", path+"/queue/1", nil, 404)
	}
	check()
	if r.Restart != nil {
		r.Restart()
		check()
	}
}

// relayTime brackets writes using the relay clock, including in live runs.
func relayTime(t *testing.T, r *Rig) int64 {
	t.Helper()
	resp, err := r.Client.Get(r.URL() + "/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	at, err := strconv.ParseInt(resp.Header.Get("Codeaf-Now"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return at
}
