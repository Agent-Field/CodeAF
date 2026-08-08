package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The gate names a gap and, from here, the words of the request that gap is a
// failure of. The quote is what a gap needs to buy new work; a gap without one
// is still a gap and still earns the revision pass, because refusing to judge
// would be a worse answer than judging without authority.
func TestTheGateReturnsTheWordsItsGapFails(t *testing.T) {
	graph := openCacheStore(t)
	settings := config.Config{Model: "worker/model"}
	node := store.Node{ID: "job", Brief: "produce it",
		Provenance: store.Provenance{Intent: "compare the two parsers and include the benchmark numbers"}}

	judge := func(reply string) deliverableJudgment {
		t.Helper()
		capture := &gateCaptureClient{model: "worker/model", response: reply}
		return judgeDeliverable(context.Background(), settings,
			&liveClient{settings: settings, model: capture.model, client: capture}, graph, node,
			"parser A wins", "", deliveryEvidence{}, "worker/model")
	}

	cited := judge(`{"pass":false,"gaps":"no numbers appear anywhere","quote":"include the benchmark numbers"}`)
	if !cited.Checked || cited.Pass || cited.Quote != "include the benchmark numbers" {
		t.Fatalf("a cited gap lost its citation: %+v", cited)
	}
	bare := judge(`{"pass":false,"gaps":"no numbers appear anywhere"}`)
	if !bare.Checked || bare.Pass || bare.Quote != "" {
		t.Fatalf("an uncited gap did not survive as a gap: %+v", bare)
	}
	// A pass carries no citation and is asked for none: only a gap has anything
	// to point at.
	if passed := judge(`{"pass":true,"exercised":true}`); passed.Quote != "" {
		t.Fatalf("a pass manufactured a citation: %+v", passed)
	}
	for name, required := range map[string]string{
		"the quote is asked for":        "quote the words of the request it is a failure of",
		"it is theirs, copied":          "copied exactly as they wrote it",
		"an unquotable gap is a taste":  "is a preference of yours rather than something they asked for and did not get",
		"and the honest answer is pass": "the honest answer for it is pass",
		"the field is in the contract":  `"quote": "<the words of the request this gap fails, copied exactly>"`,
	} {
		if !strings.Contains(judgeDeliverablePrompt, required) {
			t.Errorf("the gate no longer asks for %s: %q missing", name, required)
		}
	}
}

// The citation invariant, stated as the only rule that decides whether a
// judgement may spend money. It is a provenance check: it says nothing about
// whether a gap is a good one, only that the words it claims to fail are the
// user's own and have not already been worked on.
func TestOnlyTheAsksOwnWordsAdmitAGap(t *testing.T) {
	const intent = "compare the two parsers and include the benchmark numbers"
	for name, test := range map[string]struct {
		quote    string
		spent    []string
		admitted bool
	}{
		"a span of the ask admits":              {quote: "include the benchmark numbers", admitted: true},
		"the whole ask admits":                  {quote: intent, admitted: true},
		"re-wrapped whitespace is still theirs": {quote: "include the\n   benchmark  numbers", admitted: true},
		"no citation at all is refused":         {quote: "   "},
		"words they never said are refused":     {quote: "a chart of the results"},
		"a paraphrase is refused":               {quote: "include benchmark numbers"},
		"the spiral's own gap is refused":       {quote: "verify what the previous round produced"},
		"a span already worked on is refused":   {quote: "the benchmark numbers", spent: []string{"the benchmark  numbers"}},
		"a different span still admits":         {quote: "compare the two parsers", spent: []string{"the benchmark numbers"}, admitted: true},
	} {
		t.Run(name, func(t *testing.T) {
			refusal := admitGapCitation(intent, test.quote, test.spent)
			if test.admitted && refusal != "" {
				t.Fatalf("a legitimate citation was refused: %q", refusal)
			}
			if !test.admitted && refusal == "" {
				t.Fatal("an inadmissible citation was admitted")
			}
		})
	}
}

