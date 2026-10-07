package mock

// Words become chips. This is the whole bridge between the unstructured half
// (what a person says) and the structured half (the item's stages, cap and
// gate). It is deliberately a small, honest parser: it lifts what it
// recognizes and leaves the rest as the title, or as words folded into the
// work. A real engine would let a cheap model do the lifting; the mock shows
// the interaction, not the language work.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE FOUR CHIP WORDS ARE factory.Lift's (internal/factory/words.go), so the
// surface lifting chips off a sentence and this parser read one spelling. What
// is spelled here is what only the mock lifts: a security pass and the
// constraints a proof must show held.
var (
	reDont     = regexp.MustCompile(`(?i)\b(don'?t touch [a-z0-9/_. -]+?|keep [a-z0-9 ]+? compat(?:ible)?|no new deps?(?:endencies)?|stay in [a-z0-9/_.-]+)\b`)
	reSecurity = regexp.MustCompile(`(?i)\b(?:with |and |a |do )?security(?: review| pass| check)?\b`)
)

// chips is what lift found in a sentence. A zero field was not said.
type chips struct {
	cap         float64
	gate        factory.Gate
	rounds      int
	security    bool
	effort      string
	redo        bool
	constraints []string
	rest        string
}

// lift reads chips out of words and returns what is left, tidied, as rest.
func lift(words string) chips {
	f := factory.Lift(words)
	c := chips{cap: f.Cap, gate: f.Gate, rounds: f.Rounds, effort: f.Effort, redo: f.Redo}
	rest := f.Rest
	if reSecurity.MatchString(rest) {
		c.security = true
		rest = reSecurity.ReplaceAllString(rest, "")
	}
	for _, m := range reDont.FindAllString(rest, -1) {
		c.constraints = append(c.constraints, strings.TrimSpace(m))
		rest = strings.Replace(rest, m, "", 1)
	}
	c.rest = factory.TidyWords(rest)
	return c
}

// apply writes the chips onto an item and says what changed, one phrase per
// chip. The effort lands on the stage at effortAt, when there is one.
func (c chips) apply(it *factory.Item, effortAt int) []string {
	var said []string
	if c.cap > 0 && c.cap != it.Cap {
		it.Cap = c.cap
		said = append(said, "cap "+usd(c.cap))
	}
	if c.gate != "" && c.gate != it.Gate {
		it.Gate = c.gate
		said = append(said, "gate "+string(c.gate))
	}
	if i := stageIndex(it.Stages, "review"); c.rounds > 0 && i >= 0 && it.Stages[i].Max != c.rounds {
		it.Stages[i].Max = c.rounds
		said = append(said, fmt.Sprintf("review ×%d", c.rounds))
	}
	if i := stageIndex(it.Stages, "security"); c.security && i >= 0 && !it.Stages[i].On {
		it.Stages[i].On = true
		said = append(said, "security on")
	}
	if c.effort != "" && effortAt >= 0 && effortAt < len(it.Stages) && it.Stages[effortAt].Effort != c.effort {
		it.Stages[effortAt].Effort = c.effort
		said = append(said, it.Stages[effortAt].Name+" effort "+c.effort)
	}
	if i := stageIndex(it.Stages, "proof"); len(c.constraints) > 0 && i >= 0 {
		// A constraint is something the proof must show held.
		it.Stages[i].Proof = append(it.Stages[i].Proof, c.constraints...)
		said = append(said, "proof shows "+strings.Join(c.constraints, ", "))
	}
	return said
}

// usd spells a cap the way the floor does: whole dollars when whole.
func usd(f float64) string {
	if f == float64(int(f)) {
		return fmt.Sprintf("$%.0f", f)
	}
	return fmt.Sprintf("$%.2f", f)
}

// timeWords are where a sentence may place a stage, in recipe order. With no
// time word a stage runs after review, which is where most of them belong.
var timeWords = []string{"after plan", "after write", "after test", "after review", "before proof"}

// whenOf is the time word the sentence starts with, or "after review".
func whenOf(words string) string {
	low := strings.ToLower(strings.TrimSpace(words))
	for _, w := range timeWords {
		if strings.HasPrefix(low, w) {
			return w
		}
	}
	return "after review"
}

