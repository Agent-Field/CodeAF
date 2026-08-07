package store

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTopLevelJobUsageMeansDefinedLeafSurprises(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "surprise.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "deliver", Stage: 2},
		{ID: "known-a", Parent: "job", Brief: "first measured leaf", Stage: 1},
		{ID: "known-b", Parent: "job", Brief: "second measured leaf", Stage: 1},
		{ID: "unknown", Parent: "job", Brief: "cold bucket leaf", Stage: 1},
	}}, Provenance{Origin: OriginUser, Intent: "measure prediction error"}); err != nil {
		t.Fatal(err)
	}
	for _, usage := range []NodeUsage{
		{NodeID: "known-a", PromptTokens: 120},
		{NodeID: "known-b", PromptTokens: 45},
		{NodeID: "unknown", PromptTokens: 900},
	} {
		if err := graph.RecordUsage(usage); err != nil {
			t.Fatal(err)
		}
	}
	for _, surprise := range []NodeSurprise{
		{NodeID: "known-a", ActualTokens: 120, ExpectedTokens: 60, Surprise: 1},
		{NodeID: "known-b", ActualTokens: 45, ExpectedTokens: 12, Surprise: 2.75},
	} {
		if err := graph.RecordSurprise(surprise); err != nil {
			t.Fatal(err)
		}
	}

	assert := func(stage string) {
		t.Helper()
		jobs, err := graph.TopLevelJobUsage()
		if err != nil {
			t.Fatal(err)
		}
		got := jobs["job"]
		if got.SurpriseSamples != 2 || got.Surprise == nil || *got.Surprise != 1.875 {
			t.Fatalf("%s aggregate surprise = %+v, want mean 1.875 over two defined leaves", stage, got)
		}
		if got.SurpriseTokens != 165 || got.ExpectedTokens != 72 {
			t.Fatalf("%s prediction totals = actual %d expected %d, want 165/72", stage, got.SurpriseTokens, got.ExpectedTokens)
		}
	}
	assert("incremental")
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assert("rebuilt")

	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "cold-job", Brief: "no expectation", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "cold work"}); err != nil {
		t.Fatal(err)
	}
	jobs, err := graph.TopLevelJobUsage()
	if err != nil {
		t.Fatal(err)
	}
	if jobs["cold-job"].Surprise != nil {
		t.Fatalf("all-absent job surprise = %v, want absent", *jobs["cold-job"].Surprise)
	}
}

func TestSpendTodayUsesLocalMidnightBoundary(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "midnight.db"))
	for _, cost := range []float64{1, 2, 4} {
		if err := graph.RecordUsage(NodeUsage{NodeID: RootID, Cost: cost}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := graph.db.Query(`SELECT seq FROM usage ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, seq)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(seqs) != 3 {
		t.Fatalf("usage rows = %d, want 3", len(seqs))
	}

	now := time.Date(2032, time.March, 14, 12, 0, 0, 0, time.Local)
	start, end := localDayBounds(now)
	for index, at := range []time.Time{start.Add(-time.Nanosecond), start, end} {
		if _, err := graph.db.Exec(`UPDATE usage SET ts = ? WHERE seq = ?`, formatTime(at), seqs[index]); err != nil {
			t.Fatal(err)
		}
	}
	spend, err := graph.spendTodayAt(now)
	if err != nil {
		t.Fatal(err)
	}
	if spend != 2 {
		t.Fatalf("spend at local midnight boundary = %.2f, want 2.00", spend)
	}
}

func TestDailyRailCeilingRaisesAndRebuild(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "rail.db"))
	if err := graph.RecordUsage(NodeUsage{NodeID: RootID, Cost: 3.25}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RaiseDailyRail(5, "head:test"); err != nil {
		t.Fatal(err)
	}
	if err := graph.RaiseDailyRail(2, "headless:test"); err != nil {
		t.Fatal(err)
	}

	assert := func(stage string) {
		t.Helper()
		rail, err := graph.DailyRailToday(20)
		if err != nil {
			t.Fatal(err)
		}
		if rail.Spend != 3.25 || rail.Raised != 7 || rail.Ceiling != 27 || rail.Reached {
			t.Fatalf("%s rail = %+v, want spend 3.25 raised 7 ceiling 27", stage, rail)
		}
	}
	assert("incremental")

	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var raises []RailAdjustment
	for _, event := range events {
		if event.Kind != EventRailRaised {
			continue
		}
		var raise RailAdjustment
		if err := json.Unmarshal(event.Payload, &raise); err != nil {
			t.Fatal(err)
		}
		raises = append(raises, raise)
	}
	if len(raises) != 2 || raises[0].Amount != 5 || raises[0].Origin != "head:test" ||
		raises[1].Amount != 2 || raises[1].Origin != "headless:test" {
		t.Fatalf("rail events = %+v", raises)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assert("rebuilt")
}

func TestPauseDailyRailPostsOneQuestionPerRaise(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "pause.db"))
	if err := graph.RecordUsage(NodeUsage{NodeID: RootID, Cost: 1}); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		rail, posted, err := graph.PauseDailyRail(1, "rail-session")
		if err != nil {
			t.Fatal(err)
		}
		if !rail.Reached || posted != (attempt == 0) {
			t.Fatalf("pause %d = rail %+v posted=%t", attempt, rail, posted)
		}
	}
	messages, err := graph.Messages("rail-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := "Daily budget reached -- $1.00 spent of $1.00. Say the word and I'll continue (raises today's rail by $1.00)."
	if len(messages) != 1 || messages[0].Role != RoleAgent || messages[0].Body != want {
		t.Fatalf("rail questions = %+v, want exactly %q", messages, want)
	}
	if err := graph.RaiseDailyRail(1, "test:user"); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordUsage(NodeUsage{NodeID: RootID, Cost: 1}); err != nil {
		t.Fatal(err)
	}
	if _, posted, err := graph.PauseDailyRail(1, "rail-session"); err != nil || !posted {
		t.Fatalf("question after raise posted=%t err=%v", posted, err)
	}
	messages, err = graph.Messages("rail-session", 0, 0)
	if err != nil || len(messages) != 2 {
		t.Fatalf("questions after raise = %d err=%v, want 2", len(messages), err)
	}
}

func TestPauseDailyRailIncludesKnownUpcomingSpend(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "estimated-pause.db"))
	if err := graph.RecordUsage(NodeUsage{NodeID: RootID, Cost: 0.80}); err != nil {
		t.Fatal(err)
	}
	rail, posted, err := graph.PauseDailyRailWithAdditionalSpend(1, "rail-session", 0.30)
	if err != nil {
		t.Fatal(err)
	}
	if !posted || !rail.Reached || rail.Spend < 1.099 || rail.Spend > 1.101 {
		t.Fatalf("estimated rail = %+v posted=%t", rail, posted)
	}
	messages, err := graph.Messages("rail-session", 0, 0)
	if err != nil || len(messages) != 1 || !strings.Contains(messages[0].Body, "$1.10 spent of $1.00") {
		t.Fatalf("estimated rail message = %+v err=%v", messages, err)
	}
}
