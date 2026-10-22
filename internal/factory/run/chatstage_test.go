package run

// The chat executor (chatstage.go), held to its laws against FakeMaker: the
// brief carries what the stage needs, the person's words reach the
// conversation, its captions reach the stream, a report is the result, a round
// that never reported is not done, and a cancelled ctx closes it.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func reviewJob() Job {
	stages := factory.ApproveForGate(factory.DefaultRecipe().Stages, factory.GateNone)
	return Job{
		Item: factory.Item{
			ID: 3, Num: 12, Repo: "ledger", Title: "fix the ledger double count",
			Body:   "The ledger counts refunds twice.\nSee the March export.",
			Stages: stages,
			Stream: &factory.Stream{Room: "team-12"},
		},
		Stage: stages[3], // review · until clean · max 3
		Index: 3,
		Round: 1,
		Notes: []string{"keep the public API", "  the export   is the proof  "},
		Prior: []factory.StageResult{
			{Done: true, Output: "planned two edits"},
			{Done: true, Claims: []factory.Claim{{Text: "a"}, {Text: "b"}, {Text: "c"}}, Output: "changed ledger.go\nand its test"},
			{Done: true, Exit: 0, Output: "go test ./ledger ok"},
		},
		PriorNames: []string{"plan", "write", "test"},
		Dir:        "/src/ledger",
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
		"review: review the diff as a careful colleague; fix what you find",
		"#12 · fix the ledger double count · ledger",
		"The ledger counts refunds twice.\nSee the March export.",
		"notes:\n- keep the public API\n- the export is the proof",
		"the plan (from the plan step):\nplanned two edits",
		"before this stage:\n- plan: done · its plan is above\n- write: done · 3 claims\n  changed ledger.go\n  and its test\n- test: done\n  go test ./ledger ok",
		"claims made so far:\n- write · not ok · a\n- write · not ok · b\n- write · not ok · c",
		"until clean · max 2 · round 2",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q:\n%s", want, brief)
		}
	}
	if !strings.HasPrefix(brief, "review: ") {
		t.Errorf("the brief does not open with the ask:\n%s", brief)
	}
	if !strings.HasSuffix(brief, briefClosing) || briefClosing != "End by calling stage_result with your final result." {
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

func TestTheBriefCutsALongBodyAtItsMost(t *testing.T) {
	job := reviewJob()
	job.Item.Body = strings.Repeat("é", briefBodyMost+3000)
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
	if spec.Team != "team-12" || spec.Dir != "/src/ledger" || spec.Name != "#12 · review 1/3" || spec.Stage == nil {
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
		if err := door.Report(ctx, factory.StageResult{Done: false, Output: "looking"}); err != nil {
			t.Errorf("first report refused: %v", err)
		}
		// Not done, with two findings open: an honest shortfall is held. A
		// review that said done with them is refused at the door
		// (report_check_test.go).
		if err := door.Report(ctx, factory.StageResult{Done: false, Findings: 2, Claims: []factory.Claim{{Text: "refunds count once", OK: true, Evidence: "TestRefund", Medium: "test"}}, Notes: []string{"the export changed"}, Output: "two findings"}); err != nil {
			t.Errorf("a later report was refused: %v", err)
		}
	}}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil {
		t.Fatal(err)
	}
	if res.Done || res.Findings != 2 || len(res.Claims) != 1 || res.Output != "two findings" || len(res.Notes) != 1 || res.Reports != 2 {
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

// A ROUND ENDS BY REPORTING: a conversation that stopped with nothing
// reported is reminded, as codeaf and not as the person, and the report it
// then makes is the round's result.
func TestAStageThatStopsWithoutReportingIsAskedOnceAndItsReportCounts(t *testing.T) {
	var log logRec
	maker := &FakeMaker{
		Script: func(ctx context.Context, c *FakeConversation) { c.Say("read the issue") },
		FollowUp: func(ctx context.Context, c *FakeConversation, words string) {
			_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "planned two edits"})
		},
	}
	job := reviewJob()
	job.Log = log.log
	res, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Done || res.Output != "planned two edits" {
		t.Fatalf("result = %+v", res)
	}
	conv := maker.Opened()[0]
	if got := conv.Noted(); len(got) != 1 || got[0] != stageReminder {
		t.Fatalf("noted = %q", got)
	}
	if len(conv.Sent()) != 0 {
		t.Fatalf("the reminder went as the person's words: %q", conv.Sent())
	}
	if !slicesHas(log.all(), stageReminded+"1 of 3") {
		t.Fatalf("log = %q", log.all())
	}
}

