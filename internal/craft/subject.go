package craft

import (
	"strings"
	"unicode"
)

// Retrieval finds the workflow whose SHAPE fits a request. That is not the same
// question as whether the workflow is FOR this request, and the difference cost
// a user their trust: a three-step deep-dive workflow distilled from investment
// research on one company was reached for by a request to deep-dive a city's AI
// events, because both are a deep dive that ends in a report. The scorer was
// right — the shapes rhyme — and the answer was wrong, because the workflow's
// subject was never the request's subject. The resident's own lesson from that
// run says it plainly: a way of working bound to a subject may only be invoked
// when the request is actually about that subject, however much the request
// resembles a prior pattern.
//
// So recognition asks a second question, and this file is that question. A
// workflow's own NAME is its claim about what it is for; the words of that name
// that describe a subject rather than a shape of work are what bind it. A bound
// workflow is reached for only when the request says one of them.
//
// The bar is deliberately asymmetric. A craft that is missed costs an ordinary
// plan — the same work, planned from scratch, which is what every request got
// before the shelf existed. A craft that fires on the wrong subject costs the
// person a job about something they did not ask about, under a name they do not
// recognise. Precision is the cheaper error by a wide margin.

// shapeWordList is the frozen vocabulary of words that describe a SHAPE of work
// rather than a subject: the genre of the thing produced, the cadence it is
// produced on, the motions of producing it. They are the words every workflow
// on every shelf shares, which is exactly why a request matching only on them
// has said nothing about what it wants done.
//
// It is a word list and not a learned model on purpose, and the line is worth
// stating: this is vocabulary, like the stop words above it — the closed set of
// English words that name a form rather than a topic — and not know-how. No
// tool, workflow or strategy is encoded here; what the resident learns to DO
// still comes from the loops that learn it.
var shapeWordList = []string{
	"report", "research", "analysis", "analyse", "analyze", "summary",
	"summarize", "digest", "brief", "briefing", "overview", "breakdown",
	"recap", "roundup", "rundown", "snapshot", "review", "update", "note",
	"memo", "deck", "slide", "presentation", "document", "doc", "draft",
	"write", "writeup", "plan", "checklist", "audit", "check", "verify",
	"compare", "comparison", "deep", "dive", "deepdive", "step", "workflow",
	"process", "pipeline", "routine", "job", "task", "run", "daily", "weekly",
	"monthly", "quarterly", "nightly", "morning", "evening",
}

var shapeWords = stemmedSet(shapeWordList)

func stemmedSet(words []string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, word := range words {
		if stemmed := stem(strings.ToLower(word)); stemmed != "" {
			set[stemmed] = true
		}
	}
	return set
}

// Subject is what this workflow is BOUND to, in the same stemmed words a
// request is read into: the words of its own name that describe a subject
// rather than a shape, plus any proper name its description carries. Empty
// means unbound — a workflow written to be pointed at anything, which is the
// ordinary and desirable case for know-how like "release-notes" or a deck
// workflow with a {{topic}} hole in it.
//
// A word the workflow leaves as a HOLE is never its subject. A craft with a
// {{topic}} parameter is written to be pointed at any topic; the topic it runs
// on comes from the request, so the word "topic" in its name or description
// binds nothing.
func Subject(workflow *Workflow) []string {
	if workflow == nil {
		return nil
	}
	holes := holeWords(workflow)
	subject := make([]string, 0, 4)
	seen := map[string]bool{}
	keep := func(word string) {
		if word == "" || holes[word] || shapeWords[word] || seen[word] {
			return
		}
		seen[word] = true
		subject = append(subject, word)
	}
	for _, word := range tokenize(workflow.Name) {
		keep(word)
	}
	// A proper name in the description binds as hard as one in the file name,
	// and it is the only place a craft called something generic can say what it
	// is really about — "deep-dive" with "an investment deep dive on SpaceX"
	// under it is bound to SpaceX and to nothing else.
	for _, word := range properNouns(workflow.Description) {
		keep(word)
	}
	return subject
}

