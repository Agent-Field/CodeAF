package serve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/canvasrpc"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// canvasOrg is the org scene with canvases kept by the real `canvas` program.
//
// The program is canvas-core's, built from the desk repository
// (crates/canvas): `cargo build --release -p canvas-core --bin canvas`, then
// CODEAF_CANVAS_BIN=<that binary>. Without one these tests are skipped and
// say so: canvas behaviour is canvas-core's, and a fake of it here would test
// nothing but the fake.
func canvasOrg(t *testing.T) *wsOrg {
	t.Helper()
	bin := env.Get(canvasrpc.BinEnv)
	if bin == "" {
		if p, err := exec.LookPath("canvas"); err == nil {
			bin = p
		}
	}
	if bin == "" {
		t.Skipf("no canvas program: set %s to canvas-core's `canvas` (desk repo crates/canvas)", canvasrpc.BinEnv)
	}
	o := newWSOrg(t)
	o.h.config.Canvas = canvasrpc.New(bin, o.st.CanvasRoot())
	o.h.canvasRoutes()
	return o
}

type canvasOps struct {
	Seq int64 `json:"seq"`
	Ops []struct {
		Seq   int64           `json:"seq"`
		Actor string          `json:"actor"`
		Op    json.RawMessage `json:"op"`
	} `json:"ops"`
}

func ops(list ...string) api.CanvasOpsPost {
	out := api.CanvasOpsPost{}
	for _, s := range list {
		out.Ops = append(out.Ops, json.RawMessage(s))
	}
	return out
}

// TWO PEOPLE, ONE CANVAS, DIFFERENT FRAMES: santosh and abir each hold a hand
// on their own frame and both write at once; each sees the other's ops; a
// write into the frame the other's hand is on is refused with 409, naming
// who and carrying what the writer has not seen. Every op is stamped as the
// person who sent it.
func TestTwoPeopleWriteOneCanvasInDifferentFrames(t *testing.T) {
	o := canvasOrg(t)
	var cv map[string]any
	o.do(t, "santosh", "POST", "/v1/canvases", -1, api.CanvasNew{Title: "Webhook board", Shares: []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}}, 201, &cv)
	id := cv["id"].(string)
	if cv["my_role"] != "full" || cv["title"] != "Webhook board" {
		t.Fatalf("made: %v", cv)
	}
	path := "/v1/canvases/" + id
	o.do(t, "santosh", "POST", path+"/ops", -1, ops(
		`{"t":"put","id":"f_santosh0000a","type":"frame","props":{"kind":"board","name":"Santosh"}}`,
		`{"t":"put","id":"f_abir00000000","type":"frame","props":{"kind":"board","name":"Abir"}}`,
		`{"t":"put","id":"n_santosh0000a","parent":"f_santosh0000a","type":"text","props":{"text":"s","x":0,"y":0,"w":80,"h":20}}`,
		`{"t":"put","id":"n_abir00000000","parent":"f_abir00000000","type":"text","props":{"text":"a","x":0,"y":0,"w":80,"h":20}}`,
	), 200, nil)

	// Both take a hand on their own frame and write there.
	var base canvasOps
	o.do(t, "abir", "GET", path+"/ops?since=0", -1, nil, 200, &base)
	o.do(t, "santosh", "POST", path+"/ops", -1, ops(`{"t":"lease","frame":"f_santosh0000a","actor":"","until":"hand"}`), 200, nil)
	o.do(t, "abir", "POST", path+"/ops", -1, ops(`{"t":"lease","frame":"f_abir00000000","actor":"","until":"hand"}`), 200, nil)
	var s, a canvasOps
	o.do(t, "santosh", "POST", path+"/ops", -1, ops(`{"t":"patch","id":"n_santosh0000a","props":{"text":"santosh typing"}}`), 200, &s)
	o.do(t, "abir", "POST", path+"/ops", -1, ops(`{"t":"patch","id":"n_abir00000000","props":{"text":"abir typing"}}`), 200, &a)
	if len(s.Ops) != 1 || s.Ops[0].Actor != "person:santosh" || len(a.Ops) != 1 || a.Ops[0].Actor != "person:abir" {
		t.Fatalf("stamped as the writer: %+v %+v", s, a)
	}

	// Each sees the other's ops.
	var seen canvasOps
	o.do(t, "abir", "GET", path+"/ops?since="+seqText(base.Seq), -1, nil, 200, &seen)
	var byS bool
	for _, r := range seen.Ops {
		byS = byS || r.Actor == "person:santosh" && strings.Contains(string(r.Op), "santosh typing")
	}
	if !byS || seen.Seq != a.Seq {
		t.Fatalf("abir did not see santosh's edit: %+v", seen)
	}

	// abir in santosh's frame: 409, who, and what abir has not seen.
	w := o.raw(t, "abir", "POST", path+"/ops", api.CanvasOpsPost{BaseSeq: base.Seq, Ops: []json.RawMessage{json.RawMessage(`{"t":"patch","id":"n_santosh0000a","props":{"text":"mine"}}`)}})
	if w.Code != 409 {
		t.Fatalf("into santosh's frame: %d %s", w.Code, w.Body)
	}
	var c api.CanvasConflict
	_ = json.Unmarshal(w.Body.Bytes(), &c)
	if c.Holder != "person:santosh" || c.Frame != "f_santosh0000a" || c.Reason != "conflict" || c.Error != "santosh is working in this frame" || len(c.Ops) < 3 {
		t.Fatalf("conflict: %s", w.Body)
	}
	// Nor may abir take that hand.
	if w := o.raw(t, "abir", "POST", path+"/ops", ops(`{"t":"lease","frame":"f_santosh0000a","actor":"","until":"hand"}`)); w.Code != 409 {
		t.Fatalf("abir took santosh's hand: %d %s", w.Code, w.Body)
	}
	var snap struct {
		Nodes []struct {
			ID    string         `json:"id"`
			Props map[string]any `json:"props"`
		} `json:"nodes"`
	}
	o.do(t, "santosh", "GET", path+"/snapshot", -1, nil, 200, &snap)
	for _, n := range snap.Nodes {
		if n.ID == "n_santosh0000a" && n.Props["text"] != "santosh typing" {
			t.Fatalf("the refused write landed: %v", n.Props)
		}
	}
}

