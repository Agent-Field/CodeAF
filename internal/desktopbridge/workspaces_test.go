package desktopbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspacestore"
)

// newWorkspaceBridge is a bridge with only the tab-set store attached; it never
// opens a conversation, so it can never call a model.
func newWorkspaceBridge(t *testing.T) (*Bridge, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "workspaces")
	store, err := workspacestore.Open(workspacestore.Options{Dir: dir, WatchInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	b := New(testToken, func(string) (Connection, error) {
		t.Fatal("the workspace routes must never open an engine")
		return Connection{}, nil
	})
	t.Cleanup(b.Close)
	b.UseWorkspaces(store)
	return b, dir
}

func sharedDoc(ids ...string) map[string]any {
	tabs := make([]map[string]any, len(ids))
	for i, id := range ids {
		tabs[i] = map[string]any{"id": id, "title": "Tab " + id, "draft": "", "pinned": false, "kind": "conversation"}
	}
	return map[string]any{"schema": 1, "tabs": tabs, "groups": []any{}, "closed": []any{}, "nextNumber": len(ids) + 1}
}

func putBody(revision uint64, writer string, doc any) string {
	data, _ := json.Marshal(map[string]any{"revision": revision, "writer": writer, "workspace": doc})
	return string(data)
}

func decodeRecord(t *testing.T, w *httptest.ResponseRecorder) workspacestore.Record {
	t.Helper()
	var rec workspacestore.Record
	if err := json.Unmarshal(w.Body.Bytes(), &rec); err != nil {
		t.Fatalf("%d %s: %v", w.Code, w.Body.String(), err)
	}
	return rec
}

func TestWorkspaceRoundTrip(t *testing.T) {
	b, _ := newWorkspaceBridge(t)
	w := request(b, "GET", "/api/engine/workspaces/now", "")
	if w.Code != 200 {
		t.Fatalf("an unsaved key reads as revision 0, not a 404: %d %s", w.Code, w.Body)
	}
	if rec := decodeRecord(t, w); rec.Revision != 0 || rec.Workspace != nil && string(rec.Workspace) != "null" {
		t.Fatalf("empty record: %+v", rec)
	}
	w = request(b, "PUT", "/api/engine/workspaces/now", putBody(0, "win-a", sharedDoc("a", "b")))
	if w.Code != 200 || decodeRecord(t, w).Revision != 1 {
		t.Fatalf("first write: %d %s", w.Code, w.Body)
	}
	w = request(b, "GET", "/api/engine/workspaces/now", "")
	rec := decodeRecord(t, w)
	if rec.Revision != 1 || rec.Writer != "win-a" || !strings.Contains(string(rec.Workspace), `"id":"b"`) {
		t.Fatalf("read back: %s", w.Body)
	}
}

func TestAStaleRevisionIs409WithTheCurrentDocument(t *testing.T) {
	b, _ := newWorkspaceBridge(t)
	request(b, "PUT", "/api/engine/workspaces/now", putBody(0, "win-a", sharedDoc("a")))
	request(b, "PUT", "/api/engine/workspaces/now", putBody(1, "win-a", sharedDoc("a", "b")))
	w := request(b, "PUT", "/api/engine/workspaces/now", putBody(1, "win-b", sharedDoc("a", "x")))
	if w.Code != http.StatusConflict {
		t.Fatalf("a stale write must be 409: %d %s", w.Code, w.Body)
	}
	var refusal struct {
		Error   string                `json:"error"`
		Code    string                `json:"code"`
		Current workspacestore.Record `json:"current"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &refusal); err != nil {
		t.Fatal(err)
	}
	if refusal.Code != "conflict" || refusal.Current.Revision != 2 || !strings.Contains(string(refusal.Current.Workspace), `"id":"b"`) || strings.Contains(string(refusal.Current.Workspace), `"id":"x"`) {
		t.Fatalf("the refusal must carry the current document and nothing of the refused one: %s", w.Body)
	}
}

func TestWorkspacesNeedTheTokenAndANativeOrigin(t *testing.T) {
	b, dir := newWorkspaceBridge(t)
	r := httptest.NewRequest("PUT", "/api/engine/workspaces/now", strings.NewReader(putBody(0, "w", sharedDoc("a"))))
	r.Host = "127.0.0.1:1420"
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/engine/workspaces/now", nil)
	r.Host = "127.0.0.1:1420"
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("a foreign origin with a token is refused: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/api/engine/workspaces/now", nil)
	r.Host = "127.0.0.1:1420"
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Origin", "tauri://localhost")
	w = httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("the native origin reads: %d %s", w.Code, w.Body)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("refused requests and reads must not touch the disk: %d entries", len(entries))
	}
}

func TestWorkspacesAreAbsentUntilAttached(t *testing.T) {
	b := New(testToken, func(string) (Connection, error) { return Connection{}, fmt.Errorf("no engine") })
	t.Cleanup(b.Close)
	if w := request(b, "GET", "/api/engine/workspaces/now", ""); w.Code != 404 {
		t.Fatalf("no store, no route: %d", w.Code)
	}
}

func TestUnknownKeysAndMethodsAreRefusedWithoutTouchingTheDisk(t *testing.T) {
	b, dir := newWorkspaceBridge(t)
	for _, path := range []string{"/workspaces/..%2Fnow", "/workspaces/../now", "/workspaces/NOW", "/workspaces/pl_xyz", "/workspaces/now/extra", "/workspaces/"} {
		if w := request(b, "PUT", "/api/engine"+path, putBody(0, "w", sharedDoc("a"))); w.Code != 404 {
			t.Errorf("PUT %s: %d, want 404", path, w.Code)
		}
	}
	if w := request(b, "POST", "/api/engine/workspaces/now", putBody(0, "w", sharedDoc("a"))); w.Code != 405 {
		t.Errorf("POST: %d, want 405", w.Code)
	}
	if w := request(b, "DELETE", "/api/engine/workspaces/now", ""); w.Code != 405 {
		t.Errorf("DELETE: %d, want 405", w.Code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("nothing may be written: %d entries", len(entries))
	}
}

func TestRefusalsAreSentencesWithCodes(t *testing.T) {
	b, _ := newWorkspaceBridge(t)
	local := sharedDoc("a")
	local["activeId"] = "a"
	cases := []struct {
		name, body string
		status     int
		code       string
	}{
		{"window-local field", putBody(0, "w", local), 400, "invalid"},
		{"not an object", putBody(0, "w", []int{1}), 400, "invalid"},
		{"bad writer", putBody(0, "has space", sharedDoc("a")), 400, "invalid"},
		{"too large", putBody(0, "w", map[string]any{"schema": 1, "tabs": []any{map[string]any{"id": "a", "title": "", "pinned": false, "draft": strings.Repeat("x", workspacestore.MaxDocumentBytes)}}, "groups": []any{}, "closed": []any{}, "nextNumber": 2}), 413, ""},
	}
	for _, c := range cases {
		w := request(b, "PUT", "/api/engine/workspaces/now", c.body)
		var refusal struct{ Error, Code string }
		_ = json.Unmarshal(w.Body.Bytes(), &refusal)
		if w.Code != c.status || refusal.Error == "" || (c.code != "" && refusal.Code != c.code) || strings.Contains(refusal.Error, "workspacestore") {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
}

func TestALongPollAnswersWithTheNextRevision(t *testing.T) {
	b, _ := newWorkspaceBridge(t)
	request(b, "PUT", "/api/engine/workspaces/now", putBody(0, "win-a", sharedDoc("a")))
	got := make(chan *httptest.ResponseRecorder, 1)
	go func() { got <- request(b, "GET", "/api/engine/workspaces/now?after=1&wait=1", "") }()
	time.Sleep(40 * time.Millisecond)
	request(b, "PUT", "/api/engine/workspaces/now", putBody(1, "win-b", sharedDoc("a", "b")))
	select {
	case w := <-got:
		if rec := decodeRecord(t, w); rec.Revision != 2 || rec.Writer != "win-b" {
			t.Fatalf("the waiter sees the new revision: %s", w.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the long poll did not wake on a write")
	}
	if w := request(b, "GET", "/api/engine/workspaces/now?after=1&wait=1", ""); decodeRecord(t, w).Revision != 2 {
		t.Fatalf("a waiter already behind answers at once: %s", w.Body)
	}
	if w := request(b, "GET", "/api/engine/workspaces/now?after=x&wait=1", ""); w.Code != 400 {
		t.Fatalf("a bad cursor is 400: %d", w.Code)
	}
}

// Two windows write through the real HTTP handler at once, each retrying over
// the document its 409 carried. Every tab either window opened must survive.
func TestTwoWindowsWritingAtOnceLoseNothing(t *testing.T) {
	b, _ := newWorkspaceBridge(t)
	server := httptest.NewServer(b)
	defer server.Close()
	do := func(method, body string) (int, []byte) {
		req, _ := http.NewRequest(method, server.URL+"/api/engine/workspaces/now", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return 0, nil
		}
		defer res.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(res.Body)
		return res.StatusCode, buf.Bytes()
	}
	do("PUT", putBody(0, "seed", sharedDoc("seed")))
	const each = 15
	var wg sync.WaitGroup
	for _, win := range []string{"win-a", "win-b"} {
		wg.Add(1)
		go func(win string) {
			defer wg.Done()
			var base workspacestore.Record
			_, body := do("GET", "")
			_ = json.Unmarshal(body, &base)
			for i := 0; i < each; i++ {
				for attempt := 0; ; attempt++ {
					if attempt > 200 {
						t.Errorf("%s: no progress", win)
						return
					}
					var doc struct {
						Tabs []map[string]any `json:"tabs"`
					}
					_ = json.Unmarshal(base.Workspace, &doc)
					ids := []string{}
					for _, tab := range doc.Tabs {
						ids = append(ids, tab["id"].(string))
					}
					ids = append(ids, fmt.Sprintf("%s-%d", win, i))
					code, body := do("PUT", putBody(base.Revision, win, sharedDoc(ids...)))
					if code == 200 {
						_ = json.Unmarshal(body, &base)
						break
					}
					if code != 409 {
						t.Errorf("%s: %d %s", win, code, body)
						return
					}
					var refusal struct {
						Current workspacestore.Record `json:"current"`
					}
					_ = json.Unmarshal(body, &refusal)
					base = refusal.Current
				}
			}
		}(win)
	}
	wg.Wait()
	_, body := do("GET", "")
	var final workspacestore.Record
	_ = json.Unmarshal(body, &final)
	for _, win := range []string{"win-a", "win-b"} {
		for i := 0; i < each; i++ {
			if !strings.Contains(string(final.Workspace), fmt.Sprintf(`"%s-%d"`, win, i)) {
				t.Errorf("lost %s-%d", win, i)
			}
		}
	}
}

// The renderer's client parses what these routes really send. The fixtures are
// written from the handler's own answers; set CODEAF_UPDATE_FIXTURES=1 to
// rewrite them after a deliberate wire change.
func TestWorkspaceWireFixtures(t *testing.T) {
	b, _ := newWorkspaceBridge(t)
	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	scrub := func(raw []byte) []byte {
		var tree any
		if err := json.Unmarshal(raw, &tree); err != nil {
			t.Fatal(err)
		}
		var walk func(any)
		walk = func(v any) {
			if m, ok := v.(map[string]any); ok {
				if _, ok := m["updatedAt"]; ok {
					m["updatedAt"] = at.Format(time.RFC3339)
				}
				for _, c := range m {
					walk(c)
				}
			}
		}
		walk(tree)
		pretty, _ := json.MarshalIndent(tree, "", "  ")
		return append(pretty, '\n')
	}
	got := map[string][]byte{}
	got["empty"] = scrub(request(b, "GET", "/api/engine/workspaces/now", "").Body.Bytes())
	got["saved"] = scrub(request(b, "PUT", "/api/engine/workspaces/now", putBody(0, "win-a", sharedDoc("a"))).Body.Bytes())
	got["conflict"] = scrub(request(b, "PUT", "/api/engine/workspaces/now", putBody(0, "win-b", sharedDoc("b"))).Body.Bytes())
	got["invalid"] = scrub(request(b, "PUT", "/api/engine/workspaces/now", putBody(1, "win-b", map[string]any{"schema": 1})).Body.Bytes())
	dir := filepath.Join("..", "..", "desktop", "src", "features", "workspace-sync", "fixtures")
	for name, data := range got {
		path := filepath.Join(dir, name+".json")
		if os.Getenv("CODEAF_UPDATE_FIXTURES") != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(want, data) {
			t.Errorf("%s drifted from %s (rerun with CODEAF_UPDATE_FIXTURES=1):\n%s", name, path, data)
		}
	}
}
