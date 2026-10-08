package factory

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Words become chips. THIS IS THE ONE PLACE THE CHIP WORDS ARE SPELLED: a
// person's sentence carries a cap (`$8`), a gate (`plan first`, `ship it`,
// `self-ship`), a round count (`two review rounds`) or an effort (`stronger`,
// `cheaper`), and whatever reads a sentence, the local seam's new item or the
// surface lifting chips off an item that cannot be steered, reads it with
// these regexes so the two can never disagree about what a word means.
//
// It is a small, honest parser. It lifts what it recognizes and leaves the rest
// as words; a real engine would let a cheap model do the lifting.

var (
	reCap    = regexp.MustCompile(`\$\s?(\d+(?:\.\d+)?)`)
	reRounds = regexp.MustCompile(`(?i)\b(\d+|one|two|three|four)\s+(?:review\s+)?rounds?\b`)
	reGate   = regexp.MustCompile(`(?i)\b(?:gate[:\s]+)?(plan first|plan gate|design first|ship it|self[- ]ship|no gate|auto)\b`)
	reEffort = regexp.MustCompile(`(?i)\b(redo\s+)?(?:it\s+)?(stronger|strong|harder|cheaper|cheap|lighter)\b`)
)

var wordNums = map[string]int{"one": 1, "two": 2, "three": 3, "four": 4}

// Chips is what [Lift] found in a sentence. A zero field was not said. Rest is
// what is left of the sentence with the chips taken out, NOT YET TIDIED, so a
// reader that lifts more words of its own (a security pass, say) can go on reading it before [TidyWords].
type Chips struct {
	Cap    float64
	Gate   Gate
	Rounds int
	Effort string
	// Redo says the effort word came as `redo it stronger`: the running stage
	// starts its round over at the new effort.
	Redo bool
	Rest string
}

// Lift reads the four chips out of words.
func Lift(words string) Chips {
	var c Chips
	rest := words
	if m := reCap.FindStringSubmatch(rest); m != nil {
		if f, err := strconv.ParseFloat(m[1], 64); err == nil {
			c.Cap = f
		}
		rest = strings.Replace(rest, m[0], "", 1)
	}
	if m := reRounds.FindStringSubmatch(rest); m != nil {
		if n, ok := wordNums[strings.ToLower(m[1])]; ok {
			c.Rounds = n
		} else if n, err := strconv.Atoi(m[1]); err == nil {
			c.Rounds = n
		}
		rest = strings.Replace(rest, m[0], "", 1)
	}
	if m := reGate.FindStringSubmatch(rest); m != nil {
		switch strings.ToLower(m[1]) {
		case "plan first", "plan gate", "design first":
			c.Gate = GatePlan
		case "ship it":
			c.Gate = GateShip
		default:
			c.Gate = GateNone
		}
		rest = strings.Replace(rest, m[0], "", 1)
	}
	if m := reEffort.FindStringSubmatch(rest); m != nil {
		c.Redo = m[1] != ""
		switch strings.ToLower(m[2]) {
		case "stronger", "strong", "harder":
			c.Effort = "strong"
		default:
			c.Effort = "cheap"
		}
		rest = strings.Replace(rest, m[0], "", 1)
	}
	c.Rest = rest
	return c
}

// TidyWords is what is left of a sentence once its chips are out: the joining
// words a chip leaves stranded are dropped, the spaces closed up, and the
// stray punctuation at either end trimmed.
func TidyWords(rest string) string {
	rest = strings.Join(strings.Fields(strings.NewReplacer(",", " ", " and ", " ", " with ", " ").Replace(rest)), " ")
	return strings.Trim(rest, " ,.;:")
}

// LiftChips is [Lift] in the shape a caller with one door per chip wants: a nil
// pointer is a chip that was not said, and rest is the tidied words that were
// not a chip.
func LiftChips(words string) (gate *Gate, cap *float64, rounds *int, effort string, rest string) {
	c := Lift(words)
	if c.Gate != "" {
		g := c.Gate
		gate = &g
	}
	if c.Cap > 0 {
		f := c.Cap
		cap = &f
	}
	if c.Rounds > 0 {
		n := c.Rounds
		rounds = &n
	}
	return gate, cap, rounds, c.Effort, TidyWords(c.Rest)
}

// reSecurity is a request for a security pass: `security`, `with security`,
// `a security review`. It is not one of the four chips because it is not a
// knob on the item; it switches a banked stage on.
var reSecurity = regexp.MustCompile(`(?i)\b(?:with |and |a |do )?security(?: review| pass| check)?\b`)

// LiftSecurity says whether rest asks for a security pass and answers rest
// with that request taken out, NOT YET TIDIED, the same way [Lift] leaves its
// own rest.
func LiftSecurity(rest string) (bool, string) {
	if !reSecurity.MatchString(rest) {
		return false, rest
	}
	return true, reSecurity.ReplaceAllString(rest, "")
}

// GuessType is the triage type a person's own words imply: a question when
// they end in a question mark, a bug when they say fix, crash, wrong, broken,
// flaky, loses or the like, a chore when they rename or clean up, a feature
// otherwise. It is the cheap guess an item carries until something reads it
// properly, and it never guesses a kind: a sentence typed on the floor is an
// issue, because a pull request or a red run arrives from a source and is
// never typed.
func GuessType(words string) string {
	low := strings.ToLower(strings.TrimSpace(words))
	if strings.HasSuffix(low, "?") {
		return "question"
	}
	fields := strings.FieldsFunc(low, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	has := func(stems ...string) bool {
		for _, f := range fields {
			for _, s := range stems {
				if f == s {
					return true
				}
			}
		}
		return false
	}
	switch {
	case strings.Contains(low, "bug") || strings.Contains(low, "fix") || strings.Contains(low, "double-count") ||
		has("crash", "crashes", "wrong", "broke", "broken", "flaky", "lose", "loses", "lost", "fail", "fails", "error", "panic", "panics", "regression", "incorrect", "leak", "leaks", "hang", "hangs"):
		return "bug"
	case has("add", "adds", "support", "export", "implement", "introduce", "allow"):
		return "feat"
	case has("rename", "cleanup", "clean", "refactor", "tidy", "bump", "deprecate"):
		return "chore"
	}
	return "feat"
}

// LabelType is the triage type a label names, or "" for a label that names
// none: bug; feat, feature and enhancement; chore, maintenance, refactor, docs
// and documentation; question.
func LabelType(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "bug":
		return "bug"
	case "feat", "feature", "enhancement":
		return "feat"
	case "chore", "maintenance", "refactor", "docs", "documentation":
		return "chore"
	case "question":
		return "question"
	}
	return ""
}

// The recipe file's knob words (recipefile.go). THIS IS THE ONE PLACE THEY ARE
// SPELLED, beside the chip words above, so the file's reader, its writer and
// the manual's account of them can be checked against one list. The until
// words are [UntilDone] and its siblings, because they are a contract with the
// runner and live beside [Met].

// WhenWords are the conditions a stage may carry, the ones [Fits] reads off an
// item's triage.
var WhenWords = []string{"always", "thin", "large", "touches auth", "has ui"}

// EffortWords are the crew's one word. No word is the knee.
var EffortWords = []string{"cheap", "strong"}

// StageKinds are what may run a stage, in the order the manual names them.
var StageKinds = []StageKind{StageChat, StageCheck, StageGate, StagePost}

func oneOf(word string, words []string) bool {
	for _, w := range words {
		if word == w {
			return true
		}
	}
	return false
}
