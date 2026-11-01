package factory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const briefPatch = `diff --git a/retry/send.go b/retry/send.go
index 1..2 100644
--- a/retry/send.go
+++ b/retry/send.go
@@ -10,4 +10,6 @@ func Send(ctx context.Context) error {
 	if lease.Stale() {
-		return resend()
+		return nil
+	}
+	if err := mark(); err != nil {
 	}
@@ -40,2 +42,3 @@ func mark() error {
+	db.Write()
 }
diff --git a/go.sum b/go.sum
--- a/go.sum
+++ b/go.sum
@@ -1 +1,2 @@
+x v1 h1:abc
`

// THE HUNK INDEX names each hunk's file, symbol, start and size, and marks
// the files nobody edits by hand.
func TestPatchHunks(t *testing.T) {
	got := PatchHunks(briefPatch)
	want := []PatchHunk{
		{File: "retry/send.go", Symbol: "func Send(ctx context.Context) error {", Start: 10, Added: 3, Removed: 1},
		{File: "retry/send.go", Symbol: "func mark() error {", Start: 42, Added: 1},
		{File: "go.sum", Start: 1, Added: 1, Generated: true},
	}
	if len(got) != len(want) {
		t.Fatalf("hunks = %+v", got)
	}
	for i := range want {
		g := got[i]
		g.Text = ""
		if g != want[i] {
			t.Errorf("hunk %d = %+v, want %+v", i, g, want[i])
		}
	}
	if !strings.HasPrefix(got[1].Text, "@@ -40,2 +42,3 @@") || !strings.Contains(got[1].Text, "db.Write()") {
		t.Fatalf("hunk text = %q", got[1].Text)
	}
}

func writerJob() Item {
	it := briefJob()
	it.Change = &Change{Patch: briefPatch, Stat: &ChangeStat{Files: 2, Added: 5, Removed: 1},
		Files: []ChangedFile{{Path: "retry/send.go", Added: 4, Removed: 1}, {Path: "go.sum", Added: 1}}}
	it.Stream.Phases[2].Rounds = []RoundLine{{Round: 1, Summary: "3 failed", Rejected: true}, {Round: 2, Summary: "green"}}
	return it
}

// THE WRITER READS THE JOB'S RECORDS, never the full diff: each try of each
// step, the latest with its structured result, the hunk index and the facts.
// Its hash does not move with the brief before, and does with a step.
func TestBriefInputOf(t *testing.T) {
	it := writerJob()
	in := BriefInputOf(it)
	if in.Issue.Title != it.Title || !in.NeedsYou || in.Plan != "the plan" || len(in.Hunks) != 3 || in.Change.Files != 2 || len(in.Facts) != 4 {
		t.Fatalf("input = %+v", in)
	}
	var tries []string
	for _, s := range in.Steps {
		if s.Step == "test" {
			tries = append(tries, s.Summary+"/"+strings.Repeat("L", map[bool]int{true: 1}[s.Latest]))
		}
	}
	if strings.Join(tries, ",") != "3 failed/,/L" {
		t.Fatalf("test tries = %v", tries)
	}
	if raw, _ := json.Marshal(in); strings.Contains(string(raw), "db.Write") {
		t.Fatal("the input carries the diff's text")
	}
	h := BriefInputHash(in)
	it.KeepBrief(BriefVersion{Verdict: "Fixed."})
	if BriefInputHash(BriefInputOf(it)) != h {
		t.Fatal("a kept version moved the hash")
	}
	it.Stream.Phases[3].Result.Findings = 0
	if BriefInputHash(BriefInputOf(it)) == h {
		t.Fatal("a step's result did not move the hash")
	}
}

