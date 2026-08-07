package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuestionPracticeRetiresAfterTwoRoundsAndRebuilds(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "practice.db"))
	question, err := graph.RecordQuestion(RootID, "repo:/work/parser",
		"I didn't know which parser recovery preserves malformed records")
	if err != nil {
		t.Fatal(err)
	}
	if question.Kind != FactQuestion || question.Status != QuestionOpen {
		t.Fatalf("question = %+v", question)
	}

	expires := time.Now().Add(time.Hour)
	charter, err := NewCharter("practice-test", "Practice measured gaps",
		WatchSpec{Kind: WatchPoll, Poll: &PollWatch{Condition: "idle", Cadence: time.Minute}},
		"idle and executable", CharterAction{Template: "practice"},
		CharterRails{PerFiringBudgetUSD: 1, MaxFiringsPerDay: 2, ExpiresAt: &expires},
		CharterActive, Ratification{Origin: OriginSelf, Evidence: "test policy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.CreateCharter(charter.WithProposalShape(PracticeCharterShape)); err != nil {
		t.Fatal(err)
	}

	for round := 1; round <= 2; round++ {
		jobID := fmt.Sprintf("practice-round-%d", round)
		stored, found, err := graph.Charter(charter.ID)
		if err != nil || !found {
			t.Fatalf("read charter: found=%t err=%v", found, err)
		}
		wakeSeq, err := graph.BeginCharterWake(charter.ID, time.Now(),
			fmt.Sprintf("question #%d", question.Seq), CharterWatchState{NextDue: time.Now().Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.RecordSentinelCheck(charter.ID, SentinelCheck{
			WakeSeq: wakeSeq, Yes: true, Line: "idle and above threshold",
		}); err != nil {
			t.Fatal(err)
		}
		disposition, err := graph.FirePracticeCharter(charter.ID, wakeSeq,
			Subtree{Nodes: []NodeSpec{{ID: jobID, Brief: "run go test against parser recovery", Stage: 1, Group: PracticeGroup}}},
			question.Seq, 1, 100, 0, time.Now())
		if err != nil || disposition != FireAdmitted {
			t.Fatalf("round %d fire = %s, %v (charter %+v)", round, disposition, err, stored)
		}
		landed, _, err := graph.FactBySeq(question.Seq)
		if err != nil || landed.Status != QuestionPracticing {
			t.Fatalf("round %d practicing question = %+v, %v", round, landed, err)
		}
		claim := mustClaim(t, graph, jobID, "practice-worker")
		if err := graph.RecordUsage(NodeUsage{NodeID: jobID, PromptTokens: 40, CompletionTokens: 60}); err != nil {
			t.Fatal(err)
		}
		if err := graph.RecordSurprise(NodeSurprise{NodeID: jobID,
			ActualTokens: 100, ExpectedTokens: 50, Surprise: 1}); err != nil {
			t.Fatal(err)
		}
		if err := graph.Complete(claim, "go test passed but prediction error did not improve"); err != nil {
			t.Fatal(err)
		}
		status, err := graph.CompleteQuestionPractice(jobID, 1)
		if err != nil {
			t.Fatal(err)
		}
		want := QuestionOpen
		if round == 2 {
			want = QuestionRetired
		}
		if status != want {
			t.Fatalf("round %d status = %s, want %s", round, status, want)
		}
	}

	retired, _, err := graph.FactBySeq(question.Seq)
	if err != nil || retired.Status != QuestionRetired ||
		!strings.Contains(retired.StatusNote, "no surprise reduction after 2 practice rounds") {
		t.Fatalf("retired question = %+v, %v", retired, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, _, err := graph.FactBySeq(question.Seq)
	if err != nil || rebuilt.Status != QuestionRetired || rebuilt.StatusNote != retired.StatusNote {
		t.Fatalf("rebuilt question = %+v, want %+v, err=%v", rebuilt, retired, err)
	}
	rounds, err := graph.QuestionPractices(question.Seq)
	if err != nil || len(rounds) != 2 || rounds[0].CompletionSeq == 0 || rounds[1].CompletionSeq == 0 {
		t.Fatalf("rebuilt rounds = %+v, err=%v", rounds, err)
	}
}

func TestScopeSurprisesAndUserIdleUseJournaledUserWork(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "scopes.db"))
	for job := 1; job <= 2; job++ {
		rootID := fmt.Sprintf("parser-job-%d", job)
		nodes := []NodeSpec{{ID: rootID, Brief: "build parser and run go test", Stage: 2}}
		for leaf := 1; leaf <= 3; leaf++ {
			nodes = append(nodes, NodeSpec{ID: fmt.Sprintf("%s-leaf-%d", rootID, leaf),
				Parent: rootID, Brief: "execute parser fixture test", Stage: 1})
		}
		if err := graph.Splice(RootID, Subtree{Nodes: nodes}, Provenance{
			Origin: OriginUser, Intent: "build the parser and run go test",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := graph.RecordFact(rootID, "repo:/work/parser", FactLesson,
			"Run go test ./... against malformed parser fixtures"); err != nil {
			t.Fatal(err)
		}
		for leaf := 1; leaf <= 3; leaf++ {
			id := fmt.Sprintf("%s-leaf-%d", rootID, leaf)
			claim := mustClaim(t, graph, id, "worker")
			if err := graph.RecordSurprise(NodeSurprise{NodeID: id,
				ActualTokens: 200, ExpectedTokens: 100, Surprise: 1}); err != nil {
				t.Fatal(err)
			}
			if err := graph.Complete(claim, "test passed"); err != nil {
				t.Fatal(err)
			}
		}
		claim := mustClaim(t, graph, rootID, "worker")
		if err := graph.RecordSurprise(NodeSurprise{NodeID: rootID,
			ActualTokens: 200, ExpectedTokens: 100, Surprise: 1}); err != nil {
			t.Fatal(err)
		}
		if err := graph.Complete(claim, "go test passed"); err != nil {
			t.Fatal(err)
		}
	}

	metrics, err := graph.ScopeSurprises(8)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 || metrics[0].Scope != "repo:/work/parser" ||
		metrics[0].Samples != 8 || metrics[0].SettledJobs != 2 ||
		metrics[0].AverageSurprise != 1 || metrics[0].ExpectedTokens != 100 {
		t.Fatalf("scope metrics = %+v", metrics)
	}
	if idle, err := graph.UserIdle(time.Now(), 20*time.Minute); err != nil || idle {
		t.Fatalf("recent user splice idle=%t err=%v", idle, err)
	}
	if idle, err := graph.UserIdle(time.Now().Add(21*time.Minute), 20*time.Minute); err != nil || !idle {
		t.Fatalf("quiet user graph idle=%t err=%v", idle, err)
	}
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "pending-user", Brief: "run another test", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "run another test"}); err != nil {
		t.Fatal(err)
	}
	if idle, err := graph.UserIdle(time.Now().Add(time.Hour), 0); err != nil || idle {
		t.Fatalf("in-flight user graph idle=%t err=%v", idle, err)
	}
}

// TestPracticeFiringsReserveTheirBudgetBeforeSpendingIt pins the difference
// between projecting and reserving. Practice spend is journaled only as leaves
// land, so a carve-out read from the usage table alone sees nothing at all at
// the moment it decides: every firing of the day was admitted before any of
// them had recorded a cent, and the daily dollar ceiling first bit on the
// following day. Each admitted firing now holds its per-firing budget against
// the group's ceiling until its own spend overtakes it.
func TestPracticeFiringsReserveTheirBudgetBeforeSpendingIt(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "practice-reserve.db"))
	expires := time.Now().Add(time.Hour)
	// Two practice charters, one shared carve-out: $1 a firing, two firings a
	// day, so the practice class may commit $2 today however it spreads them.
	for _, id := range []string{"practice-a", "practice-b"} {
		charter, err := NewCharter(id, "Practice measured gaps",
			WatchSpec{Kind: WatchPoll, Poll: &PollWatch{Condition: "idle", Cadence: time.Minute}},
			"idle and executable", CharterAction{Template: "practice"},
			CharterRails{PerFiringBudgetUSD: 1, MaxFiringsPerDay: 2, ExpiresAt: &expires},
			CharterActive, Ratification{Origin: OriginSelf, Evidence: "test policy"})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.CreateCharter(charter.WithProposalShape(PracticeCharterShape)); err != nil {
			t.Fatal(err)
		}
	}

	fire := func(charterID, jobID string) FireDisposition {
		t.Helper()
		question, err := graph.RecordQuestion(RootID, "repo:/work/"+jobID,
			"I didn't know which parser recovery preserves malformed records for "+jobID)
		if err != nil {
			t.Fatal(err)
		}
		wakeSeq, err := graph.BeginCharterWake(charterID, time.Now(),
			fmt.Sprintf("question #%d", question.Seq), CharterWatchState{NextDue: time.Now().Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.RecordSentinelCheck(charterID, SentinelCheck{
			WakeSeq: wakeSeq, Yes: true, Line: "idle and above threshold"}); err != nil {
			t.Fatal(err)
		}
		disposition, err := graph.FirePracticeCharter(charterID, wakeSeq,
			Subtree{Nodes: []NodeSpec{{ID: jobID, Brief: "run go test against parser recovery",
				Stage: 1, Group: PracticeGroup}}}, question.Seq, 1, 100, 0, time.Now())
		if err != nil {
			t.Fatalf("fire %s: %v", jobID, err)
		}
		return disposition
	}

	// Nothing has journaled a cent yet, and nothing needs to: the first two
	// firings hold the whole carve-out between them.
	if got := fire("practice-a", "practice-1"); got != FireAdmitted {
		t.Fatalf("first practice firing = %s, want admitted", got)
	}
	if got := fire("practice-b", "practice-2"); got != FireAdmitted {
		t.Fatalf("second practice firing = %s, want admitted", got)
	}
	if got := fire("practice-a", "practice-3"); got != FireQuota {
		t.Fatalf("third practice firing = %s, want the carve-out to refuse it", got)
	}
	blocked, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	reason := ""
	for _, event := range blocked {
		if event.Kind != EventCharterFiringBlocked {
			continue
		}
		var payload struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		reason = payload.Reason
	}
	if reason != "practice_daily_dollar_rail" {
		t.Fatalf("refusal reason = %q, want the practice dollar rail", reason)
	}

	// A firing that outspends its reservation is counted at what it truly
	// cost, not twice and not at the smaller of the two.
	if err := graph.RecordUsage(NodeUsage{NodeID: "practice-1", Cost: 1.75}); err != nil {
		t.Fatal(err)
	}
	if got := fire("practice-b", "practice-4"); got != FireQuota {
		t.Fatalf("firing after an overspend = %s, want refused", got)
	}
}
