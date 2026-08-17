package connect

import (
	"context"
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
	if err := manager.store.put("google", stored{Account: "me@example.com", Keys: &oauth2.Token{RefreshToken: "r"}}); err != nil {
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
	if err := blind.store.put("google", stored{Keys: &oauth2.Token{RefreshToken: "r"}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if blind.Connected("google") {
		t.Errorf("stored keys with no client credential must not read as connected")
	}
}

func TestServicesReportsAccountAndConnection(t *testing.T) {
	manager, _ := testManager(t)
	if err := manager.store.put("google", stored{Account: "me@example.com", Keys: &oauth2.Token{RefreshToken: "r"}}); err != nil {
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
	if err := manager.store.put("google", stored{Account: "me@example.com", Keys: &oauth2.Token{RefreshToken: "r"}}); err != nil {
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
