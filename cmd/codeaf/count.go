package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// countFlag is a whole number typed at a flag, refused with a sentence about
// the flag rather than with the number package's own word for it.
//
// `aforge logs --tail notanumber` answered
//
//	invalid value "notanumber" for flag -tail: parse error
//
// `parse error` is [strconv]'s message reaching a person through two layers,
// neither of which wrote it for anybody to read: it says nothing about what the
// flag takes and nothing to do next. The same binary already answers
// `--timeout hello` with `a duration such as 15m or 2h, or a number of seconds`
// ([wallFlag]), which is the standard this is held to.
//
// IT CARRIES THE FLAG'S OWN NOUN AND NOT A SHARED ONE. What a number means
// differs per flag — how many calls to show, how many turns to allow — so each
// door hands its own noun in and the refusal says it. The flag package already
// prints `invalid value "x" for flag -tail: ` in front of whatever [Set]
// returns, so the value typed and the flag it was typed at are there and are
// not repeated here.
type countFlag struct {
	value int
	// noun is what the number counts, in the person's words: "calls to show".
	// A phrase rather than a word, so a flag can say what the number is FOR.
	noun string
	// example is a figure this flag would really accept — the door's own
	// default, because somebody refused by a flag is best shown the ordinary
	// answer rather than an invented one.
	example int
}

// newCountFlag registers a whole-number flag on a set and hands back where its
// value will land, exactly as [flag.FlagSet.Int] does — so a door adopts it by
// changing one line and nothing about how it reads the value afterwards.
func newCountFlag(flags *flag.FlagSet, name string, fallback int, noun, help string) *int {
	count := &countFlag{value: fallback, noun: noun, example: fallback}
	flags.Var(count, name, help)
	return &count.value
}

func (c *countFlag) String() string { return strconv.Itoa(c.value) }

// Get is what the flag package's own printer asks for the default it shows, and
// it hands back an int so `(default 40)` is spelled the way every other numeric
// flag on the page spells it.
func (c *countFlag) Get() any { return c.value }

// Set reads the number, and says what the flag takes when it is not one.
//
// A NEGATIVE COUNT IS REFUSED HERE, where the sentence can still name the flag.
// Nothing downstream can tell `--tail -5` from a slice arithmetic mistake, so
// the refusal has to be at the door or it is not a refusal at all. Zero is
// LEGAL and always was: asking for none of something is a question, not a typo.
func (c *countFlag) Set(text string) error {
	number, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return fmt.Errorf("a whole number of %s, such as %d", c.noun, c.example)
	}
	if number < 0 {
		return fmt.Errorf("a whole number of %s, and not a negative one", c.noun)
	}
	c.value = number
	return nil
}
