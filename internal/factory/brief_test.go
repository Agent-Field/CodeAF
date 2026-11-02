package factory

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

var briefNow = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// briefJob is a job whose plan and write ran, whose test check and review
// reported, and whose proof showed some of its claims.
func briefJob() Item {
	return Item{
		ID: 7, Title: "Retries double-send after a worker restart", State: StateNeedsYou, HeadCommit: "abc",
		Stages: []Stage{
			{Name: "plan", Kind: StageChat, On: true},
			{Name: "write", Kind: StageChat, On: true},
			{Name: "test", Kind: StageCheck, Until: UntilGreen, On: true},
			{Name: "review", Kind: StageChat, Until: UntilClean, On: true},
			{Name: "proof", Kind: StageChat, Until: UntilProven, On: true},
			{Name: "publish", Kind: StagePost, On: true},
		},
		RuleChecks: &RuleChecks{Start: CheckPassed, Publish: CheckPassed, PublishHead: "abc"},
		Stream: &Stream{Phases: []Phase{
			{Name: "plan", Kind: StageChat, State: PhaseDone, Result: &StageResult{Done: true, Output: "the plan"}},
			{Name: "write", Kind: StageChat, State: PhaseDone, Result: &StageResult{Done: true}},
			{Name: "test", Kind: StageCheck, State: PhaseDone, Round: 2, Result: &StageResult{Done: true, Tests: &TestCounts{Command: "go test ./...", Passed: 42, Failed: 0}}},
			{Name: "review", Kind: StageChat, State: PhaseDone, Round: 1, Result: &StageResult{Done: true, Findings: 2}},
			{Name: "proof", Kind: StageChat, State: PhaseDone, Round: 1, Result: &StageResult{Done: true, Claims: []Claim{{Text: "a", OK: true, Evidence: "t"}, {Text: "b"}, {Text: "c", OK: true, Evidence: "t"}}}},
			{Name: "publish", Kind: StagePost, State: PhasePending},
		}},
	}
}

func factLine(fs []BriefFact) string {
	parts := make([]string, len(fs))
	for i, f := range fs {
		ok := "x"
		if f.OK {
			ok = "ok"
		}
		parts[i] = fmt.Sprintf("%s=%s %s %s@%s#%d/%s", f.Key, f.Label, f.Value, ok, f.Evidence.Step, f.Evidence.Try, f.Evidence.Kind)
	}
	return strings.Join(parts, " | ")
}

// THE FACTS ROW COMES FROM THE STEPS' STRUCTURED RESULTS, never from prose:
// a check's test counts (or its exit when it named none), a clean-until
// step's findings, the proof's claims shown of total, and the factory's own
// rule checks. Each fact points at the step and try it came from.
func TestBriefFactsComeFromStructuredResults(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Item)
		want string
	}{
		{"every kind", func(*Item) {},
			"test=Test 42 passed ok@test#2/tests | review=Review 2 findings x@review#1/findings | proof=Proof 2 of 3 x@proof#1/claims | rules=Rules passed ok@#0/rule_checks"},
		{"a check that named no tests says its exit", func(it *Item) {
			it.Stream.Phases[2].Result = &StageResult{Done: true, Exit: 1}
		}, "test=Test exit 1 x@test#2/exit | review=Review 2 findings x@review#1/findings | proof=Proof 2 of 3 x@proof#1/claims | rules=Rules passed ok@#0/rule_checks"},
		{"failed and skipped tests", func(it *Item) {
			it.Stream.Phases[2].Result.Tests = &TestCounts{Passed: 40, Failed: 1, Skipped: 3}
		}, "test=Test 40 passed · 1 failed · 3 skipped x@test#2/tests | review=Review 2 findings x@review#1/findings | proof=Proof 2 of 3 x@proof#1/claims | rules=Rules passed ok@#0/rule_checks"},
		{"a clean review, one finding", func(it *Item) {
			it.Stream.Phases[3].Result.Findings = 0
			it.Stream.Phases[4].Result.Claims = it.Stream.Phases[4].Result.Claims[:1]
		}, "test=Test 42 passed ok@test#2/tests | review=Review clean ok@review#1/findings | proof=Proof 1 of 1 ok@proof#1/claims | rules=Rules passed ok@#0/rule_checks"},
		{"steps that have not reported give nothing", func(it *Item) {
			for i := 2; i < 5; i++ {
				it.Stream.Phases[i].Result = nil
			}
			it.RuleChecks = nil
		}, ""},
		{"a publish finding on an older head is not the job's", func(it *Item) {
			it.HeadCommit = "def"
		}, "test=Test 42 passed ok@test#2/tests | review=Review 2 findings x@review#1/findings | proof=Proof 2 of 3 x@proof#1/claims | rules=Rules passed ok@#0/rule_checks"},
		{"a rule that asked", func(it *Item) {
			it.RuleChecks = &RuleChecks{Start: CheckAsked}
		}, "test=Test 42 passed ok@test#2/tests | review=Review 2 findings x@review#1/findings | proof=Proof 2 of 3 x@proof#1/claims | rules=Rules asked x@#0/rule_checks"},
		{"no run at all", func(it *Item) { it.Stream = nil; it.RuleChecks = nil }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			it := briefJob()
			c.edit(&it)
			if got := factLine(BriefFacts(it)); got != c.want {
				t.Fatalf("facts\n got %s\nwant %s", got, c.want)
			}
		})
	}
}