// The ledger is read out of the journal, so it survives a restart: what bounds
// new work has to replay, or every crash hands the job a fresh allowance. Only
// a quote that actually bought a round is spent — a refused one never cost
// anything and must not block the words it names forever.
func TestTheGapLedgerCountsOnlyTheRoundsThatWereBought(t *testing.T) {
	graph := openCacheStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{ID: "job", Brief: "the job"}}},
		store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "answer every part"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{ID: "job-x1", Brief: "the repair"}}},
		store.Provenance{Origin: store.OriginSelf, SessionID: "s1", Intent: "answer every part"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordDeliveryGate("job", store.DeliveryGate{
		Gap: "no numbers", Quote: "every part", Round: 1, Extended: true}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordDeliveryGate("job-x1", store.DeliveryGate{
		Gap: "still nothing", Quote: "answer", Round: 2, Refused: "the same words were already worked on once"}); err != nil {
		t.Fatal(err)
	}

	// The lineage read follows the repair round, and the round the graph refused
	// left nothing spent behind it.
	spent := spentCitations(graph, "job")
	if len(spent) != 1 || spent[0] != "every part" {
		t.Fatalf("ledger = %v, want only the citation that bought a round", spent)
	}
	if refusal := admitGapCitation("answer every part", "every part", spent); refusal == "" {
		t.Fatal("a span that already bought a round bought a second one")
	}
	if refusal := admitGapCitation("answer every part", "answer", spent); refusal != "" {
		t.Fatalf("a refused citation blocked its own words forever: %q", refusal)
	}
	// The lineage is this job's, never the one whose id merely starts the same.
	if got := spentCitations(graph, "job-x1"); len(got) != 0 {
		t.Fatalf("a repair round read its parent's ledger as its own: %v", got)
	}
}

// assureFixture is one delivered job: a top-level node whose verbatim intent is
// the immutable thing every citation is checked against.
func assureFixture(t *testing.T, intent string) *store.Store {
	t.Helper()
	graph := openCacheStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "job", Brief: "produce it", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: intent}); err != nil {
		t.Fatal(err)
	}
	claim, won, err := graph.Claim("job", "w1")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	return graph
}

// countingPlanner is the repair planner, counted. Whether it ran at all is the
// whole question for most of these tests: a refusal that still buys a planning
// call is not a refusal, it is a refusal to splice what was already paid for.
type countingPlanner struct {
	calls int
	goals []string
}

func (p *countingPlanner) plan(_ context.Context, goal, prefix string) (store.Subtree, error) {
	p.calls++
	p.goals = append(p.goals, goal)
	return store.Subtree{Nodes: []store.NodeSpec{
		{ID: prefix, Brief: "close the gap", Title: "Close the gap"},
	}}, nil
}

func gateNode(t *testing.T, graph *store.Store, id string) store.Node {
	t.Helper()
	node, ok, err := graph.Node(id)
	if err != nil || !ok {
		t.Fatalf("node %q: ok=%t err=%v", id, ok, err)
	}
	return node
}

// The wire this whole wave is: a judgement that names a gap and quotes the ask
// may commission the work that closes it, through the one path that already
// knows how to grow a live job.
func TestACitedGapGrowsTheJobThroughTheOverrunPath(t *testing.T) {
	const intent = "compare the two parsers and include the benchmark numbers"
	graph := assureFixture(t, intent)
	planner := &countingPlanner{}
	unmet := deliverableJudgment{Checked: true, Gaps: "no numbers appear anywhere",
		Quote: "include the benchmark numbers"}

	extension := extendForGap(context.Background(), graph, gateNode(t, graph, "job"), "parser A wins",
		unmet, []string{"/tmp/draft.md"}, 0, planner.plan)
	if extension.Spliced != 1 || extension.Refused != "" || extension.Round != 1 {
		t.Fatalf("a cited gap did not grow the job: %+v", extension)
	}
	if planner.calls != 1 {
		t.Fatalf("planner calls = %d, want exactly one round", planner.calls)
	}
	// The repair is aimed at what the reviewer named, and it is told not to
	// invent checking of what already exists — the plan's own rule that no piece
	// of work may exist to look at another's product, restated at the seam that
	// commissions work.
	if !strings.Contains(planner.goals[0], "no numbers appear anywhere") {
		t.Fatalf("the repair was not aimed at the named gap:\n%s", planner.goals[0])
	}
	if !strings.Contains(planner.goals[0], "do not add verification, re-verification, or review of existing results") {
		t.Fatalf("the repair goal lost the clause that forbids checking as work:\n%s", planner.goals[0])
	}
	// It lands in this job's own split namespace, which is what makes the round
	// counter, the job-size ceiling and the ledger all read the same lineage.
	repair := gateNode(t, graph, "job-x1")
	if repair.Parent != store.RootID {
		t.Fatalf("the repair is not a continuation of the job: parent %q", repair.Parent)
	}
	if repair.Provenance.Intent != intent {
		t.Fatalf("the repair lost the ask it exists to satisfy: %q", repair.Provenance.Intent)
	}
	// And the person is told, once, in one line that says what and why.
	notice := gapContinuationNotice(unmet.Gaps)
	if strings.Count(notice, "\n") != 0 || !strings.Contains(notice, "no numbers appear anywhere") {
		t.Fatalf("the continuation notice is not one calm line naming the gap: %q", notice)
	}
	// The summary carries the receipt that suppresses the premature
	// announcement: the job speaks when it is done, not twice.
	if !resident.SplitContinued("parser A wins\n\n[" + continuationMessage(extension.Spliced) + "]") {
		t.Fatal("the extended delivery does not read as continued work")
	}
}