// THE RUNNER OWNS THE WALL: a round that runs past it is let go of and says so.
func TestAStagePastItsWallIsLetGoOf(t *testing.T) {
	old := stageWall
	stageWall = 20 * time.Millisecond
	t.Cleanup(func() { stageWall = old })
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) { <-ctx.Done() }}
	res, err := NewChatExecutor(maker).Run(context.Background(), reviewJob())
	if err != nil {
		t.Fatal(err)
	}
	if res.Done || res.Output != stageWalled || !maker.Opened()[0].Closed() {
		t.Fatalf("result = %+v", res)
	}
}

// A STAGE IS CALLED WHAT IT IS: its team handle is its name, with the round
// from round 2 on, and never something its title model chose.
func TestAStagesHandleIsItsName(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}
	if _, err := NewChatExecutor(maker).Run(context.Background(), reviewJob()); err != nil {
		t.Fatal(err)
	}
	if h := maker.Opened()[0].Spec.Handle; h != "review" {
		t.Fatalf("handle = %q", h)
	}
	for _, tc := range []struct {
		name  string
		round int
		want  string
	}{
		{"plan", 1, "plan"},
		{"review", 2, "review2"},
		{"Security", 1, "security"},
		{"documentation", 3, "documentati3"},
		{"x", 1, ""},
	} {
		job := Job{Stage: factory.Stage{Name: tc.name}, Round: tc.round}
		if got := stageHandle(job); got != tc.want {
			t.Errorf("stageHandle(%q, %d) = %q, want %q", tc.name, tc.round, got, tc.want)
		}
	}
}

// THE JOB'S MODEL: a round opens on the job's own model, a model chosen while
// it runs switches its conversation for the next turn, and the result says
// which model it ended on.
func TestAStageOpensOnTheJobsModelAndSwitchesWhenItChanges(t *testing.T) {
	models := make(chan ModelSelection, 1)
	switched := make(chan struct{})
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		models <- ModelSelection{Model: "vendor/second"}
		for c.Model() != "vendor/second" {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
		close(switched)
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}
	job := reviewJob()
	job.Item.Model = " vendor/first "
	job.ModelSelections = models
	res, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	<-switched
	if spec := maker.Opened()[0].Spec; spec.Model != "vendor/first" {
		t.Fatalf("opened on %q", spec.Model)
	}
	if res.Model != "vendor/second" {
		t.Fatalf("result model = %q", res.Model)
	}
}

