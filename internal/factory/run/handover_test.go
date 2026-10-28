package run

// What the steps before a step hand it (handover.go), held to the spec's
// laws: the plan arrives whole, the write step is handed the files the plan
// names, proof sees every claim so far, the carry stays under its cap with the
// oldest words cut first and the plan never, and a run resumed on another
// worker still knows that the steps before it finished, what they noted and
// what they claimed.

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// thePlanOutput is a plan written the way the plan step's brief asks: what
// changes, in which files, and how it is known to work. Its last line is far
// past the 120 characters the old one-line summary kept.
const thePlanOutput = "Add RefundOnce to the ledger so a refund is counted once, and call it from Total.\n\n" +
	"files:\n- internal/ledger/refund.go\n- internal/ledger/refund_test.go\n\n" +
	"Done when TestRefundOnce passes and the March export totals match the bank's statement to the cent, refunds included."

// handoverJob is a step after plan › write › test, briefed with what each
// reported.
func handoverJob(stage factory.Stage) Job {
	return Job{
		Item:  factory.Item{ID: 5, Num: 21, Kind: factory.KindIssue, Repo: "ledger", Title: "refunds count twice", ChangeBase: "3f2a9c1b7d0e55aa"},
		Stage: stage,
		Round: 1,
		Prior: []factory.StageResult{
			{Done: true, Output: thePlanOutput, Claims: []factory.Claim{{Text: "the plan adds one function", OK: true}}},
			{Done: true, Output: "added RefundOnce and its test", Claims: []factory.Claim{
				{Text: "refunds count once", OK: true, Evidence: "go test ./internal/ledger -run TestRefundOnce", Medium: "test"},
				{Text: "the March export matches", OK: false},
			}},
			{Done: true, Output: "go test ./... ok"},
		},
		PriorNames: []string{"plan", "write", "test"},
		Dir:        "/work/ledger-21",
	}
}

func TestTheWriteStepsBriefCarriesThePlanWholeAndTheFilesItNames(t *testing.T) {
	job := handoverJob(factory.Stage{Name: "write", Kind: factory.StageChat, Ask: "make the change"})
	job.Prior, job.PriorNames = job.Prior[:1], job.PriorNames[:1]
	brief := stageBrief(job)
	for _, want := range []string{
		briefPlanHead + "\n" + thePlanOutput,
		briefFilesHead + "\n- internal/ledger/refund.go\n- internal/ledger/refund_test.go",
		"before this stage:\n- plan: done · 1 claim · its plan is above",
		"claims made so far:\n- plan · ok · the plan adds one function",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the write step's brief lacks %q:\n%s", want, brief)
		}
	}
	if n := strings.Count(brief, "Done when TestRefundOnce passes"); n != 1 {
		t.Errorf("the plan's words are in the brief %d times, want once:\n%s", n, brief)
	}
}

func TestOnlyTheWriteStepIsHandedThePlansFilesAndOnlyAFinishedPlanNamesThem(t *testing.T) {
	review := handoverJob(factory.Stage{Name: "review", Kind: factory.StageChat, Ask: "read it as a stranger would"})
	if brief := stageBrief(review); strings.Contains(brief, briefFilesHead) || !strings.Contains(brief, thePlanOutput) {
		t.Errorf("review is handed the files, or not the plan:\n%s", brief)
	}
	write := handoverJob(factory.Stage{Name: "write", Kind: factory.StageChat, Ask: "make the change"})
	write.Prior = write.Prior[:1]
	write.Prior[0].Done = false
	if brief := stageBrief(write); strings.Contains(brief, briefFilesHead) {
		t.Errorf("an unfinished plan's files reached write:\n%s", brief)
	}
}

func TestTheProofStepsBriefListsEveryClaimSoFarWithItsStepAndEvidence(t *testing.T) {
	job := handoverJob(factory.Stage{Name: "proof", Kind: factory.StageChat, Ask: "show it works", Until: factory.UntilProven})
	brief := stageBrief(job)
	want := briefProofClaims + "\n" +
		"- plan · ok · the plan adds one function\n" +
		"- write · ok · refunds count once · evidence: go test ./internal/ledger -run TestRefundOnce (test)\n" +
		"- write · not ok · the March export matches"
	if !strings.Contains(brief, want) {
		t.Fatalf("the proof step's brief lacks its claims:\n%s", brief)
	}
	if !strings.Contains(brief, "- write: done · 2 claims\n  added RefundOnce and its test") {
		t.Errorf("an earlier step's words are not under its line:\n%s", brief)
	}
}

