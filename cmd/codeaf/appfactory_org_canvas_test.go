package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// canvasOrgServer answers the canvas routes the way the organization server
// does (factory/serve/canvases.go, proved there against canvas-core); here
// only the engine's half is under test: the route each method builds, the
// body it sends, and how the answer comes back.
type canvasOrgServer struct {
	mu   sync.Mutex
	asks []string
	// body and typ are the last request's body and Content-Type.
	body []byte
	typ  string
}

const testCanvas = "cv_abcdefabcdef"

var testSha = strings.Repeat("ab", 32)

func (o *canvasOrgServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/events" {
		<-r.Context().Done()
		return
	}
	if r.URL.Path == "/v1/about" || r.URL.Path == "/v1/me" {
		orgJSON(w, 200, api.Me{Contract: api.Contract, Factory: api.Factory{Name: "Acme"}, Mode: api.FactoryTeam, Profile: api.Profile{Handle: "santosh"}})
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.asks = append(o.asks, r.Method+" "+r.URL.RequestURI())
	o.body, _ = io.ReadAll(r.Body)
	o.typ = r.Header.Get("Content-Type")
	base := "/v1/canvases/" + testCanvas
	switch r.Method + " " + r.URL.Path {
	case "GET /v1/canvases":
		orgJSON(w, 200, map[string]any{"canvases": []any{map[string]any{"id": testCanvas, "title": "Board", "my_role": "edit", "outline": []any{}}}})
	case "GET " + base + "/snapshot":
		orgJSON(w, 200, map[string]any{"id": testCanvas, "seq": 4, "nodes": []any{}, "meta": map[string]string{"title": "Board"}})
	case "POST " + base + "/ops":
		var in struct {
			BaseSeq int64             `json:"base_seq"`
			Ops     []json.RawMessage `json:"ops"`
		}
		_ = json.Unmarshal(o.body, &in)
		if strings.Contains(string(o.body), "f_held00000000") {
			orgJSON(w, 409, map[string]any{"error": "abir is working in this frame", "reason": "conflict", "frame": "f_held00000000", "holder": "person:abir", "seq": 7,
				"ops": []any{map[string]any{"seq": 6, "actor": "person:abir", "op": map[string]any{"t": "lease"}}}})
			return
		}
		orgJSON(w, 200, map[string]any{"seq": 5, "ops": []any{map[string]any{"seq": 5, "actor": "person:santosh", "op": in.Ops[0]}}})
	case "PUT " + base + "/assets/" + testSha:
		orgJSON(w, 200, map[string]any{"sha": testSha, "mime": o.typ, "bytes": len(o.body), "name": r.URL.Query().Get("name")})
	case "GET " + base + "/assets/" + testSha:
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG bytes"))
	case "DELETE " + base:
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}

func attachCanvasOrg(t *testing.T) (*canvasOrgServer, *factoryWire, string) {
	t.Helper()
	o := &canvasOrgServer{}
	srv := httptest.NewServer(http.HandlerFunc(o.serve))
	t.Cleanup(srv.Close)
	wire := newFactoryWire(t, testFactoryHub(t))
	var conn appwire.FactoryConnection
	wire.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)
	o.mu.Lock()
	o.asks = nil
	o.mu.Unlock()
	return o, wire, conn.ID
}

