package serve_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

func intakeHandler(t *testing.T) (*serve.Handler, *store.Store, string) {
	t.Helper()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tokens.json")
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	row.Expires = now.Add(24 * time.Hour)
	if err := tokens.Save(path, []tokens.Token{row}); err != nil {
		t.Fatal(err)
	}
	h := serve.New(factory.FixtureSeam(now), serve.Config{Store: st, AccessToken: "floor-secret", TokensPath: path})
	t.Cleanup(h.Close)
	return h, st, row.Token
}

func postIntake(h http.Handler, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, serve.IntakePath, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:9"
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestIntakeFilesOutsideWorkOnceWithTheAccessToken(t *testing.T) {
	h, st, _ := intakeHandler(t)
	body := `{"source":"inbox:ops","external_id":"A1","title":"Fix the login timeout","priority":"p0","labels":["bug"],"url":"https://ops.example.com/A1"}`
	w := postIntake(h, "floor-secret", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", w.Code, w.Body.String())
	}
	var first inbox.Result
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || !first.Created || first.ID < 1 {
		t.Fatalf("first = %s, %v", w.Body.String(), err)
	}
	w = postIntake(h, "floor-secret", body)
	var again inbox.Result
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &again) != nil || again.Created || again.ID != first.ID {
		t.Fatalf("again = %d %s", w.Code, w.Body.String())
	}
	it, err := st.Get(first.ID)
	if err != nil || it.SourceRef != "inbox:ops:A1" || it.Priority == nil || it.Priority.Value != "urgent" {
		t.Fatalf("job = %+v, %v", it, err)
	}
}

func TestIntakeRefusals(t *testing.T) {
	h, _, teammate := intakeHandler(t)
	good := `{"source":"inbox:ops","external_id":"A1","title":"t"}`
	for _, c := range []struct {
		name, token, body string
		code              int
		want              string
	}{
		{"no credential", "", good, http.StatusUnauthorized, "bearer"},
		{"a teammate's token", teammate, good, http.StatusForbidden, "only the operator"},
		{"no title", "floor-secret", `{"source":"inbox:ops","external_id":"A1"}`, http.StatusUnprocessableEntity, "title is required"},
		{"github", "floor-secret", `{"source":"github","external_id":"1","title":"t"}`, http.StatusUnprocessableEntity, "factory's own"},
		{"unknown field", "floor-secret", `{"source":"inbox:ops","external_id":"A1","title":"t","num":4}`, http.StatusUnprocessableEntity, "unknown field"},
		{"too large", "floor-secret", `{"source":"inbox:ops","external_id":"A1","title":"t","body":"` + strings.Repeat("x", inbox.MaxRequest) + `"}`, http.StatusRequestEntityTooLarge, "64 KiB"},
	} {
		w := postIntake(h, c.token, c.body)
		if w.Code != c.code || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s = %d %s, want %d %q", c.name, w.Code, w.Body.String(), c.code, c.want)
		}
	}
	r := httptest.NewRequest(http.MethodGet, serve.IntakePath, nil)
	r.RemoteAddr = "192.0.2.1:9"
	r.Header.Set("Authorization", "Bearer floor-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d", w.Code)
	}
}