func TestThePlanStepIsToldToWriteThePlanInOutput(t *testing.T) {
	plan := Job{Item: factory.Item{ID: 1, Num: 4}, Stage: factory.Stage{Name: "plan", Ask: "read the issue and say how"}, Round: 1}
	if brief := stageBrief(plan); !strings.Contains(brief, "Write the plan in output: what you'll change, in which files, and how you'll know it works.") {
		t.Fatalf("the plan step is not told where its plan goes:\n%s", brief)
	}
	write := Job{Item: factory.Item{ID: 1, Num: 4}, Stage: factory.Stage{Name: "write", Ask: "make the change"}, Round: 1}
	if brief := stageBrief(write); strings.Contains(brief, briefPlanWrite) {
		t.Fatalf("a step that is not the plan is told to write one:\n%s", brief)
	}
}

// The 2026-10-28 job named every file, under a heading the factory does not
// read, and Auto paused it as a plan that named none. Every plan step, the
// shipped one and one a pipeline file writes itself, is told the one shape
// the factory reads, and a plan written that way clears a path rule its files
// do not touch.
func TestEveryPlanStepIsToldTheFileListShapeTheFactoryReads(t *testing.T) {
	shipped, _ := factory.ShippedStage("plan")
	own := factory.Stage{Name: "Plan", Kind: factory.StageChat, Ask: "work out the change with the team's checklist"}
	for _, st := range []factory.Stage{shipped, own} {
		job := Job{Item: factory.Item{ID: 1, Num: 4}, Stage: st, Round: 1}
		if brief := stageBrief(job); !strings.Contains(brief, briefPlanFiles) {
			t.Fatalf("plan step %q is not told the shape of its file list:\n%s", st.Ask, brief)
		}
		if brief := resumedStageBrief(job); !strings.Contains(brief, briefPlanFiles) {
			t.Fatalf("plan step %q carried on is not told the shape of its file list:\n%s", st.Ask, brief)
		}
	}
	write := Job{Item: factory.Item{ID: 1, Num: 4}, Stage: factory.Stage{Name: "write", Ask: "make the change"}, Round: 1}
	if strings.Contains(stageBrief(write), "Files:") {
		t.Fatal("a step that is not the plan is told how to list a plan's files")
	}

	if got := factory.ParsePlanFiles(briefPlanFiles); !slices.Equal(got, []string{"cmd/app/main.go", "docs/usage.md"}) {
		t.Fatalf("the brief's own example reads as %q", got)
	}
	asked := "Add Session.Working and show it on the status line.\n\n" +
		"CHANGE, PER FILE\n" +
		"1. internal/remote/server.go — add Session.Working.\n" +
		"2. cmd/codeaf/engine.go — print the working line.\n\n" +
		"Verify with go test ./internal/remote.\n\n" +
		"Files:\n" +
		"- internal/remote/server.go\n" +
		"- cmd/codeaf/engine.go\n"
	files := factory.ParsePlanFiles(asked)
	if !slices.Equal(files, []string{"internal/remote/server.go", "cmd/codeaf/engine.go"}) {
		t.Fatalf("a plan written as the brief asks reads as %q", files)
	}
	auth := &factory.StartRules{AskFirst: []string{"touch auth/"}, Never: []string{}}
	if hold := pipeline.PlanHold(auth, files); hold != "" {
		t.Fatalf("a plan that lists only unprotected files is held: %q", hold)
	}
	// THE PARSER STAYS STRICT: the same plan without its Files: list names no
	// files, however plainly its prose names them.
	prose, _, _ := strings.Cut(asked, "Files:\n")
	if got := factory.ParsePlanFiles(prose); got != nil {
		t.Fatalf("prose was read as a file list: %q", got)
	}
}

