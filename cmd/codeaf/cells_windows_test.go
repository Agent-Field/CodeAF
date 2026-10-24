package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

// TestK1WindowsIdentityDoors proves K1 without opening a terminal or a relay.
// Remote-access device books remain available when the engine is absent.
func TestK1WindowsIdentityDoors(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(false))
	assertCellCommandsRefuse(t, furrowbin.CellsUnavailable)
	kinds := openDeviceKinds()
	if len(kinds) != 1 {
		t.Fatalf("missing engine has %d device kinds, want remote access", len(kinds))
	}
	if _, ok := kinds[0].(bookKind); !ok {
		t.Fatalf("lost remote-access devices: %T", kinds[0])
	}
}
