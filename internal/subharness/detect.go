package subharness

import (
	"sort"
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
// The cost of that choice is recall, and the first real runs showed how the
// bill arrives: a goal scored 0.42 against THE HARNESS THAT GOAL BUILT. The
// designer's cue list was written in the designer's words, the goal was written
// in the person's, and nothing connected them. Two answers to that, and this
// file holds both:
//
//   - [SeedCues] derives a cue list from the goal itself, so a harness is
//     always reachable by the words that built it. Nothing about matching is
//     loosened to get there; the vocabulary is simply no longer left empty.
//   - The scorer reads a designer's PHRASE as a phrase — whole (0.9), or most
//     of it (0.6) — and a lone word as the coincidence it usually is (0.45).
//
// A missed offer still costs a person nothing — they get the ordinary turn they
// were always going to get — and a wrong offer still costs them a question. The
// numbers below are set on that asymmetry and nothing else.

// The decision Best makes, in three numbers.
//
//   - Threshold is the score at which one match stands on its own. It is met,
//     not exceeded: 0.55 is the number one whole phrase clears and one lone
//     word does not.
//   - ClearWinner is the other way in. A registry is a field of candidates, and
//     an entry that is a quarter of the scale ahead of everything else is the
//     answer to the question even when it is a middling score in absolute
//     terms. With one entry in the registry the runner-up is 0 and this rule is
//     the whole of the decision — which is right: a build with one harness has
//     nothing else the sentence could have meant.
//   - Floor is the line under which neither rule may reach. Below 0.40 the
//     evidence is a word or two of prose overlap, and a card raised on that is
//     the card that teaches people to say no without reading.
const (
	Threshold   = 0.55
	ClearWinner = 0.25
	Floor       = 0.40
)

// The evidence weights. Each is how much ONE signal moves the score on its own,
// and they combine as independent evidence — score = 1 − Π(1 − w) — so a second
// signal always helps and no amount of weak evidence ever quite reaches
// certainty.
//
// They are set so that the rungs sort like this:
//
//   - NAMING THE HARNESS FIRES ALONE (0.9). "run the research harness" is not a
//     guess about what somebody meant; it is what they said. A name is a name
//     even without the word harness beside it: harnesses are named by the goals
//     that built them, so a turn that lands one is a turn that said the thing.
//   - ONE WHOLE PHRASE CUE FIRES ALONE (0.9). A designer's multi-word cue
//     landing verbatim is the designer's own sentence coming back.
//   - MOST OF A PHRASE ALMOST FIRES (0.6). Two of the three words of "strongest
//     counterargument addressed", in order, is the same request rephrased —
//     strong enough to want one more signal, not strong enough alone.
//   - ONE SINGLE-WORD CUE NEVER FIRES ALONE (0.45). "research" is a word people
//     use about work they are doing themselves. Two of them do fire (0.70).
//
// The description is half-weighted (0.5) because it is prose written for a
// person to read, not a trigger list: its overlap corroborates a cue and must
// never carry a match by itself. Even a description quoted back word for word
// scores 0.5 alone, which is under the threshold — as it should be, since a
// person who wanted the harness had every chance to use one of its cues.
const (
	nameWeight   = 0.9
	phraseWeight = 0.9
	nearWeight   = 0.6
	cueWeight    = 0.45
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
	// counted is what stops ONE phrase from being evidence twice. A harness
	// called "research" whose first cue is "research" is the ordinary case, not
	// a mistake, and counting the sentence's one word as a name AND as a cue
	// would score it above two genuinely different cues.
	miss := 1.0
	counted := map[string]bool{}
	hit := func(phrase []string, weight float64) {
		key := strings.Join(phrase, " ")
		if key == "" || counted[key] {
			return
		}
		counted[key] = true
		miss *= 1 - weight
	}
	// The name first, so that when the name is also a cue the STRONGER reading
	// is the one that counts and the cue is the duplicate.
	if name := tokenize(entry.Name); holds(words, name) {
		hit(name, nameWeight)
	}
	for _, cue := range entry.Cues {
		phrase := tokenize(cue)
		switch {
		case len(phrase) == 0:
		case len(phrase) == 1:
			if holds(words, phrase) {
				hit(phrase, cueWeight)
			}
		case holds(words, phrase):
			hit(phrase, phraseWeight)
		case nearly(words, phrase):
			hit(phrase, nearWeight)
		}
	}
	cued := 1 - miss
	// And the description, folded in as one more independent signal at half
	// weight. (1−cued)×x is the same combination the loop above makes.
	return cued + (1-cued)*descWeight*overlap(words, entry.Description)
}

// Best is the highest-scoring entry and whether it clears the bar. The bool is
// the whole decision a surface needs: true means raise the card, false means
// this was an ordinary turn.
//
// Two ways to clear it — a score that stands alone, or a lead nothing else in
// the registry comes near — and one line under both (see [Threshold]).
//
// Ties go to the entry that comes FIRST in the registry — the comparison is
// strictly greater — so a build whose registry loads in a fixed order asks the
// same question twice for the same sentence. Two entries that score the same
// are also, by construction, not a clear winner: a sentence that describes two
// harnesses equally well is a sentence nobody should be asked about.
func Best(turn Turn, entries []Entry) (Match, bool) {
	var best Match
	runnerUp := 0.0
	for _, entry := range entries {
		score := Score(turn, entry)
		switch {
		case score > best.Score:
			runnerUp = best.Score
			best = Match{Entry: entry, Score: score}
		case score > runnerUp:
			runnerUp = score
		}
	}
	if best.Score < Floor {
		return best, false
	}
	return best, best.Score >= Threshold || best.Score-runnerUp >= ClearWinner
}

// ── the cues a goal is worth ────────────────────────────────────────────────

// SeedCues derives a harness's trigger vocabulary from the goal that built it:
// the phrases in that sentence which say what the work IS, ranked, four to eight
// of them, and every one of them a run of words the goal itself contains.
//
// It exists because of the hole a real run found. A designer writes cues in the
// designer's vocabulary; the person types the goal in theirs; and the harness
// built for that exact sentence scored 0.42 against it. Seeding from the goal
// closes that by construction — the sentence that commissioned a harness always
// reaches it — and every paraphrase of that sentence inherits the same phrases
// minus a word or two, which is what [nearly] is for.
//
// WHAT IT LOOKS FOR is contiguous spans of two to four words that begin and end
// on a word carrying meaning, hold at most one function word in the middle, and
// stay inside one clause — "adopt a component model", "ad hoc views", "small
// open weight llms". A span that opens on a verb of work scores higher, because
// a verb-object pair is what a person retypes when they want the same job done
// again. Punctuation is a wall: nothing spans the em dash in "views —
// investigate", because those are two different things being asked for.
//
// WHAT IT REFUSES is padding. A goal with three good phrases returns three; it
// will not reach the fourth by handing back a common verb that would make every
// turn containing it a near miss. The count is what the sentence had to give.
func SeedCues(goal string) []string {
	var candidates []span
	base := 0
	for index, clause := range clauses(goal) {
		candidates = append(candidates, spansOf(clause, base, index)...)
		base += len(clause)
	}
	// Best first, and every tie broken by where the words were, so the same
	// goal returns the same list on every machine and in every run.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].at != candidates[j].at {
			return candidates[i].at < candidates[j].at
		}
		return len(candidates[i].words) < len(candidates[j].words)
	})

	var picked []span
	taken := map[string]bool{}
	covered := map[string]bool{}
	take := func(s span) {
		picked = append(picked, s)
		taken[s.text()] = true
		for _, word := range s.words {
			if seedWord(word) {
				covered[canonical(word)] = true
			}
		}
	}

	// One from each clause first. A goal is a list of things being asked for —
	// investigate, argue, produce — and a ranking that took the top six spans
	// would take six of them from whichever clause happened to be wordiest.
	spoken := map[int]bool{}
	for _, s := range candidates {
		if len(picked) >= seedPhrases {
			break
		}
		if spoken[s.clause] || taken[s.text()] {
			continue
		}
		spoken[s.clause] = true
		take(s)
	}
	// Then by rank, but only what says something new. A span that repeats words
	// already cued cannot match a turn the ones already picked would miss.
	for _, s := range candidates {
		if len(picked) >= seedPhrases {
			break
		}
		if taken[s.text()] || !s.adds(covered) {
			continue
		}
		take(s)
	}
	// Then, only if the list is short, the redundant spans — "cli tool" beside
	// "cli tool that watches". They add nothing to a turn that quotes the goal
	// and they are the whole of the match for a turn that says half of it.
	for _, s := range candidates {
		if len(picked) >= seedMin {
			break
		}
		if taken[s.text()] {
			continue
		}
		take(s)
	}
	// And last, for a goal too short to hold a phrase at all, its own words.
	// Long words only: a single short common word as a cue is a false offer
	// waiting for the turn that happens to use it.
	if len(picked) < seedMin {
		for _, s := range loneWords(goal) {
			if len(picked) >= seedMin {
				break
			}
			if taken[s.text()] {
				continue
			}
			take(s)
		}
	}

	// Returned in the goal's own order, so a person reading the cue list beside
	// the goal can see where each one came from.
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].at < picked[j].at })
	out := make([]string, 0, len(picked))
	for _, s := range picked {
		out = append(out, s.text())
	}
	return out
}

