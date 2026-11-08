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
