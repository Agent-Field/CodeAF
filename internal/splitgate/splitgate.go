// Package splitgate is the enumeration evidence gate: does a piece of work name
// enough independent items for dividing it to beat one worker doing them in
// order?
//
// AND SINCE 2026-09-02 IT ASKS ONLY WHEN SOMEBODY ASKS IT TO. The gate shipped
// armed under a six-item floor, and a designed experiment — four planner arms
// against four readings of this gate, 273 judged draws on the plan door — put
// the planner with the gate OFF on the front and left every armed reading
// behind it (docs/design/plan-gate-doe/REPORT.md). So an unpinned binary keeps
// every division the planner drew, and the counting below decides only where
// somebody has pinned `AFORGE_SPLITGATE=1` or `judgment`. modes.go holds the
// pin and the reasoning; everything else in this file is the counting itself,
// which the experiment did not change and which arming still reads
// (internal/session's enumeratesWidth).
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
//
// AND IT FAILS OPEN ON WORDS IT HAS NEVER MET. The first counter carried a
// list of eighteen item-nouns and counted a number only beside one of them,
// which read "34 person-rows" and "34 research targets" as nothing at all — a
// live OSINT task was twice told its thirty-four people were zero items, on
// the honest evidence, because nobody doing that work writes "files". A list
// of the things people have piles of is open-ended and always behind the next
// domain. What is CLOSED is the opposite list: the measures, budgets and
// repetitions whose number sizes one thing rather than counting many — words,
// seconds, retries, steps. So the gate now counts a number beside any plural
// word that is not a measure, and an unfamiliar domain errs toward counting,
// with the paid reviewer behind the gate as the check on what it lets through
// (task_divide.go reads every division before admitting it).
package splitgate

import "strings"

// Floor is the smallest item count at which division has ever paid in the bench
// corpus: twelve image files won, four modules and three bugs lost.
const Floor = 6

// measures are the plural-shaped words whose number sizes ONE thing instead of
// counting many: units of text, data, time, money and screen, and the words of
// repetition and budget. This list may be a literal because it is a CLOSED
// class — new units of measure are not coined the way new kinds of item are —
// and it is a DENY list, so a word it is missing makes the gate count too
// eagerly (and the paid reviewer reads the division anyway) rather than
// silently refuse a whole domain the way the old noun list did.
//
// "steps" is here on the strength of the road itself: steps of one procedure
// are sequential by definition, and sequential work is never divided — a brief
// that says "200 steps" is stating a budget, not a pile.
var measures = map[string]bool{
	// text and data
	"words": true, "characters": true, "chars": true, "letters": true,
	"tokens": true, "bits": true, "bytes": true, "kilobytes": true,
	"megabytes": true, "gigabytes": true, "terabytes": true, "lines": true,
	// time
	"seconds": true, "minutes": true, "hours": true, "days": true,
	"weeks": true, "months": true, "years": true, "milliseconds": true,
	"microseconds": true, "nanoseconds": true,
	// money and screen
	"dollars": true, "cents": true, "euros": true, "pounds": true,
	"points": true, "pixels": true, "percents": true, "degrees": true,
	// repetition and budget
	"times": true, "retries": true, "attempts": true, "tries": true,
	"iterations": true, "rounds": true, "turns": true, "epochs": true,
	"steps": true, "runs": true, "loops": true,
	// parameter words that happen to wear an s
	"status": true,
}

// irregulars are the countable plurals that do not end in s. English coins new
// piles of things daily and new irregular plurals roughly never, so this too is
// a closed class rather than a domain list.
var irregulars = map[string]bool{
	"people": true, "men": true, "women": true, "children": true,
	"data": true, "media": true, "criteria": true, "series": true,
	"species": true, "indices": true, "matrices": true, "vertices": true,
	"analyses": true, "hypotheses": true,
}

// countable reports whether a word names a pile a number beside it could be
// counting. The shape test is plurality — an item worth enumerating arrives in
// the plural ("34 rows", "9 endpoints", "6 tiers") — and the suffix guards keep
// the singular s-enders out: "-ss" (process, across), "-us" (status, corpus,
// previous), "-is" (analysis, this). What passes the shape test is then held
// against the closed measures list above.
func countable(word string) bool {
	if irregulars[word] {
		return true
	}
	if len(word) < 4 || !strings.HasSuffix(word, "s") {
		return false
	}
	switch word[len(word)-2] {
	case 's', 'u', 'i':
		return false
	}
	return !measures[word]
}

