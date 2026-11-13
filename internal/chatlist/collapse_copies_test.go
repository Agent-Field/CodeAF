package chatlist

import (
	"strings"
	"testing"
	"time"
)

func TestCollapseCopiesIntoMainRowAndExplainLoss(t *testing.T) {
	rows := []Row{
		{
			Cell:       "chat-main",
			Title:      "Refactor router",
			Device:     "tower",
			Status:     Running,
			Head:       "turn-3",
			DurableAgo: 2 * time.Minute,
		},
		{
			Cell:        "chat-copy-same",
			Title:       "Refactor router",
			Device:      "laptop",
			Parent:      "chat-main",
			Status:      Branch,
			Head:        "turn-3",
			OrphanTurns: 1,
			DurableAgo:  5 * time.Minute,
		},
		{
			Cell:        "chat-copy-extra",
			Title:       "Refactor router",
			Device:      "desk",
			Parent:      "chat-main",
			Status:      Branch,
			Head:        "turn-other",
			OrphanTurns: 2,
			DurableAgo:  6 * time.Minute,
		},
		{
			Cell:        "orphan-no-parent",
			Title:       "Standalone branch",
			Device:      "mac",
			Status:      Branch,
			OrphanTurns: 1,
			DurableAgo:  10 * time.Minute,
		},
	}

	collapsed, byLocal := CollapseCopies(rows, nil)
	if len(byLocal) != 0 {
		t.Fatalf("unexpected local copies: %+v", byLocal)
	}
	if len(collapsed) != 2 {
		t.Fatalf("collapsed len = %d, want 2: %+v", len(collapsed), collapsed)
	}
	main := collapsed[0]
	if main.Cell != "chat-main" || len(main.Copies) != 2 {
		t.Fatalf("main row copies = %+v, want 2 collapsed copies", main)
	}
	if !main.Copies[0].Redundant || main.Copies[1].Redundant {
		t.Fatalf("copy redundancy = %+v, want [true, false]", main.Copies)
	}

	line := CopiesLine(main.Copies)
	if !strings.Contains(line, "2 copies") || !strings.Contains(line, "unsent turns set aside") || !strings.Contains(line, "discarding archives 2 turns not in this chat") {
		t.Fatalf("non-redundant CopiesLine = %q", line)
	}

	onlyRedundant := CopiesLine(main.Copies[:1])
	if !strings.Contains(onlyRedundant, "1 copy") || !strings.Contains(onlyRedundant, "unsent turns set aside") || !strings.Contains(onlyRedundant, "discarding loses nothing") {
		t.Fatalf("redundant CopiesLine = %q", onlyRedundant)
	}

	takeover := TakeoverLine(main)
	if !strings.Contains(takeover, line) {
		t.Fatalf("TakeoverLine = %q, want collapsed copy explanation %q", takeover, line)
	}
}

func TestCollapseCopiesAttachesToLocalOnlyChatAndComparesInSame(t *testing.T) {
	rows := []Row{
		{
			Cell:            "branch-1",
			Title:           "Local main",
			Device:          "laptop",
			Parent:          "local-main",
			Status:          Branch,
			OrphanTurns:     3,
			BranchRedundant: true,
		},
	}
	collapsed, byLocal := CollapseCopies(rows, map[string]bool{"local-main": true})
	if len(collapsed) != 0 {
		t.Fatalf("collapsed = %+v, want branch folded into local row", collapsed)
	}
	if got := byLocal["local-main"]; len(got) != 1 || !got[0].Redundant {
		t.Fatalf("byLocal = %+v, want 1 redundant copy", byLocal)
	}

	a := []Row{{Cell: "c1", Copies: []BranchCopy{{Cell: "b1", OrphanTurns: 1, Redundant: true}}}}
	b := []Row{{Cell: "c1", Copies: []BranchCopy{{Cell: "b1", OrphanTurns: 1, Redundant: false}}}}
	if Same(a, b) {
		t.Fatal("Same ignored Copies difference")
	}
	if !Same(a, []Row{{Cell: "c1", DurableAgo: time.Minute, Copies: []BranchCopy{{Cell: "b1", OrphanTurns: 1, Redundant: true}}}}) {
		t.Fatal("Same rejected identical Copies with different DurableAgo")
	}
}