func slicesHas(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
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
func (brokenConv) Note(context.Context, string) error     { return nil }
func (brokenConv) Wait(context.Context) error             { return errors.New("the provider refused") }
func (brokenConv) Captions(context.Context) <-chan string { return nil }
func (brokenConv) ID() string                             { return "broken" }
func (brokenConv) Close()                                 {}
func (brokenConv) Spent() float64                         { return 0.25 }

func TestTheBriefSaysWhichStageOfTheRunThisIs(t *testing.T) {
	job := reviewJob()
	job.Stage = factory.DefaultRecipe().Stages[0]
	job.Index = 0
	job.Prior = nil
	job.Item.Stream.Phases = []factory.Phase{{Name: "plan"}, {Name: "write"}, {Name: "test"}, {Name: "review"}, {Name: "proof"}}
	want := "stages: plan › write › test › review › proof · this is plan: do this stage's part, and leave the rest to the stages after it"
	if brief := stageBrief(job); !strings.Contains(brief, want) {
		t.Fatalf("the brief does not say where plan stands:\n%s", brief)
	}
	job.Stage = factory.DefaultRecipe().Stages[factory.StageIndex(factory.DefaultRecipe().Stages, "proof")]
	if brief := stageBrief(job); !strings.Contains(brief, "· this is proof, the last: do this stage's part") {
		t.Fatalf("the last stage is not told it is the last:\n%s", brief)
	}
	job.Item.Stream.Phases = job.Item.Stream.Phases[:1]
	if brief := stageBrief(job); strings.Contains(brief, "stages:") {
		t.Fatalf("a one-stage run names its stages:\n%s", brief)
	}
}

// TestASecondRoundOfReviewIsToldWhatTheFirstFound drives the real loop with
// the chat executor: review runs until clean, round 1 reports a finding, and
// round 2's conversation opens on a brief that names it and says to fix it
// first. The hand run of 2026-10-08 reviewed an unchanged tree twice.
func TestASecondRoundOfReviewIsToldWhatTheFirstFound(t *testing.T) {
	maker := &FakeMaker{}
	var mu sync.Mutex
	rounds := 0
	maker.Script = func(ctx context.Context, c *FakeConversation) {
		mu.Lock()
		rounds++
		n := rounds
		mu.Unlock()
		if n == 1 {
			// Not done, one finding open: the honest shortfall a round is
			// judged into another for. Done with it open is refused at the door.
			if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: false, Findings: 1,
				Output: "the refund is counted twice in Export",
				Claims: []factory.Claim{{Text: "refunds count once", OK: false}},
			}); err != nil {
				t.Errorf("an honest shortfall was refused: %v", err)
			}
			return
		}
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true, Output: "clean"})
	}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, nil)
	id := g.add("ledger", factory.Stage{Name: "review", Kind: factory.StageChat, Ask: "read it as a stranger would", Until: "clean", Max: 2})
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateLanded)
	convs := maker.Opened()
	if len(convs) != 2 {
		t.Fatalf("%d rounds ran, want 2", len(convs))
	}
	if strings.Contains(convs[0].Spec.Brief, "The last round reported") {
		t.Fatalf("round 1 was told of a round before it:\n%s", convs[0].Spec.Brief)
	}
	want := "round 2 of review. The last round reported:\nthe refund is counted twice in Export\n1 finding still open\nnot shown: refunds count once\n" +
		"Fix what it left in the checkout first, run the tests, then review again and report only what is still wrong."
	if !strings.Contains(convs[1].Spec.Brief, want) {
		t.Fatalf("round 2's brief lacks the findings:\n%s", convs[1].Spec.Brief)
	}
}

// EVERY ROUND AFTER THE FIRST READS THE LAST ROUND'S WHOLE REPORT, whatever
// its until: what it said line by line, the findings it left, each claim it
// could not show with its evidence, cut at [briefLastMost] and marked. A stage
// that runs until done is told to carry on, one that is judged to fix first.
func TestEveryLaterRoundReadsTheLastRoundsReport(t *testing.T) {
	job := reviewJob()
	job.Round = 2
	job.Last = &factory.StageResult{Done: true, Findings: 2,
		Output: "Export adds refunds twice\n\n  Import   drops the sign",
		Claims: []factory.Claim{{Text: "refunds count once", OK: false, Evidence: "go test ./ledger fails TestRefund"}, {Text: "the CSV opens", OK: true, Evidence: "ran it"}},
		Notes:  []string{"keep the public API"}}
	got := fixThenCheck(job)
	if want := "round 2 of review. The last round reported:\nExport adds refunds twice\nImport drops the sign\n2 findings still open\n" +
		"not shown: refunds count once (go test ./ledger fails TestRefund)\nFix what it left"; !strings.HasPrefix(got, want) {
		t.Fatalf("the carry = %q, want it to open %q", got, want)
	}
	// A claim it did show is not a shortfall, and its notes already open the
	// brief: neither is said twice.
	if strings.Contains(got, "the CSV opens") || strings.Contains(got, "keep the public API") {
		t.Fatalf("the carry repeats what is not left to do:\n%s", got)
	}

	// A STAGE THAT RUNS UNTIL DONE IS TOLD TOO: a plan that went round again
	// started from nothing before 2026-10-20.
	job.Stage = factory.Stage{Name: "plan", Kind: factory.StageChat, Ask: "write a plan", Until: factory.UntilDone, Max: 2}
	job.Last = &factory.StageResult{Done: false, Output: "read ledger.go; the export path is still to read"}
	want := "round 2 of plan. The last round reported:\nread ledger.go; the export path is still to read\n" +
		"It did not finish. Carry on from what it left in the checkout, and report done once the ask is carried out."
	if got := fixThenCheck(job); got != want {
		t.Fatalf("a done stage's carry = %q, want %q", got, want)
	}
	if brief := stageBrief(job); !strings.Contains(brief, want) {
		t.Fatalf("the brief does not carry it:\n%s", brief)
	}

	// A LONG REPORT IS CUT AT ITS MOST AND MARKED.
	job.Last = &factory.StageResult{Done: true, Findings: 1, Output: strings.Repeat("x", briefLastMost+2000)}
	if n := strings.Count(lastFound(*job.Last), "x"); n != briefLastMost || briefLastMost != 4000 {
		t.Fatalf("the report carries %d characters, want %d (4,000)", n, briefLastMost)
	}
	if !strings.HasSuffix(lastFound(*job.Last), "x …") {
		t.Fatalf("a cut report is not marked: %q", lastFound(*job.Last)[briefLastMost-20:])
	}

	// ROUND 1 HAS NO ROUND BEFORE IT.
	job.Round = 1
	if got := fixThenCheck(job); got != "" {
		t.Fatalf("round 1 was told of a round before it: %q", got)
	}
}