// The shape of a seeded cue list. seedPhrases is what a rich goal gives, seedMin
// what a thin one is padded to, and seedMax the cap nothing here can pass.
const (
	seedSpanMin = 2
	seedSpanMax = 4
	seedPhrases = 6
	seedMin     = 4
	seedMax     = 8
)

// span is one candidate cue: a run of the goal's words, where it started, which
// clause it came from, and what it is worth.
type span struct {
	words  []string
	at     int
	clause int
	score  float64
}

func (s span) text() string { return strings.Join(s.words, " ") }

// adds reports whether this span carries a meaning word no picked span has.
func (s span) adds(covered map[string]bool) bool {
	for _, word := range s.words {
		if seedWord(word) && !covered[canonical(word)] {
			return true
		}
	}
	return false
}

// spansOf is every candidate cue inside one clause.
func spansOf(words []string, base, clause int) []span {
	var out []span
	for at := 0; at < len(words); at++ {
		if !seedWord(words[at]) {
			continue
		}
		for size := seedSpanMin; size <= seedSpanMax && at+size <= len(words); size++ {
			run := words[at : at+size]
			if !seedWord(run[size-1]) {
				continue
			}
			meaning, function := 0, 0
			for _, word := range run {
				if seedWord(word) {
					meaning++
					continue
				}
				function++
			}
			// Two words that mean something, and at most one that does not.
			// "recommendation with the strongest" is four words of which two are
			// glue, and a cue like that matches a sentence about anything.
			if meaning < 2 || function > 1 {
				continue
			}
			out = append(out, span{
				words:  run,
				at:     base + at,
				clause: clause,
				score:  float64(meaning) + verbBonus(run[0]) - functionCost*float64(function),
			})
		}
	}
	return out
}

