package connect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestServicesListsOnlyWhatCanBeConnected(t *testing.T) {
	withPlugs(t,
		fakePlug{service: Service{ID: "zulip", Name: "Zulip"}},
		fakePlug{service: Service{ID: "asana", Name: "Asana"}},
		fakePlug{service: Service{ID: "google", Name: "Google"}},
	)
	manager, err := NewManager(t.TempDir(), map[string]ClientCredential{
		"google": {ID: "id", Secret: "secret"},
		"asana":  {ID: "id", Secret: "secret"},
		// Zulip is built in but this build was given no credential for it.
		"zulip": {ID: "id"},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	var names []string
	for _, status := range manager.Services() {
		names = append(names, status.Name)
	}
	// The order is the name's order, and the half-filled credential is
	// treated exactly as a missing one.
	if got, want := strings.Join(names, ","), "Asana,Google"; got != want {
		t.Errorf("Services(): got %q, want %q", got, want)
	}
	for _, status := range manager.Services() {
		if status.Connected || status.Account != "" {
			t.Errorf("%s: nothing is connected yet, got %+v", status.Name, status)
		}
	}
}

func TestConnectedNeedsBothHalves(t *testing.T) {
	manager, _ := testManager(t)

	if manager.Connected("google") {
		t.Errorf("a service with no stored keys is not connected")
	}
	if err := manager.store.put("google", stored{Account: "me@example.com", Scopes: wholeGrant(), Keys: &oauth2.Token{RefreshToken: "r"}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if !manager.Connected("google") {
		t.Errorf("a service with a credential and stored keys is connected")
	}
	if manager.Connected("nothing-like-this") {
		t.Errorf("an unknown service is never connected")
	}

	// Stored keys without a client credential cannot answer a single
	// request, so they do not count.
	blind, err := NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if err := blind.store.put("google", stored{Scopes: wholeGrant(), Keys: &oauth2.Token{RefreshToken: "r"}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if blind.Connected("google") {
		t.Errorf("stored keys with no client credential must not read as connected")
	}
}

// A CONNECTION THAT IS SHORT OF A PERMISSION IS NOT A CONNECTION. The three
// readings that matter: what an older build wrote, what a wider sign-in wrote,
// and what a narrower one wrote.
func TestAConnectionThatDoesNotCoverTheAskIsNotConnected(t *testing.T) {
	whole := wholeGrant()
	cases := []struct {
		name   string
		scopes []string
		want   bool
	}{
		{"an entry from a build that recorded nothing", nil, false},
		{"exactly what is asked for", whole, true},
		{"more than what is asked for", append(append([]string(nil), whole...), "https://example.test/auth/extra"), true},
		{"one permission short", whole[:1], false},
		{"a different permission entirely", []string{"https://example.test/auth/something-else"}, false},
	}
	for _, c := range cases {
		manager, _ := testManager(t)
		if err := manager.store.put("google", stored{
			Account: "me@example.com",
			Scopes:  c.scopes,
			Keys:    &oauth2.Token{RefreshToken: "r"},
		}); err != nil {
			t.Fatalf("%s: put: %v", c.name, err)
		}
		if got := manager.Connected("google"); got != c.want {
			t.Errorf("%s: Connected = %v, want %v", c.name, got, c.want)
		}
		// The menu says the same thing, so that a screen can never offer a
		// connection the tools would refuse to use.
		services := manager.Services()
		if len(services) != 1 || services[0].Connected != c.want {
			t.Errorf("%s: Services() = %+v, want Connected=%v", c.name, services, c.want)
		}
		// And so does the client: a sign-in that cannot do the work is not
		// handed out to do it.
		_, err := manager.Client(context.Background(), "google")
		if (err == nil) != c.want {
			t.Errorf("%s: Client err = %v, want connected=%v", c.name, err, c.want)
		}
	}
}

// The file an earlier build wrote is read, not rejected: the connection in it
// simply needs the person to sign in again, which is a thing the ordinary ask
// flow already does.
func TestAFileFromAnEarlierBuildStillOpens(t *testing.T) {
	withPlugs(t, google{})
	directory := t.TempDir()
	legacy := `{"google":{"account":"me@example.com","keys":{"access_token":"a","refresh_token":"r","token_type":"Bearer"}}}`
	if err := os.WriteFile(filepath.Join(directory, StoreFileName), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	manager, err := NewManager(directory, map[string]ClientCredential{
		"google": {ID: "client-id", Secret: "client-secret"},
	})
	if err != nil {
		t.Fatalf("NewManager on an older file: %v", err)
	}
	if manager.Connected("google") {
		t.Error("a connection whose permissions are unknown must ask the person again")
	}
	entry, ok, err := manager.store.get("google")
	if err != nil || !ok || entry.Account != "me@example.com" || entry.Keys.RefreshToken != "r" {
		t.Errorf("the older entry must survive being read: %+v ok=%v err=%v", entry, ok, err)
	}
}

func TestServicesReportsAccountAndConnection(t *testing.T) {
	manager, _ := testManager(t)
	if err := manager.store.put("google", stored{Account: "me@example.com", Scopes: wholeGrant(), Keys: &oauth2.Token{RefreshToken: "r"}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	services := manager.Services()
	if len(services) != 1 {
		t.Fatalf("Services(): got %d entries, want 1", len(services))
	}
	got := services[0]
	if !got.Connected || got.Account != "me@example.com" || got.ID != "google" || got.Name != "Google" {
		t.Errorf("Services(): got %+v", got)
	}
	if len(got.Scopes) != 2 {
		t.Errorf("the menu entry must carry the permissions it asks for, got %v", got.Scopes)
	}
}

func TestDisconnectForgetsOnlyTheKeys(t *testing.T) {
	manager, _ := testManager(t)
	if err := manager.store.put("google", stored{Account: "me@example.com", Scopes: wholeGrant(), Keys: &oauth2.Token{RefreshToken: "r"}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := manager.Disconnect("google"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if manager.Connected("google") {
		t.Errorf("the service is still connected after Disconnect")
	}
	services := manager.Services()
	if len(services) != 1 || services[0].Connected {
		t.Errorf("the service must stay on the menu, unconnected: %+v", services)
	}
	if err := manager.Disconnect("google"); err != nil {
		t.Errorf("disconnecting twice: %v", err)
	}
	if err := manager.Disconnect("nothing-like-this"); err == nil {
		t.Errorf("disconnecting an unknown service must say so")
	}
}

func TestClientRefusesWhatIsNotConnected(t *testing.T) {
	manager, _ := testManager(t)

	if _, err := manager.Client(context.Background(), "google"); err == nil {
		t.Errorf("Client on an unconnected service must fail")
	} else if !strings.Contains(err.Error(), "Google is not connected") {
		t.Errorf("the message must be in the person's words, got %q", err)
	}
	if _, err := manager.BeginAuth(context.Background(), "nothing-like-this"); err == nil {
		t.Errorf("BeginAuth on an unknown service must fail")
	}
}

// TestMessagesUseNoMachineryVocabulary holds the house rule: nothing a person
// reads from this package may be written in the vocabulary of the machinery
// underneath it.
func TestMessagesUseNoMachineryVocabulary(t *testing.T) {
	manager, _ := testManager(t)
	banned := []string{"oauth", "token", "bearer", "pkce", "grant"}

	subjects := []string{successPage}
	for _, status := range manager.Services() {
		subjects = append(subjects, status.Name, status.Blurb)
	}
	if _, err := manager.Client(context.Background(), "google"); err != nil {
		subjects = append(subjects, err.Error())
	}
	if err := manager.Disconnect("nothing-like-this"); err != nil {
		subjects = append(subjects, err.Error())
	}
	for _, subject := range subjects {
		lowered := strings.ToLower(subject)
		for _, word := range banned {
			if strings.Contains(lowered, word) {
				t.Errorf("%q says %q, which is machinery vocabulary", subject, word)
			}
		}
	}
}