// A FACTS ROW HOLDS AT MOST FIVE, and the rules fact is never the one cut.
func TestBriefFactsKeepFiveWithRulesLast(t *testing.T) {
	it := briefJob()
	for i := range 4 {
		name := fmt.Sprintf("lint%d", i)
		it.Stages = append(it.Stages, Stage{Name: name, Kind: StageCheck, On: true})
		it.Stream.Phases = append(it.Stream.Phases, Phase{Name: name, Kind: StageCheck, State: PhaseDone, Result: &StageResult{Done: true}})
	}
	got := BriefFacts(it)
	if len(got) != BriefFactsMost || got[len(got)-1].Key != "rules" {
		t.Fatalf("facts = %s", factLine(got))
	}
}

// NO BRIEF YET IS A STUB from engine data alone: the issue's one line, the
// steps done of total, the facts, and no blocks.
func TestBriefStubIsEngineDataOnly(t *testing.T) {
	it := briefJob()
	v := BriefStub(it)
	if !v.Stub || v.Rev != 0 || v.Verdict != it.Title || v.Issue != it.Title || v.StepsDone != 5 || v.StepsTotal != 6 || len(v.Blocks) != 0 || v.Model != "" || v.CostUSD != 0 {
		t.Fatalf("stub = %+v", v)
	}
	if factLine(v.Facts) != factLine(BriefFacts(it)) {
		t.Fatalf("stub facts = %s", factLine(v.Facts))
	}
	it.Stream = nil
	it.Stages[5].On = false
	if v := BriefStub(it); v.StepsDone != 0 || v.StepsTotal != 5 {
		t.Fatalf("a job with no run counts its switched-on steps: %+v", v)
	}
}

func bulletBlock(heading string, lines ...string) BriefBlock {
	b := BriefBlock{Kind: BlockBullets, Heading: heading}
	for _, l := range lines {
		b.Bullets = append(b.Bullets, BriefClaim{Text: l, Evidence: []BriefEvidence{{Step: "review", Try: 1, Kind: "findings"}}})
	}
	return b
}

// EACH VERSION SAYS WHAT CHANGED from the one before: the verdict, each fact
// whose value moved, each block that is new or different. Revisions count
// up, only the last twenty are kept, and keeping a version ends a write in
// progress or failed.
func TestKeepBriefNumbersVersionsAndSaysWhatChanged(t *testing.T) {
	it := briefJob()
	it.BriefWriting = &BriefWriting{State: BriefGenerating}
	first := it.KeepBrief(BriefVersion{Verdict: "Fixed.", Facts: BriefFacts(it), Blocks: []BriefBlock{bulletBlock("What happened", "cause")}, MadeAt: briefNow})
	if first.Rev != 1 || it.BriefWriting != nil {
		t.Fatalf("first = %+v writing %+v", first, it.BriefWriting)
	}
	if want := []string{"verdict", "test", "review", "proof", "rules", "bullets/what happened"}; !slices.Equal(first.ChangedKeys, want) {
		t.Fatalf("first changed = %v", first.ChangedKeys)
	}
	it.Stream.Phases[3].Result.Findings = 0
	second := it.KeepBrief(BriefVersion{Verdict: "Fixed.", Facts: BriefFacts(it), Blocks: []BriefBlock{bulletBlock("What happened", "cause"), bulletBlock("Left", "docs")}})
	if want := []string{"review", "bullets/left"}; second.Rev != 2 || !slices.Equal(second.ChangedKeys, want) {
		t.Fatalf("second = %d %v", second.Rev, second.ChangedKeys)
	}
	for range 25 {
		it.KeepBrief(BriefVersion{Verdict: "Fixed."})
	}
	if len(it.Briefs) != BriefKeep || it.Briefs[0].Rev != 8 || it.Briefs[BriefKeep-1].Rev != 27 {
		t.Fatalf("kept %d, revs %d..%d", len(it.Briefs), it.Briefs[0].Rev, it.Briefs[len(it.Briefs)-1].Rev)
	}
}

