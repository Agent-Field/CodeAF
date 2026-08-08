package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// The real store must keep satisfying the optional resident-life capability,
// or the presence line silently vanishes in production.
var _ selfActivityReader = (*store.Store)(nil)

func TestPracticeAndSelfRootsNeverBecomeCards(t *testing.T) {
	tests := []struct {
		name string
		root store.Node
	}{
		{
			name: "practice group",
			root: store.Node{
				ID: "practice", Parent: store.RootID, Group: store.PracticeGroup,
				Title: "Practice repo:aforge", Status: store.Running,
				Provenance: store.Provenance{Origin: store.OriginUser, SessionID: "cards"},
			},
		},
		{
			name: "self origin",
			root: store.Node{
				ID: "self", Parent: store.RootID, Title: "Background maintenance",
				Status:     store.Running,
				Provenance: store.Provenance{Origin: store.OriginSelf, SessionID: "cards"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := store.Snapshot{Nodes: []store.Node{{ID: store.RootID}, test.root}}
			if cards := deriveJobCards("cards", snapshot, nil, nil, nil, nil); len(cards) != 0 {
				t.Fatalf("self-directed root produced job cards: %#v", cards)
			}
		})
	}
}

func TestLivePracticeRendersAmbientPresenceInRailAndDock(t *testing.T) {
	snapshot := store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{
			ID: "practice-parser", Parent: store.RootID, Group: store.PracticeGroup,
			Title: "Practice repo:/work/parser", Status: store.Running, CreatedSeq: 9,
			Provenance: store.Provenance{Origin: store.OriginSelf},
		},
	}}
	model := New(&fakeBackend{snapshot: snapshot}, "presence")
	model.snapshot = snapshot
	model.cardSnapshot = snapshot
	model.selfSpendToday = 0.31
	model.graphOpen = true
	model.splitPct = 50
	model.setSize(120, 30)

	want := "practicing: repo:/work/parser · $0.31 on myself today"
	rail := ansi.Strip(model.renderGraphPane())
	if !strings.Contains(rail, want) {
		t.Fatalf("rail is missing practice presence %q:\n%s", want, rail)
	}

	model.graphOpen = false
	model.setSize(90, 24)
	dock := ansi.Strip(model.renderActivityBar())
	if !strings.Contains(dock, want) || strings.Contains(dock, "working") {
		t.Fatalf("collapsed dock presence is wrong:\n%s", dock)
	}
}

func TestHeaderSelfSpendAppearsOnlyWhenNonzero(t *testing.T) {
	backend := &fakeBackend{
		spendToday: 0.44,
		selfSpend:  0.31,
	}
	model := New(backend, "spend")
	model.applyPoll(model.poll()().(pollResultMsg))
	spend := ansi.Strip(model.renderSpend())
	if !strings.Contains(spend, "$0.44 today · 31¢ self") {
		t.Fatalf("header spend is missing self spend: %q", spend)
	}

	backend.selfSpend = 0
	model.applyPoll(model.poll()().(pollResultMsg))
	spend = ansi.Strip(model.renderSpend())
	if strings.Contains(spend, "self") || !strings.Contains(spend, "$0.44") {
		t.Fatalf("zero self spend was not hidden: %q", spend)
	}
}

func TestLearnedPresenceLastsOnePollAndNeverPostsToThread(t *testing.T) {
	now := time.Now()
	snapshot := store.Snapshot{Nodes: []store.Node{
		{ID: store.RootID},
		{
			ID: "practice-parser", Parent: store.RootID, Group: store.PracticeGroup,
			Title: "Practice repo:/work/parser", Status: store.Done,
			Provenance: store.Provenance{Origin: store.OriginSelf},
		},
	}}
	backend := &fakeBackend{
		snapshot: snapshot,
		selfReceipts: []store.SelfReceipt{{
			Seq: 20, Time: now, NodeID: "practice-parser", FactIDs: []int64{21},
		}},
		facts: map[int64]store.Fact{
			21: {Seq: 21, NodeID: "practice-parser", Body: "Validate the recovery token before advancing."},
		},
	}
	model := New(backend, "learned")
	model.applyPoll(model.poll()().(pollResultMsg))
	if got := model.residentPresenceText(); got != "learned: Validate the recovery token before advancing." {
		t.Fatalf("learned presence = %q", got)
	}
	if len(backend.posted) != 0 {
		t.Fatalf("learned presence posted to the thread: %#v", backend.posted)
	}

	model.applyPoll(model.poll()().(pollResultMsg))
	if got := model.residentPresenceText(); got != "" {
		t.Fatalf("learned presence survived the next poll: %q", got)
	}
}