// What a span is worth, beyond the words in it that mean something: a verb of
// work at the front is the shape a person retypes, and a function word in the
// middle is a word the paraphrase will move.
const (
	verbLead     = 0.75
	functionCost = 0.35
)

func verbBonus(word string) float64 {
	if actions[canonical(word)] {
		return verbLead
	}
	return 0
}

// loneWords is the last resort: the goal's own long meaning-words, longest
// first, as single-word cues. A goal like "summarize my email" has no phrase in
// it and would otherwise be reachable by nothing.
func loneWords(goal string) []span {
	var out []span
	seen := map[string]bool{}
	at := 0
	for _, clause := range clauses(goal) {
		for offset, word := range clause {
			if len(word) < 4 || !seedWord(word) || seen[canonical(word)] {
				continue
			}
			seen[canonical(word)] = true
			out = append(out, span{words: []string{word}, at: at + offset})
		}
		at += len(clause)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].words[0]) != len(out[j].words[0]) {
			return len(out[i].words[0]) > len(out[j].words[0])
		}
		return out[i].at < out[j].at
	})
	if len(out) > seedMax {
		out = out[:seedMax]
	}
	return out
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

// clauses cuts text the same way [tokenize] does, and then again at the marks
// that end a thought: a sentence stop, a comma, a dash, a bracket. The words
// come back in the same order tokenize would give them — a clause boundary is
// also a word boundary — so a cue built inside one clause is still a
// consecutive run of the whole text and still matches it.
//
// This is what stops "ad-hoc views — investigate both sides" from seeding the
// phrase "views investigate". A hyphen is inside a word and a dash is between
// two of them, and the two look nothing alike to a person reading the sentence.
func clauses(text string) [][]string {
	var out [][]string
	var clause []string
	var word []rune
	endWord := func() {
		if len(word) > 0 {
			clause = append(clause, string(word))
			word = word[:0]
		}
	}
	endClause := func() {
		endWord()
		if len(clause) > 0 {
			out = append(out, clause)
			clause = nil
		}
	}
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			word = append(word, r)
		case strings.ContainsRune(breaks, r):
			endClause()
		default:
			endWord()
		}
	}
	endClause()
	return out
}

