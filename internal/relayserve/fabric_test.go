package relayserve_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func fabricCall(t *testing.T, r *rig, d device, method, path string, body any, want int) []byte {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, r.srv.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req, raw, d, r.clock.Now())
	resp, err := r.srv.Client().Do(req)
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

func pairClient(t *testing.T, r *rig) (device, device, string) {
	t.Helper()
	id, full := newIdentity(t)
	client := newDevice(t, id, t.TempDir())
	path := "/v1/clients/" + client.dev.ID()
	fabricCall(t, r, full, "PUT", path, map[string]any{"cert": client.Cert(), "methods": []string{"chat.read", "chat.send"}}, 204)
	return full, client, path
}

func TestFabricClientScope(t *testing.T) {
	r := newRig(t)
	full, client, path := pairClient(t, r)
	raw := fabricCall(t, r, full, "GET", "/v1/dir/list", nil, 200)
	var listing struct {
		Devices map[string]struct {
			Kind    string
			Methods []string
		}
	}
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	rec := listing.Devices[client.dev.ID()]
	if rec.Kind != "client" || len(rec.Methods) != 2 {
		t.Fatalf("client record: %+v", rec)
	}
	for _, route := range []string{"/v1/dir/list", "/v1/store/stats", "/v1/identity/rotation"} {
		fabricCall(t, r, client, "GET", route, nil, 401)
	}
	fabricCall(t, r, client, "PUT", "/v1/dir/devices/"+client.dev.ID(), map[string]any{"V": 1}, 401)
	fabricCall(t, r, full, "PUT", path, map[string]any{"cert": client.Cert(), "methods": []string{}}, 409)
	r.restart()
	fabricCall(t, r, client, "GET", "/v1/dir/list", nil, 401)
	fabricCall(t, r, full, "POST", "/v1/dir/devices/"+client.dev.ID()+"/revoke", nil, 204)
	fabricCall(t, r, client, "PUT", path, nil, 401)
}

func TestFabricSlots(t *testing.T) {
	r := newRig(t)
	full, client, path := pairClient(t, r)
	slot := path + "/slots/home"
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
	if string(got.Sealed) != "first" || got.Writer != full.dev.ID() || got.At != r.clock.Now().UnixMilli() {
		t.Fatalf("slot: %+v", got)
	}
	fabricCall(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("replacement")}, 204)
	r.restart()
	raw = fabricCall(t, r, client, "GET", slot, nil, 200)
	if err := json.Unmarshal(raw, &got); err != nil || string(got.Sealed) != "replacement" {
		t.Fatalf("after restart: %s %v", raw, err)
	}
	fabricCall(t, r, client, "PUT", slot, map[string]any{"sealed": []byte("forged")}, 401)
	fabricCall(t, r, full, "GET", slot, nil, 401)
	_, other := newIdentity(t)
	fabricCall(t, r, other, "PUT", slot, map[string]any{"sealed": []byte("foreign")}, 404)
	fabricCall(t, r, full, "PUT", slot, map[string]any{"sealed": make([]byte, 65537)}, 413)
	r.clock.Advance(24 * time.Hour)
	fabricCall(t, r, client, "GET", slot, nil, 404)
	fabricCall(t, r, full, "PUT", slot, map[string]any{"sealed": []byte("fresh")}, 204)
	fabricCall(t, r, full, "POST", "/v1/dir/devices/"+client.dev.ID()+"/revoke", nil, 204)
	fabricCall(t, r, client, "GET", slot, nil, 401)
}

func TestFabricSlotCapacityAndClientIsolation(t *testing.T) {
	r := newRig(t)
	full, client, path := pairClient(t, r)
	other := newDevice(t, full.id, t.TempDir())
	otherPath := "/v1/clients/" + other.dev.ID()
	fabricCall(t, r, full, "PUT", otherPath, map[string]any{"cert": other.Cert(), "methods": []string{}}, 204)
	for n := 0; n < 64; n++ {
		fabricCall(t, r, full, "PUT", path+"/slots/"+strconv.Itoa(n), map[string]any{"sealed": []byte("sealed")}, 204)
	}
	fabricCall(t, r, full, "PUT", path+"/slots/extra", map[string]any{"sealed": []byte("sealed")}, 429)
	fabricCall(t, r, full, "PUT", path+"/slots/0", map[string]any{"sealed": []byte("replaced")}, 204)
	fabricCall(t, r, other, "GET", path+"/slots/0", nil, 401)
	fabricCall(t, r, client, "GET", otherPath+"/slots/0", nil, 401)
	r.clock.Advance(24 * time.Hour)
	fabricCall(t, r, full, "PUT", path+"/slots/extra", map[string]any{"sealed": []byte("sealed")}, 204)
}

