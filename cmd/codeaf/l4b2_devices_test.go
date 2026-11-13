package main

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/devname"
)

// L4-M10b, L4-M10c: An unpaired computer is listed from local facts without contacting any service.
func TestL4B2UnpairedDeviceListsItself(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_SYNC_URL", "off")
	door := newApprovalsDoor(dir)
	rows, err := door.Devices(context.Background())
	if err != nil || len(rows) != 1 || !rows[0].Self || rows[0].Name != devname.Name(dir) || rows[0].Platform == "" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}
