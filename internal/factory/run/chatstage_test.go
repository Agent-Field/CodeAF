package run

// The chat executor (chatstage.go), held to its laws against FakeMaker: the
// brief carries what the stage needs, the person's words reach the
// conversation, its captions reach the stream, a report is the result, a round
// that never reported is not done, and a cancelled ctx closes it.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func reviewJob() Job {
	stages := factory.DefaultRecipe().Stages
	return Job{
		Item: factory.Item{
			ID: 3, Num: 12, Repo: "ledger", Title: "fix the ledger double count",
			Body:   "The ledger counts refunds twice.\nSee the March export.",
			Stages: stages,
			Stream: &factory.Stream{Room: "team-12"},
		},
		Stage: stages[3], // review · until clean · max 1 · per-finding
		Index: 3,
		Round: 1,
		Notes: []string{"keep the public API", "  the export   is the proof  "},
		Prior: []factory.StageResult{
			{Done: true, Output: "planned two edits"},
			{Done: true, Claims: []factory.Claim{{Text: "a"}, {Text: "b"}, {Text: "c"}}, Output: "changed ledger.go\nand its test"},
			{Done: true, Exit: 0, Output: "go test ./ledger ok"},
		},
		Dir: "/src/ledger",
	}
}

// logRec is a Job.Log that keeps what it was given.
type logRec struct {
	mu    sync.Mutex
	lines []string
}

func (l *logRec) log(line string) { l.mu.Lock(); l.lines = append(l.lines, line); l.mu.Unlock() }
func (l *logRec) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

func TestTheBriefCarriesTheAskTheItemTheNotesWhatCameBeforeAndTheKnobs(t *testing.T) {
	job := reviewJob()
	job.Stage.Max = 2
	job.Round = 2
	brief := stageBrief(job)
	for _, want := range []string{
		"review: read it as a stranger would",
		"#12 · fix the ledger double count · ledger",
		"The ledger counts refunds twice.\nSee the March export.",
		"notes:\n- keep the public API\n- the export is the proof",
		"before this stage:\nplan: done · planned two edits\nwrite: done · 3 claims · changed ledger.go\ntest: done · go test ./ledger ok",
		"until clean · max 2 · fanout per-finding · round 2",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q:\n%s", want, brief)
		}
	}
	if !strings.HasPrefix(brief, "review: ") {
		t.Errorf("the brief does not open with the ask:\n%s", brief)
	}
	if !strings.HasSuffix(brief, briefClosing) || briefClosing != "End by calling stage_result once." {
		t.Errorf("the brief does not close with %q:\n%s", briefClosing, brief)
	}
	if got := stageChatName(job); got != "#12 · review 2/2" {
		t.Errorf("name = %q", got)
	}
	job.Stage.Max = 1
	if got := stageChatName(job); got != "#12 · review" {
		t.Errorf("a one-round stage is named %q", got)
	}
}

func TestTheBriefSaysNothingAboutWhatIsNotThere(t *testing.T) {
	job := Job{Item: factory.Item{ID: 1, Num: 4}, Stage: factory.Stage{Name: "plan", Ask: "read the issue and say how"}, Round: 1}
	brief := stageBrief(job)
	for _, gone := range []string{"notes:", "before this stage:", "max ", "fanout", "effort", "round"} {
		if strings.Contains(brief, gone) {
			t.Errorf("an empty job's brief says %q:\n%s", gone, brief)
		}
	}
	if !strings.Contains(brief, "until done") {
		t.Errorf("a stage with no until is not told it runs until done:\n%s", brief)
	}
}

func TestTheBriefCutsALongBodyAtTwoThousandCharacters(t *testing.T) {
	job := reviewJob()
	job.Item.Body = strings.Repeat("é", 5000)
	brief := stageBrief(job)
	if n := strings.Count(brief, "é"); n != briefBodyMost {
		t.Fatalf("the brief carries %d of the body's characters, want %d", n, briefBodyMost)
	}
	if !strings.Contains(brief, "é …") {
		t.Fatal("a cut body is not marked as cut")
	}
}

func TestTheSpecNamesTheTeamTheDirAndTheDoor(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil {
		t.Fatal(err)
	}
	spec := maker.Opened()[0].Spec
	if spec.Team != "team-12" || spec.Dir != "/src/ledger" || spec.Name != "#12 · review" || spec.Stage == nil {
		t.Fatalf("spec = %+v", spec)
	}
	if spec.Brief != stageBrief(reviewJob()) {
		t.Fatal("the conversation was not opened with the stage's brief")
	}
	if res.Chat != "fake-1" {
		t.Fatalf("Chat = %q, want the conversation's id", res.Chat)
	}
}

