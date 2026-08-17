package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// keyManager builds a manager whose only service is one keyed plug, answering
// at a server the test started. NOTHING HERE TOUCHES THE NETWORK.
func keyManager(t *testing.T, handler http.Handler, shape func(*keyPlug)) (*Manager, *keyPlug, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	plug := &keyPlug{
		service: Service{ID: "example", Name: "Example", Auth: AuthKey, Address: server.URL, Blurb: "Reach it with a key."},
		base:    server.URL,
		carry:   carry{kind: carryHeader, name: "X-Api-Key"},
	}
	if shape != nil {
		shape(plug)
	}
	withPlugs(t, plug)
	directory := t.TempDir()
	manager, err := NewManager(directory, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager, plug, directory
}

// A key service needs no client credential to be offered: the person's own key
// is the whole of what it takes.
func TestAKeyServiceIsOfferedWithNoCredential(t *testing.T) {
	manager, _, _ := keyManager(t, http.NotFoundHandler(), nil)
	services := manager.Services()
	if len(services) != 1 || services[0].ID != "example" || services[0].Connected {
		t.Fatalf("Services() = %+v", services)
	}
	if services[0].Auth != AuthKey {
		t.Errorf("Auth = %q", services[0].Auth)
	}
}

func TestConnectKeyWritesTheEntryAndReadsItBack(t *testing.T) {
	var seen string
	manager, _, directory := keyManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Api-Key")
		writeJSON(t, w, map[string]string{"hello": "there"})
	}), nil)

	status, err := manager.ConnectKey(context.Background(), "example", "  sk-secret  ")
	if err != nil {
		t.Fatalf("ConnectKey: %v", err)
	}
	if !status.Connected {
		t.Errorf("status = %+v", status)
	}
	// THE EMPTINESS LAW: a key says nothing about whose key it is.
	if status.Account != "" {
		t.Errorf("Account = %q, want nothing", status.Account)
	}
	if !manager.Connected("example") {
		t.Errorf("the service must read as connected")
	}

	entry, ok, err := manager.store.get("example")
	if err != nil || !ok {
		t.Fatalf("get: %v ok=%v", err, ok)
	}
	if entry.Auth != AuthKey || entry.Key != "sk-secret" {
		t.Errorf("entry = %+v", entry)
	}

	// The file is the same file, at the same mode, with the same shape.
	path := filepath.Join(directory, StoreFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var entries map[string]map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("the file must stay one object keyed by service: %v", err)
	}
	if entries["example"]["auth"] != "key" {
		t.Errorf("file = %s", raw)
	}

	// And the key is on the wire.
	client, err := manager.Client(context.Background(), "example")
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if _, err := ServiceRequest(context.Background(), client, Service{Name: "Example", Address: manager.plugs[0].Service().Address}, "GET", "/anything", "", ""); err != nil {
		t.Fatalf("ServiceRequest: %v", err)
	}
	if seen != "sk-secret" {
		t.Errorf("the key did not ride on the request, header was %q", seen)
	}

	// Disconnect works unchanged, and leaves the service on the menu.
	if err := manager.Disconnect("example"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if manager.Connected("example") {
		t.Errorf("still connected after Disconnect")
	}
	if services := manager.Services(); len(services) != 1 || services[0].Connected {
		t.Errorf("Services() = %+v", services)
	}
}

// An entry a Google connection wrote goes on being read as a Google connection.
func TestALegacyEntryIsStillABrowserConnection(t *testing.T) {
	entry := stored{Account: "me@example.com"}
	if entry.Auth != "" {
		t.Fatalf("an entry with no word for it says nothing")
	}
	if entry.usable() {
		t.Errorf("a browser entry with no keys is not usable")
	}
	keyed := stored{Auth: AuthKey, Key: "k"}
	if !keyed.usable() {
		t.Errorf("a key entry with a key is usable")
	}
	if (stored{Auth: AuthKey}).usable() {
		t.Errorf("a key entry with no key is not usable")
	}
}

