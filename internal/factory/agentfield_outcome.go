package factory

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ── HOW AN AGENTFIELD STEP'S END READS ──────────────────────────────────────
//
// A step run on AgentField ends with the control plane's words, never with a
// reasoner's id: the person reads the step by its label ("Build on
// AgentField"), and `swe-planner.build_issue` is a name only the pipeline file
// needs. These are the factory's sentences for that end, the same for every
// pipeline and every step — NOTHING HERE KNOWS ANY ONE PIPELINE'S NAMES.

// AgentFieldStepWords is an agentfield step as a person reads it: its label,
// else its name with a capital ("build" reads "Build").
func AgentFieldStepWords(st Stage) string {
	if label := strings.TrimSpace(st.Label); label != "" {
		return label
	}
	name := strings.TrimSpace(st.Name)
	if name == "" {
		return "This step"
	}
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[size:]
}

// AgentFieldDidNotSucceed is the sentence for an agentfield step whose run
// ended but did not do its work: the step's words, and the run's own outcome
// in plain words when it left one ([AgentFieldOutcomeWords]).
func AgentFieldDidNotSucceed(st Stage, outcome string) string {
	words := AgentFieldStepWords(st) + " did not succeed"
	if outcome = strings.TrimSpace(outcome); outcome != "" {
		return words + ": " + outcome
	}
	return words
}

// agentFieldMachineWords are words a reasoner may write that the factory never
// shows a person (the machinery-vocabulary law), each with the plain word it
// reads as. Matched whole, any case; the plain word takes the first letter's
// case.
var agentFieldMachineWords = []struct {
	re    *regexp.Regexp
	plain string
}{
	{regexp.MustCompile(`(?i)\bverification\b`), "its checks"},
	{regexp.MustCompile(`(?i)\bverified\b`), "checked"},
	{regexp.MustCompile(`(?i)\bverdicts?\b`), "result"},
	{regexp.MustCompile(`(?i)\brefuted\b`), "disproved"},
	{regexp.MustCompile(`(?i)\bauditors?\b`), "reviewer"},
}

// agentFieldCode is a snake_case code ("approval_rejected", "github_pr"):
// read as the words it joins.
var agentFieldCode = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`)

// AgentFieldOutcomeWords is a control plane's outcome sentence as a person
// reads it: one line, a closing parenthesis that carries the raw report left
// off (the report stays on the step, whole), snake_case codes read as words,
// and machinery words read as plain ones. "" stays "".
//
// "No pull request: the build did not pass verification (Partial: 1/1 …)"
// reads "No pull request: the build did not pass its checks".
func AgentFieldOutcomeWords(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	if strings.HasSuffix(s, ")") {
		if i := strings.Index(s, " ("); i > 0 {
			s = strings.TrimSpace(s[:i])
		}
	}
	s = agentFieldCode.ReplaceAllStringFunc(s, func(code string) string { return strings.ReplaceAll(code, "_", " ") })
	for _, w := range agentFieldMachineWords {
		s = w.re.ReplaceAllStringFunc(s, func(found string) string {
			if r, _ := utf8.DecodeRuneInString(found); unicode.IsUpper(r) {
				first, size := utf8.DecodeRuneInString(w.plain)
				return string(unicode.ToUpper(first)) + w.plain[size:]
			}
			return w.plain
		})
	}
	return strings.TrimRight(s, " .;:,")
}

// FirstSentence is s up to the end of its first sentence (a full stop, ! or
// ? followed by a space or the end), one line, whitespace folded.
func FirstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	for i, r := range s {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if i+1 == len(s) || s[i+1] == ' ' {
			return s[:i+1]
		}
	}
	return s
}

// agentFieldGrade is a leading grade a reasoner writes before its reason
// ("OVERALL: FAIL. ", "FAIL: ", "Result: FAILED - "): a label for a machine,
// never words for a person.
var agentFieldGrade = regexp.MustCompile(`^(?:[A-Z][A-Za-z ]{0,20}:\s*)?(?:PASS(?:ED)?|FAIL(?:ED)?|ERROR|PARTIAL)\b\s*[.:\-–—]?\s*`)

// agentFieldNoPullRequest is an outcome that leads with the forge's own
// consequence: "No pull request: <reason>".
var agentFieldNoPullRequest = regexp.MustCompile(`(?i)^no pull request\s*[:\-–—]\s*`)

// agentFieldLeadReason is the reason a graded report leads with, in plain
// words, "" when s does not lead with a grade: the grade left off, and the
// sentence cut before the first command it quotes or the first sentence end,
// so "OVERALL: FAIL. The project does not build or its tests fail: `go build
// ./...` exited 1. …" reads "The project does not build or its tests fail".
// What the command printed stays in the report, never in the reason.
func agentFieldLeadReason(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	loc := agentFieldGrade.FindStringIndex(s)
	if loc == nil || loc[1] >= len(s) {
		return ""
	}
	s = s[loc[1]:]
	if i := strings.Index(s, "`"); i >= 0 {
		s = s[:i]
	}
	return AgentFieldOutcomeWords(FirstSentence(s))
}

