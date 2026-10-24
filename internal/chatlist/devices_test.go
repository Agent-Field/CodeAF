package chatlist

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
)

func TestDevicesListSelfFirstSkipRevokedAndMarkPresence(t *testing.T) {
	l := directory.Listing{Devices: map[string]directory.Device{
		"dev_b": {Name: "b64-spark"}, "dev_a": {Name: "b64-dumb"},
		"dev_me": {Platform: "darwin"}, "dev_gone": {Revoked: true},
	}}
	open := func(s string) (string, error) {
		if s == "b64-spark" {
			return "spark", nil
		}
		if s == "b64-dumb" {
			return "dumb", nil
		}
		return "", errors.New("no")
	}
	got := Devices(l, "dev_me", open)
	if len(got) != 3 || got[0].Name != "This Mac" || got[1].Name != "dumb" || got[2].Name != "spark" {
		t.Fatalf("devices %+v", got)
	}
	if got[0].Mark(false) != "● This Mac" || got[2].Mark(true) != "● spark" || got[1].Mark(false) != "○ dumb (offline)" {
		t.Fatalf("marks %q %q %q", got[0].Mark(false), got[2].Mark(true), got[1].Mark(false))
	}
}

func TestVerbsFollowTheChatsStatus(t *testing.T) {
	if VerbFor(Row{Status: Running}) != MoveHere || VerbFor(Row{Status: Off}) != ContinueVerb {
		t.Fatal("wrong verb")
	}
	if got := VerbFrom(Row{Status: Running, Device: "spark"}); got != "Move here from spark" {
		t.Fatal(got)
	}
}
