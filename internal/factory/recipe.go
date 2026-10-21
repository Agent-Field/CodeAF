package factory

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// DefaultRecipe is the issue pipeline a repository starts from. Pull requests
// are tracked as outcomes of issue jobs, not as independent work items.
// Every call answers fresh slices so callers can edit their own copy.
//
// THIS IS THE ONE PLACE THE DEFAULT STAGES ARE SPELLED. The local seam, a job
// as it arrives, and a repository with no pipeline file
// (internal/factory/pipeline's Defaults, built from this) all start from it, so
// a job a person starts and one a start rule starts run the same steps, asks
// and rounds. Until 2026-10-20 the pipeline package kept a second default of
// its own, with other asks, no rounds and other steps for a pull request and a
// red main, and a rule-started job was quietly re-stamped with it.
//
// WRITE WORKS IN THE CHECKOUT. Its ask said `do it in a worktree` until the
// 2026-10-08 run on factory-demo, where the stage obeyed it, was refused
// `git worktree` by the session's own guard (a stage may not move its copy
// onto work it did not do), and reported its edit as landed in "this stage's
// own worktree copy", which did not exist. An ask the stage is refused the
// means to follow is a sentence that makes the report untrue.
//
// AN APPROVE STEP SITS BETWEEN PLAN AND WRITE (approve.go): the person reads
// the plan before any code is written. Publishing the resulting PR has a
// separate approval after proof.
//
// SECURITY IS SHIPPED BUT IN NO LIST ([ShippedStage]): an item that asks for a
// security pass gains the stage after review, and a pipeline file names it to
// run it everywhere.
func DefaultRecipe() Recipe {
	r := Recipe{DefaultBudget: DefaultJobBudget, ByKind: map[Kind][]Stage{}}
	for kind, names := range defaultLists {
		stages := make([]Stage, 0, len(names))
		for _, name := range names {
			st, _ := ShippedStage(name)
			stages = append(stages, st)
		}
		if kind == KindIssue {
			r.Stages = stages
		} else {
			r.ByKind[kind] = stages
		}
	}
	return r
}

// defaultLists is which shipped stages each kind runs, in order. A kind not
// named here runs the issue's.
var defaultLists = map[Kind][]string{
	KindIssue: {"plan", ApproveName, "write", "test", "review", "proof"},
}

// shippedStages is every stage codeaf ships, in the order `codeaf factory
// steps` prints them. ONE NAME IS ONE STAGE: every kind that runs review runs
// this review, the same way one name is one step in a pipeline file, so the
// two can be one definition.
//
// ROUNDS LEAVE ROOM TO FIX WHAT A ROUND FOUND. A stage that does the work gets
// two, and one that is judged on what it leaves (until green, clean or proven)
// gets three. Until 2026-10-20 most stages had one, so a review that honestly
// found one problem, or a proof with one claim not yet shown, stopped the job
// at a question instead of trying again. A pipeline file still sets its own.
//
// PROOF IS FIXED. The person may skip an optional internal approval without
// approving publication; a repository may explicitly fix its approval stages.
//
// NO SHIPPED STAGE CARRIES A FANOUT. The word reaches the brief and nothing
// else, and a pipeline file that set only a shipped step's `max:` would drop
// it without a word, since a file's step carries its own runtime words.
var shippedStages = []Stage{
	{Name: "plan", Kind: StageChat, Ask: "read the issue and the code it names; write a plan and the list of files it will touch", Until: UntilDone, Max: 2, On: true},
	{Name: ApproveName, Kind: StageGate, On: true},
	{Name: "write", Kind: StageChat, Ask: "make the change in the checkout", Until: UntilDone, Max: 2, On: true},
	{Name: "test", Kind: StageChat, Ask: "run the tests the change implies; fix what fails", Until: UntilGreen, Max: 3, On: true},
	{Name: "review", Kind: StageChat, Ask: "review the diff as a careful colleague; fix what you find", Until: UntilClean, Max: 3, On: true},
	{Name: "security", Kind: StageChat, Ask: "review for injection, authz, secrets handling", Until: UntilClean, Max: 3, On: true},
	{Name: "proof", Kind: StageChat, Ask: "show each claim in its own medium", Until: UntilProven, Max: 3, On: true, Fixed: true},
	{Name: "read", Kind: StageChat, Ask: "read the change at its head and the thread; say what it does and what it risks", Until: UntilDone, Max: 2, On: true},
	{Name: "checks", Kind: StageChat, Ask: "run what the claims imply", Until: UntilDone, Max: 2, On: true},
	{Name: "bisect", Kind: StageChat, Ask: "find the change the red runs started at", Until: UntilDone, Max: 2, On: true},
	{Name: "fix", Kind: StageChat, Ask: "make the smallest change that turns the red runs green", Until: UntilDone, Max: 2, On: true},
}

