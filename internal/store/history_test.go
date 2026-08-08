package store

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// settleHistoryJob splices one top-level job and settles it, so the row it
// leaves behind carries a finished_at the window can be measured against.
func settleHistoryJob(t *testing.T, graph *Store, id, brief string) {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: id, Brief: brief, Title: brief, Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: brief}); err != nil {
		t.Fatalf("splice %s: %v", id, err)
	}
	claim := mustClaim(t, graph, id, "worker")
	if err := graph.Complete(claim, brief+" — settled"); err != nil {
		t.Fatalf("complete %s: %v", id, err)
	}
}

func historyIDs(nodes []Node) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

// The whole point of the read: newest first, bounded on either side, and never
// including the permanent root or a job that has not settled.
func TestSettledHistoryIsNewestFirstAndWindowed(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "history-window.db"))
	settleHistoryJob(t, graph, "older", "read the older thing")
	settleHistoryJob(t, graph, "newer", "read the newer thing")
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "still-going", Brief: "not finished", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "not finished"}); err != nil {
		t.Fatal(err)
	}

	all, err := graph.SettledHistory(time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := historyIDs(all), []string{"newer", "older"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SettledHistory() = %v, want %v", got, want)
	}

	// A since bound that sits after the older job's finish keeps only the newer.
	boundary := all[0].FinishedAt
	recent, err := graph.SettledHistory(boundary, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := historyIDs(recent), []string{"newer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SettledHistory(since newest) = %v, want %v", got, want)
	}

	// And an until bound before the newer one keeps only the older.
	earlier, err := graph.SettledHistory(time.Time{}, boundary.Add(-time.Nanosecond), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := historyIDs(earlier), []string{"older"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SettledHistory(until) = %v, want %v", got, want)
	}
}

// Tuesday's job asked about on Thursday. Once a territory has packed it away
// every snapshot-derived read loses it; a question about time must not.
func TestSettledHistoryReachesTerritoryPackedWork(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "history-packed.db"))
	members := foldedTerritoryMembers(t, graph, "packed", 4)
	if err := graph.FormTerritory("territory-packed", "Packed work",
		"4 jobs packed away", []string{"node:packed-1"}, members); err != nil {
		t.Fatalf("FormTerritory: %v", err)
	}

	active, err := graph.ActiveNodes()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(active), 2; got != want {
		t.Fatalf("ActiveNodes() held %d rows, want %d — the fixture is not packed", got, want)
	}

	history, err := graph.SettledHistory(time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]bool, len(history))
	for _, node := range history {
		found[node.ID] = true
	}
	for _, id := range members {
		if !found[id] {
			t.Fatalf("packed job %q is invisible to SettledHistory: %v", id, historyIDs(history))
		}
	}
	if found["territory-packed"] {
		t.Fatal("the territory itself is furniture and must not read as a job")
	}
}

// The cap is a bound, not a suggestion.
func TestSettledHistoryClampsItsLimit(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "history-cap.db"))
	for index := 0; index < SettledHistoryCap+3; index++ {
		settleHistoryJob(t, graph, "job-"+string(rune('a'+index)), "settle one more thing")
	}
	rows, err := graph.SettledHistory(time.Time{}, time.Time{}, SettledHistoryCap*10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != SettledHistoryCap {
		t.Fatalf("SettledHistory returned %d rows, want the cap %d", len(rows), SettledHistoryCap)
	}
}