// A STAGE JUDGED ON WHAT IT LEAVES IS TOLD HOW, every round, in its brief:
// findings count what is still wrong once its own fixes are in, and a proof
// claim needs its evidence. No other stage is.
func TestAJudgedStageIsToldHowItIsJudged(t *testing.T) {
	job := reviewJob()
	want := "findings counts only what is still wrong once this round's own fixes are in: a problem you found and fixed is not a finding, and review is clean at zero."
	if got := untilSays(job); got != want {
		t.Fatalf("untilSays(review) = %q", got)
	}
	if !strings.Contains(stageBrief(job), want) {
		t.Fatalf("the review brief does not say what findings count:\n%s", stageBrief(job))
	}
	job.Stage = factory.DefaultRecipe().Stages[factory.StageIndex(factory.DefaultRecipe().Stages, "proof")]
	if got := untilSays(job); !strings.Contains(got, "every claim needs its evidence") || !strings.Contains(stageBrief(job), got) {
		t.Fatalf("the proof brief does not say a claim needs evidence: %q", got)
	}
	for _, until := range []string{factory.UntilDone, factory.UntilGreen, ""} {
		job.Stage.Until = until
		if got := untilSays(job); got != "" {
			t.Fatalf("a stage until %q was told how it is judged: %q", until, got)
		}
	}
}

func TestTheBriefSaysThereIsNoTeamPost(t *testing.T) {
	brief := stageBrief(reviewJob())
	if !strings.Contains(brief, "You have no team_post tool; report through stage_result only.") {
		t.Fatalf("the brief does not say there is no team_post:\n%s", brief)
	}
	if !strings.HasSuffix(brief, briefClosing) {
		t.Fatalf("the brief no longer closes with %q:\n%s", briefClosing, brief)
	}
}

func TestAResumedStageReopensItsChatWithACarryOnNote(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}
	job := reviewJob()
	job.Resume = "chat-7"
	var told string
	job.Opened = func(chat string) { told = chat }
	res, err := NewChatExecutor(maker).Run(context.Background(), job)
	if err != nil {
		t.Fatal(err)
	}
	spec := maker.Opened()[0].Spec
	if spec.Resume != "chat-7" || spec.Brief != stageCarryOn || strings.Contains(spec.Brief, "fix the ledger") {
		t.Fatalf("spec = %+v", spec)
	}
	if res.Chat != "chat-7" || told != "chat-7" {
		t.Fatalf("chat = %q, told %q", res.Chat, told)
	}
}

type refuseResume struct{ inner *FakeMaker }

func (m refuseResume) Open(ctx context.Context, spec ConversationSpec) (Conversation, error) {
	if spec.Resume != "" {
		return nil, errors.New("the chat is gone")
	}
	return m.inner.Open(ctx, spec)
}

