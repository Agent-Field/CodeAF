package resident

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// wakeCharter creates one active poll charter and reserves a wake on it.
func wakeCharter(t *testing.T, graph *store.Store, id string, tenured bool) (store.Charter, int64, time.Time) {
	t.Helper()
	charter := residentTestCharter(t, id, store.CharterRails{
		PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3,
	}, false)
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	if tenured {
		promoteCharterForWatchTest(t, graph, charter.ID)
	}
	stored, _, _ := graph.Charter(charter.ID)
	wakeAt := stored.NextDue.Add(time.Second)
	wakeSeq, err := graph.BeginCharterWake(charter.ID, wakeAt, "poll occurrence", store.CharterWatchState{
		NextDue: wakeAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return charter, wakeSeq, wakeAt
}

func TestAQuietCheckIsRecordedAsStateNotLostAsAnEvent(t *testing.T) {
	graph := openStore(t)
	charter, _, wakeAt := wakeCharter(t, graph, "quiet-watch", true)
	reconciler := New(graph, nil, nil).WithWatchEngine(0,
		func(context.Context, SentinelPrompt) (SentinelVerdict, error) {
			return SentinelVerdict{Yes: false, Line: "nothing new on the feed"}, nil
		})
	reconciler.now = func() time.Time { return wakeAt }
	pass, err := reconciler.WatchOnce(context.Background())
	if err != nil || pass.Checked != 1 || pass.No != 1 {
		t.Fatalf("pass = %+v err=%v", pass, err)
	}

	stored, found, err := graph.Charter(charter.ID)
	if err != nil || !found {
		t.Fatalf("charter found=%t err=%v", found, err)
	}
	if stored.LastChecked.IsZero() {
		t.Fatal("a watch that checked faithfully still reads as one that never ran")
	}
	if stored.LastCheckLine != "nothing new on the feed" {
		t.Fatalf("the sentinel's reason was written and lost: %q", stored.LastCheckLine)
	}
	// And it survives a rebuild, because the state derives from the journal.
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, _, err := graph.Charter(charter.ID)
	if err != nil || rebuilt.LastCheckLine != stored.LastCheckLine || rebuilt.LastChecked.IsZero() {
		t.Fatalf("rebuilt check state = %+v err=%v", rebuilt, err)
	}
}

func TestAnIgnoredFiringProposalLapsesAndCountsAgainstPromotion(t *testing.T) {
	graph := openStore(t)
	charter, wakeSeq, wakeAt := wakeCharter(t, graph, "ignored-watch", false)
	sentinel := func(context.Context, SentinelPrompt) (SentinelVerdict, error) {
		return SentinelVerdict{Yes: true, Line: "a release landed"}, nil
	}
	reconciler := New(graph, nil, nil).WithWatchEngine(0, sentinel)
	reconciler.now = func() time.Time { return wakeAt }
	pass, err := reconciler.WatchOnce(context.Background())
	if err != nil || pass.Proposed != 1 {
		t.Fatalf("first pass = %+v err=%v", pass, err)
	}
	if refusals, err := graph.CharterFiringRefusals(charter.ID); err != nil || refusals != 0 {
		t.Fatalf("refusals before the window closed = %d err=%v", refusals, err)
	}

	// Nobody answers. The next pass past the window reads the silence.
	later := wakeAt.Add(store.ProbationProposalWindow + time.Minute)
	reconciler.now = func() time.Time { return later }
	pass, err = reconciler.WatchOnce(context.Background())
	if err != nil || pass.No != 1 {
		t.Fatalf("lapse pass = %+v err=%v", pass, err)
	}
	stored, _, err := graph.Charter(charter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.WakePending {
		t.Fatalf("an unanswered proposal pinned the wake open forever: %+v", stored)
	}
	refusals, err := graph.CharterFiringRefusals(charter.ID)
	if err != nil || refusals == 0 {
		t.Fatalf("silence left no trace: refusals=%d err=%v", refusals, err)
	}
	_ = wakeSeq
}

func TestRefusalsRaiseTheBarThatEarnsTenure(t *testing.T) {
	graph := openStore(t)
	charter, wakeSeq, _ := wakeCharter(t, graph, "refused-watch", false)
	if err := graph.DeclineCharterFiring(charter.ID, wakeSeq, "not now", false); err != nil {
		t.Fatal(err)
	}
	refusals, err := graph.CharterFiringRefusals(charter.ID)
	if err != nil || refusals != 1 {
		t.Fatalf("refusals = %d err=%v", refusals, err)
	}

	// Three clean approved firings would have promoted this watch. With one
	// refusal behind it, three are not enough.
	for round := 1; round <= 3; round++ {
		approveAndLandFiring(t, graph, charter.ID, round)
	}
	stored, _, err := graph.Charter(charter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Autonomy != store.CharterProbation {
		t.Fatalf("a refused watch was promoted on mechanical success alone: %+v", stored)
	}
	if stored.GreenFirings != 3 {
		t.Fatalf("green firings = %d", stored.GreenFirings)
	}
	approveAndLandFiring(t, graph, charter.ID, 4)
	stored, _, err = graph.Charter(charter.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Autonomy != store.CharterTenured {
		t.Fatalf("a watch that earned back what it spent was never promoted: %+v", stored)
	}
}

// approveAndLandFiring drives one user-approved probation firing to a clean
// verified landing.
func approveAndLandFiring(t *testing.T, graph *store.Store, charterID string, round int) {
	t.Helper()
	stored, _, err := graph.Charter(charterID)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(time.Duration(round) * time.Hour)
	wakeSeq, err := graph.BeginCharterWake(charterID, at, "poll occurrence", store.CharterWatchState{
		NextDue: at.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordSentinelCheck(charterID, store.SentinelCheck{WakeSeq: wakeSeq, Yes: true, Line: "go"}); err != nil {
		t.Fatal(err)
	}
	// The ladder refuses a probation success nobody sanctioned, so the approval
	// is journaled the way the head journals it.
	approval, err := graph.RequestCommand(store.Command{
		SessionID: "charter-session", Kind: store.CommandCharterFire, Target: charterID,
		Instruction: "wake:" + strconv.FormatInt(wakeSeq, 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.ResolveCommand(approval.Seq, store.CommandApplied, "approved"); err != nil {
		t.Fatal(err)
	}
	jobID := "fired-" + charterID + "-" + string(rune('a'+round))
	subtree := store.Subtree{Nodes: []store.NodeSpec{{ID: jobID, Brief: stored.Action.Template, Stage: 1}}}
	provenance := store.Provenance{Origin: store.OriginTrigger, SessionID: "charter-session",
		Intent: stored.Action.Template, CharterID: charterID}
	if _, err := graph.FireApprovedCharter(charterID, wakeSeq, subtree, provenance, 0, at); err != nil {
		t.Fatal(err)
	}
	landNode(t, graph, jobID, "done")
	if _, err := graph.RecordCharterFiringOutcome(store.CharterFiringAssessment{
		CharterID: charterID, WakeSeq: wakeSeq, JobID: jobID, Decided: true, Success: true,
	}, 3); err != nil {
		t.Fatal(err)
	}
}

func TestAWatchThatKeepsFindingNothingIsQuestionedOnce(t *testing.T) {
	graph := openStore(t)
	charter := residentTestCharter(t, "stale-watch", store.CharterRails{
		PerFiringBudgetUSD: 0.1, MaxFiringsPerDay: 3,
	}, false)
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	// A fortnight of faithful, fruitless checking.
	start := time.Now()
	for round := 0; round < charterHygieneChecks; round++ {
		at := start.Add(time.Duration(round) * time.Hour)
		wakeSeq, err := graph.BeginCharterWake(charter.ID, at, "poll occurrence", store.CharterWatchState{
			NextDue: at.Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.RecordSentinelCheck(charter.ID, store.SentinelCheck{
			WakeSeq: wakeSeq, Yes: false, Line: "nothing",
		}); err != nil {
			t.Fatal(err)
		}
	}

	reconciler := New(graph, nil, nil)
	reconciler.now = func() time.Time { return start.Add(charterHygieneQuiet + time.Hour) }
	reconciler.proposeCharterHygiene(reconciler.now())
	reconciler.proposeCharterHygiene(reconciler.now())

	questions, err := graph.UnresolvedQuestions(20)
	if err != nil {
		t.Fatal(err)
	}
	asked := 0
	for _, question := range questions {
		if question.OriginCharterID == charter.ID {
			asked++
			if question.Urgency != store.QuestionWhenever || question.DefaultAnswer != "1" {
				t.Fatalf("the nudge was not quiet and keep-defaulted: %+v", question)
			}
			stop := false
			for _, option := range question.Options {
				stop = stop || option.Value == "charter:retire:"+charter.ID
			}
			if !stop {
				t.Fatalf("the nudge offers no way to stop it: %+v", question.Options)
			}
		}
	}
	if asked != 1 {
		t.Fatalf("hygiene nudges = %d, want exactly one", asked)
	}
}

// shelfOf is a one-workflow craft shelf the recognizer can match against.
type shelfOf struct{ workflow *craft.Workflow }

func (s shelfOf) Match(request string, k int) []craft.Scored {
	if !strings.Contains(strings.ToLower(request), "release") {
		return nil
	}
	return []craft.Scored{{Summary: craft.Summary{Name: s.workflow.Name}, Score: 100 * craft.MatchFloor}}
}
func (s shelfOf) Load(string) (*craft.Workflow, error)         { return s.workflow, nil }
func (s shelfOf) List() ([]craft.Summary, error)               { return nil, nil }
func (s shelfOf) History(string, int) ([]craft.Version, error) { return nil, nil }
func (s shelfOf) Save(*craft.Workflow, string) (string, error) { return "", nil }

func TestACharterFiringReachesTheCraftShelf(t *testing.T) {
	graph := openStore(t)
	charter, wakeSeq, wakeAt := wakeCharter(t, graph, "craft-watch", true)
	workflow := &craft.Workflow{
		Name: "release-notes", Commit: "a1b2c3d",
		Steps: []craft.Step{{ID: "gather", Brief: "gather the merged pull requests"}},
	}
	planned := false
	reconciler := New(graph,
		func(context.Context, string, string) (Compiled, error) {
			return Compiled{Goal: "Update release documentation", Scale: "task"}, nil
		},
		func(context.Context, Compiled) (store.Subtree, error) {
			planned = true
			return store.Subtree{Nodes: []store.NodeSpec{{ID: "planned", Brief: "plan", Stage: 1}}}, nil
		}).
		WithWatchEngine(0, func(context.Context, SentinelPrompt) (SentinelVerdict, error) {
			return SentinelVerdict{Yes: true, Line: "a release landed"}, nil
		}).
		WithCraftMind(NewCraftMind(shelfOf{workflow: workflow}, t.TempDir(), nil, nil))
	reconciler.now = func() time.Time { return wakeAt }
	pass, err := reconciler.WatchOnce(context.Background())
	if err != nil || pass.Fired != 1 {
		t.Fatalf("pass = %+v err=%v", pass, err)
	}
	if planned {
		t.Fatal("the firing planned from scratch beside a workflow that already knew how")
	}

	root, found, err := graph.Node(firingPrefix(charter.ID, wakeSeq))
	if err != nil || !found {
		t.Fatalf("firing root found=%t err=%v", found, err)
	}
	if root.Provenance.Craft == "" || !strings.HasPrefix(root.Provenance.Craft, "release-notes") {
		t.Fatalf("the firing does not name the craft that ran it: %+v", root.Provenance)
	}
	if root.Provenance.CharterID != charter.ID {
		t.Fatalf("the firing lost its charter: %+v", root.Provenance)
	}
}