// ShippedStages is every stage codeaf ships, in order, each a fresh copy.
func ShippedStages() []Stage { return CopyStages(shippedStages) }

// ShippedStage is the shipped stage under that exact name, a fresh copy.
func ShippedStage(name string) (Stage, bool) {
	if i := StageIndex(shippedStages, name); i >= 0 {
		return CopyStages(shippedStages[i : i+1])[0], true
	}
	return Stage{}, false
}

// CopyStages is a stage list a caller may change without reaching the one it
// was copied from, the Proof lists included.
func CopyStages(in []Stage) []Stage {
	if in == nil {
		return nil
	}
	out := make([]Stage, len(in))
	copy(out, in)
	for i := range out {
		out[i].Proof = append([]string(nil), in[i].Proof...)
	}
	return out
}

// StageIndex is the first stage with that name, or -1.
func StageIndex(stages []Stage, name string) int {
	for i, s := range stages {
		if s.Name == name {
			return i
		}
	}
	return -1
}

// stageMatches is every stage an edit's NAME LOOKUP reads as that name: the
// name trimmed and compared without case, both sides. It is the lookup of
// [Edit] alone. STORED IDENTITY STAYS BYTE-EXACT ([StageIndex]): the runner binds
// a phase to the stage's stored bytes, so nothing here renames a stage, and a
// legacy `Tidy` is answered as `Tidy` however it was asked for. More than one
// match is ambiguous, and an edit that selects it is refused whole.
func stageMatches(stages []Stage, name string) []int {
	key := strings.ToLower(strings.TrimSpace(name))
	var out []int
	for i, s := range stages {
		if strings.ToLower(strings.TrimSpace(s.Name)) == key {
			out = append(out, i)
		}
	}
	return out
}

// stagePlacement reads the placement clause a stage sentence may start with
// and answers it normalised along with the words left after it. The clauses
// are `after <stage>`, `before <stage>`, `first,` and `last,`; first and last
// need their comma, because "first run the tests" is a sentence and not a
// place. No clause answers an empty when and the words whole.
var stagePlacement = regexp.MustCompile(`(?i)^\s*(?:(after|before)\s+(?:the\s+)?([a-z][a-z-]*)\b\s*[,:;]?|(first|last)\s*[,:;])\s*`)

func stagePlacementOf(words string) (when, rest string) {
	m := stagePlacement.FindStringSubmatch(words)
	if m == nil {
		return "", strings.TrimSpace(words)
	}
	rest = strings.TrimSpace(words[len(m[0]):])
	if m[3] != "" {
		return strings.ToLower(m[3]), rest
	}
	return strings.ToLower(m[1]) + " " + strings.ToLower(m[2]), rest
}

// StageWhen is the place a stage sentence asks for: "after review",
// "before proof", "first" or "last", or "" when it names none, which
// [PlaceStage] reads as just before proof.
func StageWhen(words string) string {
	when, _ := stagePlacementOf(words)
	return when
}

// stageStop are the words a rail name leaves out: articles, pronouns, and the
// fillers a sentence starts with. "run" is dropped only from the front.
var stageStop = map[string]bool{"it": true, "the": true, "a": true, "an": true, "this": true, "that": true, "them": true}
var stageLead = map[string]bool{"then": true, "please": true, "run": true}