func TestTheProbeRefusesABadKeyAndStoresNothing(t *testing.T) {
	manager, plug, _ := keyManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"that key is not one of ours"}}`))
			return
		}
		writeJSON(t, w, map[string]bool{"ok": true})
	}), nil)
	plug.probe = probe{address: plug.base + "/me", accepts: []int{200}}

	if _, err := manager.ConnectKey(context.Background(), "example", "bad"); err == nil {
		t.Fatalf("a key the service refused must fail here")
	} else if !strings.Contains(err.Error(), "that key is not one of ours") {
		t.Errorf("the service's own sentence must survive, got %q", err)
	}
	if _, ok, _ := manager.store.get("example"); ok {
		t.Errorf("nothing may be written down for a key that was refused")
	}

	if _, err := manager.ConnectKey(context.Background(), "example", "good"); err != nil {
		t.Fatalf("ConnectKey with a good key: %v", err)
	}
	if !manager.Connected("example") {
		t.Errorf("the service must read as connected")
	}
}

// A service the catalog names no cheap read for is believed, and the first real
// call is where a bad key shows up.
func TestAServiceWithNothingToProveIsBelieved(t *testing.T) {
	manager, _, _ := keyManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("nothing may be called when there is nothing to prove: %s", r.URL)
	}), nil)
	if _, err := manager.ConnectKey(context.Background(), "example", "anything"); err != nil {
		t.Fatalf("ConnectKey: %v", err)
	}
}

func TestConnectKeyRefusesWhatItCannotUse(t *testing.T) {
	manager, plug, _ := keyManager(t, http.NotFoundHandler(), nil)

	if _, err := manager.ConnectKey(context.Background(), "example", "   "); err == nil {
		t.Errorf("an empty answer must be refused")
	}
	if _, err := manager.ConnectKey(context.Background(), "nothing-like-this", "k"); err == nil {
		t.Errorf("an unknown service must be refused")
	}
	// And a browser service is not connected with a key.
	withPlugs(t, google{})
	browser, err := NewManager(t.TempDir(), map[string]ClientCredential{"google": {ID: "i", Secret: "s"}})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if _, err := browser.ConnectKey(context.Background(), "google", "k"); err == nil {
		t.Errorf("Google is not connected with a key")
	} else if strings.Contains(strings.ToLower(err.Error()), "oauth") {
		t.Errorf("machinery vocabulary in %q", err)
	}
	_ = plug
}

// ── the blank in an address ─────────────────────────────────────────────────

func TestTheBlankComesFirstAndTheKeyIsLast(t *testing.T) {
	plug := &keyPlug{
		service: Service{ID: "example", Name: "Example", Auth: AuthKey},
		base:    "https://{{.workspace}}.example.com/api",
		blank:   blank{name: "workspace", label: "Domain"},
		carry:   carry{kind: carryHeader, name: "X-Api-Key"},
	}
	blank, key, err := plug.read("  acme   sk-1  ")
	if err != nil || blank != "acme" || key != "sk-1" {
		t.Fatalf("read = %q %q %v", blank, key, err)
	}
	if _, _, err := plug.read("sk-1"); err == nil {
		t.Errorf("a service with a blank needs both halves")
	} else if !strings.Contains(err.Error(), "domain") {
		t.Errorf("the refusal must name what is missing, got %q", err)
	}
	address, err := plug.address("acme")
	if err != nil || address != "https://acme.example.com/api" {
		t.Fatalf("address = %q %v", address, err)
	}
	if _, err := plug.address(""); err == nil {
		t.Errorf("a blank nobody filled is not an address")
	}

	plain := &keyPlug{service: Service{Name: "Example"}, base: "https://api.example.com"}
	if _, key, err := plain.read(" sk-2 "); err != nil || key != "sk-2" {
		t.Fatalf("read = %q %v", key, err)
	}
	if _, _, err := plain.read("acme sk-2"); err == nil {
		t.Errorf("a service with no blank takes one word and no more")
	}
}

func TestAConnectionRemembersTheBlank(t *testing.T) {
	var host string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		writeJSON(t, w, map[string]bool{"ok": true})
	}))
	t.Cleanup(server.Close)

	plug := &keyPlug{
		service: Service{ID: "example", Name: "Example", Auth: AuthKey, Address: "http://<domain>/api"},
		base:    "http://{{.workspace}}/api",
		blank:   blank{name: "workspace", label: "Domain"},
		carry:   carry{kind: carryHeader, name: "X-Api-Key"},
	}
	withPlugs(t, plug)
	manager, err := NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	bare := strings.TrimPrefix(server.URL, "http://")
	if _, err := manager.ConnectKey(context.Background(), "example", bare+" sk-3"); err != nil {
		t.Fatalf("ConnectKey: %v", err)
	}
	entry, _, _ := manager.store.get("example")
	if entry.Blank != bare {
		t.Errorf("the blank must be written down, got %q", entry.Blank)
	}
	// The menu now says where it actually answers.
	services := manager.Services()
	if len(services) != 1 || services[0].Address != "http://"+bare+"/api" {
		t.Fatalf("Services() = %+v", services)
	}
	if _, err := manager.Request(context.Background(), "example", "GET", "/thing", "", ""); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if host != bare {
		t.Errorf("the call went to %q", host)
	}
}

// ── where the key rides ─────────────────────────────────────────────────────

func TestTheKeyRidesWhereTheServiceWantsIt(t *testing.T) {
	cases := []struct {
		name  string
		carry carry
		key   string
		check func(t *testing.T, r *http.Request)
	}{
		{
			name:  "a header",
			carry: carry{kind: carryHeader, name: "X-Api-Key"},
			key:   "sk-1",
			check: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("X-Api-Key"); got != "sk-1" {
					t.Errorf("header = %q", got)
				}
			},
		},
		{
			name:  "a header with a word in front of the key",
			carry: carry{kind: carryHeader, name: "Authorization", prefix: "Bearer"},
			key:   "sk-2",
			check: func(t *testing.T, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer sk-2" {
					t.Errorf("header = %q", got)
				}
			},
		},
		{
			name:  "a query parameter",
			carry: carry{kind: carryQuery, name: "api_key"},
			key:   "sk-3",
			check: func(t *testing.T, r *http.Request) {
				if got := r.URL.Query().Get("api_key"); got != "sk-3" {
					t.Errorf("query = %q", got)
				}
				if got := r.URL.Query().Get("page"); got != "2" {
					t.Errorf("the caller's own query must survive, got %q", got)
				}
			},
		},
		{
			name:  "a name and no password",
			carry: carry{kind: carryBasic},
			key:   "sk-4",
			check: func(t *testing.T, r *http.Request) {
				name, password, ok := r.BasicAuth()
				if !ok || name != "sk-4" || password != "" {
					t.Errorf("pair = %q %q ok=%v", name, password, ok)
				}
			},
		},
		{
			name:  "a name and a password the person pasted whole",
			carry: carry{kind: carryBasic},
			key:   "api:sk-5",
			check: func(t *testing.T, r *http.Request) {
				name, password, ok := r.BasicAuth()
				if !ok || name != "api" || password != "sk-5" {
					t.Errorf("pair = %q %q ok=%v", name, password, ok)
				}
			},
		},
		{
			name:  "a shape the catalog wrote down",
			carry: carry{kind: carryBasic, format: "api:%s"},
			key:   "sk-6",
			check: func(t *testing.T, r *http.Request) {
				name, password, ok := r.BasicAuth()
				if !ok || name != "api" || password != "sk-6" {
					t.Errorf("pair = %q %q ok=%v", name, password, ok)
				}
			},
		},
	}

	for _, c := range cases {
		var got *http.Request
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Clone(context.Background())
			writeJSON(t, w, map[string]bool{"ok": true})
		}))
		client := &http.Client{Transport: signing{base: http.DefaultTransport, carry: c.carry, key: c.key}}
		service := Service{Name: "Example", Address: server.URL}
		if _, err := ServiceRequest(context.Background(), client, service, "GET", "/thing", "page=2", ""); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if got == nil {
			t.Errorf("%s: nothing arrived", c.name)
		} else {
			c.check(t, got)
		}
		server.Close()
	}
}

// A round tripper is handed a request it does not own.
func TestSigningLeavesTheCallersRequestAlone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]bool{"ok": true})
	}))
	t.Cleanup(server.Close)
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	transport := signing{base: http.DefaultTransport, carry: carry{kind: carryHeader, name: "X-Api-Key"}, key: "sk"}
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = response.Body.Close()
	if request.Header.Get("X-Api-Key") != "" {
		t.Errorf("the caller's request was written into")
	}
}

// ── the raw call ────────────────────────────────────────────────────────────

func TestServiceRequestBoundsWhatItHandsBack(t *testing.T) {
	long := strings.Repeat("a", 40*1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(long))
	}))
	t.Cleanup(server.Close)

	text, err := ServiceRequest(context.Background(), server.Client(), Service{Name: "Example", Address: server.URL}, "GET", "/big", "", "")
	if err != nil {
		t.Fatalf("ServiceRequest: %v", err)
	}
	if len(text) > maxToolText+256 {
		t.Errorf("the answer is %d characters, which is past the ceiling", len(text))
	}
	if !strings.Contains(text, "Shortened here") {
		t.Errorf("THE CUT IS ANNOUNCED, ALWAYS: %q", text[len(text)-120:])
	}
}

func TestServiceRequestRefusesWhatIsNotItsToDo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]bool{"ok": true})
	}))
	t.Cleanup(server.Close)
	service := Service{Name: "Example", Address: server.URL}

	cases := []struct {
		name, method, path, query string
	}{
		{"a method nobody named", "TRACE", "/thing", ""},
		{"a whole address of its own", "GET", "https://elsewhere.example.com/x", ""},
		{"an address with no scheme", "GET", "//elsewhere.example.com/x", ""},
		{"a query that cannot be read", "GET", "/thing", "%zz"},
	}
	for _, c := range cases {
		if _, err := ServiceRequest(context.Background(), server.Client(), service, c.method, c.path, c.query, ""); err == nil {
			t.Errorf("%s: must be refused", c.name)
		}
	}
	if _, err := ServiceRequest(context.Background(), nil, service, "GET", "/thing", "", ""); err == nil {
		t.Errorf("no client is no call")
	}
	if _, err := ServiceRequest(context.Background(), server.Client(), Service{Name: "Example"}, "GET", "/thing", "", ""); err == nil {
		t.Errorf("no address is no call")
	}
}

func TestServiceRequestCarriesTheVerbAndTheBody(t *testing.T) {
	var method, path, contentType, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, contentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		raw := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(raw)
		body = string(raw)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	t.Cleanup(server.Close)

	text, err := ServiceRequest(context.Background(), server.Client(),
		Service{Name: "Example", Address: server.URL}, "post", "things", "", `{"name":"a"}`)
	if err != nil {
		t.Fatalf("ServiceRequest: %v", err)
	}
	if method != "POST" || path != "/things" || body != `{"name":"a"}` {
		t.Errorf("got %s %s %q", method, path, body)
	}
	if !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("content type = %q", contentType)
	}
	if !strings.Contains(text, "201") || !strings.Contains(text, `"id": 7`) {
		t.Errorf("the answer must say what came back: %q", text)
	}
}

func TestServiceRequestSaysWhatTheServiceRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"this key may not read contacts"}}`))
	}))
	t.Cleanup(server.Close)

	_, err := ServiceRequest(context.Background(), server.Client(), Service{Name: "Example", Address: server.URL}, "GET", "/contacts", "", "")
	if err == nil {
		t.Fatalf("a refusal must be an error")
	}
	if !strings.Contains(err.Error(), "this key may not read contacts") {
		t.Errorf("the service's own sentence must survive, got %q", err)
	}
}

// THE EMPTINESS LAW on an answer with nothing in it.
func TestAnAnswerWithNothingInItSaysSo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	text, err := ServiceRequest(context.Background(), server.Client(), Service{Name: "Example", Address: server.URL}, "DELETE", "/thing/1", "", "")
	if err != nil {
		t.Fatalf("ServiceRequest: %v", err)
	}
	if !strings.Contains(text, "answered with nothing") {
		t.Errorf("got %q", text)
	}
}

func TestRequestRefusesAnAccountThatIsNotConnected(t *testing.T) {
	manager, _, _ := keyManager(t, http.NotFoundHandler(), nil)
	if _, err := manager.Request(context.Background(), "example", "GET", "/x", "", ""); err == nil {
		t.Errorf("an unconnected service cannot be called")
	}
	if _, err := manager.Request(context.Background(), "nothing-like-this", "GET", "/x", "", ""); err == nil {
		t.Errorf("an unknown service cannot be called")
	}
}
