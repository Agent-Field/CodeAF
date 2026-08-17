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

// A KEY BY REFERENCE (keyref.go), from the four sides that matter: what is
// written down, what goes on the wire, what happens when the variable is not
// there, and the promise that none of this changed what a pasted key does.

// ── 1. what a pasted value is READ as ───────────────────────────────────────

// THE TWO SHAPES, AND NO THIRD. Everything else is a key, because the reading
// that is wrong in that direction fails at the far end, and the reading that is
// wrong in the other direction writes somebody's key into a field that means
// "the name of a variable".
func TestOnlyTheTwoShapesAreAReference(t *testing.T) {
	for _, want := range []struct {
		value string
		name  string
	}{
		{"$STRIPE_KEY", "STRIPE_KEY"},
		{"${STRIPE_KEY}", "STRIPE_KEY"},
		{"  $A  ", "A"},
		{"$_PRIVATE", "_PRIVATE"},
		{"$KEY_2", "KEY_2"},
	} {
		got, ok := envReference(want.value)
		if !ok || got != want.name {
			t.Errorf("envReference(%q) = %q %v, want %q", want.value, got, ok, want.name)
		}
	}
	for _, literal := range []string{
		"", "$", "sk-live-1", "$lower", "$Mixed", "${NAME", "$NAME}", "{NAME}",
		"$NAME extra", "$NAME$OTHER", "sk-$NAME", "$9LIVES", "$NA-ME",
	} {
		if name, ok := envReference(literal); ok {
			t.Errorf("envReference(%q) read a variable %q, want a literal key", literal, name)
		}
	}
}

// ── 2. the round trip ───────────────────────────────────────────────────────

// A NAME IS STORED AS A NAME. The key field is left empty, the variable's name
// is written to its own field, and the file says which of the two it is without
// anybody having to read a value to find out.
func TestConnectingWithAVariableStoresTheReferenceAndNotTheKey(t *testing.T) {
	t.Setenv("EXAMPLE_KEY", "sk-from-the-environment")
	var seen string
	manager, _, directory := keyManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Api-Key")
		writeJSON(t, w, map[string]bool{"ok": true})
	}), nil)

	status, err := manager.ConnectKey(context.Background(), "example", " $EXAMPLE_KEY ")
	if err != nil {
		t.Fatalf("ConnectKey: %v", err)
	}
	if !status.Connected || status.KeyEnv != "EXAMPLE_KEY" {
		t.Fatalf("status = %+v, want a connection reading from EXAMPLE_KEY", status)
	}

	entry, ok, err := manager.store.get("example")
	if err != nil || !ok {
		t.Fatalf("get: %v ok=%v", err, ok)
	}
	if entry.KeyEnv != "EXAMPLE_KEY" {
		t.Errorf("entry = %+v, want the variable's name", entry)
	}
	if entry.Key != "" {
		t.Errorf("a reference was stored as a key as well: %+v", entry)
	}

	// And on disk it is a field of its own, so nothing ever has to guess.
	raw, err := os.ReadFile(filepath.Join(directory, StoreFileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var entries map[string]map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("the file must stay one object keyed by service: %v", err)
	}
	if entries["example"]["keyEnv"] != "EXAMPLE_KEY" {
		t.Errorf("file = %s", raw)
	}
	if _, wrote := entries["example"]["key"]; wrote {
		t.Errorf("the file carries a key for a connection that named a variable: %s", raw)
	}

	// The menu reads it back, and says so out loud — the ONE thing a key
	// connection may say about itself.
	services := manager.Services()
	if len(services) != 1 || !services[0].Connected || services[0].KeyEnv != "EXAMPLE_KEY" {
		t.Fatalf("Services() = %+v", services)
	}
	if services[0].Account != "" {
		t.Errorf("a key connection invented an account: %q", services[0].Account)
	}

	// And the key that went out is what the variable holds.
	client, err := manager.Client(context.Background(), "example")
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if _, err := ServiceRequest(context.Background(), client,
		Service{Name: "Example", Address: manager.plugs[0].Service().Address}, "GET", "/anything", "", ""); err != nil {
		t.Fatalf("ServiceRequest: %v", err)
	}
	if seen != "sk-from-the-environment" {
		t.Errorf("the request carried %q", seen)
	}
}

// THE VARIABLE IS READ AT EVERY USE AND NEVER CACHED, which is the whole reason
// to name one: a key rotated where it is set is rotated here, with no second
// step and nothing to reconnect.
func TestAReferenceIsResolvedFreshOnEveryRequest(t *testing.T) {
	t.Setenv("EXAMPLE_KEY", "first")
	seen := []string{}
	manager, _, _ := keyManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Api-Key"))
		writeJSON(t, w, map[string]bool{"ok": true})
	}), nil)
	if _, err := manager.ConnectKey(context.Background(), "example", "$EXAMPLE_KEY"); err != nil {
		t.Fatalf("ConnectKey: %v", err)
	}

	call := func() {
		t.Helper()
		if _, err := manager.Request(context.Background(), "example", "GET", "/thing", "", ""); err != nil {
			t.Fatalf("Request: %v", err)
		}
	}
	call()
	t.Setenv("EXAMPLE_KEY", "second")
	call()

	if len(seen) != 2 || seen[0] != "first" || seen[1] != "second" {
		t.Fatalf("the requests carried %v, want the variable as it stood each time", seen)
	}
}

// ── 3. the variable that is not there ───────────────────────────────────────

