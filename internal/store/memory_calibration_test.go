package store

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMemoryCalibrationSnapshot is an opt-in, aggregate-only compatibility
// probe. It copies a SQLite backup before opening it, never edits the source,
// and never prints memory content, owner keys, paths, or source-session names.
// Aggregate use counters cannot reconstruct historical session-open recall;
// this measures present-day admission only, without claiming historical labels.
func TestMemoryCalibrationSnapshot(t *testing.T) {
	source := os.Getenv("CODEAF_MEMORY_CALIBRATION_DB")
	if source == "" {
		t.Skip("set CODEAF_MEMORY_CALIBRATION_DB to an isolated SQLite backup")
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatal("cannot open calibration backup")
	}
	defer in.Close()
	copyPath := filepath.Join(t.TempDir(), "calibration.db")
	out, err := os.OpenFile(copyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal("cannot create isolated calibration database")
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal("cannot copy calibration backup")
	}
	s, err := Open(copyPath)
	if err != nil {
		t.Fatal("cannot migrate isolated calibration database")
	}
	defer s.Close()
	var project string
	err = s.db.QueryRow(`SELECT owner FROM memories WHERE status='active' AND owner LIKE 'project:%' AND owner<>? GROUP BY owner ORDER BY COUNT(*) DESC LIMIT 1`, OwnerLegacyProject).Scan(&project)
	if err != nil {
		t.Fatal("calibration requires a provable project owner")
	}
	ctx := MemoryContext{Owners: []string{OwnerUser, OwnerMachine, project}, Project: strings.TrimPrefix(project, "project:"), Now: time.Now()}
	if err := s.RefreshMemoryActivationDue(ctx.Now); err != nil {
		t.Fatal("calibration startup maintenance failed")
	}
	block, err := s.AssembleMemoryBlock(ctx)
	if err != nil {
		t.Fatal("calibration assembly failed")
	}
	filter, args := ownerFilterSQL(ctx.Owners)
	rows, err := s.db.Query(`SELECT id,use_count FROM memories WHERE status='active'`+filter, args...)
	if err != nil {
		t.Fatal("calibration label query failed")
	}
	labels := map[string]bool{}
	useful := 0
	for rows.Next() {
		var id string
		var uses int
		if err := rows.Scan(&id, &uses); err != nil {
			rows.Close()
			t.Fatal("calibration labels unreadable")
		}
		labels[id] = uses > 0
		if uses > 0 {
			useful++
		}
	}
	if err := closeRows(rows); err != nil {
		t.Fatal("calibration labels incomplete")
	}
	selectedUseful, unused, runes, pins := 0, 0, 0, 0
	for _, m := range block.Memories {
		runes += MemoryLineCost(m.Text)
		if m.Rule || m.Origin == OriginPerson {
			pins++
		}
		if labels[m.ID] {
			selectedUseful++
		} else {
			unused++
		}
	}
	if useful == 0 || len(block.Memories) == 0 {
		t.Fatal("calibration needs useful labels and a nonempty selection")
	}
	recall := float64(selectedUseful) / float64(useful)
	unusedShare := float64(unused) / float64(len(block.Memories))
	t.Logf("Present-day snapshot: active=%d useful=%d selected=%d selected-useful=%d recall=%.1f%% unused-share=%.1f%% runes=%d pins=%d omitted=%d. Historical session replay is unavailable without dated usefulness events.", len(labels), useful, len(block.Memories), selectedUseful, 100*recall, 100*unusedShare, runes, pins, block.Omitted)
	if recall < .8 {
		t.Errorf("present-day useful recall %.1f%% is below the design's 80%% reassessment gate", 100*recall)
	}
	if pins == 0 && runes > MemoryBlockRunes {
		t.Errorf("unpinned block exceeds the %d-rune prompt budget", MemoryBlockRunes)
	}
}
