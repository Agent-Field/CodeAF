package subharness

import (
	"strings"
	"unicode"
)

// DETECTION: which harness, if any, the person just asked for.
//
// A sub-harness has no slash command. The way to run one is to say what you
// want, which means something has to decide — every turn, before the model is
// sent anything — whether what was said is one of the things this build already
// knows how to do properly.
//
// THAT DECISION IS A TABLE LOOKUP, NOT A JUDGEMENT. There is no model call
// here, no embedding, no cache to warm: [Score] is a pure function of a turn's
// text and one entry's own words, and it returns the same number today, in a
// test, and on a machine with no network. Three reasons, in the order they
// matter:
//
//   - A MODEL CALL PER TURN IS A TAX ON EVERY TURN. Most turns are not a
//     harness. Paying a small model to say so, before the real model has been
//     sent anything, would put latency and a bill on the ordinary case to serve
//     the rare one.
//   - A QUESTION A PERSON DID NOT ASK FOR MUST BE PREDICTABLE. This produces a
//     card that interrupts what somebody was doing. A card that appears for a
//     sentence and not for the same sentence tomorrow is worse than no card,
//     because there is nothing to learn about when it happens.
//   - THE DESIGNER ALREADY KNOWS. The cues are written at build time by whoever
//     wrote the harness. Asking a model to re-derive at runtime what a person
//     wrote down at build time is paying for an answer we already have.
//
// The cost of that choice is recall: a turn that asks for research in words no
// designer wrote scores low and is never offered. That is the right way round.
// A missed offer costs a person nothing — they get the ordinary turn they were
// always going to get — and a wrong offer costs them a question, an answer, and
// a little trust in every card after it.

// Threshold is the score a match must EXCEED before a surface may raise the
// card. It is a constant rather than a setting because it is a property of the
// weights below, not a preference: the weights are chosen so that the sentences
// on either side of 0.7 are the sentences a person would sort the same way.
const Threshold = 0.7

// The evidence weights. Each is how much ONE signal moves the score on its own,
// and they combine as independent evidence — score = 1 − Π(1 − w) — so a second
// signal always helps and no amount of weak evidence ever quite reaches
// certainty.
//
// They are set so that the four cases sort like this:
//
//   - NAMING THE HARNESS FIRES ALONE (0.9). "run the research harness" is not a
//     guess about what somebody meant; it is what they said. The word "harness"
//     is what makes it a naming: a harness called "research" is not named by
//     every sentence with the word research in it, and without that rule the
//     name would quietly outweigh the cue list for every entry whose name is
//     also an ordinary verb.
//   - ONE PHRASE CUE ALMOST FIRES (0.7, and the threshold is EXCEEDED, not
//     met). A designer's multi-word cue landing verbatim is strong evidence and
//     still wants one corroborating word from the description.
//   - ONE SINGLE-WORD CUE NEVER FIRES ALONE (0.55). "research" is a word people
//     use about work they are doing themselves.
//   - TWO CUES FIRE (1 − 0.45×0.45 = 0.80). Two of the designer's own words in
//     one sentence is the sentence being about the harness.
//
// The description is half-weighted (0.5) because it is prose written for a
// person to read, not a trigger list: its overlap corroborates a cue and must
// never carry a match by itself. Even a description quoted back word for word
// scores 0.5 alone, which is under the threshold — as it should be, since a
// person who wanted the harness had every chance to use one of its cues.
const (
	nameWeight   = 0.9
	phraseWeight = 0.7
	cueWeight    = 0.55
	descWeight   = 0.5
)

// Turn is what a person just said, and the whole of what detection reads. It is
// a type rather than a string so that the day detection wants a second fact —
// the turn before it, whether an image rode along — the signature does not
// change under every caller.
type Turn struct {
	Text string
}

// Match is one entry and what it scored.
type Match struct {
	Entry Entry
	Score float64
}

// Score is how strongly one turn asks for one harness, from 0 to 1. It is pure:
// same turn, same entry, same number, every time, on every machine.
func Score(turn Turn, entry Entry) float64 {
	words := tokenize(turn.Text)
	if len(words) == 0 {
		return 0
	}
	// miss is the probability that every signal so far missed. Multiplying is
	// what makes the evidence combine without any one signal being able to
	// reach 1 on its own.
	//
	// counted is what stops ONE word in the turn from being evidence twice. A
	// harness called "research" whose first cue is "research" is the ordinary
	// case, not a mistake, and counting the sentence's one word as a name AND
	// as a cue would score it above two genuinely different cues.
	miss := 1.0
	counted := map[string]bool{}
	signal := func(phrase []string, weight float64) {
		key := strings.Join(phrase, " ")
		if key == "" || counted[key] || !holds(words, phrase) {
			return
		}
		counted[key] = true
		miss *= 1 - weight
	}
	// The name first, so that when the name is also a cue the STRONGER reading
	// is the one that counts and the cue is the duplicate.
	signal(tokenize(entry.Name), nameOrCueWeight(words))
	for _, cue := range entry.Cues {
		phrase := tokenize(cue)
		if len(phrase) > 1 {
			signal(phrase, phraseWeight)
			continue
		}
		signal(phrase, cueWeight)
	}
	cued := 1 - miss
	// And the description, folded in as one more independent signal at half
	// weight. (1−cued)×x is the same combination the loop above makes.
	return cued + (1-cued)*descWeight*overlap(words, entry.Description)
}

