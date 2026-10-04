package syncsetup

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

type listOnly struct {
	directory.Client
	list directory.Listing
}

func (l listOnly) List(context.Context) (directory.Listing, error) { return l.list, nil }

// A revoked device is not part of the fleet: the card that offers another
// machine must not be switched off by one that was stopped.
func TestFleetSizeCountsDevicesNotRevoked(t *testing.T) {
	home := t.TempDir()
	if _, err := identity.Ensure(home); err != nil {
		t.Fatal(err)
	}
	s := &Sync{Home: home, Dir: listOnly{list: directory.Listing{Devices: map[string]directory.Device{
		"dev_a": {}, "dev_b": {Revoked: true},
	}}}}
	got, err := s.FleetSize(context.Background())
	if err != nil || got != 1 {
		t.Fatalf("fleet = %d, %v; want 1", got, err)
	}
}