// OnSubject reports whether a request is about what this workflow is about. It
// is the gate in front of every unasked-for run: the scorer says the shape fits,
// this says the subject does, and a craft runs on its own only when both agree.
//
// An unbound workflow is held to a weaker bar rather than to none: the request
// must still say something the workflow CLAIMS — a word from its name or its
// description — instead of matching only the vocabulary of its step briefs,
// which is what every deep-dive in the world has in common. That bar is one a
// person clears by asking for the thing in their own words ("cut the release
// notes", "make me a deck"), and it is exactly the bar the decisive score was
// always documented as standing for.
func OnSubject(workflow *Workflow, request string) bool {
	terms := Subject(workflow)
	if len(terms) == 0 {
		terms = claimWords(workflow)
	}
	if len(terms) == 0 {
		// Nothing claimed and nothing named: there is no subject to be wrong
		// about, and the score is the only evidence there is.
		return true
	}
	asked := map[string]bool{}
	for _, word := range tokenize(request) {
		asked[word] = true
	}
	// One is the bar rather than all of them, because a person says one of the
	// words their workflow is named after, not the whole file name: "how is the
	// SpaceX research looking" is on subject for spacex-investment-research, and
	// "deep dive on Toronto AI events" is not on it at all.
	for _, word := range terms {
		if asked[word] {
			return true
		}
	}
	return false
}

// claimWords is everything an unbound workflow says about itself — its name and
// its description, holes excluded. It is deliberately not the step briefs: a
// brief is written to be EXECUTED and describes motions ("research the topic",
// "write it up"), which is the vocabulary that let a workflow about one subject
// be reached for by a request about another.
func claimWords(workflow *Workflow) []string {
	if workflow == nil {
		return nil
	}
	holes := holeWords(workflow)
	words := make([]string, 0, 12)
	seen := map[string]bool{}
	for _, word := range append(tokenize(workflow.Name),
		tokenize(paramPattern.ReplaceAllString(workflow.Description, " "))...) {
		if word == "" || holes[word] || seen[word] {
			continue
		}
		seen[word] = true
		words = append(words, word)
	}
	return words
}

func holeWords(workflow *Workflow) map[string]bool {
	holes := map[string]bool{}
	for _, param := range workflow.Params {
		for _, word := range tokenize(param.Name) {
			holes[word] = true
		}
	}
	return holes
}

// properNouns reads the names out of a description: words carrying a capital
// where prose would not put one. A word with an interior capital is a name
// whatever its position (SpaceX, GitHub, PyTorch); a leading capital counts only
// away from the start of a sentence, where ordinary English would have left it
// lowercase.
//
// A description written in Title Case would otherwise make every word a name, so
// a description that capitalizes most of its words is read as offering none —
// the honest degradation, since a signal that fires everywhere carries nothing.
func properNouns(description string) []string {
	description = strings.TrimSpace(paramPattern.ReplaceAllString(description, " "))
	if description == "" {
		return nil
	}
	fields := strings.Fields(description)
	names := make([]string, 0, len(fields))
	capitalized, alphabetic := 0, 0
	opening := true
	for _, field := range fields {
		trimmed := strings.Trim(field, `"'“”‘’(),;:!?.`)
		if trimmed == "" {
			continue
		}
		runes := []rune(trimmed)
		if !unicode.IsLetter(runes[0]) {
			opening = false
			continue
		}
		alphabetic++
		interior := false
		for _, r := range runes[1:] {
			if unicode.IsUpper(r) {
				interior = true
				break
			}
		}
		leading := unicode.IsUpper(runes[0])
		if leading {
			capitalized++
		}
		if interior || (leading && !opening) {
			for _, word := range tokenize(trimmed) {
				names = append(names, word)
			}
		}
		opening = strings.HasSuffix(field, ".") || strings.HasSuffix(field, "?") ||
			strings.HasSuffix(field, "!") || strings.HasSuffix(field, ":")
	}
	if alphabetic > 0 && capitalized*2 > alphabetic {
		return nil
	}
	return names
}
