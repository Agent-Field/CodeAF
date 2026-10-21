package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

type verificationMaker struct {
	*FakeMaker
	notes []string
}
type verificationConversation struct {
	Conversation
	notes []string
}

func (c verificationConversation) ExecutionNotes() ([]string, *factory.TestCounts) {
	return c.notes, nil
}
func (m verificationMaker) Open(ctx context.Context, spec ConversationSpec) (Conversation, error) {
	c, err := m.FakeMaker.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return verificationConversation{c, m.notes}, nil
}

func TestVerificationRejectionOverridesReportedSuccessWithConcreteReason(t *testing.T) {
	for _, name := range []string{"test", "proof", "followpr"} {
		t.Run(name, func(t *testing.T) {
			maker := verificationMaker{FakeMaker: &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
				_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "Everything is ready", Repair: &factory.CIRepairResult{Decision: "fixed"}})
			}}, notes: []string{verification.FailurePrefix + " go test ./... · failed"}}
			job := reviewJob()
			job.Stage = factory.Stage{Name: name, Kind: factory.StageChat, Until: factory.UntilDone}
			result, err := NewChatExecutor(maker).Run(context.Background(), job)
			if err != nil {
				t.Fatal(err)
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