// stageName is the first meaningful word of an ask, as a stage's one word
// ([StageWord]): `make it neater` is `make` and `please read the history` is
// `read`. Only letters and digits are kept, a word is cut at twelve cells, and
// a word too short to be a name gives way to the next; an ask with no word
// that can be a name is `stage`.
func stageName(ask string) string {
	var raw []string
	for _, w := range strings.Fields(strings.ToLower(ask)) {
		w = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, w)
		if w != "" {
			raw = append(raw, w)
		}
	}
	var kept []string
	lead := true
	for _, w := range raw {
		if lead && stageLead[w] {
			continue
		}
		lead = false
		if !stageStop[w] {
			kept = append(kept, w)
		}
	}
	for _, w := range append(kept, raw...) {
		if r := []rune(w); len(r) > stageWordMost {
			w = string(r[:stageWordMost])
		}
		if name, err := StageWord(w); err == nil {
			return name
		}
	}
	return "stage"
}

// stageNamed is the `name: ask` a typed stage sentence may start with: one or
// two words before a colon that a space or the end follows. Two words are
// still read as a name, so that [StageWord] refuses them in its own sentence
// rather than the colon quietly becoming part of the ask.
var stageNamed = regexp.MustCompile(`^([\pL\pN]+(?:\s+[\pL\pN]+)?)\s*:(?:\s+|$)`)

// StageSentence reads a typed stage sentence: an optional place
// (`after review,`, `before proof,`, `first,`, `last,`), an optional one-word
// name with a colon (`arch:`), and the ask. `after review, arch: read it for
// the architecture` is a stage named arch placed after review. A sentence with
// no name is named by its ask's first meaningful word ([stageName]), so
// `after review, make it neater` is `make`. It answers the stage, the place
// ([StageWhen]'s words), and an error when the name is not one word or there
// is no ask.
func StageSentence(words string) (Stage, string, error) {
	when, rest := stagePlacementOf(words)
	name := ""
	if m := stageNamed.FindStringSubmatch(rest); m != nil {
		w, err := StageWord(m[1])
		if err != nil {
			return Stage{}, when, err
		}
		name, rest = w, rest[len(m[0]):]
	}
	ask := strings.Trim(strings.TrimSpace(rest), " ,:;.")
	if ask == "" {
		return Stage{}, when, errors.New("say what the stage should do")
	}
	if askCells(ask) > AskMost {
		return Stage{}, when, ErrAskTooLong
	}
	if name == "" {
		name = stageName(ask)
	}
	return Stage{Name: name, Kind: StageChat, Ask: ask, Until: "done", On: true}, when, nil
}

// ParseStage reads "after review, make it neater" into a stage: a
// conversation whose ask is the words with the placement clause (and a
// `name:`) taken off, and whose name is one word ([StageSentence]). Its place
// comes from [StageWhen] and [PlaceStage], because A STAGE CARRIES NO TIME
// WORD: its place in the list is its time. An empty Ask is a sentence that
// said only when, or one [StageSentence] refused (a two-word name); a caller
// that wants the refusal's own words calls [StageSentence].
func ParseStage(words string) Stage {
	st, _, err := StageSentence(words)
	if err != nil {
		return Stage{}
	}
	return st
}

// AddStageSentence is [StageSentence] and [PlaceStage] together, within the
// bounds every stage list keeps: a one-word name the list does not have yet,
// and at most [StageMost] stages.
func AddStageSentence(stages []Stage, words string) ([]Stage, error) {
	st, when, err := StageSentence(words)
	if err != nil {
		return stages, err
	}
	if len(stages) >= StageMost {
		return stages, ErrNineStages
	}
	if StageIndex(stages, st.Name) >= 0 {
		return stages, fmt.Errorf("there is already a stage named %s; give this one its own word, `word: ask`", st.Name)
	}
	return PlaceStage(stages, st, when), nil
}

