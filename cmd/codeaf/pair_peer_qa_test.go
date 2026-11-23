package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestQALinkJoinDoesNotGuessItsApproverFromSeveralComputers(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	seal := func(name string) string {
		t.Helper()
		value, err := directory.SealName(key, name)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	listing := directory.Listing{Devices: map[string]directory.Device{
		"self":     {Name: seal("joining computer")},
		"approver": {Name: seal("desk computer")},
	}}
	if name := pairingPeer(listing, key, "self"); name != "desk computer" {
		t.Fatalf("single peer = %q", name)
	}
	listing.Devices["other"] = directory.Device{Name: seal("alphabetically first computer")}
	if name := pairingPeer(listing, key, "self"); name != "" {
		t.Fatalf("the receipt guessed %q approved the join", name)
	}
	listing.Devices["other"] = directory.Device{Name: seal("removed computer"), Revoked: true}
	if name := pairingPeer(listing, key, "self"); name != "desk computer" {
		t.Fatalf("a removed computer obscured the single peer: %q", name)
	}
}
