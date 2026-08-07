package store

import (
	"path/filepath"
	"testing"
)

func TestResidentWatermarksReplayFromTheJournal(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "watermark.db"))

	if _, found, err := graph.ResidentWatermarkFor(LaneSettlement); err != nil || found {
		t.Fatalf("virgin watermark = found %t err %v", found, err)
	}
	first, err := graph.MarkResidentWatermark(LaneSettlement, 41)
	if err != nil {
		t.Fatal(err)
	}
	second, err := graph.MarkResidentWatermark(LaneSettlement, 97)
	if err != nil || second.Seq <= first.Seq {
		t.Fatalf("second mark = %+v err %v", second, err)
	}
	if _, err := graph.MarkResidentWatermark(LaneConsolidation, 0); err != nil {
		t.Fatal(err)
	}
	// Lanes are independent: one advancing must not move the other.
	settlement, found, err := graph.ResidentWatermarkFor(LaneSettlement)
	if err != nil || !found || settlement.Cursor != 97 {
		t.Fatalf("settlement = %+v found %t err %v", settlement, found, err)
	}
	consolidation, found, err := graph.ResidentWatermarkFor(LaneConsolidation)
	if err != nil || !found || consolidation.Cursor != 0 || consolidation.At.IsZero() {
		t.Fatalf("consolidation = %+v found %t err %v", consolidation, found, err)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	replayed, found, err := graph.ResidentWatermarkFor(LaneSettlement)
	if err != nil || !found || replayed.Cursor != settlement.Cursor || replayed.Seq != settlement.Seq {
		t.Fatalf("replayed settlement = %+v found %t err %v", replayed, found, err)
	}
	replayed, found, err = graph.ResidentWatermarkFor(LaneConsolidation)
	if err != nil || !found || !replayed.At.Equal(consolidation.At) {
		t.Fatalf("replayed consolidation = %+v found %t err %v", replayed, found, err)
	}

	if _, err := graph.MarkResidentWatermark("", 1); err == nil {
		t.Fatal("empty lane was accepted")
	}
	if _, err := graph.MarkResidentWatermark(LaneSettlement, -1); err == nil {
		t.Fatal("negative cursor was accepted")
	}
}