// parseStage reads "after review, make it neater" into a stage. Its name is
// the first two words of the ask; its place comes from whenOf and placeStage,
// because A STAGE CARRIES NO TIME WORD: its place in the list is its time.
func parseStage(words string) factory.Stage {
	ask := strings.TrimSpace(words)
	low := strings.ToLower(ask)
	for _, w := range timeWords {
		if strings.HasPrefix(low, w) {
			ask = ask[len(w):]
			break
		}
	}
	ask = strings.Trim(strings.TrimSpace(ask), " ,:;.")
	f := strings.Fields(strings.ToLower(ask))
	if len(f) > 2 {
		f = f[:2]
	}
	return factory.Stage{Name: strings.Join(f, " "), Ask: ask, Until: "done", On: true}
}

// anchors are the stages a time word names. A stage placed "after X" goes
// after X and after whatever already sits between X and the next anchor, so
// stages banked at the same moment keep the order they were said in.
var anchors = map[string]bool{"plan": true, "write": true, "test": true, "review": true, "proof": true}

// placeStage inserts st at the moment when names. A recipe without the named
// stage (a pull request has no plan) takes it just before proof, and a recipe
// without proof takes it at the end.
func placeStage(stages []factory.Stage, st factory.Stage, when string) []factory.Stage {
	at := len(stages)
	if p := stageIndex(stages, "proof"); p >= 0 {
		at = p
	}
	if after, ok := strings.CutPrefix(when, "after "); ok {
		if i := stageIndex(stages, after); i >= 0 {
			at = i + 1
			for at < len(stages) && !anchors[stages[at].Name] {
				at++
			}
		}
	}
	out := make([]factory.Stage, 0, len(stages)+1)
	out = append(out, stages[:at]...)
	out = append(out, st)
	return append(out, stages[at:]...)
}

// addStage is parseStage and placeStage together: the AddStage door.
func addStage(stages []factory.Stage, words string) []factory.Stage {
	return placeStage(stages, parseStage(words), whenOf(words))
}

// Match turns a filter sentence into a predicate over items. Mocked: a
// handful of words a cheap model would obviously map, plus plain substring
// match on everything else.
func Match(q string) func(factory.Item) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return func(factory.Item) bool { return true }
	}
	terms := strings.Fields(q)
	return func(it factory.Item) bool {
		hay := strings.ToLower(it.Title + " " + it.Repo + " " + it.Triage.Area + " " + it.Triage.Type + " " + it.Author + " " + string(it.Kind) + " " + strings.Join(it.Labels, " "))
		security := false
		if i := stageIndex(it.Stages, "security"); i >= 0 {
			security = it.Stages[i].On
		}
		for _, t := range terms {
			switch t {
			case "risky", "risk", "dangerous":
				if it.Triage.Risk != "high" && it.Triage.Size != "L" && !security {
					return false
				}
			case "cheap", "quick", "small", "easy":
				if it.Triage.Est > 2.5 {
					return false
				}
			case "big", "large", "expensive":
				if it.Triage.Size != "L" {
					return false
				}
			case "stranger", "strangers", "external", "outside":
				if it.Tier != factory.TierStranger {
					return false
				}
			case "mine", "own":
				if it.Author != "santosh" {
					return false
				}
			case "ready", "actionable":
				if it.Triage.Readiness < 60 {
					return false
				}
			case "unclear", "vague", "thin":
				if it.Triage.Readiness >= 60 {
					return false
				}
			case "money", "spend", "cost", "billing":
				if !strings.Contains(hay, "spend") && !strings.Contains(hay, "billing") && !strings.Contains(hay, "ledger") && !strings.Contains(hay, "invoice") && !strings.Contains(hay, "cap") {
					return false
				}
			case "ui", "visual", "screen":
				if !strings.Contains(hay, "tui") && !strings.Contains(hay, "ui") && !strings.Contains(hay, "pages") && !strings.Contains(hay, "render") && !strings.Contains(hay, "tree") {
					return false
				}
			case "prs", "pr", "review", "reviews":
				if it.Kind != factory.KindPR {
					return false
				}
			case "bugs", "bug":
				if it.Triage.Type != "bug" {
					return false
				}
			case "dup", "dupes", "duplicates":
				if it.Triage.DupOf == 0 {
					return false
				}
			case "chat":
				if it.Origin != factory.OriginChat {
					return false
				}
			default:
				if !strings.Contains(hay, t) {
					return false
				}
			}
		}
		return true
	}
}
