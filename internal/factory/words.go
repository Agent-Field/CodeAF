package factory

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
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

// Lift reads the four chips out of words. The gate words (`plan first`, `ship
// it`, `self-ship`) are read for a new item, whose approve steps they place
// ([ApproveForGate]).
func Lift(words string) Chips { return lift(words, true) }

func lift(words string, gate bool) Chips {
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
	if m := reGate.FindStringSubmatch(rest); gate && m != nil {
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
// not a chip. THE GATE WORDS STAY IN REST: an item that exists changes where it
// holds for the person by its approve steps, never by a chip.
func LiftChips(words string) (cap *float64, rounds *int, effort string, rest string) {
	c := lift(words, false)
	if c.Cap > 0 {
		f := c.Cap
		cap = &f
	}
	if c.Rounds > 0 {
		n := c.Rounds
		rounds = &n
	}
	return cap, rounds, c.Effort, TidyWords(c.Rest)
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

// A STAGE IS ONE WORD. An item's stages are a short program a person reads at
// a glance and a digit reaches (`1` to `9`), so a name is one lowercase word
// of letters and digits, two to twelve cells long, and an item has at most
// nine of them. Every road that makes a stage name goes through [StageWord]:
// an edit ([Edit]), the recipe file's reader, and a typed stage sentence
// ([StageSentence]).
const (
	// StageMost is how many stages an item may have.
	StageMost = 9
	// AskMost is how many cells a stage's ask may be.
	AskMost = 240

	stageWordLeast = 2
	stageWordMost  = 12
)

// The refusals the stage bounds speak, word for word as the manual quotes
// them.
var (
	// ErrNineStages is an edit or a sentence that would make a tenth stage.
	ErrNineStages = errors.New("the run has nine stages already")
	// ErrAskTooLong is an ask longer than [AskMost] cells.
	ErrAskTooLong = errors.New("an ask is at most 240 cells")
	// ErrThinking is a thinking word that is not one of [EffortWords].
	ErrThinking = errors.New("thinking is cheap, strong, or nothing")
)

// StageWord is name as a stage's one word, lowercased, or the sentence that
// says why it is not one: `a stage is one word · "do through" is two` for
// more than one word, and the letters-and-digits or length sentence for one
// word that is not a name.
func StageWord(name string) (string, error) {
	fields := strings.Fields(strings.ToLower(name))
	switch {
	case len(fields) == 0:
		return "", errors.New("a stage needs a name")
	case len(fields) > 1:
		return "", fmt.Errorf("a stage is one word · %q is %s", strings.Join(fields, " "), countWord(len(fields)))
	}
	w := fields[0]
	for _, r := range w {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return "", fmt.Errorf("a stage is one word of letters and digits · %q is not", w)
		}
	}
	if n := utf8.RuneCountInString(w); n < stageWordLeast || n > stageWordMost {
		return "", fmt.Errorf("a stage is one word of 2 to 12 letters · %q is not", w)
	}
	return w, nil
}

// countWord is a small count in words, the way the refusals say it.
func countWord(n int) string {
	words := []string{"none", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n)
}

// askCells is how many cells an ask takes on one line.
func askCells(ask string) int { return utf8.RuneCountInString(ask) }
