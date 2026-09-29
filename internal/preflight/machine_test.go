package preflight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/inventory"
)

// A chat that used a tool on another machine is INSTALLABLE here until the tool
// is really on PATH; after a setup turn the machine looks and the inventory
// takes this machine's binary, by observation alone.
func TestSettleLearnsWhatASetupTurnInstalled(t *testing.T) {
	root, bin := t.TempDir(), t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	seed, err := inventory.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Update(func(inv *inventory.Inventory) {
		inv.Tools = []inventory.Tool{{Name: "setupdemo", BinaryHash: "from-elsewhere"}}
	}); err != nil {
		t.Fatal(err)
	}
	m, err := OpenMachine(root)
	if err != nil {
		t.Fatal(err)
	}
	before := m.Plan()
	if got := before.Pending(); len(got) != 1 || got[0].Name != "setupdemo" {
		t.Fatalf("before setup, pending = %+v", got)
	}

	if err := os.WriteFile(filepath.Join(bin, "setupdemo"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.Settle(before)

	after, _ := inventory.Open(root)
	tools := after.Snapshot().Tools
	if len(tools) != 1 || tools[0].BinaryHash == "from-elsewhere" || tools[0].BinaryHash == "" {
		t.Fatalf("inventory after setup = %+v, want this machine's binary", tools)
	}
	if left := m.Plan().Pending(); len(left) != 0 {
		t.Fatalf("still pending after setup: %+v", left)
	}
}

func TestSetupBriefNamesInstallableItemsOnly(t *testing.T) {
	r := Report{Items: []Item{
		{Bucket: Installable, Name: "jq"},
		{Bucket: Impossible, Name: "nvcc", Reason: "needs CUDA"},
		{Bucket: Present, Name: "git"},
	}}
	brief := r.SetupBrief()
	if !strings.Contains(brief, "needs jq") || strings.Contains(brief, "CUDA") || strings.Contains(brief, "git") {
		t.Fatalf("brief:\n%s", brief)
	}
}
