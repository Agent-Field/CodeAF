package chatlist

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestDevicesListSelfFirstSkipRevokedAndMarkPresence(t *testing.T) {
	l := directory.Listing{Devices: map[string]directory.Device{
		"dev_b": {Name: "b64-tower"}, "dev_a": {Name: "b64-desk"},
		"dev_me": {Platform: "darwin"}, "dev_gone": {Revoked: true},
	}}
	open := func(s string) (string, error) {
		if s == "b64-tower" {
			return "tower", nil
		}
		if s == "b64-desk" {
			return "desk", nil
		}
		return "", errors.New("no")
	}
	got := Devices(l, "dev_me", open)
	if len(got) != 3 || got[0].Name != "This Mac" || got[1].Name != "desk" || got[2].Name != "tower" {
		t.Fatalf("devices %+v", got)
	}
	if got[0].Mark(false) != "● This Mac" || got[2].Mark(true) != "● tower" || got[1].Mark(false) != "○ desk (offline)" {
		t.Fatalf("marks %q %q %q", got[0].Mark(false), got[2].Mark(true), got[1].Mark(false))
	}
}

func TestVerbsFollowTheChatsStatus(t *testing.T) {
	if VerbFor(Row{Status: Running}) != MoveHere || VerbFor(Row{Status: Off}) != ContinueVerb {
		t.Fatal("wrong verb")
	}
	if got := VerbFrom(Row{Status: Running, Device: "tower"}); got != "Move here from tower" {
		t.Fatal(got)
	}
}
