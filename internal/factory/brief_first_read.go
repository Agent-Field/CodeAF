package factory

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// ── THE READ BEFORE WORK ───────────────────────────────────────────────────
//
// A job that has not reported a step has nothing to report, and the issue has
// its own tab. So its first brief says what the issue leaves to the reader:
// what is really asked, the risky part, how anyone would know it is done, and
// what is unclear. Its only evidence is the issue's own words. An issue too
// thin to read says so and spends nothing.

const (
	// BriefSkipThin is why a write before work does not begin.
	BriefSkipThin = "thin"
	// BriefThinVerdict is what a thin issue's stub says.
	BriefThinVerdict = "This issue says too little to read. Add what you want, or ask the manager."
	// BriefNotReadVerdict is what the stub says until the read is written.
	BriefNotReadVerdict = "Not read yet."
	// BriefNotWrittenVerdict is what the stub says once work has reported.
	BriefNotWrittenVerdict = "No brief written yet."
	// briefThinWords is the fewest words of an issue no triage has read that
	// are worth a model call.
	briefThinWords = 12
	// briefIssueEvidence is the kind of evidence that is the issue's own
	// words, quoted in the evidence's Ref.
	briefIssueEvidence = "issue"
	// briefOverlapGram is how many words in a row count as repeating.
	briefOverlapGram = 3
	// BriefOverlapMost is the most of a first read's word runs that may also
	// be in the issue. MEASURED, not guessed: six real issues on
	// deepseek/deepseek-v4.1-flash read 0.016 to 0.169 (median 0.046), and a verdict
	// copied from an issue reads 1. Over this a read says the issue back.
	BriefOverlapMost = 0.25
	// briefClaimOverlapMost is the most of one claim's word runs that may be
	// in the issue, so own words elsewhere cannot hide a copied sentence. The six
	// real reads above peak at 0.6 on one claim.
	briefClaimOverlapMost = 0.75
)

// briefHTMLComment is a part of an issue's body no reader sees, such as a template's note.
var briefHTMLComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// briefBeforeWork says the job has not ended and no step of it has reported.
func briefBeforeWork(it Item) bool {
	if BriefEndOf(it) != "" {
		return false
	}
	if it.Stream == nil {
		return true
	}
	for _, p := range it.Stream.Phases {
		if p.State == PhaseDone || p.Result != nil {
			return false
		}
	}
	return true
}

// briefIssueThin says the issue is not worth a read: a triage read scored it
// so, or no triage has read it and it has too few words.
func briefIssueThin(it Item) bool {
	if t := it.Triage; t.Rubric != nil || t.Readiness > 0 {
		return t.Thin()
	}
	return BriefWords(briefHTMLComment.ReplaceAllString(it.Body, "")) < briefThinWords
}

// briefStubVerdict is the line of a job with no written brief. The issue's
// title is on the page already, so the line says where the brief is.
func briefStubVerdict(it Item) string {
	switch {
	case !briefBeforeWork(it):
		return BriefNotWrittenVerdict
	case briefIssueThin(it):
		return BriefThinVerdict
	}
	return BriefNotReadVerdict
}

// BriefFirstReadSystem is the writer's prompt before work. Its limits are the
// palette's own constants, like [BriefSystem].
func BriefFirstReadSystem() string {
	return fmt.Sprintf(`You write the first read of one factory job, before any work on it, for the person who owns it.
You are given the issue as JSON and any later person_requests. That person can read the issue on its own tab, so NEVER copy its title or its sentences. Use your own words and say what the issue leaves for the reader to work out.
Answer ONE JSON object and nothing else:
{"verdict": string, "verdict_evidence": [evidence], "blocks": [block]}
Every "evidence" field is a JSON ARRAY of {"kind": "issue", "ref": string}. ref is words copied exactly from the issue, at most %d, that the claim rests on. The issue is the only evidence there is.
The verdict is what is really being asked, in at most %d words.
Write these blocks in this order, leaving out one only when the issue gives nothing for it:
- {"kind":"bullets","heading":"Risky part","bullets":[{"text","evidence"}]}: what could go wrong or be missed.
- {"kind":"bullets","heading":"Done when","bullets":[{"text","evidence"}]}: how a person would know it is done.
- {"kind":"open_question","heading":"Unclear","questions":[{"text","evidence"}]}: at most %d things the issue does not settle.
At most %d bullets a block, at most %d words each. Plain text only, no markdown.
Say nothing about the code, its files or its causes unless the issue says it: no one has read the code yet. EVERY NUMBER YOU WRITE MUST BE IN THAT CLAIM'S ref; a claim that breaks this is thrown away. Unsure is a line under Unclear, never a guess.`,
		BriefWordsMost, BriefWordsMost, BriefQuestionsMost, BriefBulletsMost, BriefWordsMost)
}

