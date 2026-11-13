package run

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

type verificationMaker struct {
	*FakeMaker
	notes       []string
	notesNow    func() []string
	blockersNow func() string
}
type verificationConversation struct {
	Conversation
	notes       []string
	notesNow    func() []string
	blockersNow func() string
}

func (c verificationConversation) ExecutionNotes() ([]string, *factory.TestCounts) {
	if c.notesNow != nil {
		return c.notesNow(), nil
	}
	return c.notes, nil
}
func (c verificationConversation) VerificationEvidence() ([]string, *factory.TestCounts, string) {
	notes, tests := c.ExecutionNotes()
	if c.blockersNow != nil {
		return notes, tests, c.blockersNow()
	}
	return notes, tests, ""
}
func (m verificationMaker) Open(ctx context.Context, spec ConversationSpec) (Conversation, error) {
	c, err := m.FakeMaker.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return verificationConversation{Conversation: c, notes: m.notes, notesNow: m.notesNow, blockersNow: m.blockersNow}, nil
}

func TestBaselineReproducedFailureRemainsVisibleInSuccessfulTest(t *testing.T) {
	const pin = "0123456789abcdef0123456789abcdef01234567"
	const existing = verification.BaselineNotePrefix + pin + "."
	maker := verificationMaker{FakeMaker: &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "The required checks finished."})
	}}, notes: []string{`Check receipt: {"exit":1,"outcome":"failed","base_commit":"` + pin + `","base_receipt_id":"base"}`, existing}}
	job := reviewJob()
	job.Item.Kind, job.Item.ChangeBase = factory.KindIssue, pin
	job.Stage = factory.Stage{Name: "test", Kind: factory.StageChat, Until: factory.UntilNoNewFailures}
	result, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil || !factory.Met(job.Stage, result) || !strings.HasPrefix(result.Output, existing) {
		t.Fatalf("Test result = %+v, %v", result, err)
	}
	spec := maker.Opened()[0].Spec
	if spec.BaselineCommit != pin || !strings.Contains(spec.Brief, pin) || !strings.Contains(spec.Brief, "go test -json -count=1") {
		t.Fatalf("Test brief lacked pinned reproduction guidance:\n%s", spec.Brief)
	}
	if _, failed := terminalCheckFailure(maker.notes, pin); failed {
		t.Fatal("baseline-reproduced failure still counts as a new terminal failure")
	}
	if _, failed := terminalCheckFailure(append(maker.notes, `Check receipt: {"exit":1,"outcome":"failed"}`), pin); !failed {
		t.Fatal("unmatched failure was ignored after a matched failure")
	}
	if _, failed := terminalCheckFailure(maker.notes, ""); !failed {
		t.Fatal("strict Test ignored a failing receipt")
	}
}

func TestBaselineComparisonRequiresPinnedPrepublishIssueTest(t *testing.T) {
	const pin = "0123456789abcdef0123456789abcdef01234567"
	for _, tc := range []struct {
		name   string
		change func(*Job)
	}{
		{"strict Test", func(j *Job) { j.Stage.Until = factory.UntilGreen }},
		{"Proof", func(j *Job) { j.Stage.Name = "proof" }},
		{"command Test", func(j *Job) { j.Stage.Kind = factory.StageCheck }},
		{"PR job", func(j *Job) { j.Item.Kind = factory.KindPR }},
		{"published issue", func(j *Job) { j.Item.PullRequest = &factory.PullRequest{} }},
		{"publishing issue", func(j *Job) { j.Item.Publishing = true }},
		{"queued publication", func(j *Job) { j.Item.PublishStatus = &factory.Publishing{State: "queued"} }},
		{"abbreviated base", func(j *Job) { j.Item.ChangeBase = pin[:8] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := reviewJob()
			job.Item.Kind, job.Item.ChangeBase = factory.KindIssue, pin
			job.Stage, _ = factory.ShippedStage("test")
			tc.change(&job)
			if got := testBaselineCommit(job); got != "" {
				t.Fatalf("ineligible job exposed a baseline pin: %q", got)
			}
		})
	}
}