// AN OP NAMES NOBODY BUT ITS SENDER: a hand for someone else, a member, an
// agent's wait or a reserved meta key is refused before canvas-core sees it.
func TestCanvasOpsAreThePersonsOwn(t *testing.T) {
	v := Viewer{Handle: "abir"}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	for _, bad := range []string{
		`{"t":"lease","frame":"f_x","actor":"person:santosh","until":"hand"}`,
		`{"t":"member","actor":"person:mia","kind":"person","role":"edit"}`,
		`{"t":"wait","frame":"f_x"}`,
		`{"t":"proposal","frame":"f_x","holder":"x","op":{}}`,
		`{"t":"meta","key":"trashed","value":"now"}`,
		`{"t":"meta","key":"invite:did:x","value":"{}"}`,
		`not json`,
	} {
		if _, _, err := cleanCanvasOps([]json.RawMessage{json.RawMessage(bad)}, v, now); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	got, handsOnly, err := cleanCanvasOps([]json.RawMessage{json.RawMessage(`{"t":"lease","frame":"f_x","actor":"","until":"hand"}`)}, v, now)
	if err != nil || !handsOnly || got[0]["actor"] != "person:abir" || got[0]["until"] != "2026-11-13T12:00:10.000Z" {
		t.Fatalf("hand: %v %v %v", got, handsOnly, err)
	}
	_, handsOnly, _ = cleanCanvasOps([]json.RawMessage{json.RawMessage(`{"t":"meta","key":"title","value":"x"}`)}, v, now)
	if handsOnly {
		t.Fatal("a title is not a hand")
	}
}

// THE ROLES: someone it is not shared with gets 404 for everything; view
// reads but neither comments nor writes; comment adds a mark (kept with who
// wrote it) and takes a hand but writes nothing; only full shares or
// deletes. The operator sees a private canvas only as a name.
func TestCanvasRolesAndPrivacy(t *testing.T) {
	o := canvasOrg(t)
	var cv map[string]any
	o.do(t, "santosh", "POST", "/v1/canvases", -1, api.CanvasNew{Title: "Launch", Shares: []factory.Share{{Kind: "person", ID: "abir", Role: "view"}, {Kind: "person", ID: "mia", Role: "comment"}}}, 201, &cv)
	id := cv["id"].(string)
	path := "/v1/canvases/" + id
	o.do(t, "santosh", "POST", path+"/ops", -1, ops(`{"t":"put","id":"f_launch00000a","type":"frame","props":{"kind":"board","name":"One"}}`), 200, nil)

	o.do(t, "abir", "GET", path+"/snapshot", -1, nil, 200, nil)
	o.do(t, "abir", "POST", path+"/ops", -1, ops(`{"t":"meta","key":"title","value":"x"}`), 403, nil)
	o.do(t, "abir", "POST", path+"/marks", -1, map[string]any{"text": "hm"}, 403, nil)
	var mark map[string]any
	o.do(t, "mia", "POST", path+"/marks", -1, map[string]any{"frame": "f_launch00000a", "text": "bigger?"}, 201, &mark)
	if mark["by"] != "person:mia" {
		t.Fatalf("mark: %v", mark)
	}
	o.do(t, "mia", "POST", path+"/ops", -1, ops(`{"t":"lease","frame":"f_launch00000a","actor":"","until":"hand"}`), 200, nil)
	o.do(t, "mia", "POST", path+"/ops", -1, ops(`{"t":"put","type":"frame","props":{"kind":"board","name":"Mia"}}`), 403, nil)
	o.do(t, "mia", "PUT", path+"/shares", -1, api.CanvasSharesPut{Shares: []factory.Share{}}, 403, nil)
	o.do(t, "mia", "DELETE", path, -1, nil, 403, nil)

	// Private: shares grant nothing, the operator sees a name only.
	priv := true
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.CanvasSharesPut{Shares: []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}, Private: &priv}, 200, nil)
	o.do(t, "abir", "GET", path, -1, nil, 404, nil)
	o.do(t, "owner", "GET", path+"/snapshot", -1, nil, 404, nil)
	var list struct {
		Canvases []map[string]any `json:"canvases"`
	}
	o.do(t, "owner", "GET", "/v1/canvases", -1, nil, 200, &list)
	if len(list.Canvases) != 1 || list.Canvases[0]["name_only"] != true || list.Canvases[0]["title"] != "Launch" || list.Canvases[0]["outline"] != nil {
		t.Fatalf("operator's row: %v", list.Canvases)
	}
	o.do(t, "abir", "GET", "/v1/canvases", -1, nil, 200, &list)
	if len(list.Canvases) != 0 {
		t.Fatalf("abir sees a private canvas: %v", list.Canvases)
	}
	o.do(t, "santosh", "DELETE", path, -1, nil, 204, nil)
	o.do(t, "santosh", "GET", path, -1, nil, 404, nil)
}