// PlaceStage inserts st at the place when names. `after X` is directly after
// the stage named X and `before X` directly before it; `first` is the front
// and `last` is the end, but ahead of proof when proof is the last stage. No
// place, or a stage the recipe does not have (a pull request has no plan),
// takes it just before proof, and a recipe without proof takes it at the end.
func PlaceStage(stages []Stage, st Stage, when string) []Stage {
	at := len(stages)
	if p := StageIndex(stages, "proof"); p >= 0 {
		at = p
	}
	switch {
	case when == "first":
		at = 0
	case when == "last":
		at = len(stages)
		if n := len(stages); n > 0 && stages[n-1].Name == "proof" {
			at = n - 1
		}
	default:
		if x, ok := strings.CutPrefix(when, "after "); ok {
			if i := StageIndex(stages, x); i >= 0 {
				at = i + 1
			}
		} else if x, ok := strings.CutPrefix(when, "before "); ok {
			if i := StageIndex(stages, x); i >= 0 {
				at = i
			}
		}
	}
	out := make([]Stage, 0, len(stages)+1)
	out = append(out, stages[:at]...)
	out = append(out, st)
	return append(out, stages[at:]...)
}

// AddStageWords is [ParseStage] and [PlaceStage] together: what the AddStage
// door does to an item's own copy of the recipe.
func AddStageWords(stages []Stage, words string) []Stage {
	st := ParseStage(words)
	if st.Ask == "" {
		return stages
	}
	return PlaceStage(stages, st, StageWhen(words))
}

// The until words are a stage's exit contract: the condition a runner checks
// after each round before it lets the item go on to the next stage. THESE FOUR
// ARE THE WHOLE VOCABULARY. The recipe file accepts no other word after
// `until`, so every word a person can write maps to a check in [Met] from the
// day it is written, and a word nobody taught the runner can never be banked.
//
//   - done: the stage reported itself done through its result tool.
//   - clean: the stage reported done with zero findings.
//   - proven: every claim on the item has evidence, and there is at least one.
//   - green: the check stage's command ran to an exit, and the exit was 0.
const (
	UntilDone   = "done"
	UntilClean  = "clean"
	UntilProven = "proven"
	UntilGreen  = "green"
)

// untilWords is the until vocabulary in the order the manual names it.
var untilWords = []string{UntilDone, UntilClean, UntilProven, UntilGreen}

// StageResult is what one round of a stage hands back to the runner, which
// fills it and asks [Met] whether the stage may stop. There is no runner yet;
// this is the contract it will be written against.
//
// Done is the stage reporting back at all: a chat stage calling its result
// tool, a check stage's command running to an exit. A ZERO RESULT IS A STAGE
// THAT SAID NOTHING, and it meets no condition, because an exit code of 0 and
// a findings count of 0 on a round that never reported would otherwise read
// exactly like green and clean.
type StageResult struct {
	PublishSummary []string        `json:",omitempty"`
	Repair         *CIRepairResult `json:",omitempty"`
	// Tests is absent when the command did not report individual outcomes.
	Tests    *TestCounts `json:",omitempty"`
	Done     bool
	Findings int
	Claims   []Claim
	Output   string
	Exit     int
	// Notes are sentences the stage leaves for the stages after it; the
	// runner folds them into the next job's Notes.
	Notes []string
	// Edit is a plan stage's proposal to change the item's stages, applied by
	// the runner through Adapt under the recipe's adapt word; nil otherwise.
	Edit *PlanEdit
	// Spent is what the round cost in dollars, 0 when nothing was priced.
	Spent float64
	// Chat names the stage's conversation when the stage ran as one, so the
	// phase strip can open it.
	Chat string
	// Model is the model the stage's conversation was on when the round
	// ended, "" for a stage that was not a conversation.
	Model string `json:",omitempty"`
	// Reports is how many times a conversation step called `stage_result` in
	// the round, 0 for one that never did and for a step that is not a
	// conversation. THE LAST CALL IS THE ONE THIS RESULT HOLDS: a step that
	// reported early and kept working said so again (run/chatstage.go).
	Reports int `json:",omitempty"`
	// Inferred marks a report the factory made for a step that stopped without
	// one, from its brief, its last message and its change. It is judged like
	// any other, and [RoundSummary] says whose words it is.
	Inferred bool `json:",omitempty"`
}