// numberWords are the spelled counts worth reading. They start at six because
// anything below the floor changes no answer, and they stop at twenty because
// past that people write digits.
var numberWords = map[string]int{
	"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
	"eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14,
	"fifteen": 15, "sixteen": 16, "seventeen": 17, "eighteen": 18,
	"nineteen": 19, "twenty": 20,
}

// token is one run of letters-and-digits, with where it sat in the text — the
// position is kept because one adjacency rule below needs to see the
// punctuation between two tokens.
type token struct {
	text       string
	start, end int
}

func tokenize(lower string) []token {
	var out []token
	start := -1
	for i, r := range lower {
		alnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if alnum && start < 0 {
			start = i
		}
		if !alnum && start >= 0 {
			out = append(out, token{lower[start:i], start, i})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, token{lower[start:], start, len(lower)})
	}
	return out
}

// Items returns the largest explicit count of independent items a text names.
//
// A number counts when a countable word FOLLOWS it within three tokens — "34
// person-rows", "12 image files", "9 endpoints" — because that is the order
// English enumerates a pile in. A word BEFORE a number counts only across a
// colon ("bugs: 3", "errors: 12"): in running prose the word before a number is
// a verb or a preposition ("holds 34", "under 250"), and reading "covers 250
// words" off its verb would resurrect exactly the parameter-counting this gate
// exists to refuse. Bare numerals are never items: "limit=100" and "250 words"
// name a parameter and a measure, and a gate that read them as items would
// divide work that never should be.
func Items(text string) int {
	lower := strings.ToLower(text)
	tokens := tokenize(lower)
	most := 0
	for i, tok := range tokens {
		count := numeric(tok.text)
		if count < 0 || count <= most {
			continue
		}
		if follows(tokens, i) || enumeratedBefore(lower, tokens, i) {
			most = count
		}
	}
	// A spelled count followed by a countable word: "eight files", "twelve of
	// the image files". The window is characters rather than tokens only
	// because the spelled word is found by position, not by token index.
	for word, count := range numberWords {
		if count <= most {
			continue
		}
		idx := wholeWord(lower, word)
		if idx < 0 {
			continue
		}
		window := lower[idx+len(word):]
		if len(window) > windowChars {
			window = window[:windowChars]
		}
		for _, w := range tokenize(window) {
			if countable(w.text) {
				most = count
				break
			}
		}
	}
	return most
}

// follows reports whether a countable word stands within reach after the
// number at i. Three tokens covers an adjective or two between the count and
// its pile — "34 independent research targets" — while staying inside the
// number's own phrase.
func follows(tokens []token, i int) bool {
	for j := i + 1; j <= i+3 && j < len(tokens); j++ {
		if countable(tokens[j].text) {
			return true
		}
	}
	return false
}

// enumeratedBefore reports the one before-number shape that counts: a
// countable word, a colon, the number — the way a tally is written.
func enumeratedBefore(lower string, tokens []token, i int) bool {
	if i == 0 || !countable(tokens[i-1].text) {
		return false
	}
	return strings.Contains(lower[tokens[i-1].end:tokens[i].start], ":")
}

// numeric parses a token of digits, and answers -1 for anything else — a token
// with a letter in it ("v2", "8080p") is a name, not a count, and past six
// digits so is a run of digits: an id, a hash, a phone number. Nobody
// enumerates a million separate items in one piece of evidence.
func numeric(text string) int {
	if text == "" || len(text) > 6 {
		return -1
	}
	count := 0
	for _, c := range text {
		if c < '0' || c > '9' {
			return -1
		}
		count = count*10 + int(c-'0')
	}
	return count
}

// windowChars is how far past a spelled number the noun may sit — far enough
// for "twelve of the image files", short enough that the next sentence is not
// read as part of this one.
const windowChars = 40

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

// Armed reports whether the gate has the last word.
//
// IT IS OFF UNLESS SOMEBODY PINNED IT ON, which is the opposite of what this
// switch meant until 2026-09-02. The gate shipped armed and rolled back with
// the literal "0"; a designed experiment then measured four planner arms
// against four readings of it and the front it drew is the planner with this
// gate having no say (docs/design/plan-gate-doe/REPORT.md). So the arming moved
// into the pin: `1` for the count below, `judgment` for the plan's own sizing,
// and everything else — an unset pin included — for the gate keeping quiet.
// modes.go's [Mode] is where that is read, and this is one bit of it.
//
// It stays an environment pin and not a settings row for the reason it always
// was one (internal/config's settings.go): it picks which decomposition
// doctrine the binary runs, which is not something the product has an opinion
// about, and it disappears when nobody has a reason to reach for it.
func Armed() bool { return Mode() != ModeOff }