func TestAnEarlierStepsWordsAreCutToTheirMostNotToTheirFirstLine(t *testing.T) {
	job := handoverJob(factory.Stage{Name: "review", Kind: factory.StageChat})
	job.Prior[1].Output = "line one\n" + strings.Repeat("w", briefStepMost)
	brief := stageBrief(job)
	if !strings.Contains(brief, "  line one\n  "+strings.Repeat("w", briefStepMost-len("line one\n"))+" …") {
		t.Fatalf("write's words are not carried to %d characters:\n%s", briefStepMost, brief)
	}
}

func TestALongPlanIsCutAtItsMostAndTheBriefSaysSo(t *testing.T) {
	job := handoverJob(factory.Stage{Name: "write", Kind: factory.StageChat})
	job.Prior[0].Output = strings.Repeat("é", briefPlanMost+500)
	brief := stageBrief(job)
	if n := strings.Count(brief, "é"); n != briefPlanMost {
		t.Fatalf("the brief carries %d of the plan's characters, want %d", n, briefPlanMost)
	}
	if !strings.Contains(brief, "the plan (from the plan step; its first 6000 characters, the rest cut):") {
		t.Fatalf("a cut plan is not said to be cut:\n%s", brief[:200])
	}
}

// THE CARRY STAYS UNDER ITS CAP, OLDEST FIRST, PLAN NEVER: a plan of 5,000
// characters, three steps that each said 1,000 and twenty claims are over
// 12,000; write's words go first, then test's, and review's and the plan's
// stay whole.
func TestTheCarryStaysUnderItsCapCuttingTheOldestWordsFirstAndNeverThePlan(t *testing.T) {
	plan := strings.Repeat("p", 5000)
	var claims []factory.Claim
	for i := 0; i < 20; i++ {
		claims = append(claims, factory.Claim{Text: strings.Repeat("c", 250), OK: true})
	}
	job := Job{
		Item:  factory.Item{ID: 9, Num: 30},
		Stage: factory.Stage{Name: "proof", Kind: factory.StageChat, Until: factory.UntilProven},
		Round: 1,
		Prior: []factory.StageResult{
			{Done: true, Output: plan},
			{Done: true, Output: strings.Repeat("w", 1000)},
			{Done: true, Output: strings.Repeat("t", 1000)},
			{Done: true, Output: strings.Repeat("r", 1000), Claims: claims},
		},
		PriorNames: []string{"plan", "write", "test", "review"},
	}
	paras := handover(job)
	var all []string
	for _, p := range paras {
		all = append(all, p...)
	}
	if n := chars(all...); n > briefEarlierMost {
		t.Fatalf("what earlier steps handed on is %d characters, over its %d", n, briefEarlierMost)
	}
	brief := stageBrief(job)
	for _, want := range []string{
		briefPlanHead + "\n" + plan,
		"- write: done · " + briefStepCut,
		"- test: done · " + briefStepCut,
		"- review: done · 20 claims\n  " + strings.Repeat("r", 1000),
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q", want[:min(len(want), 80)])
		}
	}
	if strings.Contains(brief, strings.Repeat("w", 10)) || strings.Contains(brief, strings.Repeat("t", 10)) {
		t.Error("the oldest steps' words were kept while the cap was exceeded")
	}
	if strings.Count(brief, strings.Repeat("c", 250)) != 20 {
		t.Error("claims were cut although cutting the oldest words was enough")
	}
}

// A STEP IS NAMED BY ITS OWN STEP, NOT ITS PLACE AMONG THE STAGES: an approve
// step between plan and write reports nothing, and the old reading named
// everything after it by place.
func TestEarlierStepsAreNamedByPriorNamesAndByPlaceWhenUnknown(t *testing.T) {
	job := handoverJob(factory.Stage{Name: "review", Kind: factory.StageChat})
	job.PriorNames = []string{"plan", ""}
	brief := stageBrief(job)
	for _, want := range []string{"- plan: done", "- stage 2: done · 2 claims", "- stage 3: done"} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q:\n%s", want, brief)
		}
	}
}

