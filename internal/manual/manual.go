// Package manual is aforge's account of itself, embedded in the binary.
//
// Everything aforge can say about its own capabilities, mechanisms and reasons
// has to come from somewhere. The design docs are the wrong somewhere: they are
// on disk rather than in the binary, they are written to persuade rather than
// to answer, and they go stale the moment a build lands. Improvisation is the
// worse somewhere — a model asked "how does boost work" will always produce a
// fluent answer, and there is no way for the user to tell a remembered one from
// an invented one. So the pages here are the single authoritative source, they
// ship inside the binary, and the completeness tests beside the feature
// registries fail the build when a landed feature is not among them.
package manual

import (
	"embed"
	"fmt"
	"io/fs"
	"math"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode"
)

//go:embed pages/*.md
var pages embed.FS

const (
	// DefaultResults is how many sections one question is answered from. Four
	// is a topic and its neighbours; more is a document, and a model handed a
	// document quotes the wrong half of it.
	DefaultResults = 4
	// SectionBodyCap bounds one section's text where it is rendered for a
	// model. Pages are written to sit well under this; the cap exists so a
	// future long page degrades by truncation rather than by budget.
	SectionBodyCap = 2400
	// titleWeight is how many times a heading's words count against a body
	// word. A page's headings are its index, so a question that uses the words
	// of a heading is asking for that section by name.
	titleWeight = 3

	// bm25K1 and bm25B are the ordinary Okapi parameters. The corpus is a few
	// dozen short sections, so nothing here is tuned: these are the defaults,
	// and the retrieval they give is already exact on the questions the pages
	// were written to answer.
	bm25K1 = 1.2
	bm25B  = 0.75
)

// The store's BM25 is SQLite's FTS5 rank over durable tables, reachable only
// through a database handle. The manual is a fixed, tiny, read-only corpus
// known at compile time, so it carries its own scorer rather than opening a
// store to search ten files that never change.

// Section is one addressable piece of the manual: a heading and the prose under
// it. The preamble of a page — everything above its first heading — is a
// section too, titled by the page's own title.
type Section struct {
	Page  string
	Title string
	Body  string
}

type index struct {
	sections []Section
	// terms[i] is the stemmed term frequency of section i, headings weighted.
	terms   []map[string]int
	lengths []float64
	average float64
	// documents[t] is how many sections contain term t.
	documents map[string]int
	// cues is the manual's distinctive vocabulary, derived from page names and
	// headings. It is what makes a self-question recognizable without a hand
	// list that has to be remembered beside the pages.
	cues map[string]bool
	// pageText is each page whole, for a read that wants the topic entire.
	pageText map[string]string
	order    []string
}

var (
	once  sync.Once
	built *index
)

func load() *index {
	once.Do(func() { built = build() })
	return built
}

func build() *index {
	entries, err := fs.Glob(pages, "pages/*.md")
	if err != nil {
		panic("manual: glob embedded pages: " + err.Error())
	}
	sort.Strings(entries)
	idx := &index{
		documents: map[string]int{},
		cues:      map[string]bool{},
		pageText:  map[string]string{},
	}
	for _, entry := range entries {
		raw, err := pages.ReadFile(entry)
		if err != nil {
			panic("manual: read embedded page: " + err.Error())
		}
		name := strings.TrimSuffix(path.Base(entry), ".md")
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		idx.pageText[name] = strings.TrimSpace(text)
		idx.order = append(idx.order, name)
		for _, word := range tokenize(strings.ReplaceAll(name, "-", " ")) {
			idx.cues[word] = true
		}
		for _, section := range split(name, text) {
			for _, word := range tokenize(section.Title) {
				idx.cues[word] = true
			}
			idx.sections = append(idx.sections, section)
		}
	}
	for word := range cueStopWords {
		delete(idx.cues, stem(word))
	}
	for _, section := range idx.sections {
		counts := map[string]int{}
		length := 0
		for _, word := range tokenize(section.Title) {
			counts[word] += titleWeight
			length += titleWeight
		}
		for _, word := range tokenize(section.Body) {
			counts[word]++
			length++
		}
		for word := range counts {
			idx.documents[word]++
		}
		idx.terms = append(idx.terms, counts)
		idx.lengths = append(idx.lengths, float64(length))
		idx.average += float64(length)
	}
	if len(idx.sections) > 0 {
		idx.average /= float64(len(idx.sections))
	}
	return idx
}

