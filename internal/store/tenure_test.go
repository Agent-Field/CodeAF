package store

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCharterEarnsTenureOnlyFromJournaledApprovedGreenFirings(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "tenure.db"))
	charter := mustTestCharter(t, "earned-tenure", CharterActive, CharterRails{
		PerFiringBudgetUSD: 0.20, MaxFiringsPerDay: 5,
	})
	charter.Autonomy, charter.GreenFirings, charter.Demotions = CharterTenured, 9, 4
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	stored := getTestCharter(t, graph, charter.ID)
	if stored.Autonomy != CharterProbation || stored.GreenFirings != 0 || stored.Demotions != 0 {
		t.Fatalf("new charter smuggled autonomy: %+v", stored)
	}

	first := completeApprovedFiring(t, graph, charter.ID, "tenure-green-1", 2)
	stored = getTestCharter(t, graph, charter.ID)
	if stored.Autonomy != CharterProbation || stored.GreenFirings != 1 {
		t.Fatalf("first verified firing = autonomy %s greens %d", stored.Autonomy, stored.GreenFirings)
	}
	if changed, err := graph.RecordCharterFiringOutcome(first, 2); err != nil || changed {
		t.Fatalf("duplicate review changed=%t err=%v", changed, err)
	}

	completeApprovedFiring(t, graph, charter.ID, "tenure-green-2", 2)
	stored = getTestCharter(t, graph, charter.ID)
	if stored.Autonomy != CharterTenured || stored.GreenFirings != 2 {
		t.Fatalf("earned tenure = autonomy %s greens %d", stored.Autonomy, stored.GreenFirings)
	}
	messages, err := graph.Messages("charter-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !messagesContain(messages, "I'll handle this on my own now — say 'back to asking' to revert") {
		t.Fatalf("promotion notice missing: %+v", messages)
	}

	before := stored
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after := getTestCharter(t, graph, charter.ID)
	if after.Autonomy != before.Autonomy || after.GreenFirings != before.GreenFirings || after.Demotions != before.Demotions {
		t.Fatalf("rebuild lost ladder state: before=%+v after=%+v", before, after)
	}
}

func TestProbationFireRequiresApprovedAdmission(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "probation-fire.db"))
	charter := mustTestCharter(t, "probation-fire", CharterActive, CharterRails{
		PerFiringBudgetUSD: 0.20, MaxFiringsPerDay: 2,
	})
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	wakeSeq := beginCheckedTestWake(t, graph, charter.ID)
	_, err := graph.FireCharter(charter.ID, wakeSeq, Subtree{Nodes: []NodeSpec{{
		ID: "unapproved-job", Brief: "must not run", Stage: 1,
	}}}, Provenance{Origin: OriginTrigger, CharterID: charter.ID}, 0, time.Now())
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("ordinary probation fire err = %v, want ErrInvalid", err)
	}
	if _, found, err := graph.Node("unapproved-job"); err != nil || found {
		t.Fatalf("unapproved work found=%t err=%v", found, err)
	}
	stored := getTestCharter(t, graph, charter.ID)
	if !stored.WakePending || !stored.SentinelYes {
		t.Fatalf("rejected admission consumed wake: %+v", stored)
	}
}

func TestProbationDeclineResetsConsecutiveEvidenceWithoutPausing(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "probation-decline.db"))
	charter := mustTestCharter(t, "probation-decline", CharterActive, CharterRails{
		PerFiringBudgetUSD: 0.20, MaxFiringsPerDay: 3,
	})
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	completeApprovedFiring(t, graph, charter.ID, "decline-green-1", 3)
	wakeSeq := beginCheckedTestWake(t, graph, charter.ID)
	if err := graph.DeclineCharterFiring(charter.ID, wakeSeq, "user said not now", false); err != nil {
		t.Fatal(err)
	}
	stored := getTestCharter(t, graph, charter.ID)
	if stored.GreenFirings != 0 || stored.Status != CharterActive || stored.WakePending || stored.Autonomy != CharterProbation {
		t.Fatalf("declined probation firing = %+v", stored)
	}
}

