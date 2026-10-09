package desktopbridge

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspacestore"
)

const (
	transferA = "now"
	transferB = "pl_0123456789abcdef"
)

func transferBody(intent string, srcRev, dstRev uint64, src, dst any) string {
	data, _ := json.Marshal(map[string]any{
		"intent": intent, "writer": "win-1", "destination": transferB,
		"sourceRevision": srcRev, "destinationRevision": dstRev,
		"sourceWorkspace": src, "destinationWorkspace": dst,
	})
	return string(data)
}

// seedTransfer saves A=[keep, mv] and B=[home] through the public routes.
func seedTransfer(t *testing.T, b *Bridge) {
	t.Helper()
	moving := sharedDoc("keep", "mv")
	moving["tabs"].([]map[string]any)[1]["draft"] = "unsent words"
	if w := request(b, "PUT", "/api/engine/workspaces/"+transferA, putBody(0, "seed", moving)); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := request(b, "PUT", "/api/engine/workspaces/"+transferB, putBody(0, "seed", sharedDoc("home"))); w.Code != 200 {
		t.Fatal(w.Body)
	}
}

func movedDocs() (map[string]any, map[string]any) {
	dst := sharedDoc("home", "mv")
	dst["tabs"].([]map[string]any)[1]["draft"] = "unsent words"
	return sharedDoc("keep"), dst
}

func TestTransferReturnsBothRecordsKeepsSavedStateAndAcknowledgesReplays(t *testing.T) {
	b, _ := newWorkspaceBridge(t)
	seedTransfer(t, b)
	src, dst := movedDocs()
	w := request(b, "POST", "/api/engine/workspaces/"+transferA+"/transfer", transferBody("move-1", 1, 1, src, dst))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got struct {
		Intent      string                `json:"intent"`
		Source      workspacestore.Record `json:"source"`
		Destination workspacestore.Record `json:"destination"`
		Already     bool                  `json:"already"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Intent != "move-1" || got.Already || got.Source.Key != transferA || got.Destination.Key != transferB ||
		got.Source.Revision != 2 || got.Destination.Revision != 2 || got.Source.Writer != "win-1" || got.Source.UpdatedAt == nil {
		t.Fatalf("shape: %+v", got)
	}
	if !strings.Contains(string(got.Destination.Workspace), "unsent words") || got.Source.MovedTo["mv"] != transferB {
		t.Fatalf("saved state and relocation: %s / %v", got.Destination.Workspace, got.Source.MovedTo)
	}
	// What GET reads is what the route acknowledged, including movedTo.
	read := decodeRecord(t, request(b, "GET", "/api/engine/workspaces/"+transferA, ""))
	if read.Revision != 2 || read.MovedTo["mv"] != transferB {
		t.Fatalf("GET after transfer: %+v", read)
	}

	again := request(b, "POST", "/api/engine/workspaces/"+transferA+"/transfer", transferBody("move-1", 1, 1, src, dst))
	var ack struct {
		Already bool                  `json:"already"`
		Source  workspacestore.Record `json:"source"`
	}
	_ = json.Unmarshal(again.Body.Bytes(), &ack)
	if again.Code != 200 || !ack.Already || ack.Source.Revision != 2 {
		t.Fatalf("replay: %d %s", again.Code, again.Body)
	}

	reused := request(b, "POST", "/api/engine/workspaces/"+transferA+"/transfer", transferBody("move-1", 1, 1, src, sharedDoc("home", "mv", "other")))
	var refusal struct{ Code, Error string }
	_ = json.Unmarshal(reused.Body.Bytes(), &refusal)
	if reused.Code != 409 || refusal.Code != "intent_changed" || refusal.Error == "" {
		t.Fatalf("reused intent: %d %s", reused.Code, reused.Body)
	}
}

func TestTransferConflictCarriesBothCurrentRecordsAndChangesNothing(t *testing.T) {
	b, dir := newWorkspaceBridge(t)
	seedTransfer(t, b)
	if w := request(b, "PUT", "/api/engine/workspaces/"+transferB, putBody(1, "win-2", sharedDoc("home", "late"))); w.Code != 200 {
		t.Fatal(w.Body)
	}
	src, dst := movedDocs()
	w := request(b, "POST", "/api/engine/workspaces/"+transferA+"/transfer", transferBody("move-1", 1, 1, src, dst))
	var refusal struct {
		Code, Error string
		Source      workspacestore.Record `json:"source"`
		Destination workspacestore.Record `json:"destination"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &refusal)
	if w.Code != 409 || refusal.Code != "conflict" || refusal.Error == "" || refusal.Source.Revision != 1 ||
		refusal.Destination.Revision != 2 || !strings.Contains(string(refusal.Destination.Workspace), `"late"`) {
		t.Fatalf("conflict shape: %d %s", w.Code, w.Body)
	}
	if a := decodeRecord(t, request(b, "GET", "/api/engine/workspaces/"+transferA, "")); a.Revision != 1 {
		t.Fatalf("a refused transfer changes neither side: %+v", a)
	}
	if _, err := os.Stat(dir + "/.pair-pending"); err == nil {
		t.Fatal("no journal for a refused transfer")
	}
	// Rebased retry under the same intent.
	w = request(b, "POST", "/api/engine/workspaces/"+transferA+"/transfer", transferBody("move-1", 1, 2, src, sharedDoc("home", "late", "mv")))
	if w.Code != 200 {
		t.Fatalf("rebased retry: %d %s", w.Code, w.Body)
	}
}