// THE CANVAS METHODS ARE ONE REQUEST EACH, announced as org_canvases; the
// server's canvas-core shapes come back as they were sent, an asset's bytes
// travel raw to the server and base64 to the desk, and a write into another
// person's frame is conflict whose detail names the frame, the holder and
// the ops not yet seen.
func TestOrgCanvasesForwardToTheServer(t *testing.T) {
	o, wire, id := attachCanvasOrg(t)
	if !strings.Contains(wire.hello, `"org_canvases"`) {
		t.Fatalf("hello lacks org_canvases: %s", wire.hello)
	}
	c := func(extra map[string]any) map[string]any {
		out := map[string]any{"connection": id, "id": testCanvas}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	var list appwire.OrgCanvasList
	wire.must("factory.org_canvases", map[string]any{"connection": id}, &list)
	if len(list.Canvases) != 1 || !strings.Contains(string(list.Canvases[0]), `"my_role":"edit"`) {
		t.Fatalf("list: %s", list.Canvases)
	}
	var snap map[string]any
	wire.must("factory.org_canvas_snapshot", c(nil), &snap)
	if snap["seq"] != float64(4) {
		t.Fatalf("snapshot: %v", snap)
	}
	var applied appwire.OrgCanvasOps
	wire.must("factory.org_canvas_apply", c(map[string]any{"base_seq": 4, "ops": []any{map[string]any{"t": "patch", "id": "n_aaaaaaaaaaaa", "props": map[string]any{"text": "x"}}}}), &applied)
	if applied.Seq != 5 || len(applied.Ops) != 1 {
		t.Fatalf("applied: %+v", applied)
	}

	e := wire.failWith("factory.org_canvas_apply", c(map[string]any{"base_seq": 4, "ops": []any{map[string]any{"t": "lease", "frame": "f_held00000000", "actor": "", "until": "hand"}}}))
	if e.Code != appwire.CodeConflict || e.Message != "abir is working in this frame" {
		t.Fatalf("conflict: %+v", e)
	}
	detail, _ := json.Marshal(e.Detail)
	var k appwire.OrgCanvasConflict
	_ = json.Unmarshal(detail, &k)
	if k.Reason != "conflict" || k.Frame != "f_held00000000" || k.Holder != "person:abir" || k.Seq != 7 || len(k.Ops) != 1 {
		t.Fatalf("conflict detail: %s", detail)
	}

	var stored map[string]any
	wire.must("factory.org_canvas_asset_put", c(map[string]any{"sha": testSha, "data": []byte("\x89PNG bytes"), "type": "image/png", "name": "p.png"}), &stored)
	o.mu.Lock()
	body, typ := string(o.body), o.typ
	o.mu.Unlock()
	if body != "\x89PNG bytes" || typ != "image/png" || stored["name"] != "p.png" {
		t.Fatalf("asset put sent %q as %q: %v", body, typ, stored)
	}
	var got appwire.OrgCanvasAssetData
	wire.must("factory.org_canvas_asset_get", c(map[string]any{"sha": testSha}), &got)
	if string(got.Data) != "\x89PNG bytes" || got.Type != "image/png" {
		t.Fatalf("asset get: %+v", got)
	}
	wire.must("factory.org_canvas_delete", c(nil), nil)

	o.mu.Lock()
	asks := append([]string(nil), o.asks...)
	o.mu.Unlock()
	want := []string{
		"GET /v1/canvases",
		"GET /v1/canvases/" + testCanvas + "/snapshot",
		"POST /v1/canvases/" + testCanvas + "/ops",
		"POST /v1/canvases/" + testCanvas + "/ops",
		"PUT /v1/canvases/" + testCanvas + "/assets/" + testSha + "?name=p.png",
		"GET /v1/canvases/" + testCanvas + "/assets/" + testSha,
		"DELETE /v1/canvases/" + testCanvas,
	}
	if strings.Join(asks, "\n") != strings.Join(want, "\n") {
		t.Fatalf("asks:\n%s\nwant:\n%s", strings.Join(asks, "\n"), strings.Join(want, "\n"))
	}
}

// A CANVAS ID, A SHA OR A SHARE THAT COULD NOT BE ONE IS REFUSED BEFORE
// ANYTHING IS SENT.
func TestOrgCanvasMethodsRefuseBadParamsBeforeSending(t *testing.T) {
	o, wire, id := attachCanvasOrg(t)
	for _, tc := range []struct {
		method string
		params map[string]any
	}{
		{"factory.org_canvas_get", map[string]any{"connection": id, "id": "cv_x/../ops"}},
		{"factory.org_canvas_create", map[string]any{"connection": id, "title": " "}},
		{"factory.org_canvas_create", map[string]any{"connection": id, "title": "x", "shares": []any{map[string]any{"kind": "person", "id": "a", "role": "owner"}}}},
		{"factory.org_canvas_update", map[string]any{"connection": id, "id": testCanvas}},
		{"factory.org_canvas_apply", map[string]any{"connection": id, "id": testCanvas, "ops": []any{}}},
		{"factory.org_canvas_ops", map[string]any{"connection": id, "id": testCanvas, "since": -1}},
		{"factory.org_canvas_asset_get", map[string]any{"connection": id, "id": testCanvas, "sha": "../x"}},
		{"factory.org_canvas_import", map[string]any{"connection": id, "snapshot": map[string]any{}}},
		{"factory.org_canvas_restore", map[string]any{"connection": id, "id": testCanvas, "version": "a/b"}},
	} {
		wire.mustFail(tc.method, tc.params, appwire.CodeBadParams)
	}
	if len(o.asks) != 0 {
		t.Fatalf("a refused request was sent: %v", o.asks)
	}
}

// failWith sends one request that must fail and answers the whole error,
// detail included.
func (w *factoryWire) failWith(method string, params any) appwire.Error {
	w.t.Helper()
	w.next++
	raw, _ := json.Marshal(params)
	fmt.Fprintf(w.in, `{"id":%d,"method":%q,"params":%s}`+"\n", w.next, method, raw)
	for line := range w.answers {
		var f struct {
			ID    *int           `json:"id"`
			Error *appwire.Error `json:"error"`
		}
		if json.Unmarshal(line, &f) != nil || f.ID == nil || *f.ID != w.next {
			continue
		}
		if f.Error == nil {
			w.t.Fatalf("%s succeeded: %s", method, line)
		}
		return *f.Error
	}
	w.t.Fatalf("the wire ended during %s", method)
	return appwire.Error{}
}
