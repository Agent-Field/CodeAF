package serve

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE REPOSITORY SWITCH CARRIES THE PHASE CONTRACT TOO: a refusal made before
// SetRepos says so, and a failure after the list was stored does not. The
// route is exactly PUT /v1/repos/{owner}/{repo}; the whole-list door is not
// marked, because its handler does not clear the marker.
func TestRepoSwitchMarksRefusalsMadeBeforeItsFirstChange(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	marked := func(h *Handler, method, path, body, token string) (int, string) {
		w := identityRequest(h, method, path, body, token, nil)
		return w.Code, w.Header().Get(api.PhaseHeader)
	}
	for name, c := range map[string]struct {
		method, path, body, token string
		status                    int
		want                      string
	}{
		"no credential":       {"PUT", "/v1/repos/acme/api", `{"on":true}`, "", 401, api.PhasePreMutation},
		"unknown credential":  {"PUT", "/v1/repos/acme/api", `{"on":true}`, "no-such-token", 401, api.PhasePreMutation},
		"bad repo name":       {"PUT", "/v1/repos/acme/a%20b", `{"on":true}`, "operator-secret", 422, api.PhasePreMutation},
		"on missing":          {"PUT", "/v1/repos/acme/api", `{}`, "operator-secret", 422, api.PhasePreMutation},
		"unknown field":       {"PUT", "/v1/repos/acme/api", `{"on":true,"path":"/x"}`, "operator-secret", 422, api.PhasePreMutation},
		"not an object":       {"PUT", "/v1/repos/acme/api", `true`, "operator-secret", 422, api.PhasePreMutation},
		"turned on":           {"PUT", "/v1/repos/acme/api", `{"on":true}`, "operator-secret", 200, ""},
		"turned off":          {"PUT", "/v1/repos/acme/api", `{"on":false}`, "operator-secret", 200, ""},
		"whole list not":      {"PUT", "/v1/repos", `{"watched":["acme/api"]}`, "operator-secret", 200, ""},
		"read is not marked":  {"GET", "/v1/repos", "", "operator-secret", -1, ""},
		"deeper path is not":  {"PUT", "/v1/repos/acme/api/pipeline", `{"on":true}`, "operator-secret", -1, ""},
		"wrong verb not":      {"POST", "/v1/repos/acme/api", `{"on":true}`, "operator-secret", -1, ""},
		"missing name is not": {"PUT", "/v1/repos/acme/", `{"on":true}`, "operator-secret", -1, ""},
	} {
		t.Run(name, func(t *testing.T) {
			status, got := marked(f.h, c.method, c.path, c.body, c.token)
			if c.status > 0 && status != c.status {
				t.Fatalf("status %d, want %d", status, c.status)
			}
			if got != c.want {
				t.Fatalf("%s = %q, want %q (status %d)", api.PhaseHeader, got, c.want, status)
			}
		})
	}

	// A factory with no way to save the list refuses before it changes anything.
	seam := f.seam
	seam.SetRepos = nil
	bare := New(seam, f.cfg)
	t.Cleanup(bare.Close)
	if status, got := marked(bare, "PUT", "/v1/repos/acme/api", `{"on":true}`, "operator-secret"); status != http.StatusNotImplemented || got != api.PhasePreMutation {
		t.Fatalf("no SetRepos = %d %q", status, got)
	}
}

// A SAVE THAT FAILS, OR THAT FAILS AFTER STORING, IS NOT A REFUSAL: the marker
// is gone by then, so a client must treat the answer as uncertain.
func TestRepoSwitchFailureAfterTheListWasStoredCarriesNoMarker(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	seam := f.seam
	stored := seam.SetRepos
	seam.SetRepos = func(repos []string) error {
		if err := stored(repos); err != nil {
			return err
		}
		return errors.New("the list was saved, then something else failed")
	}
	h := New(seam, f.cfg)
	t.Cleanup(h.Close)
	w := identityRequest(h, "PUT", "/v1/repos/acme/api", `{"on":true}`, "operator-secret", nil)
	if w.Code < 400 || w.Header().Get(api.PhaseHeader) != "" {
		t.Fatalf("failure after the store = %d %q", w.Code, w.Header().Get(api.PhaseHeader))
	}
	repos, err := f.st.Repos()
	if err != nil || len(repos) != 1 || repos[0] != "acme/api" {
		t.Fatalf("the list was not stored: %v %v", repos, err)
	}
}
