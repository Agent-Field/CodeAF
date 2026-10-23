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
	it.Stream.Phases[0].At = time.Now()
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
	if words = inheritFactoryChecks(factoryrun.ConversationSpec{VerifyRepair: true, Item: it}, filepath.Join(t.TempDir(), "transcript.jsonl"), root); !strings.Contains(words, "0 measured checks") || !strings.Contains(words, "unresolved checks retained") {
		t.Fatal("changed checkout reused old passes or forgot the unresolved failure", words)
	}
}

func TestTestCheckpointSelectivelyReusesPassAndRetainsIndependentFailure(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run("cargo test --test cli passed")
	run("CHECK_EXIT=7 cargo test --test other broken")
	original, err := verification.Read(file)
	if err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(t.TempDir(), "next.jsonl")
	if err = os.WriteFile(next, nil, 0600); err != nil {
		t.Fatal(err)
	}
	it := factory.Item{Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Chat: file, At: time.Now()}}}}
	words := inheritFactoryChecks(factoryrun.ConversationSpec{VerificationStage: "test", Item: it}, next, root)
	if !strings.Contains(words, "1 measured checks") || !strings.Contains(words, "1 unresolved") {
		t.Fatal(words)
	}
	saved, err := verification.Read(next)
	if err != nil || len(saved) != 2 {
		t.Fatalf("selective inheritance: %+v %v", saved, err)
	}
	for _, s := range saved {
		if s.Source != file || s.CompletedAt == nil {
			t.Fatal("original provenance missing", s)
		}
	}
	if original[0].Source != saved[0].Source {
		t.Fatal("source lost")
	}
	notes, _ := (&stageConversation{file: next, dir: root}).RepairExecutionNotes()
	blocked := false
	for _, n := range notes {
		if strings.HasPrefix(n, verification.FailurePrefix) && strings.Contains(n, "broken") {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("independent failing requirement vanished: %v", notes)
	}
}

func TestFinishedRoundPassSurvivesAnInterruptedIdenticalRepeat(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run("cargo test --test cli regression")
	run("cargo test --test cli regression")
	run("cargo test --test other")
	checks, err := verification.Read(file)
	if err != nil || len(checks) != 3 {
		t.Fatalf("%+v %v", checks, err)
	}
	for _, c := range checks[1:] {
		if err = os.Remove(filepath.Join(file+".checks", c.ID) + ".exit"); err != nil {
			t.Fatal(err)
		}
	}
	it := factory.Item{Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Rounds: []factory.RoundLine{{At: time.Now(), Chat: file}}}}}}
	next := filepath.Join(t.TempDir(), "next.jsonl")
	os.WriteFile(next, nil, 0600)
	words := inheritFactoryChecks(factoryrun.ConversationSpec{VerificationStage: "test", Item: it}, next, root)
	if !strings.Contains(words, "1 measured checks") || !strings.Contains(words, "1 unresolved") {
		t.Fatal(words)
	}
	saved, err := verification.Read(next)
	if err != nil || len(saved) != 3 {
		t.Fatalf("history not preserved: %+v %v", saved, err)
	}
	superseded := 0
	for _, c := range saved {
		if c.SupersededBy != "" {
			superseded++
			if c.Exit != nil || c.Source != file {
				t.Fatal("interruption was fabricated as success", c)
			}
		}
	}
	if superseded != 1 {
		t.Fatal("interrupted repeat not explicitly retained")
	}
	notes, _ := (&stageConversation{file: next, dir: root}).RepairExecutionNotes()
	unknown := 0
	for _, n := range notes {
		if strings.HasPrefix(n, verification.FailurePrefix) && strings.Contains(n, "unknown") {
			unknown++
			if !strings.Contains(n, "other") {
				t.Fatal("qualifying original pass was thrown away", n)
			}
		}
	}
	if unknown != 1 {
		t.Fatalf("broader unknown was lost: %v", notes)
	}
	// Active/uncertain physical source conversations cannot supply reusable passes.
	it.Stream.Phases[0].Rounds = nil
	it.Stream.Phases[0].Chat = file
	it.Stream.Phases[0].State = factory.PhaseRunning
	if got := inheritFactoryChecks(factoryrun.ConversationSpec{VerificationStage: "test", Item: it}, filepath.Join(t.TempDir(), "active.jsonl"), root); got != "" {
		t.Fatal("active source reused", got)
	}
}

