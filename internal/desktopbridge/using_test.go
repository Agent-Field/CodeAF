package desktopbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// usingAgent is an open chat whose model and gate the Using doors can move.
type usingAgent struct {
	*fakeAgent
	mu       sync.Mutex
	swaps    []string
	postures []string
	dial     bool
	resolved string
	standing string
}

func (a *usingAgent) SetModel(model string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.swaps = append(a.swaps, model)
	a.model = model
}
func (a *usingAgent) SetReasoningFor(string, string) {}
func (a *usingAgent) ApprovalDial() bool             { return a.dial }
func (a *usingAgent) ResolvedApprovalPosture() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resolved
}
func (a *usingAgent) StandingApprovalPosture() string { return a.standing }
func (a *usingAgent) SetApprovalPosture(p string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p == "nope" {
		return errors.New("refused")
	}
	a.postures = append(a.postures, p)
	a.resolved = p
	return nil
}

type usingRig struct {
	*placesRig
	door   *session.PlaceGraphDoor
	agent  *usingAgent
	chat   string
	file   string
	launch *remote.LaunchShape
	opens  atomic.Int32
	local  bool
}

// newUsingRig is a bridge whose places door is the engine's own (same graph,
// same picks file) and whose engine opens one conversation saved in a folder
// named chat.
func newUsingRig(t *testing.T) *usingRig {
	t.Helper()
	base := newPlacesRig(t)
	door, err := session.PlaceGraphDoorFor(base.path)
	if err != nil {
		t.Fatal(err)
	}
	door.Sources = placegraph.SourcePolicy{Deny: []string{}}
	if err := base.p.UseDoor(door); err != nil {
		t.Fatal(err)
	}
	rig := &usingRig{placesRig: base, door: door, chat: "0123456789abcdef", local: true}
	rig.file = filepath.Join(t.TempDir(), rig.chat, "session.jsonl")
	os.MkdirAll(filepath.Dir(rig.file), 0o755)
	rig.agent = &usingAgent{fakeAgent: &fakeAgent{events: make(chan session.Event, 8), model: Model}, dial: true, resolved: session.PostureAsk, standing: session.PostureAsk}
	rig.launch = &remote.LaunchShape{OneModel: true, Interactive: true, PlaceGraph: door.Path}
	base.b.open = func(string) (Connection, error) {
		rig.opens.Add(1)
		return Connection{Agent: rig.agent, Welcome: remote.Welcome{SessionFile: rig.file, Workspace: "/p", Model: Model, Persistent: true, Launch: rig.launch}, Local: rig.local, Close: func() {}}, nil
	}
	return rig
}

// open starts the conversation and answers the bridge's token for it.
func (r *usingRig) open(body map[string]any) (string, int, apiError) {
	r.t.Helper()
	var snap struct {
		ID string `json:"id"`
		apiError
	}
	code := r.do("POST", "/sessions", body, &snap)
	return snap.ID, code, snap.apiError
}

func (r *usingRig) using(token string) UsingView {
	r.t.Helper()
	var v UsingView
	if code := r.do("GET", "/sessions/"+token+"/using", nil, &v); code != 200 {
		r.t.Fatalf("using: %d", code)
	}
	return v
}

func (r *usingRig) policyPlace(name string, pol placegraph.Policy, parents ...string) string {
	r.t.Helper()
	p, _, err := r.p.Store.CreatePlace(placegraph.NewPlace{Name: name, Parents: parents, Policy: pol})
	if err != nil {
		r.t.Fatal(err)
	}
	return p.ID
}

func (r *usingRig) fileChat(placeID string) {
	r.t.Helper()
	if _, _, err := r.p.Store.AddChat(r.chat, placeID, placegraph.AddedByYou); err != nil {
		r.t.Fatal(err)
	}
}

func setting(v UsingView, field placegraph.PolicyField) session.PlaceSetting {
	for _, s := range v.Settings {
		if s.Field == field {
			return s
		}
	}
	return session.PlaceSetting{}
}

