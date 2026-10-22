package run

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"strings"
	"testing"
	"time"
)

func TestResumedPlanRestoresPurposeDecisionAndCompletionDoor(t *testing.T) {
	plan, _ := factory.ShippedStage("plan")
	job := reviewJob()
	job.Stage = plan
	job.Resume = "saved-plan"
	job.CarryOn = []string{"Use both branches; the person delegated the choice."}
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		for _, want := range []string{"You are the plan step", stageAsk(job), planBrief(job), "Use both branches", "stage_result"} {
			if !strings.Contains(c.Spec.Brief, want) {
				t.Errorf("restored context missing %q", want)
			}
		}
		if c.Spec.VerifyRepair {
			t.Error("Plan restored as a repair verification step")
		}
		c.Answer("Existing plan drafted; reporting it now")
	}, FollowUp: func(ctx context.Context, c *FakeConversation, _ string) {
		if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "Use both retrieval branches"}); err != nil {
			t.Error(err)
		}
	}}
	result, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil || !result.Done || result.Reports != 1 || len(maker.Opened()) != 1 {
		t.Fatalf("draft was restarted or never reported: %+v %v", result, err)
	}
}

func TestMissingPlanReportRecoveryHasADeadline(t *testing.T) {
	old := planReportWindow
	planReportWindow = 10 * time.Millisecond
	defer func() { planReportWindow = old }()
	plan, _ := factory.ShippedStage("plan")
	job := reviewJob()
	job.Stage = plan
	maker := &FakeMaker{Script: func(_ context.Context, c *FakeConversation) { c.Answer("The plan is drafted") }, FollowUp: func(ctx context.Context, c *FakeConversation, _ string) { <-ctx.Done() }}
	result, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil || result.Done || !strings.Contains(result.Output, "Plan report recovery timed out") || !maker.Opened()[0].Closed() {
		t.Fatalf("unbounded or falsely successful report: %+v %v", result, err)
	}
	if len(maker.Opened()) != 1 {
		t.Fatal("missing report redid the drafted plan")
	}
}

func TestStatusQuestionDoesNotAnswerAPendingStep(t *testing.T) {
	for _, text := range []string{"Where do we stand here? What's next?", "Are we stuck?", "status please"} {
		if _, _, ok := typedAnswer(text, qkindStep); ok {
			t.Fatalf("status question consumed as a decision: %q", text)
		}
	}
	if _, words, ok := typedAnswer("Whichever way you think is best", qkindStep); !ok || words == "" {
		t.Fatal("delegated choice was not retained")
	}
}

func TestDraftedPlanMissingReportResumesInsteadOfStartingOver(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		if c.Spec.Resume == "" {
			c.Answer("The completed draft chooses both branches.")
			return
		}
		if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "Both branches, as already drafted"}); err != nil {
			t.Error(err)
		}
	}}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, nil)
	id := g.add("merge sources", factory.Stage{Name: "plan", Kind: factory.StageChat, Until: factory.UntilDone, Max: 2, On: true})
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	got := g.waitState(id, factory.StateLanded)
	opened := maker.Opened()
	if len(opened) != 2 || opened[1].Spec.Resume != opened[0].ID() || got.Stream.Phases[0].Result == nil || !got.Stream.Phases[0].Result.Done {
		t.Fatal("missing completion redid or lost the drafted plan")
	}
}

func TestExplicitPlanReportRetryRetainsTheConversationAndSpend(t *testing.T) {
	it := factory.Item{State: factory.StateNeedsYou, Cap: 10, Stream: &factory.Stream{Spent: 0.28, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseWaiting, Round: 2, Chat: "drafted-plan", Result: &factory.StageResult{Output: stageNoReport}}}}}
	if err := StepControl(&it, "plan", true, "Finish reporting the existing draft", time.Now()); err != nil {
		t.Fatal(err)
	}
	ph := it.Stream.Phases[0]
	if ph.Chat != "drafted-plan" || ph.State != factory.PhaseRunning || ph.Round != 3 || it.Stream.Spent != 0.28 {
		t.Fatal("explicit reporting retry discarded the draft or spend")
	}
}

func TestManagerStatusIsNeitherAnAnswerNorASteeringNote(t *testing.T) {
	talk := newFakeTalk()
	g, _ := managed(t, nil, talk, time.Hour)
	id := g.add("saved Plan", chat("plan"))
	_ = g.st.Update(id, func(it *factory.Item) error {
		it.Talk = "manager"
		it.State = factory.StateNeedsYou
		it.Question, it.QKind = "Choose a retrieval strategy", "step"
		it.Asking = &factory.Asked{Stage: "plan", Question: it.Question, With: factory.AskedYou, At: time.Now()}
		return nil
	})
	talk.typed("manager", "Where do we stand here? What's next?")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &loopCtl{id: id, ctx: ctx, cancel: cancel, pending: "step", answers: make(chan loopAnswer, 1)}
	g.r.loop().hear(c)
	got, _ := g.st.Get(id)
	if got.Question == "" || got.Asking == nil || len(got.Notes) != 0 || got.Heard.IsZero() {
		t.Fatal("status consumed a decision or became work steering")
	}
}

func TestFailedManagerAnswerDoesNotConsumeTheHumanLine(t *testing.T) {
	talk := newFakeTalk()
	g, _ := managed(t, nil, talk, time.Hour)
	id := g.add("saved Plan", chat("plan"))
	_ = g.st.Update(id, func(it *factory.Item) error {
		it.Talk = "manager"
		it.State = factory.StateNeedsYou
		it.Question, it.QKind = "Choose a retrieval strategy", "step"
		return nil
	})
	talk.typed("manager", "Whichever way you think is best")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The loop lost its registered control just before the answer, as a worker
	// disappearing can do. A failed delivery must leave the transcript unread.
	c := &loopCtl{id: id, ctx: ctx, cancel: cancel, pending: "step", answers: make(chan loopAnswer, 1)}
	g.r.loop().hear(c)
	got, _ := g.st.Get(id)
	if !got.Heard.IsZero() || got.Question == "" {
		t.Fatal("a rejected answer was acknowledged and lost")
	}
}
