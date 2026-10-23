package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

func TestSavedVerificationPauseRecoversWithExactChecksWithoutReplayingWork(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run("CHECK_EXIT=1 cargo test --test cli regression_one regression_two")
	run("cargo test --test cli regression_one")
	run("cargo test --test cli regression_two")
	at, err := verification.SnapshotAt(root)
	if err != nil {
		t.Fatal(err)
	}
	it := factory.Item{Kind: factory.KindIssue, Autonomy: factory.AutonomyAuto, Cap: 30, State: factory.StateNeedsYou, Publish: "pr", HeadCommit: at.Head, QKind: factory.QKindOutOfAttempts, Question: "verification blocked", Asking: &factory.Asked{Stage: factory.FollowPRStep}, FollowUp: &factory.PRFollowUp{State: "needs_you", Kind: "pr", Attempt: 2, Limit: 3, HeadSHA: at.Head}, PullRequest: &factory.PullRequest{Number: 11, State: factory.PROpen, HeadSHA: at.Head}, Stream: &factory.Stream{Spent: 18.78, Phases: []factory.Phase{{Name: factory.FollowPRStep, Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 34, Chat: file, Result: &factory.StageResult{Output: "Verification blocked: invalid cargo argv", Repair: &factory.CIRepairResult{Decision: "needs_repair", Readiness: &factory.ReadinessReview{Head: at.Head, DiffReviewed: true, DescriptionMatches: true, FeedbackAddressed: true, NoUnfinishedWork: true}}}}}}}
	if !factoryVerificationRecovered(it, root) {
		t.Fatal("legacy saved job did not recover")
	}
	for _, mode := range []string{"manual", "takeover", "budget", "stopped", "new decision", "already recovered"} {
		t.Run(mode, func(t *testing.T) {
			x := it
			f := *it.FollowUp
			x.FollowUp = &f
			switch mode {
			case "manual":
				x.Autonomy = factory.AutonomyManual
			case "takeover":
				x.Manual = true
			case "budget":
				x.Cap = 18
			case "stopped":
				x.Failure = &factory.Failure{Code: factory.CodeStopped}
			case "new decision":
				x.QKind = "scope"
			case "already recovered":
				x.FollowUp.RecoveryVersion = 1
			}
			if factoryVerificationRecovered(x, root) {
				t.Fatal("protected state automatically resumed")
			}
		})
	}
	factory.ReleaseVerificationPause(&it, time.Now())
	if it.Stream.Spent != 18.78 || it.HeadCommit != at.Head || it.FollowUp.Attempt != 2 || it.QKind != "" || it.FollowUp.RecoveryVersion != 1 {
		t.Fatal("recovery discarded state or reset limits")
	}
	next := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err = os.WriteFile(next, nil, 0600); err != nil {
		t.Fatal(err)
	}
	words := inheritFactoryChecks(factoryrun.ConversationSpec{VerifyRepair: true, Item: it}, next, root)
	if !strings.Contains(words, "2 measured checks") {
		t.Fatalf("checkpoint not inherited: %q", words)
	}
	notes, _ := (&stageConversation{file: next, dir: root}).RepairExecutionNotes()
	for _, n := range notes {
		if strings.HasPrefix(n, verification.FailurePrefix) {
			t.Fatalf("inherited evidence failed: %s", n)
		}
	}
	if err = os.WriteFile(filepath.Join(root, "code.txt"), []byte("later edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if words = inheritFactoryChecks(factoryrun.ConversationSpec{VerifyRepair: true, Item: it}, filepath.Join(t.TempDir(), "transcript.jsonl"), root); words != "" {
		t.Fatal("changed checkout reused old checks")
	}
}