// nameOrCueWeight says what a hit on the harness's own name is worth in this
// turn: a NAMING when the person also said the word harness, and one more cue
// when they did not.
//
// The two spellings the word has here are the two this codebase uses for the
// same thing, and nothing else counts: "sub-harness" tokenizes to sub + harness
// and lands on the first of them.
func nameOrCueWeight(words []string) float64 {
	if holds(words, []string{"harness"}) || holds(words, []string{"subharness"}) {
		return nameWeight
	}
	return cueWeight
}

// Best is the highest-scoring entry and whether it clears the threshold. The
// bool is the whole decision a surface needs: true means raise the card, false
// means this was an ordinary turn.
//
// Ties go to the entry that comes FIRST in the registry — the comparison is
// strictly greater — so a build whose registry loads in a fixed order asks the
// same question twice for the same sentence. A caller that wants a different
// precedence orders the slice; nothing here re-sorts it.
func Best(turn Turn, entries []Entry) (Match, bool) {
	var best Match
	for _, entry := range entries {
		score := Score(turn, entry)
		if score > best.Score {
			best = Match{Entry: entry, Score: score}
		}
	}
	return best, best.Score > Threshold
}

// ── the words ───────────────────────────────────────────────────────────────

// tokenize cuts text into lowercase words. Everything that is not a letter or a
// digit is a boundary, so punctuation, quotes and hyphens separate rather than
// stick — "find-out," is two words, which is what a person typing it meant.
func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// holds reports whether words contains phrase as a CONSECUTIVE run. A
// multi-word cue is a phrase and matches nothing else: "find out what broke"
// holds "find out", and "find the output" does not.
func holds(words, phrase []string) bool {
	if len(phrase) == 0 || len(phrase) > len(words) {
		return false
	}
	for at := 0; at+len(phrase) <= len(words); at++ {
		hit := true
		for i, want := range phrase {
			if !same(words[at+i], want) {
				hit = false
				break
			}
		}
		if hit {
			return true
		}
	}
	return false
}

// same compares one word against one cue word, tolerating the endings English
// puts on a verb in a sentence: "researching this" matches the cue "research",
// "digs into" matches "dig".
//
// It is a suffix table and not a stemmer on purpose. A real stemmer is a
// dependency, a table of exceptions, and a source of matches a designer cannot
// predict from the cue they wrote; four endings are what it takes to stop a
// cue list from needing every conjugation spelled out, and no more.
func same(word, want string) bool {
	if word == want {
		return true
	}
	left, right := stem(word), stem(want)
	// The doubled consonant a participle leaves behind — digging → digg → dig —
	// is TRIED rather than applied, because applying it would break the words
	// that genuinely end in one: stemming "falling" to "fal" would stop it
	// matching the cue "fall".
	return left == right || undouble(left) == right || left == undouble(right)
}

// stem drops one plural or participle ending. The floor is three characters:
// stripping "ing" off "sing" or "s" off "is" would make two unrelated words the
// same one, which is exactly the invented match this whole file avoids.
func stem(word string) string {
	for _, suffix := range []string{"ing", "es", "ed", "s"} {
		if len(word) > len(suffix)+2 && strings.HasSuffix(word, suffix) {
			return word[:len(word)-len(suffix)]
		}
	}
	return word
}

// undouble drops one of a trailing pair of identical letters, and leaves
// everything else alone. Three characters is the floor here too.
func undouble(word string) string {
	if len(word) < 4 || word[len(word)-1] != word[len(word)-2] {
		return word
	}
	return word[:len(word)-1]
}

// overlap is the share of the description's content words that appear in the
// turn, from 0 to 1. Empty descriptions and descriptions that are nothing but
// glue score 0 — a harness nobody described corroborates nothing.
func overlap(words []string, description string) float64 {
	content := 0
	hits := 0
	for _, word := range tokenize(description) {
		if glue[word] || len(word) < 3 {
			continue
		}
		content++
		if holds(words, []string{word}) {
			hits++
		}
	}
	if content == 0 {
		return 0
	}
	return float64(hits) / float64(content)
}

// glue is the words a description is made of that say nothing about what the
// harness does. It is short by design: every word on this list is a word the
// matcher stops counting, and a long list is a way of quietly deciding that
// somebody's description means something other than what it says.
var glue = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "but": true, "by": true, "for": true, "from": true, "in": true,
	"into": true, "is": true, "it": true, "its": true, "of": true, "on": true,
	"one": true, "or": true, "that": true, "the": true, "their": true,
	"them": true, "then": true, "this": true, "to": true, "up": true,
	"was": true, "were": true, "with": true, "you": true, "your": true,
}