// CHANGED SINCE SEEN is every key that changed in a version after the one
// the person saw and that the latest version still has; with no seen rev it
// is the latest version's own change, and seen at or past the latest is none.
func TestBriefChangedSince(t *testing.T) {
	versions := []BriefVersion{
		{Rev: 3, ChangedKeys: []string{"verdict", "test"}, Facts: []BriefFact{{Key: "test"}}},
		{Rev: 4, ChangedKeys: []string{"bullets/left"}, Blocks: []BriefBlock{{Kind: BlockBullets, Heading: "Left"}}},
		{Rev: 5, ChangedKeys: []string{"review"}, Facts: []BriefFact{{Key: "test"}, {Key: "review"}}, Blocks: []BriefBlock{{Kind: BlockBullets, Heading: "Old"}}},
	}
	cases := []struct {
		since int
		want  []string
	}{
		{0, []string{"review"}},
		{4, []string{"review"}},
		{3, []string{"review"}},
		{2, []string{"verdict", "test", "review"}},
		{1, []string{"verdict", "test", "review", "bullets/old"}},
		{5, []string{}},
		{9, []string{}},
	}
	for _, c := range cases {
		if got := BriefChangedSince(versions, c.since); !slices.Equal(got, c.want) {
			t.Errorf("since %d = %v, want %v", c.since, got, c.want)
		}
	}
	if got := BriefChangedSince(nil, 0); got == nil || len(got) != 0 {
		t.Errorf("no versions = %#v", got)
	}
}

// THE STATUS a reader shows: a stub before any version, a write on its way
// or failed over whatever is there, ready otherwise.
func TestBriefStatus(t *testing.T) {
	kept := []BriefVersion{{Rev: 1}}
	cases := []struct {
		versions []BriefVersion
		writing  *BriefWriting
		want     string
	}{
		{nil, nil, BriefStatusStub},
		{kept, nil, BriefStatusReady},
		{kept, &BriefWriting{State: BriefGenerating}, BriefStatusGenerating},
		{nil, &BriefWriting{State: BriefGenerating}, BriefStatusGenerating},
		{kept, &BriefWriting{State: BriefFailed, Failure: "model failed"}, BriefStatusFailed},
	}
	for i, c := range cases {
		if got := BriefStatusOf(Item{Briefs: c.versions, BriefWriting: c.writing}); got != c.want {
			t.Errorf("%d: status %s, want %s", i, got, c.want)
		}
	}
}

func ev() []BriefEvidence { return []BriefEvidence{{Step: "proof", Try: 1, Kind: "claims"}} }