// An uncited gap must cost nothing. The refusal happens before any planning
// call, and the draft ships with a handover that names the gap and says why
// nothing more was started — which is what puts the user's next sentence into
// the correction path.
func TestAnUncitedGapReachesNoPlannerAndShipsWithAHandover(t *testing.T) {
	graph := assureFixture(t, "compare the two parsers")
	planner := &countingPlanner{}
	unmet := deliverableJudgment{Checked: true, Gaps: "there is no chart of the results",
		Quote: "a chart of the results"}

	extension := extendForGap(context.Background(), graph, gateNode(t, graph, "job"), "parser A wins",
		unmet, nil, 0, planner.plan)
	if extension.Spliced != 0 || extension.Refused == "" {
		t.Fatalf("an uncited gap grew the job: %+v", extension)
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want none: the refusal has to precede the spend", planner.calls)
	}
	if _, ok, _ := graph.Node("job-x1"); ok {
		t.Fatal("a refused gap still spliced a repair")
	}
	handover := gapHandover(unmet.Gaps, true, extension.Refused)
	for _, want := range []string{"there is no chart of the results", "as far as repair takes it", extension.Refused} {
		if !strings.Contains(handover, want) {
			t.Fatalf("the handover does not carry %q:\n%s", want, handover)
		}
	}
	// A gap the revision never got to answer says so, because "this is the first
	// draft" is a different fact about the delivery than "it was revised once".
	unrevised := gapHandover(unmet.Gaps, false, "")
	if !strings.Contains(unrevised, "The revision pass came back empty") {
		t.Fatalf("an unrevised handover does not say so:\n%s", unrevised)
	}
}

// The novelty half of the invariant, end to end through the journal: a span of
// the ask that already bought a round cannot buy a second one, so a job cannot
// circle one requirement until the cap stops it.
func TestTheSameWordsCannotBuyASecondRound(t *testing.T) {
	const intent = "compare the two parsers and include the benchmark numbers"
	graph := assureFixture(t, intent)
	planner := &countingPlanner{}
	unmet := deliverableJudgment{Checked: true, Gaps: "no numbers", Quote: "the benchmark numbers"}

	first := extendForGap(context.Background(), graph, gateNode(t, graph, "job"), "draft",
		unmet, nil, 0, planner.plan)
	if first.Spliced == 0 {
		t.Fatalf("the first round was refused: %+v", first)
	}
	if err := graph.RecordDeliveryGate("job", store.DeliveryGate{
		Gap: unmet.Gaps, Quote: first.Quote, Round: first.Round, Extended: true}); err != nil {
		t.Fatal(err)
	}

	// Round two, from the repair node, citing the same words in different
	// whitespace — the same span by any honest reading.
	repeat := deliverableJudgment{Checked: true, Gaps: "still no numbers", Quote: "the benchmark\nnumbers"}
	second := extendForGap(context.Background(), graph, gateNode(t, graph, "job-x1"), "draft",
		repeat, nil, 0, planner.plan)
	if second.Spliced != 0 || second.Refused == "" {
		t.Fatalf("the same words bought a second round: %+v", second)
	}
	if planner.calls != 1 {
		t.Fatalf("planner calls = %d, want one: the repeat must not reach a planner", planner.calls)
	}
	if second.Round != 2 {
		t.Fatalf("the refused round did not read its own place in the lineage: %+v", second)
	}
	// A different span is still work the person asked for and still admissible.
	other := deliverableJudgment{Checked: true, Gaps: "only one parser was read", Quote: "compare the two parsers"}
	if got := extendForGap(context.Background(), graph, gateNode(t, graph, "job-x1"), "draft",
		other, nil, 0, planner.plan); got.Spliced == 0 {
		t.Fatalf("a fresh span of the ask was refused: %+v", got)
	}
}

