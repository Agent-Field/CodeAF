package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

// Contract C12 of the org-follow lane (NOTES.md): registering a runner follows
// loopback_trust like every other door. Where the setting is off (a team factory
// by default) a runner needs a runner credential, the one a remote runner uses
// (the host's access token, or a signed-in person's session), even from the
// server's own computer, and the factory's own person never stands in for a
// runner that names no handle. Where it is on (a solo factory by default, or a
// team factory that sets it) nothing changes.
func TestContractC12WorkerAttachFollowsLoopbackTrust(t *testing.T) {
	const floor, local, remote = "floor-secret", "127.0.0.1:4321", "192.0.2.7:4321"
	person, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	person.Expires = time.Now().Add(time.Hour)
	for _, tc := range []struct {
		name    string
		mode    string
		trust   *bool
		peer    string
		bearer  string
		handle  string
		headers map[string]string
		want    int
	}{
		{"team, no setting, no credential from this computer", api.FactoryTeam, nil, local, "", "ci-1", nil, http.StatusUnauthorized},
		{"team, no setting, the host token from this computer, a handle", api.FactoryTeam, nil, local, floor, "ci-1", nil, http.StatusCreated},
		{"team, no setting, the host token from this computer, no handle", api.FactoryTeam, nil, local, floor, "", nil, http.StatusUnprocessableEntity},
		{"team, no setting, a person token with no sign-in from this computer", api.FactoryTeam, nil, local, person.Token, "ci-1", nil, http.StatusForbidden},
		{"team, no setting, the host token from another machine, a handle", api.FactoryTeam, nil, remote, floor, "ci-1", nil, http.StatusCreated},
		{"team, no setting, no credential from another machine", api.FactoryTeam, nil, remote, "", "ci-1", nil, http.StatusUnauthorized},
		{"team, loopback_trust true, no credential from this computer", api.FactoryTeam, boolPtr(true), local, "", "ci-1", nil, http.StatusCreated},
		{"team, loopback_trust true, a person token with no sign-in from this computer", api.FactoryTeam, boolPtr(true), local, person.Token, "ci-1", nil, http.StatusCreated},
		{"team, loopback_trust true, forwarded for a remote client", api.FactoryTeam, boolPtr(true), local, "", "ci-1", map[string]string{"X-Forwarded-For": "203.0.113.9"}, http.StatusUnauthorized},
		{"solo, no setting, no credential, no handle", api.FactorySolo, nil, local, "", "", nil, http.StatusCreated},
		{"solo, loopback_trust false, no credential", api.FactorySolo, boolPtr(false), local, "", "ci-1", nil, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "tokens.json")
			if err := tokens.Save(path, []tokens.Token{person}); err != nil {
				t.Fatal(err)
			}
			h := New(factory.Seam{}, Config{Mode: tc.mode, LoopbackTrust: tc.trust, AccessToken: floor, TokensPath: path, Store: st, PullWait: time.Millisecond})
			t.Cleanup(h.Close)
			body, err := json.Marshal(api.WorkerAttach{Name: "laptop", Kind: api.RunnerThisMac, Handle: tc.handle})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "/v1/workers/attach", bytes.NewReader(body))
			r.RemoteAddr, r.Host = tc.peer, "factory.test"
			if tc.bearer != "" {
				r.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			for name, value := range tc.headers {
				r.Header.Set(name, value)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("attach = %d %s, want %d", w.Code, w.Body.String(), tc.want)
			}
		})
	}
}