// breaks is the punctuation that ends a clause. Everything else that is not a
// letter — a space, a hyphen, an apostrophe — only ends a word.
const breaks = ".,;:!?()[]{}<>\"'`…—–|/\\\n\r"

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

// nearly reports whether MOST of a phrase cue is in the turn: a consecutive run
// of at least half its words, at least two of them, at least two of which mean
// something. It is how a paraphrase of the goal that built a harness still
// reaches it — "the strongest counterargument" for "strongest counterargument
// addressed" — and it is deliberately unavailable to a two-word cue, where half
// a phrase is one word and one word is the coincidence [cueWeight] exists for.
func nearly(words, phrase []string) bool {
	for size := len(phrase) - 1; size >= 2 && size*2 >= len(phrase); size-- {
		for at := 0; at+size <= len(phrase); at++ {
			run := phrase[at : at+size]
			if meaning(run) >= 2 && holds(words, run) {
				return true
			}
		}
	}
	return false
}

// meaning counts the words in a run that are not glue.
func meaning(run []string) int {
	count := 0
	for _, word := range run {
		if !glue[word] {
			count++
		}
	}
	return count
}

// same compares one word against one cue word, tolerating the endings English
// puts on a word in a sentence: "researching this" matches the cue "research",
// "digs into" matches "dig", "views" matches "view".
//
// It is a suffix table and not a stemmer on purpose. A real stemmer is a
// dependency, a table of exceptions, and a source of matches a designer cannot
// predict from the cue they wrote.
func same(word, want string) bool {
	return word == want || canonical(word) == canonical(want)
}