// split cuts one page at its headings. A `# ` line names the page; every `## `
// line opens a section. Deeper headings stay inside the section they belong to,
// because a question is asked at topic granularity, not at paragraph
// granularity.
func split(name, text string) []Section {
	title := strings.ReplaceAll(name, "-", " ")
	sections := make([]Section, 0, 8)
	current := Section{Page: name, Title: title}
	var body strings.Builder
	flush := func() {
		if trimmed := strings.TrimSpace(body.String()); trimmed != "" {
			current.Body = trimmed
			sections = append(sections, current)
		}
		body.Reset()
	}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "# "):
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			flush()
			current = Section{Page: name, Title: title}
		case strings.HasPrefix(line, "## "):
			flush()
			current = Section{Page: name, Title: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
		default:
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	flush()
	return sections
}

// Search ranks the manual against a question. k at or below zero asks for the
// default; the result is ordered best first and is empty only when the question
// shares no word with any page.
func Search(query string, k int) []Section {
	idx := load()
	if k <= 0 {
		k = DefaultResults
	}
	words := tokenize(query)
	if len(words) == 0 || len(idx.sections) == 0 {
		return nil
	}
	total := float64(len(idx.sections))
	scores := make([]float64, len(idx.sections))
	for _, word := range words {
		documents := float64(idx.documents[word])
		if documents == 0 {
			continue
		}
		idf := math.Log(1 + (total-documents+0.5)/(documents+0.5))
		for i, counts := range idx.terms {
			frequency := float64(counts[word])
			if frequency == 0 {
				continue
			}
			norm := 1 - bm25B + bm25B*idx.lengths[i]/idx.average
			scores[i] += idf * frequency * (bm25K1 + 1) / (frequency + bm25K1*norm)
		}
	}
	ranked := make([]int, 0, len(scores))
	for i, score := range scores {
		if score > 0 {
			ranked = append(ranked, i)
		}
	}
	sort.SliceStable(ranked, func(a, b int) bool { return scores[ranked[a]] > scores[ranked[b]] })
	if len(ranked) > k {
		ranked = ranked[:k]
	}
	found := make([]Section, 0, len(ranked))
	for _, i := range ranked {
		found = append(found, idx.sections[i])
	}
	return found
}

// Page returns one whole page by name — "daily-rhythm", not "daily-rhythm.md".
func Page(name string) (string, bool) {
	idx := load()
	text, ok := idx.pageText[strings.TrimSuffix(strings.TrimSpace(name), ".md")]
	return text, ok
}

// Pages lists every page name, in reading order.
func Pages() []string {
	idx := load()
	return append([]string(nil), idx.order...)
}

// Sections exposes the parsed manual for the completeness tests that keep it
// honest as features land.
func Sections() []Section {
	idx := load()
	return append([]Section(nil), idx.sections...)
}

// Render turns sections into the block a model reads. Page and heading stay
// attached so a quoted answer can be traced back to the page that authorized
// it.
func Render(sections []Section) string {
	blocks := make([]string, 0, len(sections))
	for _, section := range sections {
		body := section.Body
		if len(body) > SectionBodyCap {
			body = body[:SectionBodyCap] + "…"
		}
		blocks = append(blocks, fmt.Sprintf("[%s · %s]\n%s", section.Page, section.Title, body))
	}
	return strings.Join(blocks, "\n\n")
}

// Context is the one-call shape both the belt tool and the router's grounding
// path want: search, then render, or nothing at all.
func Context(query string, k int) string {
	return Render(Search(query, k))
}

// Mentions reports whether a term appears anywhere in the manual. The
// completeness tests are written against it, so a feature that lands without a
// page fails the build rather than becoming something aforge improvises about.
func Mentions(term string) bool {
	idx := load()
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}
	for _, name := range idx.order {
		haystack := strings.ToLower(idx.pageText[name] + " " + strings.ReplaceAll(name, "-", " "))
		if strings.Contains(haystack, term) {
			return true
		}
	}
	return false
}

// Cued reports whether a message reaches for the manual's own vocabulary. It is
// half of the head's self-question trigger, and it lives here because the words
// worth recognizing are exactly the words the pages are titled with — a list
// nobody has to maintain twice.
func Cued(message string) bool {
	idx := load()
	for _, word := range tokenize(message) {
		if idx.cues[word] {
			return true
		}
	}
	return false
}