func TestTransferRefusalsAreSentencesWithCodes(t *testing.T) {
	b, dir := newWorkspaceBridge(t)
	local := sharedDoc("a")
	local["scroll"] = map[string]any{"a": 4}
	good := sharedDoc("a")
	cases := []struct {
		name, path, body string
		status           int
		code             string
	}{
		{"window-local field", "now", transferBody("i1", 0, 0, local, good), 400, "invalid"},
		{"bad intent", "now", transferBody("not ok!", 0, 0, good, good), 400, "invalid"},
		{"same place", "now", strings.Replace(transferBody("i1", 0, 0, good, good), transferB, "now", 1), 400, "invalid"},
		{"bad destination", "now", strings.Replace(transferBody("i1", 0, 0, good, good), transferB, "../x", 1), 404, "unknown_key"},
		{"bad source", "pl_zz", transferBody("i1", 0, 0, good, good), 404, "unknown_key"},
	}
	for _, c := range cases {
		w := request(b, "POST", "/api/engine/workspaces/"+c.path+"/transfer", c.body)
		var got map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &got)
		if w.Code != c.status || got["code"] != c.code || got["error"] == "" {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	if w := request(b, "POST", "/api/engine/workspaces/now/transfer", `{"intent":"i","extra":1}`); w.Code != 400 {
		t.Errorf("unknown fields are refused: %d", w.Code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("refusals touch nothing: %d entries", len(entries))
	}
}

func TestTransferNeedsPOSTTheTokenAndANativeOrigin(t *testing.T) {
	b, dir := newWorkspaceBridge(t)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		if w := request(b, method, "/api/engine/workspaces/now/transfer", ""); w.Code != 405 {
			t.Errorf("%s: %d, want 405", method, w.Code)
		}
	}
	body := transferBody("i1", 0, 0, sharedDoc("a"), sharedDoc("b"))
	r := httptest.NewRequest("POST", "/api/engine/workspaces/now/transfer", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
	r = httptest.NewRequest("POST", "/api/engine/workspaces/now/transfer", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("foreign origin: %d", w.Code)
	}
	r = httptest.NewRequest("POST", "/api/engine/workspaces/now/transfer", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Origin", "tauri://localhost")
	w = httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("the native origin with the token reaches the route: %d %s", w.Code, w.Body)
	}
	if entries, _ := os.ReadDir(dir); len(entries) == 0 {
		t.Fatal("and the accepted transfer is saved")
	}
}
