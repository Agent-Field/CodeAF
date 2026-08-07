package store

import (
	"encoding/json"
	"fmt"
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

// TestPauseDailyRailQuestionQuotesWhatConsentWillDeliver is the honesty
// contract on the one sentence the user is asked to answer. The gate may stop
// on a total the journal has never seen — a catalog-priced generation that has
// not happened — but consent arrives through the head, which recomputes the
// rail from journaled spend alone. A question worded from the inflated total
// promised "$1.10 spent … raises by $1.10" and then raised $1.00.
func TestPauseDailyRailQuestionQuotesWhatConsentWillDeliver(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "estimated-pause.db"))
	if err := graph.RecordUsage(NodeUsage{NodeID: RootID, Cost: 0.80}); err != nil {
		t.Fatal(err)
	}
	rail, posted, err := graph.PauseDailyRailWithAdditionalSpend(1, "rail-session", 0.30)
	if err != nil {
		t.Fatal(err)
	}
	// The gate still decides on everything committed, journaled or not.
	if !posted || !rail.Reached || rail.Spend < 1.099 || rail.Spend > 1.101 ||
		rail.Pending < 0.299 || rail.Pending > 0.301 {
		t.Fatalf("estimated rail = %+v posted=%t", rail, posted)
	}
	messages, err := graph.Messages("rail-session", 0, 0)
	if err != nil || len(messages) != 1 {
		t.Fatalf("estimated rail messages = %+v err=%v", messages, err)
	}
	want := "Daily budget reached -- $0.80 spent of $1.00, and the next step costs $0.30. " +
		"Say the word and I'll continue (raises today's rail by $1.00)."
	if messages[0].Body != want {
		t.Fatalf("estimated rail message = %q, want %q", messages[0].Body, want)
	}

	// The head's arithmetic, which is the arithmetic the question just quoted.
	consent, pending, err := graph.PendingDailyRailApproval(1, "rail-session")
	if err != nil || !pending {
		t.Fatalf("pending approval = %t err=%v", pending, err)
	}
	quoted := fmt.Sprintf("raises today's rail by $%.2f", consent.RaiseAmount())
	if !strings.Contains(messages[0].Body, quoted) {
		t.Fatalf("question %q does not quote the raise consent delivers (%q)", messages[0].Body, quoted)
	}
	if err := graph.RaiseDailyRail(consent.RaiseAmount(), "head:rail-session"); err != nil {
		t.Fatal(err)
	}
	raised, err := graph.DailyRailToday(1)
	if err != nil {
		t.Fatal(err)
	}
	// And the raise it promised is enough to cover the item that stopped it.
	if raised.Ceiling != 2 || raised.WithAdditionalSpend(0.30).Reached {
		t.Fatalf("after consent rail = %+v, want ceiling 2.00 with room for the pending item", raised)
	}
}

// TestEverySessionAtTheRailIsAskedAndOneRaiseFreesThemAll is the two-surface
// case: a TUI and a `serve` browser are two sessions claiming from one graph.
// Suppressing the question globally while looking for consent per session meant
// the second session's work stopped with nothing in its thread to explain it
// and no sentence it could say to restart it.
func TestEverySessionAtTheRailIsAskedAndOneRaiseFreesThemAll(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "two-sessions.db"))
	if err := graph.RecordUsage(NodeUsage{NodeID: RootID, Cost: 1}); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"tui", "web"} {
		rail, posted, err := graph.PauseDailyRail(1, session)
		if err != nil {
			t.Fatal(err)
		}
		if !rail.Reached || !posted {
			t.Fatalf("session %q at the rail = %+v posted=%t, want its own question", session, rail, posted)
		}
		// Once per session per raise, not once per session per tick.
		if _, again, err := graph.PauseDailyRail(1, session); err != nil || again {
			t.Fatalf("session %q asked twice for one raise (err=%v)", session, err)
		}
		messages, err := graph.Messages(session, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 || !strings.HasPrefix(messages[0].Body, DailyRailQuestionPrefix) {
			t.Fatalf("session %q thread = %+v, want exactly one rail question", session, messages)
		}
		if _, pending, err := graph.PendingDailyRailApproval(1, session); err != nil || !pending {
			t.Fatalf("session %q consent = pending %t err=%v, want an answerable question", session, pending, err)
		}
	}

	// One session says yes. The raise is journaled globally, so it lifts the
	// ceiling for both: the other session's question becomes moot rather than
	// unanswerable, and its claims resume without a second consent.
	consent, _, err := graph.PendingDailyRailApproval(1, "tui")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RaiseDailyRail(consent.RaiseAmount(), "head:tui"); err != nil {
		t.Fatal(err)
	}
	web, pending, err := graph.PendingDailyRailApproval(1, "web")
	if err != nil {
		t.Fatal(err)
	}
	if pending || web.Reached {
		t.Fatalf("after another session's raise, web = %+v pending=%t, want unblocked", web, pending)
	}
	rail, posted, err := graph.PauseDailyRail(1, "web")
	if err != nil {
		t.Fatal(err)
	}
	if rail.Reached || posted {
		t.Fatalf("web claim after the raise = %+v posted=%t, want work resumed", rail, posted)
	}
}

// TestDayBoundaryHoldsForBothTimestampSpellings covers the one-second window a
// variable-width timestamp used to hand to the wrong day. Rows journaled before
// the width was fixed must still land on the day they happened.
func TestDayBoundaryHoldsForBothTimestampSpellings(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "boundary.db"))
	now := time.Date(2032, time.March, 14, 12, 0, 0, 0, time.Local)
	start, end := localDayBounds(now)

	// Four rows, one per interesting instant, half of them spelled the way the
	// journal used to spell them: RFC3339Nano, which drops the fractional part
	// entirely on a whole second.
	rows := []struct {
		at    time.Time
		old   bool
		cost  float64
		today bool
	}{
		{at: start, old: true, cost: 1, today: true},
		{at: start.Add(500 * time.Millisecond), old: true, cost: 2, today: true},
		{at: start.Add(-500 * time.Millisecond), old: true, cost: 4, today: false},
		{at: start.Add(500 * time.Millisecond), old: false, cost: 8, today: true},
		{at: end, old: true, cost: 16, today: false},
	}
	var want float64
	for index, row := range rows {
		if err := graph.RecordUsage(NodeUsage{NodeID: RootID, Cost: row.cost}); err != nil {
			t.Fatal(err)
		}
		stamp := formatTime(row.at)
		if row.old {
			stamp = row.at.UTC().Format(time.RFC3339Nano)
		}
		if _, err := graph.db.Exec(`UPDATE usage SET ts = ? WHERE seq = (
			SELECT MAX(seq) FROM usage)`, stamp); err != nil {
			t.Fatalf("row %d: %v", index, err)
		}
		if row.today {
			want += row.cost
		}
	}
	spend, err := graph.spendTodayAt(now)
	if err != nil {
		t.Fatal(err)
	}
	if spend != want {
		t.Fatalf("spend across both timestamp spellings = %.2f, want %.2f", spend, want)
	}
	// The stored spelling of a whole second is still readable as a time.
	var stamp string
	if err := graph.db.QueryRow(`SELECT ts FROM usage ORDER BY seq LIMIT 1`).Scan(&stamp); err != nil {
		t.Fatal(err)
	}
	if parsed, err := parseTime(stamp); err != nil || !parsed.Equal(start) {
		t.Fatalf("parseTime(%q) = %v, %v; want the instant it was written", stamp, parsed, err)
	}
}