// THE PALETTE'S LIMITS are the schema's: a block over its limit, of a kind
// the palette lacks, or with a claim that points at nothing is at fault.
func TestBriefBlockFaults(t *testing.T) {
	long := strings.Repeat("word ", 15)
	nodes := func(n int) []BriefNode {
		out := make([]BriefNode, n)
		for i := range out {
			out[i] = BriefNode{Text: fmt.Sprint("n", i)}
		}
		return out
	}
	spot := BriefSpot{File: "a.go", Symbol: "Send", Lines: 18, Why: "the retry", Evidence: ev()}
	cases := []struct {
		name     string
		b        BriefBlock
		needsYou bool
		fault    bool
	}{
		{"bullets ok", bulletBlock("What happened", "**Cause** a stale lease", "fix"), false, false},
		{"five bullets", bulletBlock("h", "a", "b", "c", "d", "e"), false, true},
		{"a long bullet", bulletBlock("h", long), false, true},
		{"no heading", bulletBlock("", "a"), false, true},
		{"a claim without evidence", BriefBlock{Kind: BlockBullets, Heading: "h", Bullets: []BriefClaim{{Text: "a"}}}, false, true},
		{"unknown kind", BriefBlock{Kind: "chart", Heading: "h", Evidence: ev()}, false, true},
		{"flow ok", BriefBlock{Kind: BlockFlow, Heading: "h", Evidence: ev(), Flow: []BriefFlowRow{{Label: "Before", Nodes: nodes(6)}, {Label: "After", Nodes: []BriefNode{{Text: "a", Mark: true}, {Text: "b"}}}}}, false, false},
		{"seven flow nodes", BriefBlock{Kind: BlockFlow, Heading: "h", Evidence: ev(), Flow: []BriefFlowRow{{Label: "Before", Nodes: nodes(7)}}}, false, true},
		{"two marks in a row", BriefBlock{Kind: BlockFlow, Heading: "h", Evidence: ev(), Flow: []BriefFlowRow{{Label: "Before", Nodes: []BriefNode{{Text: "a", Mark: true}, {Text: "b", Mark: true}}}}}, false, true},
		{"diagram ok", BriefBlock{Kind: BlockDiagram, Heading: "h", Evidence: ev(), Mermaid: "graph LR; n0-->n1", Nodes: []string{"n0", "n1"}}, false, false},
		{"nine diagram nodes", BriefBlock{Kind: BlockDiagram, Heading: "h", Evidence: ev(), Mermaid: "graph LR", Nodes: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}}, false, true},
		{"a diagram node the mermaid lacks", BriefBlock{Kind: BlockDiagram, Heading: "h", Evidence: ev(), Mermaid: "graph LR; a-->b", Nodes: []string{"a", "c"}}, false, true},
		{"table ok", BriefBlock{Kind: BlockTable, Heading: "h", Evidence: ev(), Table: &BriefTable{Columns: []string{"", "Before", "After"}, Rows: [][]string{{"a", "1", "2"}}}}, false, false},
		{"four columns", BriefBlock{Kind: BlockTable, Heading: "h", Evidence: ev(), Table: &BriefTable{Columns: []string{"a", "b", "c", "d"}, Rows: [][]string{{"a", "1", "2", "3"}}}}, false, true},
		{"six rows", BriefBlock{Kind: BlockTable, Heading: "h", Evidence: ev(), Table: &BriefTable{Columns: []string{"a", "b"}, Rows: [][]string{{"1", "2"}, {"1", "2"}, {"1", "2"}, {"1", "2"}, {"1", "2"}, {"1", "2"}}}}, false, true},
		{"a ragged row", BriefBlock{Kind: BlockTable, Heading: "h", Evidence: ev(), Table: &BriefTable{Columns: []string{"a", "b"}, Rows: [][]string{{"1"}}}}, false, true},
		{"spots ok", BriefBlock{Kind: BlockWhereToLook, Heading: "Where to look", Spots: []BriefSpot{spot}, Rest: "tests 142"}, false, false},
		{"four spots", BriefBlock{Kind: BlockWhereToLook, Heading: "h", Spots: []BriefSpot{spot, spot, spot, spot}, Rest: "x"}, false, true},
		{"spots without the rest", BriefBlock{Kind: BlockWhereToLook, Heading: "h", Spots: []BriefSpot{spot}}, false, true},
		{"proof ok", BriefBlock{Kind: BlockProof, Heading: "Proof", Evidence: ev(), Proof: &BriefProof{Command: "go test", Shown: 2, Total: 3}}, false, false},
		{"proof shows more than it has", BriefBlock{Kind: BlockProof, Heading: "Proof", Evidence: ev(), Proof: &BriefProof{Shown: 4, Total: 3}}, false, true},
		{"questions ok", BriefBlock{Kind: BlockOpenQuestion, Heading: "Open question", Questions: []BriefQuestion{{Text: "load?", Evidence: ev(), Action: &BriefAction{Label: "Ask Proof to measure it", Step: "proof"}}}}, false, false},
		{"three questions", BriefBlock{Kind: BlockOpenQuestion, Heading: "h", Questions: []BriefQuestion{{Text: "a", Evidence: ev()}, {Text: "b", Evidence: ev()}, {Text: "c", Evidence: ev()}}}, false, true},
		{"your call with a needs-you", BriefBlock{Kind: BlockYourCall, Heading: "Your call", Evidence: ev(), Options: []string{"Ship", "Hold"}}, true, false},
		{"your call with nothing waiting", BriefBlock{Kind: BlockYourCall, Heading: "Your call", Evidence: ev(), Options: []string{"Ship", "Hold"}}, false, true},
		{"your call with four options", BriefBlock{Kind: BlockYourCall, Heading: "Your call", Evidence: ev(), Options: []string{"a", "b", "c", "d"}}, true, true},
		{"screenshots ok", BriefBlock{Kind: BlockScreenshots, Heading: "h", Shots: []BriefShot{{Label: "Before", Ref: "s1", Evidence: ev()}, {Label: "After", Ref: "s2", Evidence: ev()}}}, false, false},
		{"three screenshots", BriefBlock{Kind: BlockScreenshots, Heading: "h", Shots: []BriefShot{{Label: "a", Ref: "1", Evidence: ev()}, {Label: "b", Ref: "2", Evidence: ev()}, {Label: "c", Ref: "3", Evidence: ev()}}}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.b.Fault(c.needsYou); (got != "") != c.fault {
				t.Fatalf("fault = %q, want fault %v", got, c.fault)
			}
		})
	}
}

// A VERDICT is one sentence of at most fourteen words.
func TestBriefVerdictFault(t *testing.T) {
	for _, c := range []struct {
		v     string
		fault bool
	}{
		{"Fixed. Retries no longer double-send after a worker restart.", false},
		{"", true},
		{strings.Repeat("word ", 15), true},
	} {
		if got := BriefVerdictFault(c.v); (got != "") != c.fault {
			t.Errorf("%q: fault %q", c.v, got)
		}
	}
}
