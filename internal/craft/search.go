package craft

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Retrieval is what makes learned know-how reachable. "Make me a presentation
// about the Q3 numbers" has to find the presentation craft without the user
// knowing that a workflow by that name exists — otherwise every workflow the
// distiller writes is a file nobody ever asks for again.
//
// The scorer is a local Okapi BM25 over name, description and step briefs,
// the same shape the embedded manual carries and for the same reason: the
// corpus is a few dozen short documents on disk, and opening a database to
// rank them would be a heavier dependency than the ranking. The manual's copy
// is not imported — it indexes a fixed corpus compiled into the binary, this
// one indexes files that change every time the distiller learns something.

const (
	// DefaultMatches is how many crafts a request is offered when the caller
	// does not say. Three is a choice; more is a menu, and a menu is what the
	// resident is supposed to spare the user.
	DefaultMatches = 3
	// MatchFloor is the score below which a match is not a match. BM25 hands a
	// small positive score to any shared word, so without a floor an unrelated
	// request would still "find" whichever craft happens to be shortest — the
	// floor is what makes a miss read as a miss instead of a bad suggestion.
	MatchFloor = 1.0
	// nameWeight is how many times a word in the workflow's name counts against
	// a word in a brief. A name is the craft's own claim about what it is for,
	// so a request that uses it is asking for that craft by name.
	nameWeight = 4
	// descriptionWeight sits between the name and the briefs: the description
	// is written to be matched, the briefs are written to be executed and only
	// mention the subject in passing.
	descriptionWeight = 2

	// The ordinary Okapi parameters, untuned. The corpus is small and short;
	// these are the defaults and the retrieval they give is already exact on
	// the requests the crafts were distilled from.
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Scored is one candidate craft and how well it answers the request.
type Scored struct {
	Summary
	Score float64
}

// Match ranks the repository's workflows against a request in the user's own
// words, best first. A file that will not parse is not a candidate; a request
// that matches nothing returns nothing, which is the answer that lets the
// caller do the work from scratch instead of forcing a craft onto it.
func (r *Repo) Match(request string, k int) []Scored {
	if k <= 0 {
		k = DefaultMatches
	}
	words := tokenize(request)
	if len(words) == 0 {
		return nil
	}
	documents := r.corpus()
	if len(documents) == 0 {
		return nil
	}

	total := float64(len(documents))
	average := 0.0
	for _, document := range documents {
		average += document.length
	}
	average /= total

	seen := map[string]bool{}
	scores := make([]float64, len(documents))
	for _, word := range words {
		if seen[word] {
			continue
		}
		seen[word] = true
		carrying := 0.0
		for _, document := range documents {
			if document.terms[word] > 0 {
				carrying++
			}
		}
		if carrying == 0 {
			continue
		}
		idf := math.Log(1 + (total-carrying+0.5)/(carrying+0.5))
		for i, document := range documents {
			frequency := float64(document.terms[word])
			if frequency == 0 {
				continue
			}
			norm := 1 - bm25B + bm25B*document.length/average
			scores[i] += idf * frequency * (bm25K1 + 1) / (frequency + bm25K1*norm)
		}
	}

	ranked := make([]Scored, 0, len(documents))
	for i, score := range scores {
		if score < MatchFloor {
			continue
		}
		ranked = append(ranked, Scored{Summary: documents[i].summary, Score: score})
	}
	sort.SliceStable(ranked, func(a, b int) bool {
		if ranked[a].Score != ranked[b].Score {
			return ranked[a].Score > ranked[b].Score
		}
		return ranked[a].Name < ranked[b].Name
	})
	if len(ranked) > k {
		ranked = ranked[:k]
	}
	return ranked
}

type document struct {
	summary Summary
	terms   map[string]int
	length  float64
}

// corpus reads and indexes the workflows on disk on every call. There is no
// cache because the distiller writes to this directory while the resident is
// running, and a stale index would answer with a craft that no longer says
// what it used to; a few dozen small files is a read the user cannot feel.
func (r *Repo) corpus() []document {
	entries, err := os.ReadDir(filepath.Join(r.dir, WorkflowDir))
	if err != nil {
		return nil
	}
	documents := make([]document, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		path := WorkflowDir + "/" + entry.Name()
		data, err := os.ReadFile(filepath.Join(r.dir, path))
		if err != nil {
			continue
		}
		w, err := Parse(data)
		if err != nil {
			continue
		}
		next := document{
			summary: Summary{
				Name:        strings.TrimSuffix(entry.Name(), ".yaml"),
				Description: w.Description,
			},
			terms: map[string]int{},
		}
		if line, err := r.git("log", "-1", "--format=%H%x1f%aI", "--", path); err == nil {
			fields := strings.Split(strings.TrimSpace(line), "\x1f")
			next.summary.Commit = fields[0]
			if len(fields) > 1 {
				next.summary.When, _ = time.Parse(time.RFC3339, fields[1])
			}
		}
		count := func(text string, weight int) {
			for _, word := range tokenize(text) {
				next.terms[word] += weight
				next.length += float64(weight)
			}
		}
		count(strings.ReplaceAll(next.summary.Name, "-", " "), nameWeight)
		count(w.Description, descriptionWeight)
		for _, step := range w.Steps {
			count(step.Brief, 1)
			count(strings.ReplaceAll(step.ID, "-", " "), 1)
		}
		documents = append(documents, next)
	}
	return documents
}

// stopWords are the function words that carry no reference. BM25 already
// discounts a word that is in every document; dropping these keeps a short
// request from being ranked mostly on its grammar, and keeps "make me a thing"
// from reaching the floor on its articles alone.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "to": true, "in": true,
	"on": true, "at": true, "by": true, "for": true, "and": true, "or": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"it": true, "its": true, "that": true, "this": true, "these": true,
	"those": true, "there": true, "with": true, "as": true, "from": true,
	"i": true, "me": true, "my": true, "we": true, "our": true, "you": true,
	"your": true, "do": true, "does": true, "did": true, "will": true,
	"would": true, "should": true, "could": true, "can": true, "has": true,
	"have": true, "had": true, "but": true, "so": true, "if": true,
	"then": true, "than": true, "what": true, "which": true, "who": true,
	"why": true, "how": true, "please": true, "make": true, "get": true,
	"want": true, "need": true, "give": true, "some": true, "any": true,
	"about": true, "into": true, "over": true, "out": true, "up": true,
}

func tokenize(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	words := make([]string, 0, len(fields))
	for _, field := range fields {
		if stopWords[field] {
			continue
		}
		if word := stem(field); word != "" {
			words = append(words, word)
		}
	}
	return words
}

// stem is the smallest reduction that makes the words a request uses meet the
// words a craft was written with: plurals, gerunds and past tenses, plus the
// doubled consonant English adds before them. It is not a linguistic claim —
// it is the difference between "presentations" and a craft called
// "presentation".
func stem(word string) string {
	switch {
	case len(word) > 4 && strings.HasSuffix(word, "ies"):
		return word[:len(word)-3] + "y"
	case strings.HasSuffix(word, "ss") || strings.HasSuffix(word, "us"):
		return word
	case len(word) > 5 && strings.HasSuffix(word, "ing"):
		return undouble(word[:len(word)-3])
	case len(word) > 4 && strings.HasSuffix(word, "ed"):
		return undouble(word[:len(word)-2])
	case len(word) > 3 && strings.HasSuffix(word, "s"):
		return word[:len(word)-1]
	}
	return word
}

func undouble(word string) string {
	if len(word) > 3 && word[len(word)-1] == word[len(word)-2] {
		return word[:len(word)-1]
	}
	return word
}
