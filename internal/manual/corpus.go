package manual

// A CORPUS IS ONE PRODUCT'S ACCOUNT OF ITSELF, and this file is the machinery
// every corpus shares: the split into sections, the scorer, and the derived
// vocabulary that makes a self-question recognizable.
//
// There are two corpora in this binary because there are two products in it.
// The resident (internal/head, internal/tui) is an employee that keeps working
// while nobody is watching; the v3 chat (internal/tui3, internal/session) is a
// conversation you sit in front of. They share a repository and almost nothing
// else — different surfaces, different vocabulary, different laws about what is
// even possible. ONE POOL WOULD ANSWER BOTH QUESTIONS WRONG: a chat asked "what
// can you do" would quote the resident's standing watches, and a resident asked
// the same would quote slash commands it does not have. So each product indexes
// its own folder, and neither can reach the other's pages.
//
// The scoring is deliberately plain — ordinary Okapi BM25 over a few dozen short
// sections. Nothing here is tuned, because a corpus this small is retrieved
// exactly by the words its headings are written with, and a clever ranker would
// be a second thing to keep honest.

import (
	"math"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const (
	// titleWeight is how many times a heading's words count against a body
	// word. A page's headings are its index, so a question that uses the words
	// of a heading is asking for that section by name.
	titleWeight = 3

	// topicWeight is how many times a page's own `# ` title counts in every
	// section of that page. It exists because the title counted almost nowhere:
	// split hands it to the preamble section, and a page whose `# ` line is
	// followed straight by a `## ` line has no preamble — which is every page
	// in chat/. A count found the chat corpus's page titles carried by 0 of its
	// 1029 sections, so a person who named the topic ("the screen is blank",
	// "who can see my files") could only be matched by whichever section
	// happened to repeat the word, and a long page has more sections in which
	// to repeat it by accident. One is enough: the title counts like a body
	// word, which lifts a page that is on topic without letting a long one win
	// a question it does not answer. Measured on plainquestions_test.go's
	// twenty-five, the right page came first 11 times and was among the four
	// sections 17 times before this and 14 and 20 after, with no probe in
	// chat_test.go moved; on the held-out twenty-two, 6/15 before and 7/17
	// after. It is the only part of this change that generalises — a heading
	// written for one question only ever answers that question.
	topicWeight = 1

	// bm25K1 and bm25B are the ordinary Okapi parameters. The corpus is a few
	// dozen short sections, so nothing here is tuned: these are the defaults,
	// and the retrieval they give is already exact on the questions the pages
	// were written to answer.
	//
	// Section LENGTH is the obvious suspect when a plain question misses, and
	// it was measured and cleared. Sweeping bm25B over 0.75, 0.5, 0.3 and 0
	// made the twenty-five worse at every step (11/17 → 10/17 → 7/17 → 7/15),
	// and saturating an over-long section's length at 1.5×, 2×, 3× the corpus
	// mean cost between three and seven of chat_test.go's probes while moving
	// neither the twenty-five nor the held-out set upward; at 4× and above it
	// changes nothing at all. A section far over the ~2000-character page law
	// is a page that needs splitting at its own sub-topics, not a scorer that
	// needs a thumb on it.
	bm25K1 = 1.2
	bm25B  = 0.75
)

// The store's BM25 is SQLite's FTS5 rank over durable tables, reachable only
// through a database handle. A manual is a fixed, tiny, read-only corpus known
// at compile time, so it carries its own scorer rather than opening a store to
// search ten files that never change.

// Section is one addressable piece of a manual: a heading and the prose under
// it. The preamble of a page — everything above its first heading — is a
// section too, titled by the page's own title.
type Section struct {
	Page  string
	Title string
	Body  string
}

// corpusFiles is the narrow file surface a corpus needs. The ordinary Go
// build supplies the Markdown directly, while the shipped build supplies the
// generated compressed archive; keeping that choice outside Corpus makes both
// paths exercise the same indexing and retrieval code.
type corpusFiles interface {
	Glob(pattern string) ([]string, error)
	ReadFile(name string) ([]byte, error)
}

// Corpus is one indexed folder of pages. It is built once, on the first
// question asked of it, and never changes afterwards: the pages are embedded in
// the binary, so a corpus that has been read is a corpus that is already right.
type Corpus struct {
	files corpusFiles
	glob  string

	once     sync.Once
	sections []Section
	// terms[i] is the stemmed term frequency of section i, headings weighted.
	terms   []map[string]int
	lengths []float64
	average float64
	// documents[t] is how many sections contain term t.
	documents map[string]int
	// cues is this corpus's distinctive vocabulary, derived from page names and
	// headings. It is what makes a self-question recognizable without a hand
	// list that has to be remembered beside the pages.
	cues map[string]bool
	// pageText is each page whole, for a read that wants the topic entire.
	pageText map[string]string
	// pageTitle is each page's own `# ` title, tokenized. See topicWeight.
	pageTitle map[string][]string
	order     []string
}

// newCorpus names a folder to index. Nothing is read until the corpus is asked
// a question, so declaring one costs nothing at startup.
func newCorpus(files corpusFiles, glob string) *Corpus {
	return &Corpus{files: files, glob: glob}
}

func (c *Corpus) load() *Corpus {
	c.once.Do(c.build)
	return c
}

func (c *Corpus) build() {
	entries, err := c.files.Glob(c.glob)
	if err != nil {
		panic("manual: glob embedded pages: " + err.Error())
	}
	sort.Strings(entries)
	c.documents = map[string]int{}
	c.cues = map[string]bool{}
	c.pageText = map[string]string{}
	c.pageTitle = map[string][]string{}
	for _, entry := range entries {
		raw, err := c.files.ReadFile(entry)
		if err != nil {
			panic("manual: read embedded page: " + err.Error())
		}
		name := strings.TrimSuffix(path.Base(entry), ".md")
		text := strings.ReplaceAll(string(raw), "\r\n", "\n")
		c.pageText[name] = strings.TrimSpace(text)
		c.order = append(c.order, name)
		for _, word := range tokenize(strings.ReplaceAll(name, "-", " ")) {
			c.cues[word] = true
		}
		c.pageTitle[name] = pageTitle(text)
		for _, section := range split(name, text) {
			for _, word := range tokenize(section.Title) {
				c.cues[word] = true
			}
			c.sections = append(c.sections, section)
		}
	}
	for word := range cueStopWords {
		delete(c.cues, stem(word))
	}
	for _, section := range c.sections {
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
		// Document frequency is counted from the section's own words, before the
		// page title joins them, and each page adds one below. Otherwise a page
		// title would be in as many documents as the page has sections, so
		// splitting one long section in two would move the IDF of that title's
		// words for every question in the corpus — a page's shape is not
		// evidence about its vocabulary.
		for word := range counts {
			c.documents[word]++
		}
		for _, word := range c.pageTitle[section.Page] {
			counts[word] += topicWeight
			length += topicWeight
		}
		c.terms = append(c.terms, counts)
		c.lengths = append(c.lengths, float64(length))
		c.average += float64(length)
	}
	for _, name := range c.order {
		for _, word := range unique(c.pageTitle[name]) {
			c.documents[word]++
		}
	}
	if len(c.sections) > 0 {
		c.average /= float64(len(c.sections))
	}
}

// pageTitle is a page's own `# ` line: its one statement of what the whole page
// is about, in the words somebody would name the topic with. Every section of
// the page carries it, because a question that names the page is asking for the
// page and should not also have to land on whichever section repeats the word.
func pageTitle(text string) []string {
	for len(text) > 0 {
		line, rest, _ := strings.Cut(text, "\n")
		if strings.HasPrefix(line, "# ") {
			return unique(tokenize(line[2:]))
		}
		text = rest
	}
	return nil
}

// unique keeps the first of each word. A title that says a word twice is still
// one statement about the page, and counting it twice in every section would
// make a long title louder than a short one for no reason anybody wrote down.
func unique(words []string) []string {
	if len(words) < 2 {
		return words
	}
	seen := make(map[string]bool, len(words))
	kept := words[:0]
	for _, word := range words {
		if !seen[word] {
			seen[word] = true
			kept = append(kept, word)
		}
	}
	return kept
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

// Search ranks this corpus against a question. k at or below zero asks for the
// default; the result is ordered best first and is empty only when the question
// shares no word with any page.
func (c *Corpus) Search(query string, k int) []Section {
	c.load()
	if k <= 0 {
		k = DefaultResults
	}
	words := tokenize(query)
	if len(words) == 0 || len(c.sections) == 0 {
		return nil
	}
	total := float64(len(c.sections))
	scores := make([]float64, len(c.sections))
	for _, word := range words {
		documents := float64(c.documents[word])
		if documents == 0 {
			continue
		}
		idf := math.Log(1 + (total-documents+0.5)/(documents+0.5))
		for i, counts := range c.terms {
			frequency := float64(counts[word])
			if frequency == 0 {
				continue
			}
			norm := 1 - bm25B + bm25B*c.lengths[i]/c.average
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
		found = append(found, c.sections[i])
	}
	return found
}

// Page returns one whole page by name — "daily-rhythm", not "daily-rhythm.md".
func (c *Corpus) Page(name string) (string, bool) {
	c.load()
	text, ok := c.pageText[strings.TrimSuffix(strings.TrimSpace(name), ".md")]
	return text, ok
}

// Pages lists every page name, in reading order.
func (c *Corpus) Pages() []string {
	c.load()
	return append([]string(nil), c.order...)
}

// Sections exposes the parsed corpus for the completeness tests that keep it
// honest as features land.
func (c *Corpus) Sections() []Section {
	c.load()
	return append([]Section(nil), c.sections...)
}

// Context is the one-call shape a tool wants: search, then render, or nothing
// at all.
func (c *Corpus) Context(query string, k int) string {
	return Render(c.Search(query, k))
}

// Mentions reports whether a term appears anywhere in this corpus. The
// completeness tests are written against it, so a feature that lands without a
// page fails the build rather than becoming something the product improvises
// about.
func (c *Corpus) Mentions(term string) bool {
	c.load()
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return false
	}
	for _, name := range c.order {
		haystack := strings.ToLower(c.pageText[name] + " " + strings.ReplaceAll(name, "-", " "))
		if strings.Contains(haystack, term) {
			return true
		}
	}
	return false
}

// Cued reports whether a message reaches for this corpus's own vocabulary. It
// is half of the self-question trigger, and it lives here because the words
// worth recognizing are exactly the words the pages are titled with — a list
// nobody has to maintain twice.
func (c *Corpus) Cued(message string) bool {
	c.load()
	for _, word := range tokenize(message) {
		if c.cues[word] {
			return true
		}
	}
	return false
}

// Cues is the derived vocabulary itself, for tests and for anything that wants
// to see what the trigger will fire on.
func (c *Corpus) Cues() []string {
	c.load()
	words := make([]string, 0, len(c.cues))
	for word := range c.cues {
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

// cueStopWords are words a manual's own headings use that would fire the
// self-question trigger on ordinary requests. They stay searchable; they just
// stop being evidence that a message is about the product itself.
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