func TestRepeatedMeasuredVerificationPausesBeforeRoundLimit(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "same evidence"
		if changed {
			name = "changed evidence"
		}
		t.Run(name, func(t *testing.T) {
			var rounds atomic.Int32
			maker := verificationMaker{FakeMaker: &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
				round := rounds.Add(1)
				if round < 3 {
					_ = c.Spec.Stage.Report(ctx, factory.StageResult{Exit: 1, Output: "A check failed"})
					return
				}
				_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "Checks passed"})
			}}, notesNow: func() []string {
				if rounds.Load() < 3 {
					return []string{`Check receipt: {"exit":1,"outcome":"failed"}`, verification.FailurePrefix + " go test ./... · failed"}
				}
				return []string{`Check receipt: {"exit":0,"outcome":"passed"}`}
			}, blockersNow: func() string {
				switch {
				case rounds.Load() == 1 || (rounds.Load() == 2 && !changed):
					return "first checkout and checks"
				case rounds.Load() == 2:
					return "updated checkout and checks"
				}
				return ""
			}}
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, nil)
			stage := chat("test")
			stage.Max, stage.Until = 5, factory.UntilGreen
			id := g.add("check a change", stage)
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			if !changed {
				held := g.wait(id, "waiting or landed", func(it factory.Item) bool {
					return it.State == factory.StateNeedsYou || it.State == factory.StateLanded
				})
				phase := held.Stream.Phases[0]
				if held.State != factory.StateNeedsYou || phase.Round != 2 || phase.RoundLimit != 5 || !strings.Contains(held.Question, "same checks failed") {
					t.Fatalf("state=%s round=%d limit=%d question=%q", held.State, phase.Round, phase.RoundLimit, held.Question)
				}
				if len(phase.Rounds) != 2 || !phase.Rounds[0].Rejected || !phase.Rounds[1].Rejected || phase.Result == nil || phase.Result.Done {
					t.Fatalf("waiting lost failed evidence: %+v", phase)
				}
				if err := g.r.Answer(id, true, ""); err != nil {
					t.Fatal(err)
				}
			}
			final := g.waitState(id, factory.StateLanded)
			if phase := final.Stream.Phases[0]; phase.Round != 3 || phase.RoundLimit != 5 || rounds.Load() != 3 {
				t.Fatalf("passing retry: round=%d limit=%d ran=%d", phase.Round, phase.RoundLimit, rounds.Load())
			}
		})
	}
}

func TestVerificationAutomaticallyCorrectsInTheSameConversation(t *testing.T) {
	var corrected atomic.Bool
	report := factory.StageResult{Done: true, Output: "checked", Claims: []factory.Claim{{Text: "behavior holds", OK: true, Evidence: "observed test", Medium: "test"}}, Repair: &factory.CIRepairResult{Decision: "fixed", Reason: "real repair checked"}}
	maker := verificationMaker{FakeMaker: &FakeMaker{
		Script: func(ctx context.Context, c *FakeConversation) { _ = c.Spec.Stage.Report(ctx, report) },
		FollowUp: func(ctx context.Context, c *FakeConversation, words string) {
			if !strings.Contains(words, "go test ./...") || !strings.Contains(words, "Preserve this step's purpose") {
				t.Error("correction lost cause or purpose")
			}
			corrected.Store(true)
			_ = c.Spec.Stage.Report(ctx, report)
		},
	}, notesNow: func() []string {
		if !corrected.Load() {
			return []string{verification.FailurePrefix + " go test ./... · executable not found"}
		}
		return []string{`Check receipt: {"exit":0,"outcome":"passed"}`}
	}}
	j := reviewJob()
	j.Stage = proofStage()
	res, err := NewChatExecutor(maker).Run(context.Background(), j)
	if err != nil || !res.Done || res.Exit != 0 || !corrected.Load() || len(maker.Opened()) != 1 || len(maker.Opened()[0].Noted()) != 1 {
		t.Fatalf("automatic correction failed: done=%v exit=%d err=%v", res.Done, res.Exit, err)
	}
}

