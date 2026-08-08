package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
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
