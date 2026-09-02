package splitgate

import "strings"

// Counting the shapes a division is actually written down in.
//
// A PERSON WHO HAS ALREADY DIVIDED THE WORK STOPS COUNTING IT. That is the
// whole of #418's cheaper repair. [Items] reads the one shape where somebody
// says how many things there are — "twelve image files", "34 person-rows" — and
// a brief that instead NAMES its parts has no such phrase in it anywhere: it
// says "L1: ... L2: ... L3: ...", or lists the four files, or writes the number
// out in words. Every one of those names more independent work than the digit
// count can see, and the shipped counter reads all of them as zero.
//
// The readings below are taken together and the largest wins, the same way
// [Items] keeps the largest number it can justify. Each one asks for at least
// two of its shape before it counts anything, because a single labelled item or
// a lone file path is a mention, not a list.

// Lanes is [Items] plus the shapes a written-down division takes. It is never
// smaller than [Items], so the bench corpus decisions it was measured on can
// only move in one direction, and the table in modes_test.go pins that they do
// not move at all.
//
// It is a COUNT, and a count is weighed against [Floor] like any other. The one
// reading that skips the floor is [ExplicitDivision] below, and it is separate
// from this function for exactly that reason.
func Lanes(text string) int {
	most := Items(text)
	for _, reading := range []func(string) int{
		labelledLanes, listMarkers, spelledLanes, distinctPaths,
	} {
		if count := reading(text); count > most {
			most = count
		}
	}
	return most
}

// labelledLanes reads a lane naming scheme: a short letter prefix with a number
// stuck to it, used at least twice — "L1 … L3", "P1/P2/P3", "task1, task2".
//
// The answer is the HIGHEST number in the largest such family rather than how
// many of them appear, because people write the range and not the list: "L1 …
// L3" names three lanes and mentions two labels. The prefix is capped at four
// letters and the number at three digits so that identifiers, versions and
// ports do not turn into lane counts by accident.
func labelledLanes(text string) int {
	families := map[string]struct{ seen, highest int }{}
	for _, tok := range tokenize(strings.ToLower(text)) {
		split := 0
		for split < len(tok.text) && tok.text[split] >= 'a' && tok.text[split] <= 'z' {
			split++
		}
		if split == 0 || split > 4 || split == len(tok.text) || len(tok.text)-split > 3 {
			continue
		}
		number := numeric(tok.text[split:])
		if number < 1 {
			continue
		}
		family := families[tok.text[:split]]
		family.seen++
		if number > family.highest {
			family.highest = number
		}
		families[tok.text[:split]] = family
	}
	most := 0
	for _, family := range families {
		if family.seen >= 2 && family.highest > most {
			most = family.highest
		}
	}
	return most
}

// listMarkers reads a list a person laid out down the page: numbered items at
// the head of a line ("1. ", "2) "), or bullets ("- ", "* ", "• ").
//
// Only the head of a line counts. A "(1)" in running prose is a citation or an
// aside — the research-synthesis task in the bench corpus numbers its four
// requirements that way inside one paragraph, and reading those as four
// independent lanes would be reading a sentence's grammar as a work breakdown.
func listMarkers(text string) int {
	numbered, highest, bullets := 0, 0, 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimLeft(line, " \t>")
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "• ") {
			bullets++
			continue
		}
		digits := 0
		for digits < len(line) && digits < 3 && line[digits] >= '0' && line[digits] <= '9' {
			digits++
		}
		if digits == 0 || digits+1 >= len(line) {
			continue
		}
		if (line[digits] != '.' && line[digits] != ')') || (line[digits+1] != ' ' && line[digits+1] != '\t') {
			continue
		}
		numbered++
		if number := numeric(line[:digits]); number > highest {
			highest = number
		}
	}
	most := 0
	if numbered >= 2 {
		most = highest
	}
	if bullets >= 2 && bullets > most {
		most = bullets
	}
	return most
}

// spelledNumbers is [numberWords] widened to the range a person writes their
// own lanes in. [Items] starts at six because nothing under the floor changes
// its answer; this reading has to see "three lanes" and "four modules" too,
// because those are counts the experiment needs recorded even where they fold.
// It stops at thirty for the reason the narrower table stops at twenty — past
// that people write digits — with thirty itself kept because "thirty chapters"
// is the phrase in #418 that reads as nothing today.
var spelledNumbers = map[string]int{
	"two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7,
	"eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12,
	"thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16,
	"seventeen": 17, "eighteen": 18, "nineteen": 19, "twenty": 20,
	"thirty": 30,
}

// spelledLanes reads a spelled-out count standing before a plural that is not a
// measure — the same adjacency [Items] uses for its own spelled numbers, over a
// wider table.
func spelledLanes(text string) int {
	lower := strings.ToLower(text)
	most := 0
	for word, count := range spelledNumbers {
		if count <= most {
			continue
		}
		for offset := 0; ; {
			idx := wholeWord(lower[offset:], word)
			if idx < 0 {
				break
			}
			idx += offset
			offset = idx + len(word)
			window := lower[offset:]
			if len(window) > windowChars {
				window = window[:windowChars]
			}
			found := false
			for _, w := range tokenize(window) {
				if countable(w.text) {
					found = true
					break
				}
			}
			if found {
				most = count
				break
			}
		}
	}
	return most
}

