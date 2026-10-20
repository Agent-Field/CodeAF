package run

// A step that stops without reporting still ends with a report, and a step's
// latest report stands (stagereport.go, chatstage.go), held against FakeMaker
// and, where the judging matters, the real loop.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE REQUIRED-TOOL TURN: a step that answers both reminders in prose is asked
// a third time on a turn that must call `stage_result`, reports there, and is
// judged on that report like any other.
func TestAStepThatEndsInProseTwiceReportsOnTheTurnThatMust(t *testing.T) {
	var mu sync.Mutex
	var required []string
	maker := &FakeMaker{
		Script: func(ctx context.Context, c *FakeConversation) { c.Answer("finishing here · what was asked is done") },
		FollowUp: func(ctx context.Context, c *FakeConversation, words string) {
			mu.Lock()
			required = append(required, c.Required())
			mu.Unlock()
			// A MODEL THAT HONOURS tool_choice: it can only call the verb.
			if c.Required() == stageResultTool {
				_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "planned two edits"})
				return
			}
			c.Answer("all done")
		},
	}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, nil)
	id := g.add("ledger", chat("plan"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	conv := maker.Opened()[0]
	job := Job{Stage: chat("plan")}
	want := []string{stageReminder, stageReminderAgain(job), stageReminderLast}
	if got := conv.Noted(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("noted = %q\nwant %q", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(required, ",") != ",,"+stageResultTool {
		t.Fatalf("the turns required %q; only the third must report", required)
	}
	if !logHas(it, stageReminded+"3 of 3"+stageRequired) {
		t.Fatalf("the log does not say the third turn had to report: %+v", it.Stream.Log)
	}
	if r := it.Stream.Phases[0].Result; r == nil || !r.Done || r.Output != "planned two edits" || r.Inferred {
		t.Fatalf("result = %+v", r)
	}
}

// THE LAST RESORT: a step whose model never calls the verb is reported for,
// from its brief, its last message and its change, and the report says whose
// it is.
func TestAStepWhoseModelNeverReportsIsReportedFor(t *testing.T) {
	rec := &logRec{}
	maker := &FakeMaker{
		Script: func(ctx context.Context, c *FakeConversation) { c.Answer("I changed ledger.go so refunds count once") },
		Infer: func(ctx context.Context, c *FakeConversation, ask string) (factory.StageResult, error) {
			return factory.StageResult{Done: true, Output: "refunds count once in ledger.go"}, nil
		},
	}
	job := reviewJob()
	job.Stage.Until = factory.UntilDone
	job.Log = rec.log
	res, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Inferred || res.Reports != 0 || !res.Done || res.Output != "refunds count once in ledger.go" {
		t.Fatalf("result = %+v", res)
	}
	if !factory.Met(job.Stage, res) {
		t.Fatal("a report made for the step was not judged like any other")
	}
	if got := factory.RoundSummary(res); got != "reported for the step · refunds count once in ledger.go" {
		t.Fatalf("the Timeline reads %q", got)
	}
	conv := maker.Opened()[0]
	if len(conv.Noted()) != stageReminders {
		t.Fatalf("the report was made before every reminder went: %q", conv.Noted())
	}
	asks := conv.Inferred()
	if len(asks) != 1 {
		t.Fatalf("%d reports were made", len(asks))
	}
	for _, want := range []string{"call stage_result once", "The step's brief:\n" + stageAsk(job),
		"The step's last message:\nI changed ledger.go so refunds count once", "The change in its checkout:\n"} {
		if !strings.Contains(asks[0], want) {
			t.Errorf("the ask lacks %q:\n%s", want, asks[0])
		}
	}
	if !slicesHas(rec.all(), stageInferred) {
		t.Fatalf("log = %q", rec.all())
	}
}

// AND ONLY WHEN THAT FAILS TOO does a round end without reporting, saying why.
func TestTheWordsEndedWithoutReportingComeOnlyAfterEverythingWasTried(t *testing.T) {
	maker := &FakeMaker{
		Infer: func(context.Context, *FakeConversation, string) (factory.StageResult, error) {
			return factory.StageResult{}, errors.New("the model answered without a report")
		},
	}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil {
		t.Fatal(err)
	}
	if res.Done || res.Inferred || res.Output != stageNoReport+": no report could be made for it: the model answered without a report" {
		t.Fatalf("result = %+v", res)
	}
	conv := maker.Opened()[0]
	if len(conv.Noted()) != stageReminders || len(conv.Inferred()) != 1 {
		t.Fatalf("noted %q, inferred %d", conv.Noted(), len(conv.Inferred()))
	}
}

// A PROVIDER ERROR IS NOT A FORGOTTEN REPORT: the turn is tried again on the
// same conversation, and then reminded like any other silence.
func TestAProviderErrorMidStepIsTriedAgainAndReminded(t *testing.T) {
	rec := &logRec{}
	var mu sync.Mutex
	turns := 0
	maker := &FakeMaker{
		Script: func(ctx context.Context, c *FakeConversation) { c.Fail(errors.New("the provider refused")) },
		FollowUp: func(ctx context.Context, c *FakeConversation, words string) {
			mu.Lock()
			turns++
			n := turns
			mu.Unlock()
			if n == 1 {
				c.Answer("carried on")
				return
			}
			_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "fixed it"})
		},
	}
	job := reviewJob()
	job.Log = rec.log
	res, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Done || res.Output != "fixed it" {
		t.Fatalf("a step whose turn failed was not given its report back: %+v", res)
	}
	got := maker.Opened()[0].Noted()
	if len(got) != 2 || got[0] != stageRetry("the provider refused") || got[1] != stageReminder {
		t.Fatalf("noted = %q", got)
	}
	if !slicesHas(rec.all(), stageRetried+"the provider refused") {
		t.Fatalf("log = %q", rec.all())
	}
}

// A DAILY LIMIT IS A STOP: the round ends on it without a reminder or a report
// made for it, and the job stops on the limit's own words with nothing asked.
func TestADailyLimitReadsAsASpendStop(t *testing.T) {
	maker := &FakeMaker{
		Script: func(ctx context.Context, c *FakeConversation) {
			c.Fail(SpendStop{Daily: true, Said: "daily limit reached · $3 spent of $3 · /budget day changes it"})
		},
		Infer: func(context.Context, *FakeConversation, string) (factory.StageResult, error) {
			t.Error("a report was made past a spending limit")
			return factory.StageResult{}, nil
		},
	}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != stageSpendDaily || !spendStopped(res) || strings.Contains(res.Output, stageNoReport) {
		t.Fatalf("result = %+v", res)
	}
	if got := maker.Opened()[0].Noted(); len(got) != 0 {
		t.Fatalf("a spend stop was reminded: %q", got)
	}
	if other := (SpendStop{Said: "conversation limit reached · $5 spent of $5"}); other.Error() != stageSpendStopped+": conversation limit reached · $5 spent of $5" {
		t.Fatalf("another limit reads %q", other.Error())
	}

	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, nil)
	stage := chat("plan")
	stage.Max = 3
	id := g.add("ledger", stage)
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.wait(id, "stopped on the limit", func(it factory.Item) bool { return it.Failure != nil })
	if it.Question != "" || it.Failure.Step != "plan" || it.Failure.Detail != stageSpendDaily || it.Failure.Next != factory.FailureStopped {
		t.Fatalf("failure = %+v question %q", it.Failure, it.Question)
	}
	if n := len(maker.Opened()); n != 2 {
		t.Fatalf("%d rounds were opened in the loop, want 1", n-1)
	}
}