func TestVerificationRejectionOverridesReportedSuccessWithConcreteReason(t *testing.T) {
	for _, name := range []string{"test", "proof", "followpr"} {
		t.Run(name, func(t *testing.T) {
			maker := verificationMaker{FakeMaker: &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
				_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "Everything is ready", Repair: &factory.CIRepairResult{Decision: "fixed"}})
			}}, notes: []string{`Check receipt: {"check":["go","test","./..."],"exit":1,"outcome":"failed"}`, verification.FailurePrefix + " go test ./... · failed"}}
			job := reviewJob()
			job.Stage = factory.Stage{Name: name, Kind: factory.StageChat, Until: factory.UntilDone}
			result, err := NewChatExecutor(maker).Run(context.Background(), job)
			if err != nil {
				t.Fatal(err)
			}
			if result.Repair != nil && result.Repair.Decision == "fixed" {
				t.Fatal("a rejected repair still claimed fixed")
			}
			if result.Done || result.Exit == 0 || factory.Met(job.Stage, result) {
				t.Fatalf("rejected verification met gate: %+v", result)
			}
			if !strings.HasPrefix(result.Output, "Verification blocked: go test ./...") || !strings.Contains(shortfall(result), "verification blocked: go test ./...") {
				t.Fatalf("lost reason: %+v", result)
			}
			item := factory.Item{Stream: &factory.Stream{Phases: []factory.Phase{{Name: name, Round: 1}}}}
			foldResult(&item, job.Stage, 0, result, time.Now())
			if !item.Stream.Phases[0].Rounds[0].Rejected {
				t.Fatal("rejected round recorded as successful")
			}
			if len(maker.Opened()[0].Noted()) > stageVerificationCorrections+stageReminders {
				t.Fatal("unbounded correction loop")
			}
		})
	}
}

func TestVerificationReceiptSuccessDoesNotRequireShellReporterSuccess(t *testing.T) {
	maker := verificationMaker{FakeMaker: &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}, notes: []string{`Command receipt: {"outcome":"failed","scope":"shell presentation"}`, `Check receipt: {"check":["go","test","./..."],"exit":0,"outcome":"passed"}`}}
	job := reviewJob()
	job.Stage = factory.Stage{Name: "test", Kind: factory.StageChat, Until: factory.UntilDone}
	result, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil || !result.Done || result.Exit != 0 || !factory.Met(job.Stage, result) {
		t.Fatalf("reporter failed the check: %+v %v", result, err)
	}
}

func TestVerificationRetryRetainsReasonAndRejectedRound(t *testing.T) {
	release := make(chan struct{})
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: ExecutorFunc(func(ctx context.Context, j Job) (factory.StageResult, error) {
		if j.Round == 1 {
			return factory.StageResult{Done: false, Exit: 1, Output: "Verification blocked: go test ./... failed", Notes: []string{verification.FailurePrefix + " go test ./... failed"}}, nil
		}
		select {
		case <-release:
			return factory.StageResult{Done: true, Output: "verified corrected check"}, nil
		case <-ctx.Done():
			return factory.StageResult{}, ctx.Err()
		}
	})}, nil)
	stage := chat("test")
	stage.Max = 2
	id := g.add("repair verification", stage)
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.wait(id, "second verification round", func(it factory.Item) bool {
		return it.Stream != nil && len(it.Stream.Phases) == 1 && it.Stream.Phases[0].Round == 2
	})
	phase := it.Stream.Phases[0]
	if len(phase.Rounds) != 1 || !phase.Rounds[0].Rejected || !strings.Contains(phase.Note, "Verification blocked:") || !strings.Contains(phase.Note, "Retrying round 2 of 2") {
		t.Fatalf("retry hid failure: %+v", phase)
	}
	found := false
	for _, line := range it.Stream.Log {
		found = found || strings.Contains(line.Text, "verification blocked: go test ./... failed · going again")
	}
	if !found {
		t.Fatal("retry logged only no result")
	}
	close(release)
	final := g.waitState(id, factory.StateLanded)
	if len(final.Stream.Phases[0].Rounds) != 2 || final.Stream.Phases[0].Rounds[1].Rejected {
		t.Fatal("corrected round was not accepted")
	}
}