// distinctPaths reads a list of files. Naming the files IS the division when
// the parts are one file each, and it is the shape a brief takes when somebody
// has already worked out what the parts are.
//
// A path is a run of path characters that ends in a short lowercase extension
// after a name of two characters or more, or that has a named segment after a
// slash. The name-length guard is what keeps "e.g." and "i.e." out, and
// trailing dots and slashes are trimmed first so that a directory at the end of
// a sentence — "any file under `tests/`." — is the directory it is and not a
// path to something called ".".
func distinctPaths(text string) int {
	seen := map[string]bool{}
	for _, run := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return false
		case r == '/' || r == '.' || r == '_' || r == '-':
			return false
		}
		return true
	}) {
		run = strings.Trim(run, "./-_")
		if looksLikePath(run) {
			seen[run] = true
		}
	}
	if len(seen) < 2 {
		return 0
	}
	return len(seen)
}

// looksLikePath reports whether one trimmed run of path characters names a file
// somebody could open.
func looksLikePath(run string) bool {
	if slash := strings.LastIndexByte(run, '/'); slash > 0 && slash+1 < len(run) {
		return true
	}
	dot := strings.LastIndexByte(run, '.')
	if dot < 2 || dot+1 >= len(run) {
		return false
	}
	extension := run[dot+1:]
	if len(extension) > 5 {
		return false
	}
	for i := 0; i < len(extension); i++ {
		if extension[i] < 'a' || extension[i] > 'z' {
			return false
		}
	}
	return true
}

// THE MODE'S ONE LAW: A DIVISION SOMEBODY WROTE OUT IS NOT A PILE TO BE
// COUNTED. [Floor] was calibrated on counts of ITEMS — twelve image files paid,
// four modules and three bugs did not — and every one of those numbers answers
// the question "is there enough here that handing it out beats doing it in
// order?". A person who has written `L1 … L3`, or `lane 1 / lane 2`, or `part A
// / part B` has already answered a different question and answered it
// themselves: they are not reporting how much material there is, they are
// naming the workers. So an explicit division is kept outright, with no floor
// applied, and this is the only reading in the package that skips it.
//
// A PLAIN LIST IS NOT AN EXPLICIT DIVISION, and this is where the law has an
// edge somebody will want to move. A numbered or bulleted list is how people
// write down ITEMS — the bench corpus's own bugfix task numbers its three bugs
// down the page and its codegen task numbers its four modules, and those two
// are the measurement that three and four do not pay. Reading a list marker as
// a named lane would re-decide both of them and throw the calibration away to
// fix a phrasing. So list markers stay in [Lanes], counted against the floor
// like everything else, and only a LABEL — a name attached to a part — buys the
// bypass.

// laneWords are the words people put in front of a designator when they are
// naming who does what rather than what there is. Kept short and singular on
// purpose: this list is the bypass, so every word on it must be one that a
// person writes only when dividing labour.
var laneWords = map[string]bool{
	"lane": true, "part": true, "phase": true, "track": true,
	"batch": true, "stream": true, "worker": true, "agent": true,
}

// ExplicitDivision reports whether the text names its parts as parts: a family
// of at least two labels, either stuck together (`L1`, `L2`, `p3`) or written
// as a lane word and a designator (`lane 1`, `part A`).
//
// Two is the threshold and not six, because two named lanes are two people's
// work however small each one is; the floor is a statement about material and
// this is a statement about labour.
func ExplicitDivision(text string) bool {
	return len(stuckLabels(text)) >= 2 || len(spelledOutLanes(text)) >= 2
}

// stuckLabels collects the largest family of labels written as a prefix with a
// number stuck to it — the `L1 … L3` shape. The family is what makes it a
// division: one `v2` in a sentence is a version, and `L1` beside `L2` is a plan.
func stuckLabels(text string) map[string]bool {
	families := map[string]map[string]bool{}
	largest := map[string]bool{}
	for _, tok := range tokenize(strings.ToLower(text)) {
		split := 0
		for split < len(tok.text) && tok.text[split] >= 'a' && tok.text[split] <= 'z' {
			split++
		}
		if split == 0 || split > 4 || split == len(tok.text) || len(tok.text)-split > 3 {
			continue
		}
		if numeric(tok.text[split:]) < 1 {
			continue
		}
		family := families[tok.text[:split]]
		if family == nil {
			family = map[string]bool{}
			families[tok.text[:split]] = family
		}
		family[tok.text[split:]] = true
		if len(family) > len(largest) {
			largest = family
		}
	}
	return largest
}

// spelledOutLanes collects the distinct designators that follow one lane word —
// the `lane 1 / lane 2` and `part A / part B` shapes. A designator is a number
// or a single letter, and nothing else: "part of the report" names no part.
func spelledOutLanes(text string) map[string]bool {
	byWord := map[string]map[string]bool{}
	largest := map[string]bool{}
	tokens := tokenize(strings.ToLower(text))
	for i := 0; i+1 < len(tokens); i++ {
		word := strings.TrimSuffix(tokens[i].text, "s")
		if !laneWords[word] {
			continue
		}
		designator := tokens[i+1].text
		if numeric(designator) < 1 && !(len(designator) == 1 && designator[0] >= 'a' && designator[0] <= 'z') {
			continue
		}
		family := byWord[word]
		if family == nil {
			family = map[string]bool{}
			byWord[word] = family
		}
		family[designator] = true
		if len(family) > len(largest) {
			largest = family
		}
	}
	return largest
}