func TestUsingNamesTheChatByItsTranscriptNeverByTheBridgeToken(t *testing.T) {
	rig := newUsingRig(t)
	release := rig.policyPlace("Release", placegraph.Policy{Model: "place/flash"})
	ctx := placegraph.Context{Instructions: "Write for customers", Sources: []placegraph.Source{{Kind: placegraph.SourceURL, Ref: "https://codeaf.dev/docs"}}}
	if _, err := rig.p.Store.SetContext(release, ctx); err != nil {
		t.Fatal(err)
	}
	rig.fileChat(release)
	token, code, _ := rig.open(map[string]any{})
	if code != 200 || token == "" || token == rig.chat {
		t.Fatalf("open: %d %q", code, token)
	}
	v := rig.using(token)
	if v.ChatID != rig.chat || !v.Engine.Places || len(v.Bundle.Places) != 1 || v.Bundle.Places[0].ID != release || len(v.Bundle.Instructions) != 1 || v.Bundle.Counts.Sources != 1 {
		t.Fatalf("view: %+v", v)
	}
	if s := setting(v, placegraph.PolicyModel); s.State != session.PlaceSettingPending || s.Value != "place/flash" || s.DecidedBy != release {
		t.Fatalf("a new chat's place model must read pending: %+v", s)
	}
	// The chat id is not a token: it opens nothing.
	var e apiError
	if code := rig.do("GET", "/sessions/"+rig.chat+"/using", nil, &e); code != 404 {
		t.Fatalf("the chat id answered as a token: %d", code)
	}
}

func TestUsingIsAuthenticatedLikeEveryRoute(t *testing.T) {
	rig := newUsingRig(t)
	token, _, _ := rig.open(map[string]any{})
	resp, err := rig.srv.Client().Get(rig.srv.URL + "/api/engine/sessions/" + token + "/using")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("unauthenticated using: %d", resp.StatusCode)
	}
}

func TestUsingReloadsWhenThePlaceChangesAndStopsAtTheBudget(t *testing.T) {
	rig := newUsingRig(t)
	id := rig.policyPlace("Research", placegraph.Policy{})
	rig.fileChat(id)
	token, _, _ := rig.open(map[string]any{})
	before := rig.using(token)
	var sources []placegraph.Source
	for i := 0; i < placegraph.ContextSourceBudget+3; i++ {
		sources = append(sources, placegraph.Source{Kind: placegraph.SourceURL, Ref: fmt.Sprintf("https://example.com/%d", i)})
	}
	if _, err := rig.p.Store.SetContext(id, placegraph.Context{Sources: sources}); err != nil {
		t.Fatal(err)
	}
	after := rig.using(token)
	if after.Revision == before.Revision {
		t.Fatal("the Using list did not reload the graph")
	}
	if len(after.Bundle.Sources) != placegraph.ContextSourceBudget || len(after.Bundle.Trimmed) != 3 || after.Bundle.Counts.Sources != placegraph.ContextSourceBudget {
		t.Fatalf("budget: given %d trimmed %d", len(after.Bundle.Sources), len(after.Bundle.Trimmed))
	}
}

func TestAConflictPickMustNameOneOfThePlacesThatDisagree(t *testing.T) {
	rig := newUsingRig(t)
	a := rig.policyPlace("Marketing", placegraph.Policy{Model: "place/a"})
	b := rig.policyPlace("Software", placegraph.Policy{Model: "place/b"})
	outsider := rig.policyPlace("Elsewhere", placegraph.Policy{Model: "place/c"})
	rig.fileChat(a)
	rig.fileChat(b)
	token, _, _ := rig.open(map[string]any{})
	if s := setting(rig.using(token), placegraph.PolicyModel); s.State != session.PlaceSettingNeedsPick || s.Value != "" {
		t.Fatalf("a conflict must apply nothing: %+v", s)
	}
	var e apiError
	path := "/sessions/" + token + "/using/choice"
	if code := rig.do("POST", path, map[string]any{"field": "model", "placeId": outsider}, &e); code != 422 || e.Code != "not_a_candidate" {
		t.Fatalf("an outsider place was accepted: %d %+v", code, e)
	}
	if code := rig.do("POST", path, map[string]any{"field": "permissions", "placeId": a}, &e); code != 409 || e.Code != "no_conflict" {
		t.Fatalf("a field nobody disagrees on was accepted: %d %+v", code, e)
	}
	if code := rig.do("POST", path, map[string]any{"field": "temperature", "placeId": a}, &e); code != 400 {
		t.Fatalf("an unknown field: %d", code)
	}
	if _, err := os.Stat(rig.door.ChoicesPath); err == nil {
		t.Fatal("a refused pick wrote the picks file")
	}
	var v UsingView
	if code := rig.do("POST", path, map[string]any{"field": "model", "placeId": b}, &v); code != 200 {
		t.Fatalf("pick: %d", code)
	}
	if s := setting(v, placegraph.PolicyModel); s.State != session.PlaceSettingPending || s.Value != "place/b" || s.DecidedBy != b {
		t.Fatalf("after the pick: %+v", s)
	}
	// Remembered beside the graph, where the engine reads it.
	picks, err := placegraph.ReadChoices(filepath.Join(filepath.Dir(rig.path), session.PlaceChoicesFile), rig.chat)
	if err != nil || len(picks) != 1 || picks[0].PlaceID != b {
		t.Fatalf("picks: %+v %v", picks, err)
	}
	if len(rig.agent.swaps) != 0 {
		t.Fatalf("a pick moved the model from the bridge: %v", rig.agent.swaps)
	}
}