// THE LATEST REPORT STANDS: a step that reports done false, keeps working and
// reports done true is judged on the second, and the Timeline says it changed.
func TestAStepThatReportsAgainIsJudgedOnItsLastReport(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: false, Output: "the test fails"})
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "fixed the test, it passes"})
	}}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, nil)
	id := g.add("ledger", chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	ph := it.Stream.Phases[0]
	if ph.Result == nil || !ph.Result.Done || ph.Result.Reports != 2 {
		t.Fatalf("result = %+v", ph.Result)
	}
	if len(ph.Rounds) != 1 || ph.Rounds[0].Summary != "fixed the test, it passes · updated 1 time" {
		t.Fatalf("rounds = %+v", ph.Rounds)
	}
	if len(maker.Opened()) != 1 {
		t.Fatal("a step that met its until on its last report ran again")
	}
}

// NO FIXED CUT AFTER A REPORT: a step that reported and keeps working is
// waited on until it stops, however long that is inside the wall.
func TestAStepThatReportsAndKeepsWorkingIsNotCutOff(t *testing.T) {
	working, release := make(chan struct{}), make(chan struct{})
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: false, Output: "the test fails"})
		close(working)
		select {
		case <-release:
		case <-ctx.Done():
			return
		}
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "it passes"})
	}}
	out := make(chan factory.StageResult, 1)
	go func() {
		res, _ := NewChatExecutor(maker).Run(context.Background(), reviewJob())
		out <- res
	}()
	<-working
	select {
	case res := <-out:
		t.Fatalf("the round ended while the step was still working: %+v", res)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case res := <-out:
		if !res.Done || res.Output != "it passes" || res.Reports != 2 {
			t.Fatalf("result = %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the round did not end when the step stopped")
	}
}

// AND A STEP THAT REPORTS AND GOES IDLE ENDS AT ONCE, asked nothing more.
func TestAStepThatReportsAndGoesIdleEndsPromptly(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "fine"})
	}}
	start := time.Now()
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil || !res.Done || res.Reports != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("an idle reported step took %s to end", took)
	}
	if got := maker.Opened()[0].Noted(); len(got) != 0 {
		t.Fatalf("a step that reported was reminded: %q", got)
	}
}

// THE WALL STILL ENDS A STEP THAT NEVER STOPS, and its last report stands.
func TestTheWallStillEndsAStepThatReportedAndNeverStops(t *testing.T) {
	old := stageWall
	stageWall = 20 * time.Millisecond
	t.Cleanup(func() { stageWall = old })
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "partial"})
		<-ctx.Done()
	}}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Done || res.Output != "partial" || !maker.Opened()[0].Closed() {
		t.Fatalf("result = %+v", res)
	}
}

func TestTheTimelineSaysWhoseReportItIsAndHowOftenItChanged(t *testing.T) {
	for _, tc := range []struct {
		res  factory.StageResult
		want string
	}{
		{factory.StageResult{Done: true, Output: "ok", Reports: 1}, "ok"},
		{factory.StageResult{Done: true, Output: "ok", Reports: 3}, "ok · updated 2 times"},
		{factory.StageResult{Done: true, Inferred: true}, "reported for the step · done"},
		{factory.StageResult{}, "no result"},
	} {
		if got := factory.RoundSummary(tc.res); got != tc.want {
			t.Errorf("RoundSummary(%+v) = %q, want %q", tc.res, got, tc.want)
		}
	}
	if spendStopped(factory.StageResult{Output: stageSpendDaily, Reports: 1}) {
		t.Fatal("a step that reported reads as a spend stop")
	}
}
