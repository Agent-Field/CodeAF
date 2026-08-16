package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/command"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/homes"
)

// The notebook's WIRING gate: that the page is filled at all, that it is filled
// from one read at one instant, that a lens nobody is looking at does not pay
// for it, and that every crossing between the read's vocabulary and the page's
// is the one the reader should see.
//
// What the page DRAWS is internal/tui2/homes' own gate; nothing here asserts a
// pixel that package already owns.

// The production seam, asserted at compile time: the engine is what answers, and
// a signature drifting apart from its one implementation is a wiring bug that
// should fail the build rather than the page.
var _ Learned = (*command.Commander)(nil)

// notebookAllTabs is one frame per tab, concatenated — the page shows one
// section at a time now, so a wiring assertion that spans sections walks them.
// It walks with the page's own jump keys rather than reaching into the pane,
// because a direct JumpTo would skip the shell's invalidation and assert
// against a cached frame.
func notebookAllTabs(app *App) string {
	var out strings.Builder
	for _, key := range []string{"1", "2", "3"} {
		press(app, key)
		out.WriteString(app.Frame(120, 40))
		out.WriteByte('\n')
	}
	return out.String()
}

// learnedCommander is a [Commander] that can also answer the notebook read, the
// way *command.Commander does.
type learnedCommander struct {
	fakeCommander
	page  command.NotebookPage
	reads int
}

func (l *learnedCommander) NotebookPage(time.Time) command.NotebookPage {
	l.reads++
	return l.page
}

// learnedPage is one of everything, so a mapping that drops a field or crosses
// an enum wrongly shows up as a missing word rather than as a silent zero.
func learnedPage() command.NotebookPage {
	at := fixedNow().Add(-3 * time.Hour)
	return command.NotebookPage{
		Total: 512, AtCeiling: true,
		Beliefs: []command.NotebookBelief{
			{
				Seq: 41, Body: "prefers tables over prose in reports", Scope: "user",
				Kind: "preference", Channel: string(store.FactChannelStated),
				Trust: "strong", Learned: at, Uses: 12, HasUses: true,
				Evidence: []command.NotebookEvidence{{Name: "wisp-parity", Room: "job-3", When: at}},
			},
			{
				Seq: 40, Body: "shorter commit lines", Kind: "preference",
				Class: command.NotebookBeliefTaste, Status: "forming",
				Channel: string(store.FactChannelInferred), Learned: at,
			},
			{
				Seq: 39, Body: "accepts first drafts", Kind: "trait",
				Class: command.NotebookBeliefTrait, Samples: 14, HasSamples: true,
				Learned: at,
			},
		},
		Crafts: []command.NotebookCraft{{
			Name: "release-notes", Description: "collect merged PRs, draft the notes",
			Proved: 4, Against: 1, HasRecord: true,
			LastCostUSD: 0.31, HasCost: true, Version: 3, Updated: at,
			CeilingUSD: 0.5, HasCeiling: true, Wall: 10 * time.Minute,
			Steps:   []command.NotebookCraftStep{{Brief: "gather merged PRs", Needs: []string{"1"}}},
			History: []command.NotebookCraftVersion{{Version: 3, Subject: "tightened the link check", When: at}},
		}},
		Skills: []command.NotebookSkill{{
			Seq: 7, Name: "imgshrink", Body: "shrink a PNG", Path: "/tmp/skills/imgshrink",
			Uses: 11, HasUses: true, Learned: at,
		}},
		Questions: []command.NotebookQuestion{{
			Seq: 3, Body: "how flaky is the e2e suite", Scope: "repo:/x",
			Status: store.QuestionPracticing, Runs: 2, CostUSD: 0.12, HasCost: true, Asked: at,
			Attempts: []command.NotebookAttempt{{CostUSD: 0.04, HasCost: true, Delta: -0.12, HasDelta: true, When: at}},
		}},
		Competence: command.NotebookCompetence{Strongest: "repo:aforge-v2", Frontier: "tool:docker"},
		Today:      command.NotebookDay{SpendUSD: 0.84, HasSpend: true, Learned: 3, Practiced: 42 * time.Minute},
	}
}

