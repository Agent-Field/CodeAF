package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"
)

// NO TEST IN THIS PACKAGE TOUCHES THE NETWORK. Every address the package knows
// is a package variable, and every test points those variables at a server it
// started itself, so the laws here are proved against a stand-in that answers
// exactly what the test wants to reason about.

// fakeService starts a stand-in for the service and points the package's
// addresses at it for the duration of one test.
func fakeService(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	auth, token, gmail, calendar := googleAuthURL, googleTokenURL, gmailBaseURL, googleCalendarURL
	t.Cleanup(func() {
		googleAuthURL, googleTokenURL = auth, token
		gmailBaseURL, googleCalendarURL = gmail, calendar
	})
	googleAuthURL = server.URL + "/auth"
	googleTokenURL = server.URL + "/token"
	gmailBaseURL = server.URL + "/gmail/v1"
	googleCalendarURL = server.URL + "/calendar/v3"
	return server
}

// writeJSON answers a request with an encoded value, the way the real services
// do.
func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("write fake answer: %v", err)
	}
}

// testManager builds a manager over a fresh profile directory, with a client
// credential for Google so that the service is on the menu.
//
// THE REGISTRY IS NARROWED TO GOOGLE. The catalog (catalog.go) puts hundreds of
// services on the same registry, and a test about the browser half would
// otherwise be reasoning about a menu it did not write. The catalog has tests of
// its own, where the whole list is the subject.
func testManager(t *testing.T) (*Manager, string) {
	t.Helper()
	withPlugs(t, google{})
	directory := t.TempDir()
	manager, err := NewManager(directory, map[string]ClientCredential{
		"google": {ID: "client-id", Secret: "client-secret"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager, directory
}

// wholeGrant is everything the Google plug asks for, which is what a stored
// connection made by today's build carries. A test that writes an entry by hand
// uses it so that the entry reads as connected rather than as one made before
// this build's permissions grew.
func wholeGrant() []string { return (google{}).Service().Scopes }

// withPlugs replaces the registry for one test, so that ordering and filtering
// can be proved against a cast the test controls.
func withPlugs(t *testing.T, plugs ...Plug) {
	t.Helper()
	registryMu.Lock()
	saved := registry
	registry = append([]Plug(nil), plugs...)
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	})
}

// fakePlug is a service that exists only inside a test.
type fakePlug struct {
	service Service
	account string
}

func (f fakePlug) Service() Service                         { return f.service }
func (f fakePlug) Endpoint() oauth2.Endpoint                { return oauth2.Endpoint{} }
func (f fakePlug) AuthCodeOptions() []oauth2.AuthCodeOption { return nil }
func (f fakePlug) Account(context.Context, *http.Client) (string, error) {
	return f.account, nil
}
