package taskplace_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
	"github.com/Agent-Field/codeaf/internal/taskplace"
)

type namedComputerDirectory struct {
	directory.Client
	rows directory.Listing
}

func (d namedComputerDirectory) List(context.Context) (directory.Listing, error) { return d.rows, nil }

// R7-12 lists the same labels used to distinguish devices on the devices page.
func TestPairedComputersWithTheSameNameUseDeviceLabels(t *testing.T) {
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	device, err := identity.LoadDevice(home)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := directory.SealName(directory.MetadataKey(id.CellKey()), "mac mini.local")
	if err != nil {
		t.Fatal(err)
	}
	c := taskplace.Computers{Home: home, Copies: taskdoor.CopyDeps{Identity: id, Device: device}, Directory: namedComputerDirectory{rows: directory.Listing{Devices: map[string]directory.Device{
		"dev_0123456789abcdef0123456789ab1111": {Name: sealed, Platform: "linux"},
		"dev_0123456789abcdef0123456789ab2222": {Name: sealed, Platform: "darwin"},
	}}}}
	rows, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Name != "mac mini #1111" || rows[1].Name != "mac mini #2222" {
		t.Fatalf("computers = %+v", rows)
	}
}