// notebookApp is an app sitting on the notebook lens with a backend that can
// answer the read.
func notebookApp(t *testing.T) (*App, *learnedCommander) {
	t.Helper()
	commander := &learnedCommander{page: learnedPage()}
	app := newTestApp(&fakeBackend{}, commander, nil)
	app.showPage(pageNotebook)
	poll(t, app)
	return app, commander
}

// The page is FILLED: every band draws what the read carried, in the page's own
// words rather than the store's.
func TestTheNotebookPageIsFilledFromTheRead(t *testing.T) {
	app, commander := notebookApp(t)
	out := notebookAllTabs(app)
	if commander.reads == 0 {
		t.Fatal("the notebook lens never read the store")
	}
	for _, want := range []string{
		"beliefs 512+",
		"prefers tables over prose in reports", "user · strong",
		"you keep correcting: shorter commit lines", "taste · forming",
		"measured: accepts first drafts", "trait · 14 samples",
		"know-how", "release-notes", // The rest of the receipt is the page's business and the pane's width;
		// what this asserts is that the survival record and the money crossed
		// the seam at all.
		"workflow · proved 4 runs against 1", "$0.31",
		"imgshrink", "tool · 11 uses",
		"practice", "how flaky is the e2e suite", "practicing · 2 runs · $0.12",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the filled page is missing %q:\n%s", want, out)
		}
	}
	// A fact sequence is a handle, not a number to be read (5.14).
	for _, id := range []string{"#41", "#39", "belief:", "craft:", "question:"} {
		if strings.Contains(out, id) {
			t.Fatalf("a row id reached the frame (%q):\n%s", id, out)
		}
	}
}

// The day receipt and the competence line are the practice band's closing
// readings, and a quiet day draws NEITHER rather than $0.00 (§16's EMPTINESS).
func TestTheDayReceiptIsMeasuredOrAbsent(t *testing.T) {
	app, _ := notebookApp(t)
	press(app, "3")
	out := app.Frame(120, 40)
	for _, want := range []string{
		"strongest repo:aforge-v2 · frontier tool:docker",
		"today $0.84 · 42m practiced · 3 learned",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the practice band is missing %q:\n%s", want, out)
		}
	}

	quiet := &learnedCommander{page: command.NotebookPage{}}
	app = newTestApp(&fakeBackend{}, quiet, nil)
	app.showPage(pageNotebook)
	poll(t, app)
	out = notebookAllTabs(app)
	if strings.Contains(out, "$0.00") || strings.Contains(out, "0 learned") {
		t.Fatalf("a quiet day was reported as zeroes:\n%s", out)
	}
	// An empty page teaches rather than going blank, which is what makes an
	// unwired window and a brand new machine show the same true thing.
	for _, want := range []string{"beliefs", "know-how", "practice", "I write one down"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the empty page lost %q:\n%s", want, out)
		}
	}
}

// A backend that cannot answer loses the CONTENTS and nothing else: the page is
// still there, still teaching, and the window still works.
func TestABackendThatCannotAnswerLeavesThePageEmpty(t *testing.T) {
	app := newTestApp(&fakeBackend{}, &fakeCommander{}, nil)
	app.showPage(pageNotebook)
	poll(t, app)
	out := app.Frame(120, 40)
	for _, want := range []string{"beliefs", "know-how", "practice"} {
		if !strings.Contains(out, want) {
			t.Fatalf("an unwired page lost %q:\n%s", want, out)
		}
	}
}

// A lens nobody is looking at does not pay for the read — and the moment the
// reader arrives, the page they see is the one the journal already knows about.
func TestTheReadWaitsForTheLensThenCatchesUp(t *testing.T) {
	commander := &learnedCommander{page: learnedPage()}
	app := newTestApp(&fakeBackend{}, commander, nil)
	poll(t, app)
	if commander.reads != 0 {
		t.Fatalf("the thread lens paid for %d notebook reads", commander.reads)
	}

	app.showPage(pageNotebook)
	out := app.Frame(120, 40)
	if commander.reads != 1 {
		t.Fatalf("arriving on the lens made %d reads", commander.reads)
	}
	// The arrival tab is beliefs, so the proof the read reached the frame is a
	// belief — know-how's rows are one tab key away and gated above.
	if !strings.Contains(out, "prefers tables over prose in reports") {
		t.Fatalf("the catch-up read did not reach the frame:\n%s", out)
	}
	// And it is a CATCH-UP, not a per-frame read: the frames after it cost
	// nothing at all.
	app.Frame(120, 40)
	app.Frame(120, 40)
	if commander.reads != 1 {
		t.Fatalf("rendering the lens read the store %d times", commander.reads)
	}
}

