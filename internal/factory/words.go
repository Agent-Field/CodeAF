package factory

import (
	"regexp"
	"strconv"
	"strings"
)

// Words become chips. THIS IS THE ONE PLACE THE CHIP WORDS ARE SPELLED: a
// person's sentence carries a cap (`$8`), a gate (`plan first`, `ship it`,
// `self-ship`), a round count (`two review rounds`) or an effort (`stronger`,
// `cheaper`), and whatever reads a sentence, the mock's parser or the surface
// lifting chips off a new item the mock cannot steer, reads it with these
// regexes so the two can never disagree about what a word means.
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
// reader that lifts more words of its own (the mock's security pass and its
// constraints) can go on reading it before [TidyWords].
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