func TestAWiderPlacePostureWaitsForThePersonsOwnAct(t *testing.T) {
	rig := newUsingRig(t)
	sandbox := rig.policyPlace("Sandbox", placegraph.Policy{Permissions: session.PostureAllow})
	rig.fileChat(sandbox)
	token, _, _ := rig.open(map[string]any{})
	if s := setting(rig.using(token), placegraph.PolicyPermissions); s.State != session.PlaceSettingNeedsYou {
		t.Fatalf("a wider posture: %+v", s)
	}
	if len(rig.agent.postures) != 0 {
		t.Fatal("reading the Using list moved the gate")
	}
	var v UsingView
	if code := rig.do("POST", "/sessions/"+token+"/using/apply", map[string]any{"field": "permissions"}, &v); code != 200 {
		t.Fatalf("apply: %d", code)
	}
	if len(rig.agent.postures) != 1 || rig.agent.postures[0] != session.PostureAllow {
		t.Fatalf("the person's apply did not reach the gate: %v", rig.agent.postures)
	}
	rig.agent.dial = false
	var e apiError
	if code := rig.do("POST", "/sessions/"+token+"/using/apply", map[string]any{"field": "permissions"}, &e); code != 501 || e.Code != "unsupported" {
		t.Fatalf("no dial: %d %+v", code, e)
	}
}

func TestApplyRefusesAConflictAndAFieldNoPlaceSets(t *testing.T) {
	rig := newUsingRig(t)
	a := rig.policyPlace("A", placegraph.Policy{Model: "place/a"})
	b := rig.policyPlace("B", placegraph.Policy{Model: "place/b"})
	rig.fileChat(a)
	rig.fileChat(b)
	token, _, _ := rig.open(map[string]any{})
	var e apiError
	if code := rig.do("POST", "/sessions/"+token+"/using/apply", map[string]any{"field": "model"}, &e); code != 409 || e.Code != "needs_pick" {
		t.Fatalf("conflict apply: %d %+v", code, e)
	}
	if code := rig.do("POST", "/sessions/"+token+"/using/apply", map[string]any{"field": "permissions"}, &e); code != 409 || e.Code != "nothing_to_apply" {
		t.Fatalf("nothing to apply: %d %+v", code, e)
	}
	if len(rig.agent.swaps) != 0 || len(rig.agent.postures) != 0 {
		t.Fatal("a refused apply changed something")
	}
}

func TestAnEngineThatReadsNoPlacesIsNeverClaimedToApplyThem(t *testing.T) {
	for name, mutate := range map[string]func(*usingRig){
		"terminal": func(r *usingRig) { r.launch = &remote.LaunchShape{OneModel: true} },
		"other": func(r *usingRig) {
			r.launch = &remote.LaunchShape{OneModel: true, PlaceGraph: "/elsewhere/places.json"}
		},
		"remote": func(r *usingRig) { r.local = false },
	} {
		t.Run(name, func(t *testing.T) {
			rig := newUsingRig(t)
			mutate(rig)
			id := rig.policyPlace("Release", placegraph.Policy{Model: "place/flash"})
			rig.fileChat(id)
			token, _, _ := rig.open(map[string]any{})
			v := rig.using(token)
			if v.Engine.Places || v.Engine.Reason == "" {
				t.Fatalf("engine: %+v", v.Engine)
			}
			if s := setting(v, placegraph.PolicyModel); s.State != session.PlaceSettingUnavailable {
				t.Fatalf("setting: %+v", s)
			}
		})
	}
}

