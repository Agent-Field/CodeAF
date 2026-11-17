package serve_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestFactoryNameIsAdditiveContractOneOnEveryResponse(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"Agent-Field/codeaf", "other/repo"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "Our team"} {
		cfg := config
		cfg.Store, cfg.Name = st, name
		want := factory.DisplayName(name, []string{"Agent-Field/codeaf"})
		h := serve.New(factory.FixtureSeam(now), cfg)
		t.Cleanup(h.Close)
		for _, path := range []string{"/v1/about", "/v1/me", "/v1/queue", "/v1/jobs/2", "/v1/missing"} {
			r := httptest.NewRequest("GET", path, nil)
			r.RemoteAddr = "127.0.0.1:1234"
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			var body struct {
				Factory api.Factory `json:"factory"`
				Name    string      `json:"name"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Factory.Name != want || body.Factory.Address != cfg.Address || body.Factory.Version != cfg.Version || api.Contract != 2 {
				t.Fatalf("%s: %s", path, w.Body)
			}
			if path == "/v1/about" && body.Name != want {
				t.Fatalf("preview name = %q, want %q", body.Name, want)
			}
		}
	}
	if err := st.SetRepos(nil); err != nil {
		t.Fatal(err)
	}
	cfg := config
	cfg.Store = st
	h := serve.New(factory.FixtureSeam(now), cfg)
	defer h.Close()
	r := httptest.NewRequest("GET", "/v1/about", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var body struct {
		Factory api.Factory `json:"factory"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Factory.Name != factory.DisplayName("", nil) {
		t.Fatalf("no repositories = %s %v", w.Body, err)
	}
}