// The regression this whole design exists for. One real run replanned a
// finished leaf 27 rounds deep, each round inventing verification of the round
// before it, and burned $4.48 of a $20 rail in 22 minutes while the job's actual
// work sat pending. That shape is now unrepresentable rather than capped: the
// gap it kept naming is a substring of nothing anyone typed, so it is refused
// before any planning call, for as many rounds as anyone cares to try.
func TestTheTwentySevenRoundShapeIsStructurallyImpossible(t *testing.T) {
	const intent = "inventory the folder and list what is in it"
	graph := assureFixture(t, intent)
	planner := &countingPlanner{}

	produced := "the inventory is complete and verified"
	for round := 1; round <= 27; round++ {
		// Each round's gap is derived from the previous round's own output,
		// which is exactly how the spiral fed itself.
		spiral := deliverableJudgment{
			Checked: true,
			Gaps:    fmt.Sprintf("verify what round %d produced: %s", round, produced),
			Quote:   fmt.Sprintf("verify what round %d produced", round),
		}
		extension := extendForGap(context.Background(), graph, gateNode(t, graph, "job"), produced,
			spiral, nil, 0, planner.plan)
		if extension.Spliced != 0 || extension.Refused == "" {
			t.Fatalf("round %d of invented verification was admitted: %+v", round, extension)
		}
		produced = spiral.Gaps
	}
	if planner.calls != 0 {
		t.Fatalf("planner calls = %d, want none across 27 rounds", planner.calls)
	}
	ids, err := graph.NodeIDsWithPrefix("job")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("the job grew to %d nodes on invented verification: %v", len(ids), ids)
	}
}

// The caps stay exactly what they were meant to be: the thing that never fires
// on a well-formed job, and the backstop that still does. Three rounds of
// genuinely different spans of the ask are admitted; the fourth is refused by
// the round cap, and the person is told in the governor's own words.
func TestTheRoundCapStillBoundsEvenACitedLineage(t *testing.T) {
	const intent = "one two three four five"
	graph := assureFixture(t, intent)
	planner := &countingPlanner{}

	from := "job"
	for round, quote := range []string{"one", "two", "three", "four"} {
		unmet := deliverableJudgment{Checked: true, Gaps: "missing " + quote, Quote: quote}
		extension := extendForGap(context.Background(), graph, gateNode(t, graph, from), "draft",
			unmet, nil, 0, planner.plan)
		if round < resident.MaxOverrunRounds {
			if extension.Spliced == 0 {
				t.Fatalf("round %d of a cited lineage was refused: %+v", round+1, extension)
			}
			if err := graph.RecordDeliveryGate(from, store.DeliveryGate{
				Gap: unmet.Gaps, Quote: extension.Quote, Round: extension.Round, Extended: true}); err != nil {
				t.Fatal(err)
			}
			from = fmt.Sprintf("job-x%d", round+1)
			continue
		}
		if extension.Spliced != 0 || extension.Refused == "" {
			t.Fatalf("the round cap did not bind: %+v", extension)
		}
	}
	if planner.calls != resident.MaxOverrunRounds {
		t.Fatalf("planner calls = %d, want %d", planner.calls, resident.MaxOverrunRounds)
	}
	messages, err := graph.Messages("s1", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	said := ""
	for _, message := range messages {
		said += message.Body + "\n"
	}
	if !strings.Contains(said, "split as many times as splitting helps") {
		t.Fatalf("a governor stopped the work quietly:\n%s", said)
	}
}
