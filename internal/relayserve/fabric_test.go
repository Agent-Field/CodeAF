package relayserve_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

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