// briefFirstReadInput is the input of a read before work: the issue and what a
// person has said since. No state, steps or facts: there is no work to read.
func briefFirstReadInput(in BriefInput) string {
	raw, _ := json.Marshal(struct {
		Issue          any                  `json:"issue"`
		PersonRequests []BriefPersonRequest `json:"person_requests,omitempty"`
	}{in.Issue, in.PersonRequests})
	return string(raw)
}

// BriefPrompt is what the writer is given for this job now.
func BriefPrompt(it Item, in BriefInput) (system, input string) {
	if briefBeforeWork(it) {
		return BriefFirstReadSystem(), briefFirstReadInput(in)
	}
	return BriefSystem(), BriefInputText(in)
}

// issueNumbers is the numbers of a quote the issue really has. Only a read
// before work may rest on the issue; after work a claim rests on what happened.
func issueNumbers(e briefEvidence, ref BriefEvidence) ([]int, bool) {
	quote := strings.Join(briefPlainWords(ref.Ref), " ")
	issue := " " + strings.Join(briefPlainWords(e.it.Title+" "+e.it.Body), " ") + " "
	if quote == "" || !briefBeforeWork(e.it) || !strings.Contains(issue, " "+quote+" ") {
		return nil, false
	}
	return textNumbers(ref.Ref), true
}

// briefPlainWords is text as lower-case words, without its punctuation.
func briefPlainWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// briefWrittenParts is what a person reads in a version: its verdict and its
// claims. Evidence is left out: a quote is meant to be the issue's.
func briefWrittenParts(v BriefVersion) []string {
	parts := []string{v.Verdict}
	for _, b := range v.Blocks {
		for _, c := range b.Bullets {
			parts = append(parts, c.Text)
		}
		for _, q := range b.Questions {
			parts = append(parts, q.Text)
		}
	}
	return parts
}

// briefIssueWords is the issue as one run of plain words, spaces at each end.
func briefIssueWords(it Item) string {
	return " " + strings.Join(briefPlainWords(it.Title+" . "+it.Body), " ") + " "
}

// briefRuns counts a text's three-word runs, and those also in the issue.
func briefRuns(text, issue string) (runs, same int) {
	words := briefPlainWords(text)
	for i := 0; i+briefOverlapGram <= len(words); i++ {
		runs++
		if strings.Contains(issue, " "+strings.Join(words[i:i+briefOverlapGram], " ")+" ") {
			same++
		}
	}
	return runs, same
}

// briefPlainQuestions takes markup out of a read's questions, which a reader
// shows as plain text whatever the prompt asked for.
func briefPlainQuestions(blocks []BriefBlock) {
	for _, b := range blocks {
		for i := range b.Questions {
			b.Questions[i].Text = strings.ReplaceAll(b.Questions[i].Text, "**", "")
		}
	}
}

// BriefIssueOverlap is the share of a version's three-word runs that are also
// in the job's issue: 0 is all its own words, 1 is a copy. Runs do not cross
// from one claim to the next.
func BriefIssueOverlap(v BriefVersion, it Item) float64 {
	issue := briefIssueWords(it)
	runs, same := 0, 0
	for _, part := range briefWrittenParts(v) {
		r, s := briefRuns(part, issue)
		runs, same = runs+r, same+s
	}
	if runs == 0 {
		return 0
	}
	return float64(same) / float64(runs)
}

// briefRepeatsFault is what is wrong with a read before work that says the
// issue back, "" for nothing: as a whole, in one claim, or as the bare title.
func briefRepeatsFault(v BriefVersion, it Item) string {
	if !briefBeforeWork(it) {
		return ""
	}
	if overlap := BriefIssueOverlap(v, it); overlap > BriefOverlapMost {
		return fmt.Sprintf("the read repeats the issue: %.0f%% of it", overlap*100)
	}
	issue, title := briefIssueWords(it), strings.Join(briefPlainWords(it.Title), " ")
	for _, part := range briefWrittenParts(v) {
		r, s := briefRuns(part, issue)
		if strings.Join(briefPlainWords(part), " ") == title || (r > 0 && float64(s)/float64(r) > briefClaimOverlapMost) {
			return fmt.Sprintf("the read repeats the issue in %q", part)
		}
	}
	return ""
}
