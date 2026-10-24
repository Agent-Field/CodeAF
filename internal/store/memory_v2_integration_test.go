package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The rule predates every status line and sits beyond the common-topic lookup
// window. This exercises indexed candidate collection as well as admission.
func TestV2IndexedCrowdingKeepsOldConciseRule(t *testing.T) {
	s := openTestStore(t, filepath.Join(t.TempDir(), "graph.db"))
	owner := OwnerProject("mock-routing")
	rule, err := s.AddMemory(Memory{ID: "zzzz-concise-rule", Owner: owner, Type: MemoryCorrection, Origin: OriginNoticed, Text: "For the amber queue UI, use shape labels instead of severity labels."})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 320; i++ {
		text := fmt.Sprintf("Task %03d completed: amber queue UI project review passed and build verified;", i)
		text += strings.Repeat(" progress", 50)
		for MemoryLineCost(text) > 400 {
			text = text[:len(text)-1]
		}
		text = text[:len(text)-1] + "."
		if MemoryLineCost(text) != 400 {
			t.Fatal("status fixture must exactly fill twelve budget slots")
		}
		result, err := s.AddMemory(Memory{ID: fmt.Sprintf("a-status-%03d", i), Owner: owner, Type: MemoryProjectState, Origin: OriginNoticed, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.RecordMemoryEvent(result.ID, "used", "mock-history"); err != nil {
			t.Fatal(err)
		}
	}
	// Isolate the admission policy from recency and give the concise rule no
	// historical practice advantage over the practiced progress paragraphs.
	now := time.Now()
	if _, err := s.db.Exec(`UPDATE memory_activation SET mass=1,at=? WHERE memory_id=?`, now.Add(-14*24*time.Hour).Unix(), rule.ID); err != nil {
		t.Fatal(err)
	}
	block, err := s.AssembleMemoryBlock(MemoryContext{Owners: []string{owner}, Project: "mock-routing", Session: "mock-new-chat", Keywords: strings.Fields("Review the amber queue UI labels for this project"), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if !block.Decisions[rule.ID].Riding {
		t.Fatalf("old concise correction absent from actual indexed block: %+v", block.Decisions[rule.ID])
	}
	runes, progress := 0, 0
	for _, memory := range block.Memories {
		if memory.Owner != owner {
			t.Fatal("another project's memory entered the block")
		}
		cost := MemoryLineCost(memory.Text)
		runes += cost
		if memory.SemanticType == MemoryProjectState || (memory.SemanticType == "" && memory.Type == MemoryProjectState) {
			progress += cost
		}
	}
	if runes > MemoryBlockRunes {
		t.Fatalf("ordinary block exceeds budget: %d", runes)
	}
	// The durable lane lends unused space. With one short rule, its actual
	// cost, rather than an artificially empty 40% reservation, is protected.
	if progress > MemoryBlockRunes-MemoryLineCost(rule.Text) {
		t.Fatal("progress consumed the concise rule's protected space")
	}
}