func TestTheWorkTreeIsSaidToBeTheSourceOfTruthOnceAStepHasRun(t *testing.T) {
	job := handoverJob(factory.Stage{Name: "write", Kind: factory.StageChat})
	want := "The work tree, not this brief, is the source of truth for what earlier steps changed: `git log 3f2a9c1b7d0e..HEAD` and `git diff 3f2a9c1b7d0e` on your branch show it."
	if brief := stageBrief(job); !strings.Contains(brief, want) {
		t.Fatalf("the brief does not say where earlier steps' changes are:\n%s", brief)
	}
	job.Prior, job.PriorNames = nil, nil
	if brief := stageBrief(job); strings.Contains(brief, "source of truth") {
		t.Fatalf("the first step is pointed at earlier steps' changes:\n%s", brief)
	}
}

// A RUN RESUMED ON ANOTHER WORKER KEEPS WHAT CAME BEFORE. The job is paused
// after plan, crosses the worker's pull as JSON ([api.WorkerJob] and
// [api.ToItem]), is stored on a machine that never ran it, and run again
// there: write is handed plan's done, its notes and its claims, named as
// plan, exactly as it would have been on the first machine.
func TestARunResumedOnAnotherWorkerKeepsEarlierStepsDoneNotesAndClaims(t *testing.T) {
	p := newPauseExec()
	claim := factory.Claim{Text: "the plan adds RefundOnce", OK: true, Evidence: "the plan's files list", Medium: "transcript"}
	exec := ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		if job.Stage.Name == "plan" {
			return factory.StageResult{Done: true, Output: thePlanOutput, Notes: []string{"keep the old flag"}, Claims: []factory.Claim{claim}}, nil
		}
		return p.Run(ctx, job)
	})
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: exec}, nil)
	id := g.add("one", chat("plan"), chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	if err := g.r.Pause(id); err != nil {
		t.Fatal(err)
	}
	g.wait(id, "paused", func(it factory.Item) bool { return it.Stream != nil && it.Stream.Paused })
	g.r.loop().ctl(id).cancel()
	time.Sleep(20 * time.Millisecond)

	// The pull, on the wire.
	held, err := g.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(api.WorkerPull{Job: api.WorkerJob(held)})
	if err != nil {
		t.Fatal(err)
	}
	var pulled api.WorkerPull
	if err := json.Unmarshal(raw, &pulled); err != nil {
		t.Fatal(err)
	}
	moved := api.ToItem(pulled.Job)
	moved.ID = 0
	other, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	created, err := other.Create(moved)
	if err != nil {
		t.Fatal(err)
	}

	p2 := newPauseExec()
	r2 := New(Options{Store: other, Exec: map[factory.StageKind]Executor{factory.StageChat: p2}, Benches: 1, Events: make(chan Event, 100)})
	if err := r2.Launch(created.ID); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p2)
	j := p2.job(0)
	if len(j.Prior) != 1 || !j.Prior[0].Done || j.Prior[0].Output != thePlanOutput {
		t.Fatalf("the resumed write's prior is %+v", j.Prior)
	}
	if !slices.Equal(j.Prior[0].Notes, []string{"keep the old flag"}) || len(j.Prior[0].Claims) != 1 || j.Prior[0].Claims[0] != claim {
		t.Fatalf("the resumed write lost plan's notes or claims: %+v", j.Prior[0])
	}
	if !slices.Equal(j.PriorNames, []string{"plan"}) || !slices.Contains(j.Notes, "keep the old flag") {
		t.Fatalf("the resumed write's names %q, notes %q", j.PriorNames, j.Notes)
	}
	if brief := stageBrief(j); !strings.Contains(brief, "- plan: done · 1 claim · its plan is above") || !strings.Contains(brief, briefFilesHead) {
		t.Fatalf("the resumed write's brief does not read plan as done:\n%s", brief)
	}
	close(p2.release)
	deadline := time.Now().Add(10 * time.Second)
	for {
		it, err := other.Get(created.ID)
		if err == nil && it.State == factory.StateLanded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the resumed run never landed; it is %s", it.State)
		}
		time.Sleep(3 * time.Millisecond)
	}
}