// A MISSING VARIABLE IS A REFUSAL THAT NAMES IT. Not a silence, not a request
// signed with nothing, and not the far end's own 401 explaining our bookkeeping.
func TestAMissingVariableRefusesAndSaysWhichOne(t *testing.T) {
	t.Setenv("EXAMPLE_KEY", "sk-1")
	manager, _, _ := keyManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request went out for a variable nobody set: %s", r.URL)
	}), nil)
	if _, err := manager.ConnectKey(context.Background(), "example", "$EXAMPLE_KEY"); err != nil {
		t.Fatalf("ConnectKey: %v", err)
	}
	t.Setenv("EXAMPLE_KEY", "")

	// It still reads as connected: "you have not connected this" and "the
	// variable you named is empty" are two different things to be told, and only
	// the second one says what to do about it.
	if !manager.Connected("example") {
		t.Errorf("an unset variable dropped the connection instead of refusing")
	}
	_, err := manager.Client(context.Background(), "example")
	if err == nil {
		t.Fatal("a client was handed out for a variable nobody set")
	}
	if !strings.Contains(err.Error(), "$EXAMPLE_KEY") {
		t.Errorf("the refusal does not name the variable: %q", err)
	}
	if !strings.Contains(err.Error(), "Example") {
		t.Errorf("the refusal does not name the service: %q", err)
	}
	if _, err := manager.Request(context.Background(), "example", "GET", "/thing", "", ""); err == nil ||
		!strings.Contains(err.Error(), "$EXAMPLE_KEY") {
		t.Errorf("a request answered %v", err)
	}
}

// AND NOTHING IS WRITTEN DOWN FOR A NAME THAT RESOLVES TO NOTHING. A row that
// said "connected" over a variable nobody set would be a row that lies at rest
// and fails at work.
func TestConnectingToAnUnsetVariableStoresNothing(t *testing.T) {
	manager, _, _ := keyManager(t, http.NotFoundHandler(), nil)
	os.Unsetenv("NOTHING_IS_SET_HERE")

	_, err := manager.ConnectKey(context.Background(), "example", "$NOTHING_IS_SET_HERE")
	if err == nil {
		t.Fatal("connecting to an unset variable succeeded")
	}
	if !strings.Contains(err.Error(), "$NOTHING_IS_SET_HERE") {
		t.Errorf("the refusal does not name the variable: %q", err)
	}
	if _, ok, _ := manager.store.get("example"); ok {
		t.Errorf("something was written down for a variable nobody set")
	}
}

// ── 4. what did not change ──────────────────────────────────────────────────

// A PASTED KEY IS EXACTLY WHAT IT ALWAYS WAS, and a value that merely looks a
// little like a reference is one of them.
func TestALiteralKeyIsUntouchedByAnyOfThis(t *testing.T) {
	for _, key := range []string{"sk-live-1", "$lower", "${NAME", "$"} {
		var seen string
		manager, _, _ := keyManager(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = r.Header.Get("X-Api-Key")
			writeJSON(t, w, map[string]bool{"ok": true})
		}), nil)
		if _, err := manager.ConnectKey(context.Background(), "example", key); err != nil {
			t.Fatalf("ConnectKey(%q): %v", key, err)
		}
		entry, _, _ := manager.store.get("example")
		if entry.Key != key || entry.KeyEnv != "" {
			t.Fatalf("%q was stored as %+v", key, entry)
		}
		if status := manager.Services()[0]; status.KeyEnv != "" {
			t.Errorf("%q reads as a reference on the menu: %+v", key, status)
		}
		if _, err := manager.Request(context.Background(), "example", "GET", "/thing", "", ""); err != nil {
			t.Fatalf("Request: %v", err)
		}
		if seen != key {
			t.Errorf("the request carried %q, want %q", seen, key)
		}
	}
}

// THE BLANK STILL COMES FIRST AND THE REFERENCE IS LAST. A service that needs a
// domain takes "<domain> $VARIABLE" exactly as it takes "<domain> <key>": a
// reference has no space in it, which is what makes the split safe either way.
func TestAServiceWithABlankTakesAReferenceToo(t *testing.T) {
	t.Setenv("EXAMPLE_KEY", "sk-blank")
	var seen, host string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, host = r.Header.Get("X-Api-Key"), r.Host
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
	if _, err := manager.ConnectKey(context.Background(), "example", bare+" $EXAMPLE_KEY"); err != nil {
		t.Fatalf("ConnectKey: %v", err)
	}
	entry, _, _ := manager.store.get("example")
	if entry.Blank != bare || entry.KeyEnv != "EXAMPLE_KEY" || entry.Key != "" {
		t.Fatalf("entry = %+v", entry)
	}
	if _, err := manager.Request(context.Background(), "example", "GET", "/thing", "", ""); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if seen != "sk-blank" || host != bare {
		t.Errorf("the call went to %q carrying %q", host, seen)
	}
}

// An entry naming a variable is usable; one naming neither is not. This is the
// reading [Manager.standing] takes on every call.
func TestAnEntryNamingAVariableIsUsable(t *testing.T) {
	if !(stored{Auth: AuthKey, KeyEnv: "NAME"}).usable() {
		t.Errorf("an entry naming a variable is usable")
	}
	if (stored{Auth: AuthKey}).usable() {
		t.Errorf("an entry naming neither is not usable")
	}
	// And a browser entry is untouched by the new field.
	if (stored{KeyEnv: "NAME"}).usable() {
		t.Errorf("a browser entry with no keys is not usable")
	}
}