// A CHANGE IS HEARD by those who may view the canvas, with its seq and who
// wrote it, and by nobody else.
func TestCanvasEventsReachOnlyItsPeople(t *testing.T) {
	o := canvasOrg(t)
	var cv map[string]any
	o.do(t, "santosh", "POST", "/v1/canvases", -1, api.CanvasNew{Title: "Shared", Owner: &api.WorkstreamOwner{Kind: "team", ID: "tm_platform"}}, 201, &cv)
	id := cv["id"].(string)
	var out canvasOps
	o.do(t, "abir", "POST", "/v1/canvases/"+id+"/ops", -1, ops(`{"t":"put","type":"frame","props":{"kind":"board","name":"A"}}`), 200, &out)
	events, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var last live.Event
	for _, e := range events {
		if e.Kind == api.EventCanvasChanged {
			last = e
		}
	}
	sees := func(who string) (string, bool) {
		r := httptest.NewRequest("GET", "/v1/events", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Authorization", "Bearer "+o.tokens[who])
		e, ok := o.h.liveForViewer(r, last)
		return string(e.Data), ok
	}
	want := `{"kind":"canvas.changed","id":"` + id + `","seq":` + seqText(out.Seq) + `,"by":"abir"}`
	if got, ok := sees("santosh"); !ok || got != want {
		t.Fatalf("santosh: %s %v; want %s", got, ok, want)
	}
	if _, ok := sees("mia"); ok {
		t.Fatal("mia is not in Platform and heard it")
	}
}

// A LAPTOP CANVAS MOVES IN ONCE: its nodes, tokens and assets arrive, it is
// the importer's, and moving it again answers the same canvas.
func TestCanvasImportMovesALaptopCanvasInOnce(t *testing.T) {
	o := canvasOrg(t)
	img := []byte("\x89PNG pretend")
	sum := sha256.Sum256(img)
	sha := hex.EncodeToString(sum[:])
	snap := `{"id":"cv_laptop000001","meta":{"title":"Mine"},"tokens":[],"members":[],"leases":[],"seq":9,
	  "nodes":[{"id":"f_pics00000000","parent":null,"ord":"a0","type":"frame","props":{"kind":"board","name":"Pics"},"rev":1},
	           {"id":"n_pic000000000","parent":"f_pics00000000","ord":"a0","type":"image","props":{"asset":"` + sha + `","x":0,"y":0,"w":4,"h":4},"rev":1}]}`
	body := api.CanvasImport{Snapshot: json.RawMessage(snap), FromLocalID: "cv_laptop000001", Assets: []api.CanvasAsset{{Sha: sha, Data: img, Type: "image/png", Name: "p.png"}}}
	var first, again map[string]any
	o.do(t, "santosh", "POST", "/v1/canvases/import", -1, body, 201, &first)
	o.do(t, "santosh", "POST", "/v1/canvases/import", -1, body, 200, &again)
	if first["id"] != again["id"] || first["title"] != "Mine" || first["my_role"] != "full" {
		t.Fatalf("import: %v / %v", first, again)
	}
	w := o.raw(t, "santosh", "GET", "/v1/canvases/"+first["id"].(string)+"/assets/"+sha, nil)
	if w.Code != 200 || w.Body.String() != string(img) || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("asset: %d %q %s", w.Code, w.Body, w.Header())
	}
	// Someone else moving their own laptop canvas of the same id gets their own.
	var abirs map[string]any
	o.do(t, "abir", "POST", "/v1/canvases/import", -1, body, 201, &abirs)
	if abirs["id"] == first["id"] {
		t.Fatal("abir's import answered santosh's canvas")
	}
}

