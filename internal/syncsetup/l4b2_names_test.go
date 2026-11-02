package syncsetup

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

type l4B2NameDirectory struct {
	directory.Client
	listing directory.Listing
	err     error
}

func (d *l4B2NameDirectory) List(context.Context) (directory.Listing, error) { return d.listing, d.err }

// L4-M14e: Only a successful roster read can establish that a labelled member's device is absent.
func TestL4B2ClusterNameEvidence(t *testing.T) {
	dir := t.TempDir()
	id, err := identity.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := directory.SealName(directory.MetadataKey(id.CellKey()), "desk-b")
	if err != nil {
		t.Fatal(err)
	}
	roster := &l4B2NameDirectory{listing: directory.Listing{Devices: map[string]directory.Device{"kept": {Name: sealed}, "unreadable": {Name: "not encrypted"}}}}
	s := &Sync{Home: dir, Identity: id, Dir: roster}
	status := clusterctl.Status{Self: 1, Members: []cs.MemberLabel{{Node: 2, Device: "kept"}, {Node: 3, Device: "gone"}, {Node: 4, Device: "unreadable"}, {Node: 5, Anchor: true}}}
	names := s.ClusterNames(context.Background(), status)
	if names[2] != "desk-b" {
		t.Fatal(names)
	}
	if value, known := names[3]; !known || value != "" {
		t.Fatal("missing device not recorded")
	}
	if _, known := names[4]; known {
		t.Fatal("decryption failure claimed removal")
	}
	roster.err = errors.New("fixture read failure")
	names = s.ClusterNames(context.Background(), status)
	for _, node := range []uint64{2, 3, 4} {
		if _, known := names[node]; known {
			t.Fatal("failed read claimed a missing device")
		}
	}
}
