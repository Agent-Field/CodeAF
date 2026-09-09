package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestHomePreviewKeepsALargeFamilyCompactAndReachable(t *testing.T) {
	now := time.Now()
	entries := []session.TaskIndexEntry{treeTask("1", "", "Repair parser pipeline", session.TaskDone, now)}
	for id := 2; id <= 100; id++ {
		entries = append(entries, treeTask(itoa(id), "1", "Parser regression "+itoa(id), session.TaskDone, now))
	}
	a := treeLab(t, homeCardWidest, entries...)
	text := homeText(a)
	if !strings.Contains(text, "Repair parser pipeline") || !strings.Contains(text, "97 more tasks") {
		t.Fatalf("the large family displaced its own preview or full-tree door:\n%s", text)
	}
}

func TestHomeSiblingNamesKeepDistinctClickAndTapTargets(t *testing.T) {
	now := time.Now()
	entries := []session.TaskIndexEntry{treeTask("1", "", "Repair parser pipeline", session.TaskDone, now)}
	for id := 2; id <= 4; id++ {
		entries = append(entries, treeTask(itoa(id), "1", "Add regression coverage", session.TaskDone, now.Add(-time.Duration(id)*time.Minute)))
	}
	a := treeLab(t, homeCardWidest, entries...)
	card := workCard(t, a)
	var ids []string
	for _, line := range card {
		if entry, ok := a.cardTaskAt(line); ok {
			ids = append(ids, entry.ID)
		}
	}
	if strings.Join(ids, ",") != "1,2,3" {
		t.Fatalf("identical sibling names resolved to %v", ids)
	}
	row := treeRow(entries...)
	ctx := ambientBandContext(a, row, now, 44)
	rows := drawWorkBand(a, ctx)
	hits := homeSheetTaskHits(rows, ctx)
	ids = nil
	for _, hit := range hits {
		if hit.kind == homeSheetHitTask {
			ids = append(ids, row.Tasks.Rows[hit.index].ID)
		}
	}
	if strings.Join(ids, ",") != "1,2,3,4" {
		t.Fatalf("identical sibling names resolved to phone targets %v", ids)
	}
}