// Met says whether a round's result meets the stage's until. An empty until
// is done, which is what a stage without one waits for. An until word outside
// the vocabulary is never met, so the runner stops at its rounds and asks
// rather than passing a stage on a word it cannot check.
func Met(s Stage, r StageResult) bool {
	met, _ := judge(s, r)
	return met
}

// ReportFault is what is wrong with a report as its step's own rule reads it,
// said as the fix the step can make, or "" for a report the judge may take as
// it stands. A conversation step's report is checked with it the moment it is
// made (run/chatstage.go's stageDoor), so the step puts it right in the same
// turn instead of spending a whole round to learn it.
//
// ONLY A REPORT THAT CONTRADICTS ITSELF IS AT FAULT. An honest shortfall —
// done: false, or a claim marked ok: false — is a report like any other, and
// the judge fails it into another round. At fault is a report the judge would
// fail for a reason the report itself gives away: done with findings it says
// are still open, a claim marked ok with nothing showing it, a proof done with
// nothing claimed.
//
// THE FAULTS ARE THE JUDGE'S OWN RULES, read by the one [judge] that answers
// [Met], so the check at the door cannot refuse a report the judge would
// pass, and there is no second copy of the rules to drift from the first.
func ReportFault(s Stage, r StageResult) string {
	if IsCIRepairStage(s) {
		if r.Repair == nil || (r.Repair.Decision != "fixed" && r.Repair.Decision != "no_change" && r.Repair.Decision != "needs_you") || strings.TrimSpace(r.Repair.Reason) == "" {
			return "CI repair needs repair: {decision: fixed or needs_you, reason: evidence and what is needed}"
		}
	}
	if IsCIRepairStage(s) && r.Repair != nil && (r.Repair.Decision == "needs_you" || r.Repair.Decision == "no_change") {
		return ""
	}
	_, fault := judge(s, r)
	return fault
}

// The sentences [ReportFault] answers. Each names the fix, not the rule: the
// rule is already in the step's brief, and the sentence is read back to the
// model as `not reported: <sentence>` on every refused call.
const (
	faultOpenFindings = "you reported %s still open: fix %s and report again, or report done: false"
	faultNoEvidence   = "claim %d has no evidence: add what shows it, or set ok: false"
	faultNoClaims     = "you reported done with no claims: add each claim with what shows it, or report done: false"
)

// judge reads a result against its stage's until once, for both readers:
// whether the result meets it ([Met]), and what in it contradicts itself
// under that rule ([ReportFault]), "" for nothing.
func judge(s Stage, r StageResult) (met bool, fault string) {
	switch strings.TrimSpace(s.Until) {
	case "", UntilDone:
		return r.Done, ""
	case UntilClean:
		if r.Done && r.Findings > 0 {
			them := "them"
			if r.Findings == 1 {
				them = "it"
			}
			return false, fmt.Sprintf(faultOpenFindings, plural(r.Findings, "finding", "findings"), them)
		}
		return r.Done && r.Findings == 0, ""
	case UntilGreen:
		return r.Done && r.Exit == 0, ""
	case UntilProven:
		// A CLAIM MARKED OK WITH NOTHING SHOWING IT is at fault whatever done
		// says: it is the one claim that reads as shown and is not, and the
		// rounds and steps after this one read it as written.
		met = r.Done && r.Exit == 0 && len(r.Claims) > 0
		for i, c := range r.Claims {
			if c.OK && strings.TrimSpace(c.Evidence) == "" {
				return false, fmt.Sprintf(faultNoEvidence, i+1)
			}
			if !c.OK {
				met = false
			}
		}
		if r.Done && len(r.Claims) == 0 {
			return false, faultNoClaims
		}
		return met, ""
	}
	return false, ""
}