func TestANewChatStartedInAPlaceIsFiledBeforeItsFirstTurn(t *testing.T) {
	rig := newUsingRig(t)
	id := rig.policyPlace("Release", placegraph.Policy{Model: "place/flash"})
	token, code, _ := rig.open(map[string]any{"placeId": id})
	if code != 200 || token == "" {
		t.Fatalf("open in place: %d", code)
	}
	snap, _ := rig.p.Store.Snapshot()
	ms := snap.PlacesOf(rig.chat)
	if len(ms) != 1 || ms[0].PlaceID != id || ms[0].AddedBy != placegraph.AddedByYou {
		t.Fatalf("memberships: %+v", ms)
	}
	// Refused before any engine is opened.
	opens := rig.opens.Load()
	if _, _, err := rig.p.Store.CreatePlace(placegraph.NewPlace{Name: "Old"}); err != nil {
		t.Fatal(err)
	}
	old := rig.policyPlace("Gone", placegraph.Policy{})
	if _, err := rig.p.Store.Archive(old); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body   map[string]any
		status int
		code   string
	}{
		{map[string]any{"placeId": "pl_ffffffffffffffff"}, 404, "not_found"},
		{map[string]any{"placeId": old}, 409, "archived"},
		{map[string]any{"placeId": placegraph.NowID}, 400, "reserved"},
		{map[string]any{"placeId": id, "sessionFile": rig.file}, 400, "invalid"},
	} {
		if _, code, e := rig.open(tc.body); code != tc.status || e.Code != tc.code {
			t.Errorf("%v: %d %+v", tc.body, code, e)
		}
	}
	if rig.opens.Load() != opens {
		t.Fatal("a refused place still opened an engine")
	}
}

func TestPlacePolicyIsCheckedAgainstTheRealPosturesAndModelList(t *testing.T) {
	rig := newUsingRig(t)
	rig.b.UseModels(&Models{ProfileDir: t.TempDir(), Catalog: func(context.Context) ([]CatalogModel, error) {
		return []CatalogModel{{ID: "deepseek/deepseek-v4.1-flash"}, {ID: "place/listed"}}, nil
	}})
	id := rig.mk("Release")
	var e apiError
	for _, body := range []map[string]any{
		{"policy": map[string]any{"permissions": "sudo"}},
		{"policy": map[string]any{"permissions": " ask"}},
		{"policy": map[string]any{"model": "not/listed"}},
	} {
		if code := rig.do("POST", "/places/"+id, body, &e); code != 400 || e.Code != "invalid_policy" {
			t.Errorf("%v: %d %+v", body, code, e)
		}
	}
	for _, word := range append(append([]string{}, session.ApprovalPostures...), "") {
		var m Mutation
		if code := rig.do("POST", "/places/"+id, map[string]any{"policy": map[string]any{"permissions": word, "model": "place/listed"}}, &m); code != 200 {
			t.Errorf("%q refused: %d", word, code)
		}
	}
}

func TestASourceTheEngineWouldRefuseIsRefusedWhenAdded(t *testing.T) {
	rig := newUsingRig(t)
	root := t.TempDir()
	secret := filepath.Join(root, "secrets")
	os.MkdirAll(secret, 0o755)
	link := filepath.Join(root, "innocent")
	os.Symlink(secret, link)
	rig.door.Sources = placegraph.SourcePolicy{Deny: []string{secret}}
	id := rig.mk("Release")
	var e apiError
	for _, ref := range []string{secret, link} {
		if code := rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "folder", "ref": ref}, &e); code != 422 || e.Code != "refused_source" || !strings.Contains(e.Error, "can't be given") {
			t.Errorf("%s: %d %+v", ref, code, e)
		}
	}
	// A path that is two spellings of one folder is one source.
	ok := filepath.Join(root, "notes")
	os.MkdirAll(ok, 0o755)
	var m Mutation
	if code := rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "folder", "ref": ok}, &m); code != 200 {
		t.Fatalf("plain folder: %d", code)
	}
	if code := rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "folder", "ref": ok + "/."}, &e); code != 409 || e.Code != "duplicate_source" {
		t.Fatalf("second spelling: %d %+v", code, e)
	}
}