// The mapping itself, field for field, without a terminal in the way. It is
// asserted separately from the frame because a crossing that goes wrong quietly
// — a lifecycle landing on the wrong glyph, a class landing on plain — is
// exactly the kind of bug a rendered haystack hides.
func TestApplyNotebookCrossesEveryEnum(t *testing.T) {
	state := homesStateFor(learnedPage())
	if got := len(state.Notebook.Beliefs); got != 3 {
		t.Fatalf("%d beliefs mapped", got)
	}
	if state.Notebook.Total != 512 || !state.Notebook.AtCeiling {
		t.Fatalf("the window's ceiling was lost: %+v", state.Notebook)
	}
	first := state.Notebook.Beliefs[0]
	if first.ID != "41" || first.Channel.Word() != "you said it" || !first.HasUses {
		t.Fatalf("a plain belief mapped as %+v", first)
	}
	if state.Notebook.Beliefs[1].Class != homes.BeliefTaste {
		t.Fatalf("a taste rule mapped as %v", state.Notebook.Beliefs[1].Class)
	}
	if state.Notebook.Beliefs[2].Class != homes.BeliefTrait || state.Notebook.Beliefs[2].Samples != 14 {
		t.Fatalf("a trait mapped as %+v", state.Notebook.Beliefs[2])
	}
	craft := state.Knowhow.Crafts[0]
	if craft.ID != "release-notes" || !craft.HasRecord || craft.Version != 3 {
		t.Fatalf("a workflow mapped as %+v", craft)
	}
	if !craft.Ceilings.HasCost || craft.Ceilings.WallClock != 10*time.Minute {
		t.Fatalf("a workflow's ceilings mapped as %+v", craft.Ceilings)
	}
	if len(craft.Steps) != 1 || len(craft.History) != 1 {
		t.Fatalf("a workflow's body mapped as %+v", craft)
	}
	if state.Practice.Questions[0].Life != homes.QuestionPracticing {
		t.Fatalf("a practicing gap mapped as %v", state.Practice.Questions[0].Life)
	}
	if got := state.Practice.Questions[0].Attempts; len(got) != 1 || !got[0].HasDelta {
		t.Fatalf("an attempt mapped as %+v", got)
	}
	if !state.Practice.Today.HasSpend || state.Practice.Today.Practiced != 42*time.Minute {
		t.Fatalf("the day mapped as %+v", state.Practice.Today)
	}

	// An unknown word from either side resolves to the plain reading rather than
	// to a guess: a row drawn as the wrong kind of thing is worse than a row
	// drawn as an ordinary one.
	odd := command.NotebookPage{
		Beliefs:   []command.NotebookBelief{{Seq: 1, Body: "x", Class: "moon", Channel: "telepathy"}},
		Questions: []command.NotebookQuestion{{Seq: 2, Body: "y", Status: "levitating"}},
	}
	state = homesStateFor(odd)
	if state.Notebook.Beliefs[0].Class != homes.BeliefPlain {
		t.Fatalf("an unknown class became %v", state.Notebook.Beliefs[0].Class)
	}
	if state.Notebook.Beliefs[0].Channel != homes.ChannelUnknown {
		t.Fatalf("an unknown channel became %v", state.Notebook.Beliefs[0].Channel)
	}
	if state.Practice.Questions[0].Life != homes.QuestionAsked {
		t.Fatalf("an unknown lifecycle became %v", state.Practice.Questions[0].Life)
	}
}

func homesStateFor(page command.NotebookPage) homes.State {
	state := homes.State{}
	applyNotebook(&state, page)
	return state
}