// AgentFieldOutcome is how a run that did not succeed ended, in one plain
// line, from the run's own last note and its result's summary. THE SHAPE OF
// THE WORDS DECIDES, NEVER A PIPELINE'S OR A STEP'S NAME:
//
//   - a report that leads with a grade gives its reason, the grade and any
//     command it quotes left off (agentFieldLeadReason);
//   - a note that leads with "No pull request:" reads "<reason>, so no pull
//     request was opened", its reason the report's own when the note carries
//     a graded one in parentheses, else the note's;
//   - otherwise the note in plain words, else the summary's first sentence.
//
// The raw report is never here: it stays the step's output, whole.
func AgentFieldOutcome(note, summary string) string {
	note = strings.Join(strings.Fields(note), " ")
	inner := ""
	if strings.HasSuffix(note, ")") {
		if i := strings.Index(note, " ("); i > 0 {
			inner = strings.TrimSuffix(note[i+2:], ")")
		}
	}
	specific := firstNonEmptyWords(agentFieldLeadReason(inner), agentFieldLeadReason(summary))
	if loc := agentFieldNoPullRequest.FindStringIndex(note); loc != nil {
		reason := firstNonEmptyWords(specific, AgentFieldOutcomeWords(note[loc[1]:]))
		if reason == "" {
			return "No pull request was opened"
		}
		return capitalFirst(reason) + ", so no pull request was opened"
	}
	if specific != "" {
		return capitalFirst(specific)
	}
	if words := AgentFieldOutcomeWords(note); words != "" {
		return words
	}
	return AgentFieldOutcomeWords(FirstSentence(summary))
}

func firstNonEmptyWords(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

func capitalFirst(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

// AgentFieldNothingToChange is how a run that made no changes ends, in the
// words a person reads first: the work it was asked for is already there, or
// it found nothing to do, and so there is no pull request to open. IT IS NOT
// SHIPPED: nothing went out. The job is done, and its read says this as its
// state's detail rather than a bare "Done".
const AgentFieldNothingToChange = "Nothing to change: the run made no changes, so no pull request was opened."

// AgentFieldNothingChanged says one of the job's steps ended saying its run
// made no change: its output is [AgentFieldNothingToChange], whole.
func AgentFieldNothingChanged(it Item) bool {
	if it.Stream == nil {
		return false
	}
	for _, ph := range it.Stream.Phases {
		if ph.Result != nil && strings.TrimSpace(ph.Result.Output) == AgentFieldNothingToChange {
			return true
		}
	}
	return false
}

// agentFieldNoChange is a run's own words for a change that came out empty,
// in git's and the forge's vocabulary rather than any one pipeline's: "No
// commits between main and feature/…", "has no commits ahead of the base
// branch", "the run changed nothing", "nothing to commit".
var agentFieldNoChange = regexp.MustCompile(`(?i)\bno commits (?:between|ahead)\b|\b(?:changed|changes) nothing\b|\bmade no changes\b|\bnothing to commit\b|\bno changes (?:to commit|were made)\b`)

// AgentFieldNoChange says any of a run's words (its notes, its summary) say
// the run made no change to the repository. THE SHAPE OF THE WORDS DECIDES,
// never a pipeline's or a step's name.
func AgentFieldNoChange(said ...string) bool {
	for _, s := range said {
		if agentFieldNoChange.MatchString(s) {
			return true
		}
	}
	return false
}

// agentFieldBad is an outcome that says something did not happen or did
// not pass: "failed", "did not", "could not", "error".
var agentFieldBad = regexp.MustCompile(`(?i)\b(?:fail(?:s|ed|ure)?|did not|didn't|could not|couldn't|cannot|can't|errors?|refused|rejected|not pass(?:ed)?)\b`)

// agentFieldPullRequestWords is an outcome about the pull request itself.
var agentFieldPullRequestWords = regexp.MustCompile(`(?i)\b(?:pull request|PR)\b`)

// agentFieldNoteRank is how much one note says about how a run ended: a
// pull request that was not opened, or a change that came out empty, says
// the most; a failure of any kind next; a note that only reports progress
// ("Repo finalized: …", "Phase 3b: …") says nothing about the outcome.
func agentFieldNoteRank(s string) int {
	switch {
	case agentFieldNoPullRequest.MatchString(strings.TrimSpace(s)), AgentFieldNoChange(s),
		agentFieldPullRequestWords.MatchString(s) && agentFieldBad.MatchString(s):
		return 2
	case agentFieldBad.MatchString(s):
		return 1
	}
	return 0
}

// AgentFieldTellingNote is the note, of a run's notes oldest first, that says
// how the run ended: the newest of those that say the most
// ([agentFieldNoteRank]), else the newest. A RUN'S LAST NOTE IS NOT ALWAYS
// ITS OUTCOME: a reasoner may tidy up after it decided ("Repo finalized: …"
// 8 ms after "No pull request: the build did not pass …"), and the tidying
// never reads as the reason a build failed. "" when there is none.
func AgentFieldTellingNote(notes []string) string {
	best, rank := "", -1
	for _, n := range notes {
		if strings.TrimSpace(n) == "" {
			continue
		}
		if r := agentFieldNoteRank(n); r >= rank {
			best, rank = n, r
		}
	}
	return best
}