// Cues is the derived vocabulary itself, for tests and for anything that wants
// to see what the trigger will fire on.
func Cues() []string {
	idx := load()
	words := make([]string, 0, len(idx.cues))
	for word := range idx.cues {
		words = append(words, word)
	}
	sort.Strings(words)
	return words
}

// searchStopWords are the function words that carry no reference. Dropping them
// costs nothing — BM25 already discounts a word that is in every section — and
// it keeps a short question from being scored mostly on its grammar.
var searchStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "to": true, "in": true,
	"on": true, "at": true, "by": true, "for": true, "and": true, "or": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"it": true, "its": true, "that": true, "this": true, "these": true,
	"those": true, "there": true, "with": true, "as": true, "from": true,
	"i": true, "me": true, "my": true, "im": true, "s": true, "t": true,
	"do": true, "does": true, "did": true, "will": true, "would": true,
	"should": true, "could": true, "has": true, "have": true, "had": true,
	"but": true, "so": true, "if": true, "then": true, "than": true,
	"what": true, "which": true, "who": true, "why": true, "how": true,
}

// cueStopWords are words the manual's own headings use that would fire the
// self-question trigger on ordinary requests. They stay searchable; they just
// stop being evidence that a message is about aforge itself.
var cueStopWords = map[string]bool{
	"what": true, "when": true, "where": true, "why": true, "how": true,
	"who": true, "which": true, "you": true, "your": true, "yours": true,
	"it": true, "its": true, "the": true, "and": true, "for": true, "with": true,
	"work": true, "works": true, "working": true, "job": true, "jobs": true,
	"task": true, "tasks": true, "thing": true, "things": true, "one": true,
	"ones": true, "all": true, "every": true, "run": true, "runs": true,
	"running": true, "queued": true, "failed": true, "new": true, "get": true,
	"make": true, "made": true, "use": true, "used": true, "ask": true,
	"asks": true, "asked": true, "say": true, "said": true, "want": true,
	"day": true, "days": true, "time": true, "times": true, "out": true,
	"about": true, "into": true, "over": true, "under": true, "than": true,
	"can": true, "does": true, "did": true, "not": true, "never": true,
	"first": true, "last": true, "next": true, "same": true, "own": true,
	"read": true, "reads": true, "write": true, "writes": true, "file": true,
	"files": true, "line": true, "lines": true, "name": true, "names": true,
	"place": true, "places": true, "part": true, "parts": true, "way": true,
	"ways": true, "keep": true, "keeps": true, "stay": true, "stays": true,
	// The control verbs never count as manual cues. A sentence carrying one is
	// about work already underway, and that has its own trigger; letting
	// "cancel" open the manual would put the two arms in each other's way.
	"cancel": true, "stop": true, "pause": true, "resume": true,
	"restart": true, "kill": true, "hold": true, "steer": true,
	"answer": true, "answers": true, "reply": true, "replies": true,
	"open": true, "opens": true, "close": true, "start": true, "starts": true,
	"change": true, "changes": true, "set": true, "sets": true, "put": true,
	// Status vocabulary. "What is happening?" is a read of the board, and the
	// board arm already owns it; a manual cue here would spend a model call on
	// every ordinary status question.
	"happen": true, "happens": true, "happening": true, "going": true,
	"look": true, "looks": true, "see": true, "know": true, "think": true,
	"mean": true, "means": true, "need": true, "needs": true, "give": true,
	"take": true, "come": true, "back": true, "now": true, "here": true,
	"still": true, "long": true, "much": true, "many": true, "more": true,
	"less": true, "good": true, "bad": true, "up": true, "down": true,
}

func tokenize(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	words := make([]string, 0, len(fields))
	for _, field := range fields {
		if searchStopWords[field] {
			continue
		}
		if word := stem(field); word != "" {
			words = append(words, word)
		}
	}
	return words
}

// stem is the smallest reduction that makes the questions people actually ask
// meet the words the pages actually use: plurals, gerunds and past tenses, plus
// the doubled consonant English adds before them. Nothing here is a linguistic
// claim — it is the difference between "why did you ask before cancelling" and
// a page that says "cancel".
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
