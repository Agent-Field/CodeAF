package baked

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/packed"
)

// TestBakedAgentsAreTheFolderOnDisk is what makes packing safe: agents/ is the
// source of truth, agents.pack.gz is generated from it, and an agent edited
// without regenerating fails here rather than shipping the old prompt.
func TestBakedAgentsAreTheFolderOnDisk(t *testing.T) {
	if err := packed.Verify(agentArchive, "agents"); err != nil {
		t.Fatalf("%v\n\nrun: go generate ./internal/swepro/internal/baked", err)
	}
}

// TestEveryRosterAgentUnpacks reads the roster the way the pipeline does, so a
// name that lost its file is a failing test rather than a panic in a swe run.
func TestEveryRosterAgentUnpacks(t *testing.T) {
	for _, name := range ListBakedAgents() {
		markdown, ok := GetBakedAgentMarkdown(name)
		if !ok || markdown == "" {
			t.Errorf("baked agent %q unpacked to nothing", name)
		}
	}
}