// canonical is one word with the ending English gave it taken back off. Three
// steps, each with a length floor, because the floor is the whole safety of the
// thing: stripping "ing" off "sing" or "s" off "is" would make two unrelated
// words the same one, which is exactly the invented match this file avoids.
//
//   - the plural or the participle: watches → watche, views → view,
//     adopted → adopt, digging → digg
//   - the doubled consonant a participle leaves behind: digg → dig, and never
//     fall → fal on its own, which is why it runs only after a suffix came off
//   - the silent e the two steps above leave on either side of a pair:
//     sources → source → sourc, and source → sourc
//
// What it does not do is spelling. "used" and "use" are two words here, and a
// designer who wants both writes both.
func canonical(word string) string {
	switch {
	case len(word) > 4 && strings.HasSuffix(word, "ing"):
		word = undouble(word[:len(word)-3])
	case len(word) > 4 && strings.HasSuffix(word, "ed"):
		word = undouble(word[:len(word)-2])
	case len(word) > 3 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss"):
		word = word[:len(word)-1]
	}
	if len(word) > 3 && strings.HasSuffix(word, "e") {
		word = word[:len(word)-1]
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

// overlap is the share of the description's distinct content words that appear
// in the turn, from 0 to 1, compared as stems so that a description written in
// the plural corroborates a turn written in the singular. Empty descriptions and
// descriptions that are nothing but glue score 0 — a harness nobody described
// corroborates nothing.
func overlap(words []string, description string) float64 {
	said := make(map[string]bool, len(words))
	for _, word := range words {
		said[canonical(word)] = true
	}
	seen := map[string]bool{}
	content, hits := 0, 0
	for _, word := range tokenize(description) {
		if glue[word] || len(word) < 3 {
			continue
		}
		stem := canonical(word)
		if seen[stem] {
			continue
		}
		seen[stem] = true
		content++
		if said[stem] {
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

// seedWord reports whether a word is worth building a cue around: not a
// function word, not a bare number, and not a single letter. It is a longer
// list than [glue] on purpose — glue decides what a DESCRIPTION means and is
// kept short so it cannot quietly rewrite one, while this decides which of a
// goal's own words are worth typing again, and a cue made of "should" or
// "whether" would answer to every sentence in the language.
func seedWord(word string) bool {
	return len(word) >= 2 && !seedStop[word] && !number(word)
}

func number(word string) bool {
	for _, r := range word {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

var seedStop = map[string]bool{
	// The counting words go here beside the function words for the same reason:
	// "three punchy taglines" is a cue about taglines, and the three is how many
	// somebody wanted this once.
	"zero": true, "two": true, "three": true, "four": true,
	"five": true, "six": true, "seven": true, "eight": true, "nine": true,
	"ten": true, "eleven": true, "twelve": true, "dozen": true,
	"hundred": true, "thousand": true, "million": true, "couple": true,
	"several": true,

	"about": true, "above": true, "after": true, "again": true, "against": true,
	"all": true, "also": true, "am": true, "an": true, "and": true, "any": true,
	"anything": true, "are": true, "as": true, "at": true, "be": true,
	"because": true, "been": true, "before": true, "being": true, "below": true,
	"between": true, "both": true, "but": true, "by": true, "can": true,
	"could": true, "did": true, "do": true, "does": true, "doing": true,
	"done": true, "down": true, "during": true, "each": true, "either": true,
	"else": true, "even": true, "ever": true, "every": true, "few": true,
	"for": true, "from": true, "further": true, "get": true, "give": true,
	"got": true, "had": true, "has": true, "have": true, "having": true,
	"he": true, "her": true, "here": true, "hers": true, "him": true,
	"his": true, "how": true, "however": true, "i": true, "if": true,
	"in": true, "into": true, "is": true, "it": true, "its": true,
	"just": true, "like": true, "make": true, "many": true, "may": true,
	"me": true, "might": true, "mine": true, "more": true, "most": true,
	"much": true, "must": true, "my": true, "neither": true, "no": true,
	"nor": true, "not": true, "now": true, "of": true, "off": true,
	"on": true, "once": true, "one": true, "only": true, "or": true,
	"other": true, "otherwise": true, "ought": true, "our": true, "ours": true,
	"out": true, "over": true, "own": true, "per": true, "please": true,
	"rather": true, "same": true, "shall": true, "she": true, "should": true,
	"since": true, "so": true, "some": true, "something": true, "still": true,
	"such": true, "sure": true, "than": true, "that": true, "the": true,
	"theirs": true, "them": true, "then": true, "there": true, "these": true,
	"they": true, "thing": true, "things": true, "this": true, "those": true,
	"though": true, "through": true, "thus": true, "to": true, "too": true,
	"under": true, "until": true, "up": true, "us": true, "very": true,
	"want": true, "was": true, "we": true, "well": true, "were": true,
	"what": true, "when": true, "where": true, "whether": true, "which": true,
	"while": true, "who": true, "whom": true, "why": true, "will": true,
	"with": true, "within": true, "without": true, "would": true, "yet": true,
	"you": true, "your": true, "yours": true,
}

// actions is the verbs a goal opens a request with. It is a ranking hint and
// nothing else — a verb outside this list costs a span nothing but the bonus —
// so the list is the common ones and is not trying to be a lexicon. Held as
// stems ([canonical]) so that "investigating" and "investigate" are one entry.
var actions = map[string]bool{}

func init() {
	for _, verb := range []string{
		"adopt", "analyse", "analyze", "argue", "assess", "audit", "benchmark",
		"build", "check", "choose", "cite", "collect", "compare", "convert",
		"decide", "deploy", "design", "debug", "document", "draft", "evaluate",
		"explain", "extract", "find", "fix", "gather", "generate", "implement",
		"improve", "investigate", "list", "measure", "migrate", "monitor",
		"plan", "port", "produce", "profile", "propose", "publish", "rank",
		"recommend", "refactor", "release", "research", "review", "score",
		"ship", "summarise", "summarize", "test", "track", "translate",
		"verify", "watch", "weigh", "write",
	} {
		actions[canonical(verb)] = true
	}
}
