package desktopbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/workspacestore"
)

func TestOpenElsewhereCanonicalOpenViewsAndCache(t *testing.T) {
	b, dir := newWorkspaceBridge(t)
	graph, err := placegraph.Open(placegraph.Options{Path: filepath.Join(t.TempDir(), "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	b.UsePlaces(NewPlaces(graph))
	current, _, err := graph.CreatePlace(placegraph.NewPlace{Name: "Current"})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := graph.CreatePlace(placegraph.NewPlace{Name: "Marketing"})
	if err != nil {
		t.Fatal(err)
	}
	unrelated, _, err := graph.CreatePlace(placegraph.NewPlace{Name: "Unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	target := "/projects/one/chat.json"
	put := func(key string, revision uint64, doc any) {
		t.Helper()
		w := request(b, "PUT", "/api/engine/workspaces/"+key, putBody(revision, "test", doc))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	doc := func(kind, path string) map[string]any {
		d := sharedDoc("chat")
		tab := d["tabs"].([]map[string]any)[0]
		tab["kind"] = kind
		tab["sessionFile"] = path
		return d
	}
	put(current.ID, 0, doc("conversation", target))
	split := sharedDoc("split")
	split["tabs"].([]map[string]any)[0]["split"] = map[string]any{"layout": "1x2", "panes": []map[string]any{
		{"id": "left", "kind": "conversation", "title": "Chat", "draft": "", "sessionFile": "/projects/one/../one/chat.json"},
		{"id": "right", "kind": "task", "title": "Task", "draft": "", "sessionFile": target},
	}}
	put(other.ID, 0, split)
	put(unrelated.ID, 0, doc("conversation", "/projects/two/chat.json"))
	put("now", 0, doc("conversation", target))
	get := func(pane string) []openElsewherePlace {
		t.Helper()
		w := request(b, "GET", "/api/engine/workspaces/"+current.ID+"/open-elsewhere?pane="+pane, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result struct {
			Places []openElsewherePlace `json:"places"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Places
	}
	index := b.workspaceOpenCache(b.workspaces)
	reads := 0
	original := index.read
	index.read = func(key string) (workspacestore.Record, error) { reads++; return original(key) }
	for i := 0; i < 3; i++ {
		got := get("chat")
		if len(got) != 1 || got[0].ID != other.ID || got[0].Name != "Marketing" {
			t.Fatal(got)
		}
	}
	if reads != 2 {
		t.Fatalf("unchanged candidate files decoded %d times", reads)
	}
	if got := get("unknown"); len(got) != 0 {
		t.Fatal(got)
	}
	// Closing a stored view removes the alias immediately, without deleting its history.
	closed := sharedDoc("new")
	closed["closed"] = []map[string]any{split["tabs"].([]map[string]any)[0]}
	put(other.ID, 1, closed)
	if got := get("chat"); len(got) != 0 {
		t.Fatal(got)
	}
	put(other.ID, 2, split)
	if got := get("chat"); len(got) != 1 {
		t.Fatal(got)
	}
	if _, err := graph.Rename(other.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if got := get("chat"); len(got) != 1 || got[0].Name != "Renamed" {
		t.Fatal(got)
	}
	// An unchanged signature is periodically revalidated, rather than trusted forever.
	hash, _ := durableConversation(workspaceConversationPane{Kind: "conversation", SessionFile: target})
	before := reads
	if !index.contains(other.ID, hash, time.Now().Add(time.Minute)) || reads != before+1 {
		t.Fatal("periodic verification missing", reads)
	}
	if w := request(b, "POST", "/api/engine/workspaces/"+current.ID+"/open-elsewhere?pane=chat", ""); w.Code != 405 {
		t.Fatal(w.Code)
	}
	// A removed external workspace file invalidates its cached projection.
	if err := os.Remove(filepath.Join(dir, other.ID+".json")); err != nil {
		t.Fatal(err)
	}
	if got := get("chat"); len(got) != 0 {
		t.Fatal(got)
	}
	// Membership or a task with the same path cannot authorize a conversation alias.
	put(current.ID, 1, doc("task", target))
	if got := get("chat"); len(got) != 0 {
		t.Fatal(got)
	}
	put(current.ID, 2, doc("inbox", target))
	if got := get("chat"); len(got) != 0 {
		t.Fatal(got)
	}
	index.retain(nil)
	if len(index.entries) != 0 {
		t.Fatal("removed graph entries retained")
	}
}

func TestOpenElsewhereCacheSeesJournalOnlyDestination(t *testing.T) {
	b, dir := newWorkspaceBridge(t)
	graph, err := placegraph.Open(placegraph.Options{Path: filepath.Join(t.TempDir(), "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	b.UsePlaces(NewPlaces(graph))
	current, _, err := graph.CreatePlace(placegraph.NewPlace{Name: "Current"})
	if err != nil {
		t.Fatal(err)
	}
	destination, _, err := graph.CreatePlace(placegraph.NewPlace{Name: "Marketing"})
	if err != nil {
		t.Fatal(err)
	}
	target := "/project/chat.jsonl"
	conversation := func(id string) map[string]any {
		d := sharedDoc(id)
		d["tabs"].([]map[string]any)[0]["sessionFile"] = target
		return d
	}
	for _, key := range []string{current.ID, "now"} {
		if w := request(b, "PUT", "/api/engine/workspaces/"+key, putBody(0, "writer", conversation("chat"))); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	get := func() []openElsewherePlace {
		t.Helper()
		w := request(b, "GET", "/api/engine/workspaces/"+current.ID+"/open-elsewhere?pane=chat", "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result struct {
			Places []openElsewherePlace `json:"places"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Places
	}
	if got := get(); len(got) != 0 {
		t.Fatal(got)
	} // warm a missing destination projection
	// This is the actual committed journal format at a crash before materializing
	// either file. The read-only bridge has no access to the store's fault seam.
	envelope := func(key string, revision uint64, doc any) map[string]any {
		return map[string]any{"schema": 1, "key": key, "revision": revision, "writer": "writer", "updatedAt": time.Now().UTC(), "workspace": doc}
	}
	journal := map[string]any{"schema": 1, "intent": "cache-pair", "binding": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "writer": "writer", "at": time.Now().UTC(), "source": envelope("now", 2, sharedDoc("fresh")), "destination": envelope(destination.ID, 1, conversation("moved")), "moved": map[string]string{"chat": destination.ID}}
	bytes, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pair-pending"), bytes, 0600); err != nil {
		t.Fatal(err)
	}
	if got := get(); len(got) != 1 || got[0].ID != destination.ID {
		t.Fatal("cached missing file hid the logical destination", got)
	}
	if _, err := os.Stat(filepath.Join(dir, destination.ID+".json")); !os.IsNotExist(err) {
		t.Fatal("read materialized destination", err)
	}
	// An ordinary writer recovers both halves first, then closes the actual view.
	if w := request(b, "PUT", "/api/engine/workspaces/"+destination.ID, putBody(1, "closer", sharedDoc("new"))); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := get(); len(got) != 0 {
		t.Fatal("closed materialized destination remained cached", got)
	}
}