func TestTenuredFailureClassesDemoteAndSecondDemotionPauses(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*testing.T, *Store, string)
		wantReason string
	}{
		{name: "failed subtree", mutate: func(t *testing.T, graph *Store, jobID string) {
			claim, won, err := graph.Claim(jobID, "worker")
			if err != nil || !won {
				t.Fatalf("claim won=%t err=%v", won, err)
			}
			if err := graph.Fail(claim, "worker crashed"); err != nil {
				t.Fatal(err)
			}
		}, wantReason: "subtree failed"},
		{name: "budget breach", mutate: func(t *testing.T, graph *Store, jobID string) {
			if err := graph.RecordUsage(NodeUsage{NodeID: jobID, Cost: 0.25}); err != nil {
				t.Fatal(err)
			}
		}, wantReason: "budget breached"},
		{name: "output rejected", mutate: func(t *testing.T, graph *Store, jobID string) {
			if err := graph.RecordDeliveryGate(jobID, DeliveryGate{Gap: "user rejected the result"}); err != nil {
				t.Fatal(err)
			}
		}, wantReason: "output rejected"},
		{name: "user cancelled", mutate: func(t *testing.T, graph *Store, jobID string) {
			if err := graph.CancelPending(jobID, "cancelled by user"); err != nil {
				t.Fatal(err)
			}
		}, wantReason: "cancelled by user"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := openTestStore(t, filepath.Join(t.TempDir(), "demotion.db"))
			charter := mustTestCharter(t, "demote-"+strings.ReplaceAll(test.name, " ", "-"), CharterActive, CharterRails{
				PerFiringBudgetUSD: 0.10, MaxFiringsPerDay: 4,
			})
			if err := graph.CreateCharter(charter); err != nil {
				t.Fatal(err)
			}
			if err := graph.PromoteCharter(charter.ID, "test fixture earned tenure", false); err != nil {
				t.Fatal(err)
			}
			jobID := "failure-1"
			fireTenuredTestJob(t, graph, charter.ID, jobID)
			test.mutate(t, graph, jobID)
			assessment, decided, err := graph.AssessCharterFiring(jobID)
			if err != nil || !decided || assessment.Success || !strings.Contains(assessment.Reason, test.wantReason) {
				t.Fatalf("assessment = %+v decided=%t err=%v", assessment, decided, err)
			}
			if changed, err := graph.RecordCharterFiringOutcome(assessment, 3); err != nil || !changed {
				t.Fatalf("demotion review changed=%t err=%v", changed, err)
			}
			stored := getTestCharter(t, graph, charter.ID)
			if stored.Autonomy != CharterProbation || stored.Status != CharterActive || stored.Demotions != 1 {
				t.Fatalf("first demotion = %+v", stored)
			}

			if err := graph.PromoteCharter(charter.ID, "re-earned tenure after supervision", false); err != nil {
				t.Fatal(err)
			}
			secondID := "failure-2"
			fireTenuredTestJob(t, graph, charter.ID, secondID)
			claim, won, err := graph.Claim(secondID, "worker")
			if err != nil || !won {
				t.Fatalf("second claim won=%t err=%v", won, err)
			}
			if err := graph.Fail(claim, "second verified failure"); err != nil {
				t.Fatal(err)
			}
			second, decided, err := graph.AssessCharterFiring(secondID)
			if err != nil || !decided {
				t.Fatalf("second assessment = %+v decided=%t err=%v", second, decided, err)
			}
			if _, err := graph.RecordCharterFiringOutcome(second, 3); err != nil {
				t.Fatal(err)
			}
			stored = getTestCharter(t, graph, charter.ID)
			if stored.Autonomy != CharterProbation || stored.Status != CharterPaused || stored.Demotions != 2 {
				t.Fatalf("second demotion = %+v", stored)
			}
		})
	}
}

func completeApprovedFiring(t *testing.T, graph *Store, charterID, jobID string, tenureAfter int) CharterFiringAssessment {
	t.Helper()
	wakeSeq := beginCheckedTestWake(t, graph, charterID)
	command, err := graph.RequestCommand(Command{SessionID: "charter-session", Kind: CommandCharterFire,
		Target: charterID, Instruction: "wake:" + formatTenureWakeSeq(wakeSeq)})
	if err != nil {
		t.Fatal(err)
	}
	disposition, err := graph.FireApprovedCharter(charterID, wakeSeq, Subtree{Nodes: []NodeSpec{{
		ID: jobID, Brief: "verified charter work", Stage: 1,
	}}}, Provenance{Origin: OriginTrigger, CharterID: charterID, Intent: "verified charter work"}, 0, time.Now())
	if err != nil || disposition != FireAdmitted {
		t.Fatalf("approved firing disposition=%s err=%v", disposition, err)
	}
	if err := graph.ResolveCommand(command.Seq, CommandApplied, "approved firing admitted"); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim(jobID, "worker")
	if err != nil || !won {
		t.Fatalf("claim %s won=%t err=%v", jobID, won, err)
	}
	if err := graph.Complete(claim, "verified green"); err != nil {
		t.Fatal(err)
	}
	assessment, decided, err := graph.AssessCharterFiring(jobID)
	if err != nil || !decided || !assessment.Success {
		t.Fatalf("assessment = %+v decided=%t err=%v", assessment, decided, err)
	}
	if changed, err := graph.RecordCharterFiringOutcome(assessment, tenureAfter); err != nil || !changed {
		t.Fatalf("review changed=%t err=%v", changed, err)
	}
	return assessment
}

func formatTenureWakeSeq(seq int64) string { return strconv.FormatInt(seq, 10) }

func fireTenuredTestJob(t *testing.T, graph *Store, charterID, jobID string) {
	t.Helper()
	wakeSeq := beginCheckedTestWake(t, graph, charterID)
	disposition, err := graph.FireCharter(charterID, wakeSeq, Subtree{Nodes: []NodeSpec{{
		ID: jobID, Brief: "autonomous charter work", Stage: 1,
	}}}, Provenance{Origin: OriginTrigger, CharterID: charterID, Intent: "autonomous charter work"}, 0, time.Now())
	if err != nil || disposition != FireAdmitted {
		t.Fatalf("tenured firing disposition=%s err=%v", disposition, err)
	}
}

func beginCheckedTestWake(t *testing.T, graph *Store, charterID string) int64 {
	t.Helper()
	charter := getTestCharter(t, graph, charterID)
	at := time.Now()
	wakeSeq, err := graph.BeginCharterWake(charterID, at, "test occurrence", CharterWatchState{
		NextDue: at.Add(time.Hour), FileFingerprint: charter.FileFingerprint,
		GraphCursor: charter.GraphCursor, GraphDay: charter.GraphDay, GraphTriggered: charter.GraphTriggered,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordSentinelCheck(charterID, SentinelCheck{WakeSeq: wakeSeq, Yes: true, Line: "condition met"}); err != nil {
		t.Fatal(err)
	}
	return wakeSeq
}

func getTestCharter(t *testing.T, graph *Store, id string) Charter {
	t.Helper()
	charter, found, err := graph.Charter(id)
	if err != nil || !found {
		t.Fatalf("charter %s found=%t err=%v", id, found, err)
	}
	return charter
}

func messagesContain(messages []Message, body string) bool {
	for _, message := range messages {
		if message.Body == body {
			return true
		}
	}
	return false
}