func TestAChatThatCannotBeReopenedStartsAgainAndSaysSo(t *testing.T) {
	inner := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}
	rec := &logRec{}
	job := reviewJob()
	job.Resume = "chat-7"
	job.Log = rec.log
	res, err := NewChatExecutor(refuseResume{inner}).Run(context.Background(), job)
	if err != nil || !res.Done {
		t.Fatalf("res %+v err %v", res, err)
	}
	if got := inner.Opened()[0].Spec; got.Resume != "" || got.Brief != stageBrief(reviewJob()) {
		t.Fatalf("fresh spec = %+v", got)
	}
	if lines := rec.all(); len(lines) == 0 || !strings.HasPrefix(lines[0], stageNoResume) {
		t.Fatalf("log = %v", lines)
	}
}

func TestJobModelAndEffortReachEveryConversationSpec(t *testing.T) {
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}
	job := reviewJob()
	enabled := false
	job.Item.AutomaticModels = &enabled
	job.Item.Model = "open-weight/pro"
	job.Item.Effort = "high"
	if _, err := NewChatExecutor(maker).Run(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	spec := maker.Opened()[0].Spec
	if spec.Model != "open-weight/pro" || spec.Effort != "high" || spec.AutomaticModels == nil || *spec.AutomaticModels {
		t.Fatalf("job settings lost: %s %s", spec.Model, spec.Effort)
	}
}

type identityStageMaker struct {
	policyErr    error
	inner        *FakeMaker
	conversation *identityStageConversation
}
type identityStageConversation struct {
	policyErr error
	*FakeConversation
	mu         sync.Mutex
	operations []string
}

func (c *identityStageConversation) LastModel() string { return "vendor/served" }
func (c *identityStageConversation) SetAutomaticModels(enabled bool) error {
	if c.policyErr != nil {
		return c.policyErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if enabled {
		c.operations = append(c.operations, "automatic")
	} else {
		c.operations = append(c.operations, "fixed")
	}
	return nil
}
func (c *identityStageConversation) SetModel(model string) {
	c.mu.Lock()
	c.operations = append(c.operations, "model")
	c.mu.Unlock()
	c.FakeConversation.SetModel(model)
}
func (m *identityStageMaker) Open(ctx context.Context, spec ConversationSpec) (Conversation, error) {
	opened, err := m.inner.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	m.conversation = &identityStageConversation{FakeConversation: opened.(*FakeConversation), policyErr: m.policyErr}
	return m.conversation, nil
}
func TestModelSelectionAppliesPolicyBeforeModelAndRecordsExecution(t *testing.T) {
	maker := &identityStageMaker{inner: &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		for c.Model() != "vendor/next" {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
		_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
	}}}
	job := reviewJob()
	choices := make(chan ModelSelection, 1)
	fixed := false
	choices <- ModelSelection{Model: "vendor/next", Automatic: &fixed}
	job.ModelSelections = choices
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := NewChatExecutor(maker).Run(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "vendor/served" {
		t.Fatalf("recorded preference rather than execution: %q", result.Model)
	}
	c := maker.conversation
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.operations) != 2 || c.operations[0] != "fixed" || c.operations[1] != "model" {
		t.Fatalf("selection order: %v", c.operations)
	}
}

func TestModelPolicyRefusalDoesNotStopStageOrDiscardModelSelection(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		t.Run(fmt.Sprint(unsupported), func(t *testing.T) {
			fake := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
				for c.Model() != "vendor/final" {
					select {
					case <-ctx.Done():
						return
					case <-time.After(time.Millisecond):
					}
				}
				_ = c.Spec.Stage.Report(ctx, factory.StageResult{Done: true})
			}}
			var maker ConversationMaker = fake
			if !unsupported {
				maker = &identityStageMaker{inner: fake, policyErr: errors.New("one-model launch")}
			}
			job := reviewJob()
			choices := make(chan ModelSelection, 2)
			automatic := true
			choices <- ModelSelection{Model: "vendor/intermediate", Automatic: &automatic}
			choices <- ModelSelection{Model: "vendor/final", Automatic: &automatic}
			job.ModelSelections = choices
			var notices []string
			job.Log = func(line string) { notices = append(notices, line) }
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			result, err := NewChatExecutor(maker).Run(ctx, job)
			if err != nil || !result.Done {
				t.Fatalf("control request stopped useful work: done=%v error=%v", result.Done, err)
			}
			if !strings.Contains(strings.Join(notices, "\n"), "Model policy change not applied:") {
				t.Fatalf("missing policy notice: %v", notices)
			}
		})
	}
}
