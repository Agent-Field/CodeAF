package factory

import (
	"encoding/json"
	"strings"
	"testing"
)

const firstReadBody = "When a worker restarts mid-send, the retry loop sends the message again although the first send landed. Customers saw 7 duplicate webhooks last week."

func firstReadJob() Item {
	return Item{ID: 7, Title: "Retries double-send after a worker restart", Body: firstReadBody}
}

func firstReadWorked() Item {
	it := firstReadJob()
	it.Stream = &Stream{Phases: []Phase{{Name: "plan", State: PhaseDone, Round: 1, Result: &StageResult{Done: true, Output: "plan"}}}}
	return it
}

func firstReadDraft(t *testing.T, verdict, quote string) []byte {
	t.Helper()
	ev := []BriefEvidence{{Kind: "issue", Ref: quote}}
	raw, err := json.Marshal(BriefDraft{Verdict: verdict, VerdictEvidence: ev, Blocks: []BriefBlock{
		{Kind: BlockBullets, Heading: "Risky part", Bullets: []BriefClaim{{Text: "Delivery state must survive a crash", Evidence: ev}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAThinIssueSpendsNothingBeforeWork(t *testing.T) {
	short := Item{Title: "it doesn't work", Body: "It broke again."}
	unclear := firstReadJob()
	unclear.Triage.Rubric = &Rubric{Clarity: Factor{Score: FactorMost}}
	clear := Item{Title: short.Title, Body: short.Body}
	clear.Triage.Rubric = &Rubric{Clarity: Factor{Score: 1}}
	worked := firstReadWorked()
	worked.Body = "short"
	for _, c := range []struct {
		name string
		it   Item
		want string
	}{
		{"few words, no read", short, BriefSkipThin},
		{"the read scored it unclear", unclear, BriefSkipThin},
		{"the read says it is clear enough", clear, ""},
		{"a full issue", firstReadJob(), ""},
		{"work has reported: no longer before work", worked, ""},
	} {
		if got := BriefBeginSkip(c.it, BriefTriggerRetry, "h", briefNow); got != c.want {
			t.Errorf("%s: skip %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAThinIssueSaysSoPlainlyAndAStubNeverRepeatsTheTitle(t *testing.T) {
	thin := Item{Title: "it doesn't work", Body: "It broke again."}
	if v := BriefStub(thin).Verdict; v != BriefThinVerdict {
		t.Errorf("thin stub verdict %q", v)
	}
	if v := BriefStub(firstReadJob()).Verdict; v != BriefNotReadVerdict {
		t.Errorf("unread stub verdict %q", v)
	}
	if v := BriefStub(firstReadWorked()).Verdict; v != BriefNotWrittenVerdict {
		t.Errorf("a stub after work still repeats the title, got %q", v)
	}
}

func TestBeforeWorkTheWriterReadsOnlyTheIssue(t *testing.T) {
	it := firstReadJob()
	it.Question = "SECRET-QUESTION"
	system, input := BriefPrompt(it, BriefInputOf(it))
	if system != BriefFirstReadSystem() {
		t.Fatal("before work the writer gets the first read's prompt")
	}
	if !strings.Contains(input, "mid-send") || strings.Contains(input, "SECRET-QUESTION") || strings.Contains(input, "steps") {
		t.Fatalf("the first read's input is not the issue alone: %s", input)
	}
	for _, want := range []string{"risky", "done", "unclear", `"issue"`} {
		if !strings.Contains(strings.ToLower(system), want) {
			t.Errorf("the prompt does not say %q", want)
		}
	}
	worked := firstReadWorked()
	if system, _ := BriefPrompt(worked, BriefInputOf(worked)); system != BriefSystem() {
		t.Fatal("after work the writer keeps its prompt")
	}
}

func TestBeforeWorkAClaimRestsOnWordsTheIssueHas(t *testing.T) {
	it := firstReadJob()
	v, drops, fault := CheckBriefDraft(it, firstReadDraft(t, "Make a resend after a crash harmless", "the first send landed"))
	if fault != "" || len(drops) != 0 || len(v.Blocks) != 1 {
		t.Fatalf("a true quote is kept: fault %q drops %+v blocks %d", fault, drops, len(v.Blocks))
	}
	if _, _, fault := CheckBriefDraft(it, firstReadDraft(t, "Make a resend after a crash harmless", "the database lost a row")); fault == "" {
		t.Fatal("a quote the issue does not have passed")
	}
	if _, _, fault := CheckBriefDraft(it, firstReadDraft(t, "Fix 7 duplicates per week", "the first send landed")); fault == "" {
		t.Fatal("a number its quote does not show passed")
	}
	if _, _, fault := CheckBriefDraft(it, firstReadDraft(t, "Fix 7 duplicates per week", "7 duplicate webhooks")); fault != "" {
		t.Fatalf("a number its quote shows was refused: %s", fault)
	}
	if _, _, fault := CheckBriefDraft(firstReadWorked(), firstReadDraft(t, "Make a resend harmless", "the first send landed")); fault == "" {
		t.Fatal("after work the issue is not evidence of an outcome")
	}
}

func TestAFirstReadThatRepeatsTheIssueIsNotKept(t *testing.T) {
	copied := "the retry loop sends the message again although the first send landed"
	if _, _, fault := CheckBriefDraft(firstReadJob(), firstReadDraft(t, copied, "the first send landed")); !strings.Contains(fault, "repeats") {
		t.Fatalf("a copied verdict passed: %q", fault)
	}
	diluted := firstReadJob()
	raw, _ := json.Marshal(BriefDraft{Verdict: "Retries double-send after a worker restart", VerdictEvidence: []BriefEvidence{{Kind: "issue", Ref: "the first send landed"}}, Blocks: []BriefBlock{
		{Kind: BlockBullets, Heading: "Risky part", Bullets: []BriefClaim{
			{Text: "Crash state must survive somewhere durable and also be checked before any resend happens today", Evidence: []BriefEvidence{{Kind: "issue", Ref: "the first send landed"}}},
			{Text: "Webhook consumers probably cannot tolerate repeats even when spaced apart over many long hours", Evidence: []BriefEvidence{{Kind: "issue", Ref: "the first send landed"}}},
		}},
	}})
	if _, _, fault := CheckBriefDraft(diluted, raw); !strings.Contains(fault, "repeats") {
		t.Fatalf("a copied title passed under own words: %q", fault)
	}
	own := BriefVersion{Verdict: "Make a resend after a crash harmless"}
	if got := BriefIssueOverlap(own, firstReadJob()); got != 0 {
		t.Fatalf("own words overlap %.2f", got)
	}
	if got := BriefIssueOverlap(BriefVersion{Verdict: copied}, firstReadJob()); got != 1 {
		t.Fatalf("copied words overlap %.2f", got)
	}
}

func TestAFirstReadDoesNotHoldBackTheFirstStepsBrief(t *testing.T) {
	it := firstReadWorked()
	it.Briefs = []BriefVersion{{Rev: 1, CostUSD: 0.002, InputHash: "old"}}
	if got := BriefBeginSkip(it, BriefTriggerStep, "new", briefNow); got != "" {
		t.Fatalf("skip %q: the read before work counted against the first step's write", got)
	}
}

func TestAnEndedJobIsNotBeforeWork(t *testing.T) {
	it := firstReadJob()
	if !briefBeforeWork(it) {
		t.Fatal("a job with no work is before work")
	}
	it.DoneHow = "stopped"
	if briefBeforeWork(it) {
		t.Fatal("a stopped job has an outcome to report")
	}
}

func TestAnIssuesHiddenNoteIsNotItsWords(t *testing.T) {
	it := Item{Body: "It broke.\n\n<!-- one two three four five six seven eight nine ten eleven twelve thirteen -->"}
	if !briefIssueThin(it) {
		t.Fatal("a hidden note counted as the issue's words")
	}
}

func TestBeforeWorkOnlyTheIssueMovesTheRead(t *testing.T) {
	it := firstReadJob()
	was := BriefInputHash(BriefInputOf(it))
	it.Stages = []Stage{{Name: "plan", Kind: StageChat, On: true}}
	it.Model = "another/model"
	if BriefInputHash(BriefInputOf(it)) != was {
		t.Fatal("a step or model edit moved a read that only read the issue")
	}
	it.Body += " More words."
	if BriefInputHash(BriefInputOf(it)) == was {
		t.Fatal("an edited issue did not move the read")
	}
	if worked := firstReadWorked(); BriefInputHash(BriefInputOf(worked)) == was {
		t.Fatal("work did not move the read")
	}
}

func TestAnOpenQuestionHasNoMarkup(t *testing.T) {
	ev := []BriefEvidence{{Kind: "issue", Ref: "the first send landed"}}
	raw, _ := json.Marshal(BriefDraft{Verdict: "Make a resend harmless", VerdictEvidence: ev, Blocks: []BriefBlock{
		{Kind: BlockOpenQuestion, Heading: "Unclear", Questions: []BriefQuestion{{Text: "Is an **error** preferred?", Evidence: ev}}},
	}})
	v, _, fault := CheckBriefDraft(firstReadJob(), raw)
	if fault != "" || len(v.Blocks) != 1 || v.Blocks[0].Questions[0].Text != "Is an error preferred?" {
		t.Fatalf("fault %q, %+v", fault, v.Blocks)
	}
}