func TestFabricQueueClaimsAndProof(t *testing.T) {
	r := newRig(t)
	full, client, path := pairClient(t, r)
	second := newDevice(t, full.id, t.TempDir())
	queue := path + "/queue"
	body := map[string]any{"number": 1, "at": r.clock.Now().UnixMilli(), "method": "chat.send", "target": "any", "sealed": []byte("sealed call")}
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
	req, _ := http.NewRequest("POST", r.srv.URL+e.RequestURI, bytes.NewReader(e.Body))
	req.Header.Set(reqsign.HeaderIdentity, e.Identity)
	req.Header.Set(reqsign.HeaderCert, e.Cert)
	req.Header.Set(reqsign.HeaderTime, e.RequestTime)
	req.Header.Set(reqsign.HeaderSig, e.Signature)
	if _, dev, err := reqsign.Verify(req, e.Body, r.clock.Now()); err != nil || dev != client.dev.ID() {
		t.Fatalf("entry proof: %s %v", dev, err)
	}
	fabricCall(t, r, full, "DELETE", queue+"/1", nil, 409)
	fabricCall(t, r, full, "POST", queue+"/1/claim", nil, 204)
	r.restart()
	fabricCall(t, r, full, "POST", queue+"/1/claim", nil, 204)
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

func TestFabricQueueLimitsRoutingAndExpiry(t *testing.T) {
	r := newRig(t)
	full, client, path := pairClient(t, r)
	queue := path + "/queue"
	in := map[string]any{"number": 1, "at": r.clock.Now().UnixMilli(), "method": "chat.send", "target": "any", "sealed": []byte("sealed")}
	in["method"] = "shell.run"
	fabricCall(t, r, client, "POST", queue, in, 403)
	in["method"] = "chat.send"
	in["at"] = r.clock.Now().Add(-24 * time.Hour).UnixMilli()
	fabricCall(t, r, client, "POST", queue, in, 400)
	in["at"] = r.clock.Now().Add(5*time.Minute + time.Millisecond).UnixMilli()
	fabricCall(t, r, client, "POST", queue, in, 400)
	in["at"] = r.clock.Now().UnixMilli()
	in["sealed"] = make([]byte, 4097)
	fabricCall(t, r, client, "POST", queue, in, 413)
	in["sealed"] = []byte("sealed")
	in["target"] = full.dev.ID()
	fabricCall(t, r, client, "POST", queue, in, 400)
	fabricCall(t, r, full, "PUT", "/v1/dir/devices/"+full.dev.ID(), map[string]any{"V": 1}, 204)
	fabricCall(t, r, client, "POST", queue, in, 204)
	other := newDevice(t, full.id, t.TempDir())
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
	r.clock.Advance(24 * time.Hour)
	fabricCall(t, r, full, "POST", queue+"/2/claim", nil, 404)
	in["at"] = r.clock.Now().UnixMilli()
	fabricCall(t, r, client, "POST", queue, in, 204)
	r.restart()
	in["number"] = 129
	fabricCall(t, r, client, "POST", queue, in, 409)
}

func TestFabricQueueOneClaimer(t *testing.T) {
	r := newRig(t)
	full, client, path := pairClient(t, r)
	other := newDevice(t, full.id, t.TempDir())
	fabricCall(t, r, client, "POST", path+"/queue", map[string]any{"number": 1, "at": r.clock.Now().UnixMilli(), "method": "chat.send", "target": "any", "sealed": []byte("sealed")}, 204)
	statuses := make(chan int, 2)
	for _, d := range []device{full, other} {
		go func(d device) {
			req, _ := http.NewRequest("POST", r.srv.URL+path+"/queue/1/claim", nil)
			reqsign.Sign(req, nil, d, r.clock.Now())
			resp, err := r.srv.Client().Do(req)
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
