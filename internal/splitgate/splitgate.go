// Package splitgate is the enumeration evidence gate: does a piece of work name
// enough independent items for dividing it to beat one worker doing them in
// order?
//
// IT IS ONE ANSWER, ASKED IN THREE PLACES. The gate was written for the
// resident's planner and its leaves (cmd/aforge/cooperative.go) and measured
// against the swarm bench corpus, where it reproduced the empirically best
// arm's decision on every task in the table: twelve image files and eight
// endpoints divided, four modules and three bugs did not. The v3 session engine
// now asks the same question of a task's own work (internal/session's
// task_divide.go), and a second implementation of it would be a second answer —
// the fault CLAUDE.md's one-source-of-truth law names, applied to a decision
// instead of to a number. So the counting lives here, with no dependency on
// either product, and both call it.
//
// IT COSTS NOTHING. There is no model call in this package and there never
// should be: the gate is asked on every division request and on every task
// admission, and a gate that spent money to say no would be more expensive than
// the division it refused.
package splitgate

import (
	"os"
	"strings"
)

// Floor is the smallest item count at which division has ever paid in the bench
// corpus: twelve image files won, four modules and three bugs lost.
const Floor = 6

// nouns are the words a number has to stand beside before it counts as a count
// of INDEPENDENT ITEMS. The list is deliberately concrete — things a person
// enumerates when they have a pile of them — and prefix matching covers the
// plurals.
var nouns = []string{"file", "module", "image", "note", "bug", "test",
	"function", "section", "chapter", "document", "item", "component",
	"task", "endpoint", "table", "page", "record", "case"}

// numberWords are the spelled counts worth reading. They start at six because
// anything below the floor changes no answer, and they stop at twenty because
// past that people write digits.
var numberWords = map[string]int{
	"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
	"eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14,
	"fifteen": 15, "sixteen": 16, "seventeen": 17, "eighteen": 18,
	"nineteen": 19, "twenty": 20,
}

// Items returns the largest explicit count of independent items a text names,
// counting a number only when it stands next to an item-noun — "twelve image
// files", "bugs: 3", "note-1 … note-5". Bare numerals are not items: a task
// that says "limit=100" or "250 words" is naming a parameter, and a gate that
// read it as 100 items would divide work that never should be.
func Items(text string) int {
	lower := strings.ToLower(text)
	most := 0
	// A number followed shortly by a noun: "12 image files", "bugs: 3".
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	for i, field := range fields {
		count := 0
		for _, c := range field {
			if c < '0' || c > '9' {
				count = -1
				break
			}
			count = count*10 + int(c-'0')
		}
		if count < 0 {
			continue
		}
		near := false
		for j := i - 1; j <= i+1 && !near; j++ {
			if j < 0 || j >= len(fields) || j == i {
				continue
			}
			near = hasNoun(fields[j])
		}
		if near && count > most {
			most = count
		}
	}
	// A number-word followed by a noun: "eight files", "twelve images".
	for word, count := range numberWords {
		idx := wholeWord(lower, word)
		if idx < 0 {
			continue
		}
		window := lower[idx+len(word):]
		if len(window) > windowChars {
			window = window[:windowChars]
		}
		for _, noun := range nouns {
			if strings.Contains(window, noun) && count > most {
				most = count
			}
		}
	}
	return most
}

// windowChars is how far past a spelled number the noun may sit — far enough
// for "twelve of the image files", short enough that the next sentence is not
// read as part of this one.
const windowChars = 40

func hasNoun(field string) bool {
	for _, noun := range nouns {
		if strings.HasPrefix(field, noun) {
			return true
		}
	}
	return false
}

// wholeWord finds a word that is not part of a longer one — "ten" inside
// "flatten" is not a count — and answers -1 when there is none.
func wholeWord(text, word string) int {
	for off := 0; ; {
		k := strings.Index(text[off:], word)
		if k < 0 {
			return -1
		}
		k += off
		leftOK := k == 0 || text[k-1] < 'a' || text[k-1] > 'z'
		rightOK := k+len(word) >= len(text) || text[k+len(word)] < 'a' || text[k+len(word)] > 'z'
		if leftOK && rightOK {
			return k
		}
		off = k + 1
	}
}

// WorthIt reports whether a text enumerates enough independent items for a
// division to beat one worker doing them in sequence.
func WorthIt(evidence string) bool {
	return Items(evidence) >= Floor
}

// Armed reports whether the gate has the last word. It is on unless somebody
// has turned it off, and the switch is the literal "0" that
// cmd/aforge/cooperative.go has always read — a rollback for a wave, not a
// preference, which is why it is an environment pin and not a settings row
// (internal/config's settings.go).
func Armed() bool {
	return strings.TrimSpace(os.Getenv("AFORGE_SPLITGATE")) != "0"
}
