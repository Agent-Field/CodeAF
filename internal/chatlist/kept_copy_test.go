package chatlist

import (
	"strings"
	"testing"
)

// A copy that Home folded onto its chat's row must still be able to say which
// branch it is, because the surface raises the same branch card for it as for a
// branch row that stands alone, and that card acts on the copy's own id (#2780).
func TestAFoldedCopyIsTheBranchRowTheDirectoryListedAndIsOfferedAsOne(t *testing.T) {
	rows := []Row{
		{Cell: "chat-main", Title: "Refactor router", Device: "tower", Status: Here, Mine: true, Head: "turn-3"},
		{Cell: "kept-a", Title: "Refactor router", Device: "laptop", Parent: "chat-main", Status: Branch, Head: "turn-a", OrphanTurns: 1},
		{Cell: "kept-b", Title: "Refactor router", Device: "desk", Parent: "chat-main", Status: Branch, Head: "turn-3", OrphanTurns: 4},
	}
	collapsed, _ := CollapseCopies(rows, nil)
	if len(collapsed) != 1 || len(collapsed[0].Copies) != 2 {
		t.Fatalf("collapsed = %+v, want the chat's one row carrying both copies", collapsed)
	}
	for i, kept := range collapsed[0].Copies {
		got := kept.Row("chat-main", "Refactor router")
		want := rows[i+1]
		if got.Cell != want.Cell || got.Device != want.Device || got.Parent != want.Parent || got.Title != want.Title ||
			got.OrphanTurns != want.OrphanTurns || got.Status != Branch {
			t.Errorf("copy %d as a row = %+v, want the directory's branch %+v", i, got, want)
		}
		if offer := OfferFor(got); offer.Kind != Merge {
			t.Errorf("copy %d is offered %v, want the branch offer a standalone branch row gets", i, offer.Kind)
		}
	}
	if rows := collapsed[0].Copies; rows[0].Redundant || !rows[1].Redundant {
		t.Fatalf("redundancy = %+v, want [false, true]: a copy at the chat's own head holds nothing new", rows)
	}
}

// The same when the chat is known only on this computer: the copies come back
// under the chat's id, in the listing's order, one for each the note counts.
func TestCopiesOfALocalChatAreReachableByIDInListingOrder(t *testing.T) {
	rows := []Row{
		{Cell: "kept-a", Parent: "local-chat", Device: "laptop", Status: Branch, OrphanTurns: 2},
		{Cell: "kept-b", Parent: "local-chat", Device: "mac", Status: Branch, OrphanTurns: 1, BranchRedundant: true},
	}
	collapsed, byLocal := CollapseCopies(rows, map[string]bool{"local-chat": true})
	if len(collapsed) != 0 {
		t.Fatalf("collapsed = %+v, want every copy folded onto the local chat", collapsed)
	}
	var ids []string
	for _, c := range byLocal["local-chat"] {
		ids = append(ids, c.Cell)
	}
	if strings.Join(ids, ",") != "kept-a,kept-b" {
		t.Fatalf("copies = %v, want kept-a,kept-b", ids)
	}
	if line := CopiesLine(byLocal["local-chat"]); !strings.Contains(line, "2 copies") {
		t.Fatalf("the note counts %q, want 2 copies", line)
	}
}

func TestCopyLineSaysWhatTheCopyIsAndWhetherDiscardingLosesAnything(t *testing.T) {
	fresh := BranchCopy{Cell: "kept-a", Device: "laptop", OrphanTurns: 1}
	same := BranchCopy{Cell: "kept-b", Device: "mac", OrphanTurns: 3, Redundant: true}
	for _, tc := range []struct {
		name         string
		copy         BranchCopy
		nth, of      int
		merge, sep   bool
		want, absent string
	}{
		{"one copy, open and discard", fresh, 1, 1, false, true, "1 turn from laptop: open separately / discard", "copy 1"},
		{"one copy, no open behind it", fresh, 1, 1, false, false, "1 turn from laptop: discard", "open separately"},
		{"a copy already in the chat", same, 1, 1, false, true, "3 turns from mac: open separately / discard; already in this chat, so discarding loses nothing", "(copy"},
		{"the second of three", fresh, 2, 3, false, true, "1 turn from laptop: open separately / discard (copy 2 of 3)", "loses nothing"},
	} {
		got := CopyLine(tc.copy, tc.nth, tc.of, tc.merge, tc.sep)
		if got != tc.want || strings.Contains(got, tc.absent) {
			t.Errorf("%s: CopyLine = %q, want %q without %q", tc.name, got, tc.want, tc.absent)
		}
	}
	// The row's note and the card for the copy say the loss clause in one voice.
	if note := CopiesLine([]BranchCopy{same}); !strings.Contains(note, "already in this chat, so discarding loses nothing") {
		t.Errorf("the note's clause drifted from the card's: %q", note)
	}
}