func TestLaterDefiniteFailurePreventsReusingAnOlderPass(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run("cargo test --test cli regression")
	run("CHECK_EXIT=7 cargo test --test cli regression")
	it := factory.Item{Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", At: time.Now(), Chat: file}}}}
	next := filepath.Join(t.TempDir(), "next.jsonl")
	if got := inheritFactoryChecks(factoryrun.ConversationSpec{VerificationStage: "proof", Item: it}, next, root); !strings.Contains(got, "0 measured checks") || !strings.Contains(got, "1 unresolved") {
		t.Fatal("newer failure was not carried into the next round", got)
	}
	checks, err := verification.Read(next)
	if err != nil || len(checks) != 1 || checks[0].Exit == nil || *checks[0].Exit != 7 {
		t.Fatal("old pass erased newer failure", checks, err)
	}
}

func TestSameContentFromAnotherWorktreeDoesNotAuthorizeCheckpoint(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run("cargo test --test cli regression")
	checks, err := verification.Read(file)
	if err != nil {
		t.Fatal(err)
	}
	foreign := t.TempDir()
	checks[0].Cwd = foreign
	checks[0].OwnerRoot = foreign
	source := filepath.Join(t.TempDir(), "foreign.jsonl")
	if err = verification.Inherit(source, checks); err != nil {
		t.Fatal(err)
	}
	it := factory.Item{Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", At: time.Now(), Chat: source}}}}
	if got := inheritFactoryChecks(factoryrun.ConversationSpec{VerificationStage: "test", Item: it}, filepath.Join(t.TempDir(), "new.jsonl"), root); got != "" {
		t.Fatal("another worktree authorized same-content verification", got)
	}
}

func TestTestCheckpointRetainsRequiredScopeAfterAnEdit(t *testing.T) {
	root, file, run := repairCheckFixture(t)
	run("cargo test --test cli regression")
	if err := os.WriteFile(filepath.Join(root, "code.txt"), []byte("edited"), 0600); err != nil {
		t.Fatal(err)
	}
	it := factory.Item{Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", At: time.Now(), Chat: file}}}}
	next := filepath.Join(t.TempDir(), "next.jsonl")
	if err := os.WriteFile(next, nil, 0600); err != nil {
		t.Fatal(err)
	}
	words := inheritFactoryChecks(factoryrun.ConversationSpec{VerificationStage: "test", Item: it}, next, root)
	if !strings.Contains(words, "0 measured checks") || !strings.Contains(words, "1 unresolved") {
		t.Fatal("required stale scope was forgotten", words)
	}
	notes, _ := (&stageConversation{file: next, dir: root}).ExecutionNotes()
	blocked := false
	for _, n := range notes {
		blocked = blocked || strings.HasPrefix(n, verification.FailurePrefix) && strings.Contains(n, "stale")
	}
	if !blocked {
		t.Fatal("new round completed without rechecking changed inputs")
	}
}

func TestTestCheckpointDoesNotForgetUnreadablePriorEvidence(t *testing.T) {
	root, file, _ := repairCheckFixture(t)
	if err := os.MkdirAll(file+".checks", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(file+".checks", "inherited.json"), []byte("broken committed records"), 0600); err != nil {
		t.Fatal(err)
	}
	it := factory.Item{Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", At: time.Now(), Chat: file}}}}
	next := filepath.Join(t.TempDir(), "next.jsonl")
	if err := os.WriteFile(next, nil, 0600); err != nil {
		t.Fatal(err)
	}
	words := inheritFactoryChecks(factoryrun.ConversationSpec{VerificationStage: "test", Item: it}, next, root)
	if !strings.Contains(words, "1 unresolved") {
		t.Fatal("unreadable previous evidence was forgotten", words)
	}
	notes, _ := (&stageConversation{file: next, dir: root}).ExecutionNotes()
	blocked := false
	for _, n := range notes {
		blocked = blocked || strings.HasPrefix(n, verification.FailurePrefix) && strings.Contains(n, "read prior verification records")
	}
	if !blocked {
		t.Fatal("corrupt evidence became an empty successful round")
	}
}
