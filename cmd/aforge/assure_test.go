package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/revision"
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

	judge := func(reply string) revision.Judgment {
		t.Helper()
		capture := &gateCaptureClient{model: "worker/model", response: reply}
		return revision.JudgeDeliverable(context.Background(), settings,
			adoptLiveClient(settings, capture.model, capture), graph, node,
			"parser A wins", "", revision.Evidence{}, "worker/model")
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
		if !strings.Contains(revision.DeliverablePrompt, required) {
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
			refusal := revision.AdmitGapCitation(revision.Grounds{Intent: intent}, []string{test.quote}, test.spent)
			if test.admitted && refusal != "" {
				t.Fatalf("a legitimate citation was refused: %q", refusal)
			}
			if !test.admitted && refusal == "" {
				t.Fatal("an inadmissible citation was admitted")
			}
		})
	}
}

// The same grounding, one layer earlier: what a failed gate may buy with a
// paid revision round. The measured failure this closes is a gate that held a
// worker to a working decision aforge wrote for itself after reading its own
// output, bought a re-run against it, and got back a worse deliverable. The
// working method is admitted beside the ask because it is the one other
// standard that was fixed before the work started and cannot move in response
// to it.
func TestAGapTheAskNeverSetBuysNoRevisionRound(t *testing.T) {
	const intent = "what was March revenue"
	const method = "Done means the figure is traced to a named source row."
	for name, test := range map[string]struct {
		quote    string
		admitted bool
	}{
		"a span of the ask admits":            {quote: "March revenue", admitted: true},
		"a span of the working method admits": {quote: "traced to a named source row", admitted: true},
		"an invented working decision is refused": {
			quote: "March refers to any calendar year present in the data"},
		"a formatting preference is refused": {quote: "wrapped in a markdown code fence"},
		"no citation at all is refused":      {quote: "  "},
	} {
		t.Run(name, func(t *testing.T) {
			refusal := revision.AdmitGapRevision(revision.Grounds{Intent: intent, Method: method}, []string{test.quote})
			if test.admitted && refusal != "" {
				t.Fatalf("a grounded gap was refused its round: %q", refusal)
			}
			if !test.admitted {
				if refusal == "" {
					t.Fatal("an ungrounded gap bought a paid round")
				}
				// A refusal is not silence: the person reads the review's own
				// words and the reason nothing was redone over them.
				note := revision.GapNote("the year was never disambiguated", refusal)
				if !strings.Contains(note, "the year was never disambiguated") ||
					!strings.Contains(note, refusal) {
					t.Fatalf("the note hid either the gap or the reason: %q", note)
				}
			}
		})
	}
	// A job with no working method is the ordinary case and must not become
	// a job where every gap is grounded by an empty string.
	if refusal := revision.AdmitGapRevision(revision.Grounds{Intent: intent}, []string{"any calendar year present"}); refusal == "" {
		t.Fatal("an empty working method grounded a gap it never contained")
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
	spent := revision.SpentCitations(graph, "job")
	if len(spent) != 1 || spent[0] != "every part" {
		t.Fatalf("ledger = %v, want only the citation that bought a round", spent)
	}
	if refusal := revision.AdmitGapCitation(revision.Grounds{Intent: "answer every part"}, []string{"every part"}, spent); refusal == "" {
		t.Fatal("a span that already bought a round bought a second one")
	}
	if refusal := revision.AdmitGapCitation(revision.Grounds{Intent: "answer every part"}, []string{"answer"}, spent); refusal != "" {
		t.Fatalf("a refused citation blocked its own words forever: %q", refusal)
	}
	// The lineage is this job's, never the one whose id merely starts the same.
	if got := revision.SpentCitations(graph, "job-x1"); len(got) != 0 {
		t.Fatalf("a repair round read its parent's ledger as its own: %v", got)
	}
}

// WORDS SPENT ON A ROUND THAT MOVED THE TREE ARE NOT SPENT. The ledger used to
// be a count of one wearing an invariant's clothes: words that bought a round
// could never buy another, whatever that round did. That is the same species of
// mistake as a retry count, and the settle lane already built the thing that
// decides it properly — the growth journal records, per round, how many files
// the work actually left behind. So the ledger bounds SCOPE against a finite
// ask and the journal bounds REPETITION against measured change, and EVERYTHING
// UNKNOWN STAYS SPENT so the direction never moves where the evidence is absent.
func TestTheLedgerReleasesOnlyTheWordsWhoseRoundMovedTheTree(t *testing.T) {
	for name, test := range map[string]struct {
		growth *store.JobGrowth
		spent  bool
	}{
		"a round that wrote seven files": {
			growth: &store.JobGrowth{Reason: resident.GrowGap, Lineage: "job", Round: 1,
				Allowed: true, Measured: true, Produced: 7},
		},
		"a round that wrote nothing":   {growth: &store.JobGrowth{Reason: resident.GrowGap, Lineage: "job", Round: 1, Allowed: true, Measured: true}, spent: true},
		"a round nobody measured":      {growth: &store.JobGrowth{Reason: resident.GrowGap, Lineage: "job", Round: 1, Allowed: true, Produced: 7}, spent: true},
		"a round the governor refused": {growth: &store.JobGrowth{Reason: resident.GrowGap, Lineage: "job", Round: 1, Measured: true, Produced: 7}, spent: true},
		"no journal at all":            {spent: true},
	} {
		t.Run(name, func(t *testing.T) {
			graph := openCacheStore(t)
			if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
				{ID: "job", Brief: "answer every part"}}},
				store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "answer every part"}); err != nil {
				t.Fatal(err)
			}
			if err := graph.RecordDeliveryGate("job", store.DeliveryGate{
				Gap: "no numbers", Quote: "every part", Round: 1, Extended: true}); err != nil {
				t.Fatal(err)
			}
			if test.growth != nil {
				if err := graph.RecordJobGrowth("job", *test.growth); err != nil {
					t.Fatal(err)
				}
			}
			spent := revision.SpentCitations(graph, "job")
			if got := len(spent) > 0; got != test.spent {
				t.Fatalf("ledger = %v, want spent=%t", spent, test.spent)
			}
			refusal := revision.AdmitGapCitation(revision.Grounds{Intent: "answer every part"},
				[]string{"every part"}, spent)
			if test.spent && refusal == "" {
				t.Fatal("words whose round changed nothing bought another round")
			}
			if !test.spent && refusal != "" {
				t.Fatalf("words whose round wrote seven files were refused: %q", refusal)
			}
		})
	}
	// And a sibling lineage's productivity is not evidence about this one: a
	// bound that borrowed it would let one branch of a job buy the other's
	// rounds.
	graph := openCacheStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "answer every part"}}},
		store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "answer every part"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordDeliveryGate("job", store.DeliveryGate{
		Gap: "no numbers", Quote: "every part", Round: 1, Extended: true}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordJobGrowth("job", store.JobGrowth{Reason: resident.GrowGap,
		Lineage: "job-n2", Round: 1, Allowed: true, Measured: true, Produced: 7}); err != nil {
		t.Fatal(err)
	}
	if spent := revision.SpentCitations(graph, "job"); len(spent) == 0 {
		t.Fatal("one lineage's productive round released another lineage's words")
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
	unmet := revision.Judgment{Checked: true, Gaps: "no numbers appear anywhere",
		Quote: "include the benchmark numbers"}

	extension := revision.ExtendForGap(context.Background(), graph, gateNode(t, graph, "job"), "parser A wins",
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
	notice := revision.GapContinuationNotice(unmet.Gaps)
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
	unmet := revision.Judgment{Checked: true, Gaps: "there is no chart of the results",
		Quote: "a chart of the results"}

	extension := revision.ExtendForGap(context.Background(), graph, gateNode(t, graph, "job"), "parser A wins",
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
	handover := revision.GapHandover(unmet.Gaps, true, extension.Refused)
	for _, want := range []string{"there is no chart of the results", "as far as repair takes it", extension.Refused} {
		if !strings.Contains(handover, want) {
			t.Fatalf("the handover does not carry %q:\n%s", want, handover)
		}
	}
	// A gap the revision never got to answer says so, because "this is the first
	// draft" is a different fact about the delivery than "it was revised once".
	unrevised := revision.GapHandover(unmet.Gaps, false, "")
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
	unmet := revision.Judgment{Checked: true, Gaps: "no numbers", Quote: "the benchmark numbers"}

	first := revision.ExtendForGap(context.Background(), graph, gateNode(t, graph, "job"), "draft",
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
	repeat := revision.Judgment{Checked: true, Gaps: "still no numbers", Quote: "the benchmark\nnumbers"}
	second := revision.ExtendForGap(context.Background(), graph, gateNode(t, graph, "job-x1"), "draft",
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
	other := revision.Judgment{Checked: true, Gaps: "only one parser was read", Quote: "compare the two parsers"}
	if got := revision.ExtendForGap(context.Background(), graph, gateNode(t, graph, "job-x1"), "draft",
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
		spiral := revision.Judgment{
			Checked: true,
			Gaps:    fmt.Sprintf("verify what round %d produced: %s", round, produced),
			Quote:   fmt.Sprintf("verify what round %d produced", round),
		}
		extension := revision.ExtendForGap(context.Background(), graph, gateNode(t, graph, "job"), produced,
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
		unmet := revision.Judgment{Checked: true, Gaps: "missing " + quote, Quote: quote}
		extension := revision.ExtendForGap(context.Background(), graph, gateNode(t, graph, from), "draft",
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
	// The governor leaves its receipt on the work's own record rather than in
	// the conversation (13.18): how many times a job was allowed to divide is
	// the machinery's arithmetic, and the delivery that follows is what the
	// person is owed. Quietly stopping is still the failure being guarded
	// against — the line has to exist, and it has to be findable where the work
	// is.
	recorded, err := graph.NodeMessages(from, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	said := ""
	for _, message := range recorded {
		if message.SessionID != "" {
			t.Fatalf("a governor receipt carries a room: %+v", message)
		}
		said += message.Body + "\n"
	}
	if !strings.Contains(said, "split as many times as splitting helps") {
		t.Fatalf("a governor stopped the work quietly:\n%s", said)
	}
	spoken, err := graph.Messages("s1", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range spoken {
		if strings.Contains(message.Body, "split as many times") {
			t.Fatalf("the governor receipt reached the conversation: %q", message.Body)
		}
	}
}

// ONLY A REFUSAL CHECKED AGAINST THE WORLD DELIVERS WHOLE. Refused holds two
// categorically different refusals. One weighed the finding against the
// filesystem or against the delivered text and found it wrong — the file is
// there, the things it names are there — and that acquits, because charging it a
// non-zero code would teach a harness to distrust the gate's own corrections.
// The other checked only where the review got its words and declined to BUY a
// round; it settles nothing about whether the work landed, because no ruling
// about a citation makes missing work appear. The DeepSWE sweep exited 0 seven
// times out of eight over the second kind, each time with a review that was
// right (bench/deepswe/AUTOPSY.md, docs/design/gate/SETTLEMENT.md §2).
func TestOnlyARefusalCheckedAgainstTheWorldDeliversWhole(t *testing.T) {
	graph := openCacheStore(t)
	for name, test := range map[string]struct {
		gate  store.DeliveryGate
		whole bool
	}{
		"a file the plan promised and the disk does not hold": {
			gate: store.DeliveryGate{Gap: "report.md", Quote: "report.md", Quotes: []string{"report.md"},
				Mechanical: true, Refused: "what the review asked for next is not in the request"},
		},
		"a finding refused for where its words came from": {
			gate: store.DeliveryGate{Gap: "no chart of the results", Quote: "a chart of the results",
				Quotes:  []string{"a chart of the results"},
				Refused: "what the review asked for next is not in the request"},
		},
		"a finding whose repair nothing would fund": {
			gate: store.DeliveryGate{Gap: "no chart of the results", Quotes: []string{"a chart of the results"},
				Refused: "no more work could be started on it", Unclosed: true},
		},
		"a finding the disk overturned": {
			gate: store.DeliveryGate{Gap: "no report.md", Quotes: []string{"report.md"},
				Refused:    "what it asked for is already on disk under the name the request used",
				Overturned: true},
			whole: true,
		},
		"a finding the delivered text overturned": {
			gate: store.DeliveryGate{Gap: "the twelve profiles are missing", Quotes: []string{"twelve profiles"},
				Refused:    "everything it names is already in the delivered text, in the words the request used",
				Overturned: true},
			whole: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			id := "job-" + strings.ReplaceAll(name, " ", "-")
			if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
				{ID: id, Brief: "write the report"}}},
				store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "write report.md"}); err != nil {
				t.Fatal(err)
			}
			if err := graph.RecordDeliveryGate(id, test.gate); err != nil {
				t.Fatal(err)
			}
			node, found, err := graph.Node(id)
			if err != nil || !found {
				t.Fatalf("node %s: found %t, err %v", id, found, err)
			}
			watch := &settlementWatch{graph: graph}
			if whole := watch.deliveredWhole(node); whole != test.whole {
				if test.whole {
					t.Fatal("a finding the system overturned against the world was charged a non-zero exit code")
				}
				t.Fatal("a finding nobody closed and nothing overturned was delivered as done")
			}
		})
	}
	// And the repair that worked is still whole, mechanical or not: the polish
	// round wrote the file, the second gate read it, and there is nothing left
	// for an exit code to complain about.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job-closed", Brief: "write the report"}}},
		store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "write report.md"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordDeliveryGate("job-closed", store.DeliveryGate{
		Gap: "report.md", Quotes: []string{"report.md"}, Mechanical: true, PolishClosed: true}); err != nil {
		t.Fatal(err)
	}
	node, found, err := graph.Node("job-closed")
	if err != nil || !found {
		t.Fatalf("node job-closed: found %t, err %v", found, err)
	}
	if watch := (&settlementWatch{graph: graph}); !watch.deliveredWhole(node) {
		t.Fatal("a mechanical gap the repair round closed was still called partial")
	}
}

// AND THE STREAM SAYS WHICH FINDING, NOT ONLY THAT ONE WAS REFUSED. Ten runs
// printed "gate: refused — what the review asked for next is not in the request"
// and not one of them told the person watching what the review had said was
// missing — which was, every time, the whole news. FAILSAFE clause 3.
func TestTheRefusalLineNamesTheFindingBeforeTheReason(t *testing.T) {
	verdict, detail := gateWords(store.DeliveryGate{
		Gap:     "The deliverable does not contain the code that writes feature_schema.joblib.",
		Refused: "what the review asked for next is not in the request",
	})
	if verdict != "refused" {
		t.Fatalf("verdict = %q, want refused", verdict)
	}
	if !strings.Contains(detail, "feature_schema.joblib") {
		t.Fatalf("the line never names the finding: %q", detail)
	}
	if !strings.Contains(detail, "not in the request") {
		t.Fatalf("the line never says why nothing was bought: %q", detail)
	}
	if !strings.HasPrefix(detail, "The deliverable does not contain") {
		t.Fatalf("the reason is standing in front of the news: %q", detail)
	}
	// A refusal with no finding behind it still says the only thing it knows.
	if _, only := gateWords(store.DeliveryGate{Refused: "no more work could be started on it"}); only != "no more work could be started on it" {
		t.Fatalf("a refusal with no gap said %q", only)
	}
}

// THE MEASUREMENT REACHES THE GATE, AND THE GATE RAISES IT WITHOUT BEING ASKED.
// A worker that took two readings of the project's own verification hands back
// the checks its change turned red; the seam that assembles what the gate is
// shown has to carry them, or the whole measurement is a number in a struct
// nobody reads. Everything else about the record is unchanged.
func TestWhatTheWorkTurnedRedReachesTheGateAndBecomesAFinding(t *testing.T) {
	broke := []string{"tests/test_widget.py::test_size_is_known"}
	evidence := gateEvidence(store.Node{}, plan.Spec{}, &exec.Outcome{
		Baseline:  []string{"one check was already red before this began"},
		Regressed: broke,
	}, nil, true, "")
	if len(evidence.Regressed) != 1 || evidence.Regressed[0] != broke[0] {
		t.Fatalf("the gate was never shown what the work turned red: %v", evidence.Regressed)
	}
	regression, raised := revision.Regressions(evidence.Regressed)
	if !raised || !regression.Sourced || regression.Pass {
		t.Fatalf("a measured regression did not become a finding: %+v", regression)
	}
	// And a worker that took no reading says nothing, which is not the same
	// fact as a worker that read twice and found nothing broken.
	quiet := gateEvidence(store.Node{}, plan.Spec{}, &exec.Outcome{}, nil, true, "")
	if len(quiet.Regressed) != 0 {
		t.Fatalf("a worker that measured nothing claimed something: %v", quiet.Regressed)
	}
}
