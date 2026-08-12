package command

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The notebook read's gate. It asserts what is TRUE about the answer — which
// class a row is, whether a figure was measured, what is deliberately left out —
// and never what it looks like, because how any of it is drawn belongs to
// internal/tui2/homes and is gated there.

func openNotebookStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "notebook.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	return graph
}

// Four kinds of row come out of one read, each classed by what it IS rather than
// by which query found it — and the three collections that are drawn elsewhere
// (skills, questions, unsettled pairs) stay out of the beliefs list, so nothing
// is drawn twice.
func TestNotebookClassesEveryBeliefAndDrawsNothingTwice(t *testing.T) {
	graph := openNotebookStore(t)
	if _, err := graph.RecordFact("root", "user", store.FactPreference,
		"prefers tables over prose in reports"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("root", "repo:/x", store.FactPlaybook,
		"run the migration before the seed script"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordTasteCandidate("root", "user", "shorter commit lines"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordTrait("accepts-first-drafts", store.TraitMeasurement{
		Value: "usually", N: 14, Updated: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordQuestion("root", "tool:docker", "how flaky is the e2e suite"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordSkillCandidate("root", "user", "shrink a PNG", "/tmp/skills/imgshrink"); err != nil {
		t.Fatal(err)
	}

	page := New(Options{Store: graph}).NotebookPage(time.Now())

	classes := map[NotebookBeliefClass]NotebookBelief{}
	for _, belief := range page.Beliefs {
		if _, twice := classes[belief.Class]; twice && belief.Class != NotebookBeliefPlain {
			t.Fatalf("class %q appeared twice", belief.Class)
		}
		classes[belief.Class] = belief
	}
	for _, class := range []NotebookBeliefClass{
		NotebookBeliefPlain, NotebookBeliefTaste, NotebookBeliefTrait, NotebookBeliefPlaybook,
	} {
		if _, ok := classes[class]; !ok {
			t.Fatalf("class %q is missing from %+v", class, page.Beliefs)
		}
	}
	if got := classes[NotebookBeliefTaste]; got.Status != "forming" || got.Scope != "user" {
		t.Fatalf("a fresh taste shelf read as %+v", got)
	}
	// A trait's body is JSON in the store and a phrase on the page: the raw
	// payload never crosses (12.5).
	trait := classes[NotebookBeliefTrait]
	if trait.Samples != 14 || !trait.HasSamples {
		t.Fatalf("a trait's samples read as %+v", trait)
	}
	if trait.Body == "" || trait.Body[0] == '{' {
		t.Fatalf("a trait's payload reached the surface: %q", trait.Body)
	}
	// The question and the skill are drawn in their own bands, and a belief list
	// carrying them would draw both twice.
	for _, belief := range page.Beliefs {
		if belief.Kind == string(store.FactQuestion) || belief.Kind == string(store.FactSkill) {
			t.Fatalf("%q reached the beliefs list", belief.Kind)
		}
	}
	if len(page.Skills) != 1 || page.Skills[0].Name != "imgshrink" {
		t.Fatalf("the skills band read as %+v", page.Skills)
	}
	if len(page.Questions) != 1 || page.Questions[0].Status != store.QuestionOpen {
		t.Fatalf("the practice band read as %+v", page.Questions)
	}
	// Nothing has run, so nothing has a figure: 12.9.2, all the way down.
	if page.Questions[0].HasCost || page.Questions[0].Runs != 0 {
		t.Fatalf("an undrilled gap carried a receipt: %+v", page.Questions[0])
	}
	if page.Today.HasSpend || page.Today.Practiced != 0 || page.Today.Learned != 0 {
		t.Fatalf("a quiet day carried a receipt: %+v", page.Today)
	}
	if page.Competence.Strongest != "" || page.Competence.Frontier != "" {
		t.Fatalf("an unmeasured machine claimed competence: %+v", page.Competence)
	}
}

// A let-go belief stays in the answer and says it was let go. A belief that
// vanished is a belief nobody can tell you you stopped holding.
func TestNotebookKeepsALetGoBelief(t *testing.T) {
	graph := openNotebookStore(t)
	fact, err := graph.RecordFact("root", "env", store.FactPlain, "the head answers every question itself")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.QuarantineFact(fact.Seq, 0, store.FactOriginUser); err != nil {
		t.Fatal(err)
	}
	page := New(Options{Store: graph}).NotebookPage(time.Now())
	if len(page.Beliefs) != 1 {
		t.Fatalf("the window dropped a let-go belief: %+v", page.Beliefs)
	}
	if !page.Beliefs[0].Retired {
		t.Fatalf("a quarantined belief read as %+v", page.Beliefs[0])
	}
}

// A commander with no store answers with an empty page rather than a panic or a
// half-filled one — the same picture a brand new machine draws, which is what
// lets an unwired window look honestly like an empty one.
func TestNotebookWithNoStoreIsEmpty(t *testing.T) {
	var commander *Commander
	if got := commander.NotebookPage(time.Now()); len(got.Beliefs) != 0 || got.Total != 0 {
		t.Fatalf("a nil commander answered %+v", got)
	}
	if got := New(Options{}).NotebookPage(time.Now()); len(got.Beliefs) != 0 {
		t.Fatalf("a storeless commander answered %+v", got)
	}
}

// The day window is LOCAL midnight either side of now, which is the same day
// store.SelfSpendToday sums over. A window computed in UTC would put the spend
// and the practice on different days for most of the world.
func TestNotebookDayBoundsAreTheLocalDay(t *testing.T) {
	now := time.Date(2026, 8, 11, 14, 30, 0, 0, time.Local)
	start, end := notebookDayBounds(now)
	if !start.Before(now) || !end.After(now) {
		t.Fatalf("the day is %s..%s around %s", start, end, now)
	}
	if got := start.In(time.Local); got.Hour() != 0 || got.Minute() != 0 {
		t.Fatalf("the day starts at %s, not at local midnight", got)
	}
	if got := end.Sub(start); got < 23*time.Hour || got > 25*time.Hour {
		t.Fatalf("the day is %s long", got)
	}
	// A zero clock is a clock that has not arrived, and the window falls back to
	// the real one rather than to the Unix epoch (10.2.8).
	if zeroStart, _ := notebookDayBounds(time.Time{}); zeroStart.Year() < 2020 {
		t.Fatalf("a zero clock dated the day from %s", zeroStart)
	}
}

// WHAT TAUGHT A BELIEF IS A NAME (5.14, design-law-v2 §19). The surface used to
// be handed the node id and drew it — `taught by  task-5381` — so this read now
// resolves the work's own title and carries the handle beside it as something a
// host can open rather than something a reader has to decode.
func TestNotebookEvidenceIsResolvedToANameAndAHandle(t *testing.T) {
	graph := openNotebookStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "ship the pricing page", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "room", Intent: "ship it"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("job", "user", store.FactLesson,
		"the pricing page needs the VPN up first"); err != nil {
		t.Fatal(err)
	}

	page := New(Options{Store: graph}).NotebookPage(time.Now())
	if len(page.Beliefs) != 1 {
		t.Fatalf("the window read as %+v", page.Beliefs)
	}
	evidence := page.Beliefs[0].Evidence
	if len(evidence) != 1 {
		t.Fatalf("the belief's evidence read as %+v", evidence)
	}
	if evidence[0].Name != "ship the pricing page" {
		t.Fatalf("the work was not named: %+v", evidence[0])
	}
	if evidence[0].Room != "job" {
		t.Fatalf("the handle was dropped: %+v", evidence[0])
	}
	if evidence[0].When.IsZero() {
		t.Fatalf("the reference carries no instant: %+v", evidence[0])
	}

	// A belief written under no node at all has nothing to name and nothing to
	// open, and says so by carrying neither — never by falling back to a
	// sequence number, which is the id the surface is forbidden to draw.
	if _, err := graph.RecordFact("", "env", store.FactPlain, "the head answers itself"); err != nil {
		t.Fatal(err)
	}
	page = New(Options{Store: graph}).NotebookPage(time.Now())
	for _, belief := range page.Beliefs {
		for _, ref := range belief.Evidence {
			if ref.Room == "" && ref.Name != "" {
				t.Fatalf("a reference with no room invented a name: %+v", ref)
			}
		}
	}
}

// The naming rule itself: the planner's title wins, the brief is the fallback,
// and a node with neither is left UNNAMED rather than named after its id — which
// is the whole point, since the surface says "a past task" for an empty name and
// an id on a screen is banned (5.14).
//
// It is asserted at the function because the store refuses to admit a node with
// no brief at all, so the last case cannot be built through a splice; it is
// reachable from a folded row and from a graph written by an older version, and
// a fallback nobody can construct is exactly the one that rots.
func TestNotebookNodeNamePrefersTheTitleAndNeverTheId(t *testing.T) {
	for _, c := range []struct {
		node store.Node
		want string
	}{
		{store.Node{ID: "task-5381", Title: "ship the pricing page", Brief: "do the thing"}, "ship the pricing page"},
		{store.Node{ID: "task-5381", Brief: "do the thing"}, "do the thing"},
		{store.Node{ID: "task-5381", Title: "  first line\nsecond"}, "first line"},
		{store.Node{ID: "task-5381"}, ""},
		{store.Node{ID: "task-5381", Title: "   ", Brief: "\n"}, ""},
	} {
		if got := notebookNodeName(c.node); got != c.want {
			t.Fatalf("%+v named %q, want %q", c.node, got, c.want)
		}
	}
}