func TestAReportIsTheResultAndAProposalRidesOnIt(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		door := c.Spec.Stage
		_ = door.Edit(ctx, factory.PlanEdit{Skip: []string{"neaten"}})
		_ = door.Edit(ctx, factory.PlanEdit{Add: []factory.Stage{{Ask: "after review, read it for auth holes"}}, Why: "touches billing"})
		if err := door.Report(ctx, factory.StageResult{Done: true, Findings: 2, Claims: []factory.Claim{{Text: "refunds count once", OK: true, Evidence: "TestRefund", Medium: "test"}}, Notes: []string{"the export changed"}, Output: "two findings"}); err != nil {
			t.Errorf("first report refused: %v", err)
		}
		if err := door.Report(ctx, factory.StageResult{Done: true}); err == nil || err.Error() != stageSecondReport {
			t.Errorf("a second report was not refused: %v", err)
		}
	}}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Done || res.Findings != 2 || len(res.Claims) != 1 || res.Output != "two findings" || len(res.Notes) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if factory.Met(reviewJob().Stage, res) {
		t.Fatal("two findings met until clean")
	}
	if res.Edit == nil || len(res.Edit.Skip) != 1 || len(res.Edit.Add) != 1 || res.Edit.Why != "touches billing" {
		t.Fatalf("edit = %+v", res.Edit)
	}
	if !maker.Opened()[0].Closed() {
		t.Fatal("the conversation was not let go of")
	}
}

func TestARoundThatNeverReportedIsNotDone(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) { c.Say("looked around") }}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil {
		t.Fatal(err)
	}
	if res.Done || res.Output != "the stage ended without reporting" || res.Chat != "fake-1" {
		t.Fatalf("result = %+v", res)
	}
	if factory.Met(factory.Stage{Until: "done"}, res) {
		t.Fatal("a round that never reported met until done")
	}
}

func TestThePersonsWordsReachTheConversationAndItsCaptionsReachTheStream(t *testing.T) {
	steer := make(chan string, 2)
	steer <- "  also check the export  "
	rec := &logRec{}
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		c.Say("reading ledger.go")
		select {
		case words := <-c.Heard():
			c.Say("heard: " + words)
		case <-time.After(5 * time.Second):
			t.Error("the steer never arrived")
		}
		c.Say("  done   reading  ")
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}
	job := reviewJob()
	job.Steer = steer
	job.Log = rec.log
	if _, err := NewChatExecutor(maker).Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if got := maker.Opened()[0].Sent(); len(got) != 1 || got[0] != "also check the export" {
		t.Fatalf("sent = %q", got)
	}
	got := strings.Join(rec.all(), "\n")
	if got != "reading ledger.go\nheard: also check the export\ndone reading" {
		t.Fatalf("the stream got:\n%s", got)
	}
}

func TestACancelledRoundClosesTheConversationAndReturnsNoResult(t *testing.T) {
	started := make(chan struct{})
	ended := make(chan struct{})
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		close(started)
		<-ctx.Done()
		close(ended)
	}}
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan error, 1)
	go func() {
		res, err := NewChatExecutor(maker).Run(ctx, reviewJob())
		if res.Done || res.Chat != "" {
			t.Errorf("a cancelled round answered %+v", res)
		}
		out <- err
	}()
	<-started
	cancel()
	select {
	case err := <-out:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the round did not return on cancel")
	}
	if !maker.Opened()[0].Closed() {
		t.Fatal("the conversation was not closed")
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn in flight was not interrupted")
	}
}

func TestAReportThatKeepsGoingIsLetGoOfAfterTheGrace(t *testing.T) {
	old := reportGrace
	reportGrace = 20 * time.Millisecond
	t.Cleanup(func() { reportGrace = old })
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "fine"})
		<-ctx.Done()
	}}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil || !res.Done || res.Output != "fine" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestAMakerThatCannotOpenFailsTheRound(t *testing.T) {
	if _, err := NewChatExecutor(&FakeMaker{Fail: errors.New("no folder")}).Run(context.Background(), reviewJob()); err == nil || err.Error() != "no folder" {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewChatExecutor(nil).Run(context.Background(), reviewJob()); err == nil || err.Error() != stageNoMaker {
		t.Fatalf("err = %v", err)
	}
}

func TestATurnThatBrokeSaysWhy(t *testing.T) {
	maker := brokenMaker{}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil {
		t.Fatal(err)
	}
	if res.Done || res.Output != stageNoReport+": the provider refused" || res.Spent != 0.25 || res.Chat != "broken" {
		t.Fatalf("res = %+v", res)
	}
}

type brokenMaker struct{}

func (brokenMaker) Open(context.Context, ConversationSpec) (Conversation, error) {
	return brokenConv{}, nil
}

type brokenConv struct{}

func (brokenConv) Send(context.Context, string) error     { return nil }
func (brokenConv) Wait(context.Context) error             { return errors.New("the provider refused") }
func (brokenConv) Captions(context.Context) <-chan string { return nil }
func (brokenConv) ID() string                             { return "broken" }
func (brokenConv) Close()                                 {}
func (brokenConv) Spent() float64                         { return 0.25 }
