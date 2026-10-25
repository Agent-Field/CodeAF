// Package fabrictest holds HTTP conformance cases shared by in-process and live relays.
package fabrictest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"crypto/ed25519"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func fabricCall(t *testing.T, r *Rig, d device, method, path string, body any, want int) []byte {
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
	for n := 0; n < 64; n++ {
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
	t.Run("QueueClaimsAndProof", func(t *testing.T) { testFabricQueueClaimsAndProof(t, makeRig(t)) })
	t.Run("QueueLimitsRoutingAndExpiry", func(t *testing.T) { testFabricQueueLimitsRoutingAndExpiry(t, makeRig(t)) })
	t.Run("QueueOneClaimer", func(t *testing.T) { testFabricQueueOneClaimer(t, makeRig(t)) })
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
