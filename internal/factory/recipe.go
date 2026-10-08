package factory

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// DefaultRecipe is the recipe a repo starts from before anybody banks one of
// its own: stages for an issue, and the fixed shapes for the two kinds an
// issue's stages do not describe. A pull request is read and checked; a red
// main is bisected and fixed. Every call answers fresh slices, so a caller may
// toggle its copy without reaching anybody else's.
//
// WRITE WORKS IN THE CHECKOUT. Its ask said `do it in a worktree` until the
// 2026-10-08 run on factory-demo, where the stage obeyed it, was refused
// `git worktree` by the session's own guard (a stage may not move its copy
// onto work it did not do), and reported its edit as landed in "this stage's
// own worktree copy", which did not exist. An ask the stage is refused the
// means to follow is a sentence that makes the report untrue.
//
// SECURITY IS BANKED OFF and switched on per item, so an item that asks for a
// security pass gains the stage without the recipe changing. THIS IS THE ONE
// PLACE THE DEFAULT STAGES ARE SPELLED: the local seam starts from it, so no
// floor can disagree with another about what a fresh repo runs.
func DefaultRecipe() Recipe {
	return Recipe{
		Stages: []Stage{
			{Name: "plan", Ask: "read the issue and say how", Until: "done", On: true},
			{Name: "write", Ask: "make the change in the checkout", Fanout: "per-file", Until: "done", On: true},
			{Name: "test", Ask: "run what the change implies", Until: "green", Max: 2, On: true},
			{Name: "review", Ask: "read it as a stranger would", Fanout: "per-finding", Until: "clean", Max: 1, On: true},
			{Name: "security", Ask: "secrets, injection and authz", Until: "clean", On: false},
			{Name: "proof", Ask: "show each claim in its own medium", Until: "proven", Gate: GateShip, On: true},
		},
		ByKind: map[Kind][]Stage{
			KindPR: {
				{Name: "read", Ask: "the diff and its claims", Until: "done", On: true},
				{Name: "checks", Ask: "run what the claims imply", Fanout: "per-claim", Until: "done", On: true},
				{Name: "review", Ask: "findings as a comment", Fanout: "per-finding", Until: "clean", Max: 1, On: true},
				{Name: "security", Ask: "secrets, injection and authz", Until: "clean", On: false},
				{Name: "proof", Ask: "the sheet", Until: "proven", Gate: GateShip, On: true},
			},
			KindCI: {
				{Name: "bisect", Ask: "the three red runs", Until: "done", On: true},
				{Name: "fix", Ask: "the smallest change that turns them green", Until: "done", On: true},
				{Name: "test", Ask: "the red test and its neighbours", Until: "green", Max: 2, On: true},
				{Name: "proof", Ask: "the sheet", Until: "proven", Gate: GateShip, On: true},
			},
		},
	}
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
}

// Met says whether a round's result meets the stage's until. An empty until
// is done, which is what a stage without one waits for. An until word outside
// the vocabulary is never met, so the runner stops at its rounds and asks
// rather than passing a stage on a word it cannot check.
func Met(s Stage, r StageResult) bool {
	if !r.Done {
		return false
	}
	switch strings.TrimSpace(s.Until) {
	case "", UntilDone:
		return true
	case UntilClean:
		return r.Findings == 0
	case UntilGreen:
		return r.Exit == 0
	case UntilProven:
		if len(r.Claims) == 0 {
			return false
		}
		for _, c := range r.Claims {
			if !c.OK || strings.TrimSpace(c.Evidence) == "" {
				return false
			}
		}
		return true
	}
	return false
}