// THE COST GUARD: the first version is free to write; then a version must
// have cost at most three cents and the brief at most 5% of the job's spend.
// An ask, the end and a retry are written anyway; the same inputs are not
// written twice; a write on its way holds the next one back until it is stale.
func TestBriefBeginSkip(t *testing.T) {
	now := briefNow
	at := func(spent float64, costs ...float64) Item {
		it := Item{Stream: &Stream{Spent: spent}}
		for _, c := range costs {
			it.Briefs = append(it.Briefs, BriefVersion{CostUSD: c, InputHash: "old"})
		}
		return it
	}
	cases := []struct {
		name    string
		it      Item
		trigger string
		hash    string
		want    string
	}{
		{"first version", at(0), BriefTriggerStep, "h", ""},
		{"within the guard", at(1, 0.01, 0.01), BriefTriggerStep, "h", ""},
		{"over 5% of the job", at(0.3, 0.01, 0.01), BriefTriggerStep, "h", BriefSkipCost},
		{"a dear version", at(100, 0.04), BriefTriggerStep, "h", BriefSkipCost},
		{"an ask is written anyway", at(0.3, 0.01, 0.01), BriefTriggerAsk, "h", ""},
		{"the end is written anyway", at(0.3, 0.04), BriefTriggerEnd, "h", ""},
		{"unchanged inputs", at(1, 0.01), BriefTriggerAsk, "old", BriefSkipUnchanged},
		{"a retry writes the same inputs", at(1, 0.01), BriefTriggerRetry, "old", ""},
		{"a write on its way", func() Item {
			it := at(1)
			it.BriefWriting = &BriefWriting{State: BriefGenerating, At: now.Add(-time.Minute)}
			return it
		}(), BriefTriggerRetry, "h", BriefSkipWriting},
		{"a lost write", func() Item {
			it := at(1)
			it.BriefWriting = &BriefWriting{State: BriefGenerating, At: now.Add(-3 * time.Minute)}
			return it
		}(), BriefTriggerStep, "h", ""},
		{"a failed write's inputs", func() Item {
			it := at(1)
			it.BriefWriting = &BriefWriting{State: BriefFailed, Hash: "h"}
			return it
		}(), BriefTriggerStep, "h", BriefSkipUnchanged},
	}
	for _, c := range cases {
		if got := BriefBeginSkip(c.it, c.trigger, c.hash, now); got != c.want {
			t.Errorf("%s: skip %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTextNumbers(t *testing.T) {
	got := textNumbers("tests 142, generated client 1,069; v2 a1.go 3.5 and (7)")
	if want := []int{142, 1069, 3, 7}; len(got) != len(want) || got[0] != 142 || got[1] != 1069 || got[2] != 3 || got[3] != 7 {
		t.Fatalf("numbers = %v", got)
	}
}

func draft(t *testing.T, d any) []byte {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func evs(step string, try int, kind string) []BriefEvidence {
	return []BriefEvidence{{Step: step, Try: try, Kind: kind}}
}

// THE DRAFT IS CHECKED AGAINST THE JOB: a claim whose evidence names a step,
// try or file the job does not have, or whose number its evidence does not
// show, is dropped; a block left empty, over the palette, or past the fourth
// goes too; and each drop is said.
func TestCheckBriefDraftDropsWhatTheEvidenceDoesNotShow(t *testing.T) {
	it := writerJob()
	d := BriefDraft{
		Verdict:         "Fixed. Retries no longer double-send; 42 tests pass.",
		VerdictEvidence: evs("test", 2, "tests"),
		Blocks: []BriefBlock{
			{Kind: BlockBullets, Heading: "What happened", Bullets: []BriefClaim{
				{Text: "**Cause** a stale lease resent", Evidence: evs("review", 1, "findings")},
				{Text: "Review found 2 findings", Evidence: evs("review", 1, "findings")},
				{Text: "Review found 3 findings", Evidence: evs("review", 1, "findings")},
				{Text: "first test try had 3 failed", Evidence: evs("test", 1, "summary")},
				{Text: "an older try", Evidence: evs("review", 4, "findings")},
			}},
			{Kind: BlockWhereToLook, Heading: "Where to look", Rest: "go.sum 1 line, generated", Spots: []BriefSpot{
				{File: "retry/send.go", Symbol: "Send", Lines: 4, Why: "the retry returns early", Evidence: []BriefEvidence{{Step: "write", Try: 1, Kind: "hunk", Ref: "retry/send.go:10"}}},
				{File: "nope.go", Lines: 2, Why: "x", Evidence: []BriefEvidence{{Kind: "file", Ref: "nope.go"}}},
			}},
			{Kind: BlockProof, Heading: "Proof", Evidence: evs("proof", 1, "claims"), Proof: &BriefProof{Command: "go test ./...", Shown: 3, Total: 3}},
			{Kind: BlockYourCall, Heading: "Your call", Evidence: evs("proof", 1, "claims"), Options: []string{"Ship it", "Hold"}},
			{Kind: BlockOpenQuestion, Heading: "Not checked", Questions: []BriefQuestion{{Text: "load under 2 workers?", Evidence: evs("proof", 1, "claims"), Action: &BriefAction{Label: "Ask Proof to measure it", Step: "proof"}}}},
			{Kind: BlockBullets, Heading: "Left", Bullets: []BriefClaim{{Text: "docs", Evidence: evs("plan", 1, "output")}}},
		},
	}
	v, drops, fault := CheckBriefDraft(it, draft(t, d))
	if fault != "" {
		t.Fatalf("verdict fault %q", fault)
	}
	var kinds []string
	for _, b := range v.Blocks {
		kinds = append(kinds, b.Key())
	}
	if strings.Join(kinds, ",") != "bullets/what happened,where_to_look/where to look,your_call/your call,open_question/not checked" {
		t.Fatalf("kept blocks = %v", kinds)
	}
	if n := len(v.Blocks[0].Bullets); n != 3 || len(v.Blocks[1].Spots) != 1 {
		t.Fatalf("kept %d bullets, %d spots", n, len(v.Blocks[1].Spots))
	}
	var reasons []string
	for _, dr := range drops {
		reasons = append(reasons, dr.Part+": "+dr.Reason)
	}
	got := strings.Join(reasons, "\n")
	for _, want := range []string{
		`"Review found 3 findings": 3 in`,
		`"an older try": evidence review try 4`,
		`"nope.go": evidence  try 0 file nope.go names nothing`,
		`block 3 (proof): the proof says 3 of 3, and proof showed 2 of 3`,
		`block 6 (bullets): more than 4 blocks`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("drops lack %q:\n%s", want, got)
		}
	}
	if len(drops) != 5 {
		t.Errorf("%d drops:\n%s", len(drops), got)
	}
	if factLine(v.Facts) != factLine(BriefFacts(it)) || v.StepsDone != 5 {
		t.Fatalf("facts are not the engine's: %s", factLine(v.Facts))
	}
}

// A VERDICT THAT FAILS fails the whole draft, and so does an answer that is
// not the schema's JSON.
func TestCheckBriefDraftVerdict(t *testing.T) {
	it := writerJob()
	for _, c := range []struct {
		raw   string
		fault string
	}{
		{`{"verdict":"Fixed; 41 tests pass.","verdict_evidence":[{"step":"test","try":2,"kind":"tests"}],"blocks":[]}`, "41"},
		{`{"verdict":"","blocks":[]}`, "empty"},
		{`not json`, "not a brief"},
		{`{"verdict":"Fixed.","verdict_evidence":"x","blocks":[]}`, "not a list"},
	} {
		if _, _, fault := CheckBriefDraft(it, []byte(c.raw)); !strings.Contains(fault, c.fault) {
			t.Errorf("%s: fault %q", c.raw, fault)
		}
	}
	// A field the draft does not use is ignored, a lone evidence object is a
	// list of one, and a block that is not a block is one drop.
	shape := `{"verdict":"Fixed.","facts":[{"key":"made up"}],"blocks":[` +
		`{"kind":"bullets","heading":"Left","bullets":[{"text":"docs","evidence":{"step":"plan","try":1,"kind":"output"}}]},` +
		`{"kind":"bullets","heading":7}]}`
	v, drops, fault := CheckBriefDraft(it, []byte(shape))
	if fault != "" || len(v.Blocks) != 1 || len(v.Blocks[0].Bullets) != 1 || len(drops) != 1 || !strings.Contains(drops[0].Reason, "not a block") || v.Facts[0].Key == "made up" {
		t.Fatalf("shape: %+v %+v %q", v, drops, fault)
	}
	it.State = StateRunning
	raw := `{"verdict":"Fixed.","verdict_evidence":[],"blocks":[{"kind":"your_call","heading":"Your call","options":["a","b"],"evidence":[{"step":"proof","try":1,"kind":"claims"}]}]}`
	if v, drops, fault := CheckBriefDraft(it, []byte(raw)); fault != "" || len(v.Blocks) != 0 || len(drops) != 1 {
		t.Fatalf("your call with nothing waiting: %+v %+v %q", v.Blocks, drops, fault)
	}
}

// A WRITE BEGINS AND ENDS ON THE JOB: begun, it says it is on its way after
// the last step done; ended with a passing draft, it keeps a version that
// names its model, cost, inputs and drops; ended otherwise, the versions stay
// and it failed. Its cost is the job's either way.
func TestBeginAndEndBrief(t *testing.T) {
	it := writerJob()
	spent := JobSpent(it)
	if skip := it.BeginBrief(BriefTriggerStep, "h1", briefNow); skip != "" || BriefStatusOf(it) != BriefStatusGenerating || it.BriefWriting.AfterStep != "proof" {
		t.Fatalf("begin: %q %+v", skip, it.BriefWriting)
	}
	if skip := it.BeginBrief(BriefTriggerStep, "h2", briefNow); skip != BriefSkipWriting {
		t.Fatalf("second begin = %q", skip)
	}
	good := `{"verdict":"Fixed.","verdict_evidence":[],"blocks":[{"kind":"bullets","heading":"Left","bullets":[{"text":"docs","evidence":[{"step":"plan","try":1,"kind":"output"}]},{"text":"9 more","evidence":[{"step":"plan","try":1,"kind":"output"}]}]}]}`
	v, drops, kept := it.EndBrief(BriefWritten{Hash: "h1", Model: "m", CostUSD: 0.002, Draft: []byte(good)}, briefNow)
	if !kept || v.Rev != 1 || v.Model != "m" || v.CostUSD != 0.002 || v.InputHash != "h1" || v.AfterStep != "proof" || v.Dropped != 1 || len(drops) != 1 || it.BriefWriting != nil {
		t.Fatalf("kept %v %+v drops %+v", kept, v, drops)
	}
	it.BeginBrief(BriefTriggerAsk, "h3", briefNow)
	if _, _, kept := it.EndBrief(BriefWritten{Hash: "h3", CostUSD: 0.001, Draft: []byte(`{"verdict":""}`)}, briefNow); kept || BriefStatusOf(it) != BriefStatusFailed ||
		!strings.Contains(it.BriefWriting.Failure, "did not pass") || len(it.Briefs) != 1 {
		t.Fatalf("a failing verdict: %+v", it.BriefWriting)
	}
	it.EndBrief(BriefWritten{Failure: "model_failed"}, briefNow)
	if it.BriefWriting.Failure != "model_failed" || BriefSpent(it) != 0.002 {
		t.Fatalf("failure: %+v", it.BriefWriting)
	}
	if got := JobSpent(it) - spent; got < 0.0029 || got > 0.0031 {
		t.Fatalf("the job was charged %v", got)
	}
}

// THE INPUT FITS ONE CALL, however large the job.
func TestBriefInputTextFits(t *testing.T) {
	it := writerJob()
	it.Body = strings.Repeat("body ", 20000)
	var patch strings.Builder
	for i := range 400 {
		patch.WriteString("diff --git a/f" + strings.Repeat("x", i%7) + ".go b/f.go\n@@ -1 +1 @@ func F() {\n+x\n")
	}
	it.Change.Patch = patch.String()
	for i := range it.Stream.Phases {
		if it.Stream.Phases[i].Result != nil {
			it.Stream.Phases[i].Result.Output = strings.Repeat("out ", 4000)
		}
	}
	if got := len(BriefInputText(BriefInputOf(it))); got > BriefInputMost {
		t.Fatalf("input is %d bytes", got)
	}
	if !strings.Contains(BriefSystem(), "at most 14 words") {
		t.Fatal("the prompt does not carry the palette's limits")
	}
}

// THE CHANGE'S STAT IS EVIDENCE of its own, for a claim about its size.
func TestBriefChangeEvidence(t *testing.T) {
	it := writerJob()
	e := briefEvidenceOf(it)
	ok := e.claimFault("2 files, 5 added, 1 removed", []BriefEvidence{{Kind: "change"}})
	bad := e.claimFault("3 files", []BriefEvidence{{Kind: "change"}})
	it.Change = nil
	none := briefEvidenceOf(it).claimFault("2 files", []BriefEvidence{{Kind: "change"}})
	if ok != "" || bad == "" || none == "" {
		t.Fatalf("ok %q bad %q none %q", ok, bad, none)
	}
}
