package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// TestC11WindowsIdentityDoors proves C11 without opening a terminal or a relay.
// Remote-access device books remain present independently of shared chats.
func TestC11WindowsIdentityDoors(t *testing.T) {
	assertCellCommandsRefuse(t, cell.WindowsReason)
	kinds := openDeviceKinds()
	if len(kinds) != 1 {
		t.Fatalf("Windows has %d device kinds, want only remote access", len(kinds))
	}
	if _, ok := kinds[0].(bookKind); !ok {
		t.Fatalf("Windows lost remote-access devices: %T", kinds[0])
	}
}