// WHO IS IN A CANVAS, AND ON WHICH FRAME: abir in santosh's shared canvas
// sees santosh on his frame; mia, who it is not shared with, gets 404 for a
// heartbeat and hears none of it.
func TestCanvasPresenceSaysWhoIsOnWhichFrame(t *testing.T) {
	o := canvasOrg(t)
	var cv map[string]any
	o.do(t, "santosh", "POST", "/v1/canvases", -1, api.CanvasNew{Title: "Board", Shares: []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}}, 201, &cv)
	id := cv["id"].(string)
	o.beat(t, "santosh", page(id, "canvas", "f_santosh0000a", true), 200)
	got := o.beat(t, "abir", page(id, "canvas", "f_abir00000000", false), 200)
	want := []api.PresenceHere{{Handle: "abir", Part: "f_abir00000000"}, {Handle: "santosh", Part: "f_santosh0000a", Editing: true}}
	if !reflect.DeepEqual(got.Here, want) {
		t.Fatalf("here: %+v; want %+v", got.Here, want)
	}
	o.beat(t, "mia", page(id, "canvas", "f_santosh0000a", false), 404)
	if seen := o.presenceSeen(t, "mia"); len(seen) != 0 {
		t.Fatalf("mia heard a canvas she cannot see: %+v", seen)
	}
	if seen := o.presenceSeen(t, "abir"); len(seen) == 0 {
		t.Fatal("abir heard nobody")
	}
}

func seqText(n int64) string { return strconv.FormatInt(n, 10) }

// raw sends one request as who and answers the recorder whatever its status.
func (o *wsOrg) raw(t *testing.T, who, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	text := ""
	if body != nil {
		data, _ := json.Marshal(body)
		text = string(data)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(text))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+o.tokens[who])
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	return w
}
