package appapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/store"
)

func resourceRequest(s *Server, method, path, body, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:53021"+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer test-resource-token")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func resourceServer(t *testing.T, home string, now *time.Time, ctx context.Context) *Server {
	t.Helper()
	agents := NewAgentRegistry(home, nil)
	agents.now = func() time.Time { return *now }
	s := New("test-resource-token", Options{ProfileDir: home, Agents: agents, Context: ctx})
	t.Cleanup(s.Close)
	return s
}
func seedMemory(t *testing.T, home, text, owner string) store.Memory {
	t.Helper()
	db, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	row, err := db.AddMemory(store.Memory{Title: "Deployment", Text: text, Owner: owner, Type: store.MemoryFact})
	if err != nil {
		t.Fatal(err)
	}
	return row
}
func TestMemoryRoutesEditForgetAndUndoAcrossLaunches(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	row := seedMemory(t, home, "Deploy on Fridays", store.OwnerUser)
	s := resourceServer(t, home, &now, nil)
	w := resourceRequest(s, "GET", "/app/v1/memory?shelf=user&q=FRIDAY", "", "")
	var list MemoryResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Items) != 1 || list.Shelves[0].Name != "Yours" {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	path := "/app/v1/memory/" + row.ID
	w = resourceRequest(s, "PATCH", path, `{"text":"Deploy on Mondays"}`, "")
	var changed MemoryItem
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &changed) != nil || changed.Text != "Deploy on Mondays" || changed.Shelf != store.OwnerUser {
		t.Fatalf("edit: %d %s", w.Code, w.Body.String())
	}
	w = resourceRequest(s, "POST", path+"/forget", `{}`, "forget")
	if w.Code != 200 {
		t.Fatalf("forget: %d %s", w.Code, w.Body.String())
	}
	// A keyed retry must not move the original deadline.
	now = now.Add(4 * time.Second)
	if retry := resourceRequest(s, "POST", path+"/forget", `{}`, "forget"); retry.Code != 200 || retry.Body.String() != w.Body.String() {
		t.Fatal("forget replay changed")
	}
	w = resourceRequest(s, "GET", "/app/v1/memory", "", "")
	if json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Items) != 0 {
		t.Fatalf("forgotten memory remains visible: %s", w.Body.String())
	}
	second := resourceServer(t, home, &now, nil)
	w = resourceRequest(second, "POST", path+"/restore", `{}`, "restore")
	if w.Code != 200 {
		t.Fatalf("cross-launch undo: %d %s", w.Code, w.Body.String())
	}
	w = resourceRequest(second, "GET", "/app/v1/memory?q=monday", "", "")
	if json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Items) != 1 {
		t.Fatalf("restored memory missing: %s", w.Body.String())
	}
	if second.events.seq != 1 || second.events.ring[0].kind != EventMemoryChanged {
		t.Fatal("restore did not publish memory.changed")
	}
}
func TestMemoryUndoEndsAtExactlyFiveSeconds(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	row := seedMemory(t, home, "Deploy", store.OwnerUser)
	s := resourceServer(t, home, &now, nil)
	path := "/app/v1/memory/" + row.ID
	if w := resourceRequest(s, "POST", path+"/forget", `{}`, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	now = now.Add(MemoryUndoWindow)
	if w := resourceRequest(s, "POST", path+"/restore", `{}`, ""); w.Code != 409 || !strings.Contains(w.Body.String(), "window has ended") {
		t.Fatalf("late undo: %d %s", w.Code, w.Body.String())
	}
	db, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	saved, ok, err := db.MemoryRecord(row.ID)
	if err != nil || !ok || saved.Status != store.MemoryForgotten {
		t.Fatalf("late restore resurrected memory: %+v %v", saved, err)
	}
}
func TestMemoryRefusesStaleInvalidAndUnsupportedMutations(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	row := seedMemory(t, home, "Deploy", store.OwnerUser)
	s := resourceServer(t, home, &now, nil)
	path := "/app/v1/memory/" + row.ID
	cases := []struct {
		method, path, body string
		status             int
	}{
		{"PATCH", path, `{"text":"new","expect":{"version":0}}`, 409},
		{"PATCH", path, `{"text":"new","shelf":"crew:release"}`, 422},
		{"PATCH", path, `{"text":""}`, 422},
		{"PATCH", path, `{"text":"new","expect":null}`, 422},
		{"PATCH", path, `{}`, 422},
		{"PATCH", path, `{"secret":"no"}`, 422},
		{"PATCH", path, `null`, 422},
		{"POST", path + "/forget", `{} {}`, 422},
		{"POST", path + "/restore", `{}`, 409},
		{"POST", "/app/v1/memory/missing/forget", `{}`, 404},
		{"GET", "/app/v1/memory?shelf=unknown", ``, 422},
	}
	for _, tc := range cases {
		w := resourceRequest(s, tc.method, tc.path, tc.body, "")
		if w.Code != tc.status {
			t.Errorf("%s %s %s: %d %s", tc.method, tc.path, tc.body, w.Code, w.Body.String())
		}
	}
	w := resourceRequest(s, "GET", "/app/v1/memory", "", "")
	var result MemoryResponse
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.Items[0].Text != "Deploy" {
		t.Fatal(w.Body.String())
	}
}

type fakeCrewMemory struct {
	item  MemoryItem
	patch PatchMemoryRequest
	until time.Time
}

func (f *fakeCrewMemory) ReadMemory(context.Context) (MemoryResponse, error) {
	return MemoryResponse{Shelves: []MemoryShelf{{ID: "crew:release", Name: "Release"}}, Items: []MemoryItem{f.item}}, nil
}
func (f *fakeCrewMemory) PatchMemory(_ context.Context, _ string, body PatchMemoryRequest) (MemoryItem, error) {
	f.patch = body
	return f.item, nil
}
func (f *fakeCrewMemory) ForgetMemory(_ context.Context, _ string, _ *AgentExpect, now time.Time) error {
	f.until = now.Add(MemoryUndoWindow)
	return nil
}
func (f *fakeCrewMemory) RestoreMemory(_ context.Context, _ string, _ *AgentExpect, _ time.Time) error {
	return nil
}
func TestMemoryUsesCrewResourceInterface(t *testing.T) {
	now := time.Now()
	backend := &fakeCrewMemory{item: MemoryItem{ID: "m-release", Shelf: "crew:release", Text: "Release only"}}
	s := resourceServer(t, t.TempDir(), &now, WithMemoryBackend(context.Background(), backend))
	w := resourceRequest(s, "GET", "/app/v1/memory?shelf=crew:release", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Release only") {
		t.Fatal(w.Body.String())
	}
	w = resourceRequest(s, "PATCH", "/app/v1/memory/m-release", `{"shelf":"user","expect":{"version":4}}`, "")
	if w.Code != 200 || backend.patch.Shelf == nil || *backend.patch.Shelf != "user" || *backend.patch.Expect.Version != 4 {
		t.Fatalf("patch not forwarded: %d %+v", w.Code, backend.patch)
	}
	w = resourceRequest(s, "POST", "/app/v1/memory/m-release/forget", `{}`, "")
	if w.Code != 200 || !backend.until.Equal(now.Add(5*time.Second)) {
		t.Fatal("forget time not forwarded")
	}
}

func TestMemoryCrewShelvesAndMovesUseNativeMembership(t *testing.T) {
	f := newAgentFixture(t)
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "Helper")
	crew := crewTestMake(t, f, "Release", a, b)
	row := seedMemory(t, f.home, "Monday release", store.OwnerUser)
	var result MemoryResponse
	crewTestRead(t, f.request(t, "GET", "/app/v1/memory", ""), 200, &result)
	found := false
	for _, shelf := range result.Shelves {
		if shelf.ID == store.OwnerCrew(crew.ID) && shelf.Name == "Release" {
			found = true
		}
	}
	if !found {
		t.Fatalf("empty crew shelf missing: %v", result.Shelves)
	}
	path := "/app/v1/memory/" + row.ID
	var changed MemoryItem
	crewTestRead(t, f.request(t, "PATCH", path, `{"shelf":`+strconvQuote(store.OwnerCrew(crew.ID))+`}`), 200, &changed)
	if changed.Shelf != store.OwnerCrew(crew.ID) {
		t.Fatal(changed)
	}
	crewTestRead(t, f.request(t, "GET", "/app/v1/memory?shelf=user", ""), 200, &result)
	if len(result.Items) != 0 {
		t.Fatal("personal shelf retained crew-owned memory")
	}
	crewTestRead(t, f.request(t, "GET", "/app/v1/memory?shelf="+store.OwnerCrew(crew.ID), ""), 200, &result)
	if len(result.Items) != 1 {
		t.Fatal("crew shelf lost the memory")
	}
}
