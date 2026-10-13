package appapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestActivityPersistsLifecyclePinAndSkipsFailuresAndReplays(t *testing.T) {
	f := newAgentFixture(t)
	a := createSearchAgent(t, f, "Keep history")
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	f.registry.now = func() time.Time { return at }
	headers := bearer()
	headers.Set("Idempotency-Key", "close-once")
	for i := 0; i < 2; i++ {
		w := request(t, f.server, "POST", "/app/v1/agents/"+a.ID+"/close", `{}`, headers, "127.0.0.1:4000")
		readAgent(t, w, 200)
	}
	for _, action := range []string{"archive", "reopen"} {
		at = at.Add(time.Minute)
		readAgent(t, f.request(t, "POST", "/app/v1/agents/"+a.ID+"/"+action, `{}`), 200)
	}
	at = at.Add(time.Minute)
	readAgent(t, f.request(t, "PATCH", "/app/v1/agents/"+a.ID, `{"pinned":true}`), 200)
	readAgent(t, f.request(t, "PATCH", "/app/v1/agents/"+a.ID, `{"pinned":true}`), 200)
	if w := f.request(t, "POST", "/app/v1/agents/missing/close", `{}`); w.Code != 404 {
		t.Fatalf("missing: %d", w.Code)
	}
	if w := f.request(t, "POST", "/app/v1/agents/"+a.ID+"/archive", `{}`); w.Code != 409 {
		t.Fatalf("bad archive: %d", w.Code)
	}
	got := searchDecode[ActivityResponse](t, f.request(t, "GET", "/app/v1/activity", ""))
	if len(got.Items) != 4 {
		t.Fatalf("items %+v", got)
	}
	want := []string{"changed", "reopened", "archived", "closed"}
	for i, verb := range want {
		if got.Items[i].Verb != verb || got.Items[i].Object.ID != a.ID || got.Items[i].Actor != "you" {
			t.Fatalf("item %d: %+v", i, got.Items[i])
		}
	}
	records, _, gap, _, _ := f.server.events.read(0)
	events := []Activity{}
	for _, record := range records {
		if record.kind == EventActivity {
			var item Activity
			if err := json.Unmarshal(record.data, &item); err != nil {
				t.Fatal(err)
			}
			events = append(events, item)
		}
	}
	if gap || len(events) != 4 {
		t.Fatalf("Activity stream: gap %v items %+v", gap, events)
	}
	// Restart a server against the same home after reopening clears lifecycle timestamps.
	restarted := New(testToken, Options{Agents: NewAgentRegistry(f.home, nil)})
	t.Cleanup(restarted.Close)
	saved := searchDecode[ActivityResponse](t, request(t, restarted, "GET", "/app/v1/activity", "", bearer(), "127.0.0.1:4000"))
	if len(saved.Items) != 4 || saved.Items[3].Verb != "closed" {
		t.Fatalf("restart lost history: %+v", saved)
	}
	filtered := searchDecode[ActivityResponse](t, f.request(t, "GET", "/app/v1/activity?since=2026-10-12T12:01:00Z", ""))
	if len(filtered.Items) != 2 {
		t.Fatalf("strict since: %+v", filtered)
	}
	info, err := os.Stat(filepath.Join(f.home, "v3", "app", "activity.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions %v", info.Mode())
	}
}

func TestActivityOtherResourceInterfacesAndConcurrentWindows(t *testing.T) {
	f := newAgentFixture(t)
	var mu sync.Mutex
	crewState := "active"
	rulePresent := true
	memoryPresent := true
	install := func(s *Server) {
		s.Handle("GET /app/v1/crews/{cid}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			write(w, 200, CrewResponse{Crew: Crew{ID: r.PathValue("cid"), State: crewState}})
		}))
		s.Handle("POST /app/v1/crews/{cid}/finish", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			crewState = "closed"
			write(w, 200, ActionResponse{OK: true})
		}))
		s.Handle("GET /app/v1/rules", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			rows := []PermissionRule{}
			if rulePresent {
				rows = append(rows, PermissionRule{ID: "rule"})
			}
			write(w, 200, RulesResponse{Rules: rows})
		}))
		s.Handle("DELETE /app/v1/rules/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			rulePresent = false
			write(w, 200, ActionResponse{OK: true})
		}))
		s.Handle("POST /app/v1/rules/{id}/restore", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			rulePresent = true
			write(w, 200, ActionResponse{OK: true})
		}))
		s.Handle("GET /app/v1/memory", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			items := []MemoryItem{}
			if memoryPresent {
				items = append(items, MemoryItem{ID: "memory"})
			}
			write(w, 200, MemoryResponse{Items: items})
		}))
		s.Handle("POST /app/v1/memory/{id}/forget", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			memoryPresent = false
			write(w, 200, ActionResponse{OK: true})
		}))
		s.Handle("POST /app/v1/memory/{id}/restore", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			memoryPresent = true
			write(w, 200, ActionResponse{OK: true})
		}))
	}
	second := New(testToken, Options{Agents: NewAgentRegistry(f.home, nil)})
	t.Cleanup(second.Close)
	install(f.server)
	install(second)
	// Two windows observing the same finish produce a single Activity line.
	var wg sync.WaitGroup
	for _, s := range []*Server{f.server, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(t, s, "POST", "/app/v1/crews/crew/finish", `{}`, bearer(), "127.0.0.1:4000")
			if w.Code != 200 {
				t.Errorf("finish: %d %s", w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	for _, test := range []struct{ method, path string }{{"DELETE", "/app/v1/rules/rule"}, {"POST", "/app/v1/rules/rule/restore"}, {"POST", "/app/v1/memory/memory/forget"}, {"POST", "/app/v1/memory/memory/restore"}} {
		if w := f.request(t, test.method, test.path, `{}`); w.Code != 200 {
			t.Fatalf("%s: %d %s", test.path, w.Code, w.Body)
		}
	}
	got := searchDecode[ActivityResponse](t, f.request(t, "GET", "/app/v1/activity", ""))
	if len(got.Items) != 5 || got.Items[4].Verb != "finished" {
		t.Fatalf("other Activity: %+v", got)
	}
}
func TestActivityRefusesUnavailableJournalBeforeMutation(t *testing.T) {
	f := newAgentFixture(t)
	a := createSearchAgent(t, f, "Still open")
	if err := os.Remove(filepath.Join(f.home, "v3", "app", "activity.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.home, "v3", "app", "activity.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	if w := f.request(t, "POST", "/app/v1/agents/"+a.ID+"/close", `{}`); w.Code != 503 {
		t.Fatalf("journal: %d %s", w.Code, w.Body)
	}
	got := readAgent(t, f.request(t, "GET", "/app/v1/agents/"+a.ID, ""), 200)
	if got.State != "idle" {
		t.Fatalf("mutation happened: %+v", got)
	}
}