func TestASettingsDefaultChangeLeavesAPlacesModelAlone(t *testing.T) {
	rig := newUsingRig(t)
	id := rig.policyPlace("Release", placegraph.Policy{Model: "place/flash"})
	rig.fileChat(id)
	token, _, _ := rig.open(map[string]any{})
	// The engine applied it at the first turn: the chat runs on it now.
	rig.agent.model = "place/flash"
	if err := session.SaveMeta(filepath.Dir(rig.file), session.Meta{ID: rig.chat, Model: "place/flash", LastUserAt: time.Now(), PlaceDefaults: &session.PlaceDefaults{Model: "place/flash", ModelBy: id, ModelFollows: true}}); err != nil {
		t.Fatal(err)
	}
	if s := setting(rig.using(token), placegraph.PolicyModel); s.State != session.PlaceSettingApplied {
		t.Fatalf("setting: %+v", s)
	}
	rig.b.moveConversations(RoleView{Model: "other/model"})
	if len(rig.agent.swaps) != 0 {
		t.Fatalf("a settings default moved a place's model: %v", rig.agent.swaps)
	}
}

func TestUsingViewWireShape(t *testing.T) {
	rig := newUsingRig(t)
	token, _, _ := rig.open(map[string]any{})
	resp, err := rig.srv.Client().Do(func() *http.Request {
		req, _ := http.NewRequest("GET", rig.srv.URL+"/api/engine/sessions/"+token+"/using", nil)
		req.Header.Set("Authorization", "Bearer "+placesToken)
		return req
	}())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"chatId", "engine", "revision", "readAt", "bundle", "settings"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing %q", key)
		}
	}
	if string(raw["settings"]) != "[]" {
		t.Fatalf("an unplaced chat's settings must be an empty list: %s", raw["settings"])
	}
}

// TestUsingWireFixtures writes the Using answers the TypeScript client parses
// (using-client.test.ts), from the real handlers, on the same terms as
// TestPlacesWireFixtures: a change here fails until the fixtures are
// regenerated with UPDATE_PLACES_FIXTURES=1.
func TestUsingWireFixtures(t *testing.T) {
	rig := newUsingRig(t)
	got := map[string][]byte{}
	capture := func(name, method, path string, body any, wantStatus int) {
		t.Helper()
		var raw json.RawMessage
		if code := rig.do(method, path, body, &raw); code != wantStatus {
			t.Fatalf("%s: %s %s answered %d, want %d: %s", name, method, path, code, wantStatus, raw)
		}
		var tree any
		if err := json.Unmarshal(raw, &tree); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		pretty, _ := json.MarshalIndent(tree, "", "  ")
		got[name] = append(pretty, '\n')
	}
	software := rig.policyPlace("Software", placegraph.Policy{Permissions: session.PostureAsk})
	marketing := rig.policyPlace("Marketing", placegraph.Policy{Model: "place/writer"})
	release := rig.policyPlace("Release", placegraph.Policy{Model: "place/flash", Permissions: session.PostureAllow}, software)
	if _, err := rig.p.Store.SetContext(release, placegraph.Context{Instructions: "Write for customers, not engineers.", Sources: []placegraph.Source{{Kind: placegraph.SourceURL, Ref: "https://codeaf.dev/docs"}}}); err != nil {
		t.Fatal(err)
	}
	rig.fileChat(release)
	rig.fileChat(marketing)
	token, code, _ := rig.open(map[string]any{})
	if code != 200 {
		t.Fatal(code)
	}
	base := "/sessions/" + token + "/using"
	capture("using", "GET", base, nil, 200)
	capture("using-error-not-a-candidate", "POST", base+"/choice", map[string]any{"field": "model", "placeId": software}, 422)
	capture("using-choice", "POST", base+"/choice", map[string]any{"field": "model", "placeId": release}, 200)
	update := os.Getenv("UPDATE_PLACES_FIXTURES") == "1"
	for name, data := range got {
		file := filepath.Join(placesFixtureDir, name+".json")
		if update {
			if err := os.WriteFile(file, data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("fixture %s is missing; regenerate with UPDATE_PLACES_FIXTURES=1: %v", name, err)
		}
		if string(want) != string(data) {
			t.Errorf("the wire answer %q changed; regenerate the fixtures\n--- committed\n%s\n--- now\n%s", name, want, data)
		}
	}
}
