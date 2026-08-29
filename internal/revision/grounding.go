package revision

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// ── what a review's finding is weighed against ───────────────────────────────
//
// A gap is what the person asked for and did not get. The rule that decides
// whether a review may buy work over one has always been a provenance check —
// are the words it claims to be a failure of somebody's but its own — and for a
// long time it asked that question of ONE piece of text, in ONE way: is this
// finding a contiguous verbatim run of characters inside the request.
//
// Measured over ten headless runs, that question refuses the right answer more
// often than the wrong one. Seven of ten refused findings were quotations of the
// request with a middle skipped; two more quoted the working method, which one
// door of the rule already accepted and the other had never been told about.
// Every one of them named work the run had genuinely promised and not done, and
// each refusal delivered the shortfall as done. See docs/design/gate/SETTLEMENT.md.
//
// GROUNDS ARE THE PROMISES THIS RUN MADE BEFORE IT BEGAN WORKING. Three things
// answer to that and nothing else does: the person's own request, the working
// method the leaf was held to, and the compiled plan's stopping criterion. All
// three were fixed before a word was produced, so none of them can have moved in
// response to what the work turned out to be — which is the whole property the
// invariant is protecting. The compiled goal, the working decisions and the last
// round's output are aforge talking to aforge, and a finding that can only quote
// those is a preference rather than a failure.
//
// One value, read by both doors. The revision round and the extension used to
// weigh a finding against different ground sets, which is how a run could pay
// for a repair against a standard and then be told the same standard was an
// invention.
type Grounds struct {
	// Intent is the person's verbatim request.
	Intent string
	// Method is the working method this kind of work was held to, written
	// before anything was produced.
	Method string
	// Done is the compiled plan's stopping criterion: what this work promised
	// to produce and the checks that settle it. It is structured, and it is the
	// same structure the mechanical half of the gate emits its citations FROM
	// (see MissingProduces) — so a gate that named a promised output and an
	// invariant that could not see the promise were two components disagreeing
	// about one fact.
	Done plan.Done
}

// texts is every ground as one piece of readable text, which is the form both
// the quotation door and the symbol door read. The criterion renders through
// its own Sentence, so the plan's promises are quoted in the words the plan
// wrote them in rather than in a second rendering owned by this file.
func (g Grounds) texts() []string {
	texts := make([]string, 0, 3)
	for _, text := range []string{g.Intent, g.Method, g.Done.Sentence()} {
		if text = strings.TrimSpace(text); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

// Empty reports that this run promised nothing anybody wrote down. A finding
// weighed against nothing is refused, which is the fail-safe direction: the
// rule bounds new work, and a bound that admits everything when it knows
// nothing is not a bound.
func (g Grounds) Empty() bool { return len(g.texts()) == 0 }

// groundIndex is the grounds read once for a whole citation list. A gap naming
// five files would otherwise re-scan every ground five times, and the answers
// cannot differ between elements.
type groundIndex struct {
	// keyed is each ground's text, whitespace-normalised, for the quotation door.
	keyed []string
	// named is the files each ground names, for the file-identity door.
	named [][]string
	// symbols is every distinctively-spelled name any ground uses, lowercased,
	// for the entailment door.
	symbols map[string]bool
}

func (g Grounds) index() groundIndex {
	index := groundIndex{symbols: map[string]bool{}}
	for _, text := range g.texts() {
		key := citationKey(text)
		if key == "" {
			continue
		}
		index.keyed = append(index.keyed, key)
		index.named = append(index.named, NamedFiles(text))
		for _, symbol := range symbolsIn(text) {
			index.symbols[symbol] = true
		}
	}
	return index
}

// admitGapCitations is the citation invariant's core, and the one door every
// reader of it goes through. A gap is admitted when EVERY citation it carries is
// grounded in something nobody in this system wrote for itself after seeing the
// work: see Grounds.
//
// Every citation, and not merely one of them, because a list that smuggles an
// invented requirement in among four real ones is still an invention, and the
// round it would buy is a round against a standard the run wrote for itself.
//
// A citation is grounded three ways, each on its own terms, and each of the
// three exists because the one before it was measured refusing real work.
func admitGapCitations(citations []string, grounds Grounds) string {
	citations = trimmedCitations(citations)
	if len(citations) == 0 {
		return "the review could not point at anything in the request that is missing"
	}
	index := grounds.index()
	for _, citation := range citations {
		if !citationGrounded(citation, index) {
			return "what the review asked for next is not in the request"
		}
	}
	return ""
}

// citationGrounded applies the three doors to one citation, cheapest first.
func citationGrounded(citation string, index groundIndex) bool {
	if quotationGrounded(citation, index.keyed) {
		return true
	}
	if cited, ok := citedFile(citation); ok {
		for _, files := range index.named {
			for _, file := range files {
				if namesSameFile(cited, file) {
					return true
				}
			}
		}
		// A citation that IS a file name is judged as a file name and nothing
		// else. Falling through to the symbol door would ground "src/other.go"
		// against a request that merely mentions "src/", which is the exact
		// inference the file half is written to forbid.
		return false
	}
	return symbolsGrounded(citation, index.symbols)
}

// elision matches the mark a quotation uses where it skips a middle: three or
// more dots, or the single ellipsis character, in any run.
//
// It is a structural mark and not a word, which is what keeps this a structural
// test. A model asked for a verbatim span of a long request answers the way any
// person quoting a long request answers — it quotes the two clauses that matter
// and elides between them — and for as long as the rule read the result as one
// token of prose, an honest quotation of the person's own sentence grounded
// against nothing at all. Seven of the ten findings the DeepSWE sweep refused
// were exactly this, including one whose two halves were both verbatim and
// eleven words apart.
var elision = regexp.MustCompile(`\s*(?:\.{3,}|…)+\s*`)

// quotationGrounded reads a citation as what it is: a QUOTATION, which is a
// sequence of spans rather than a single span. Every segment either side of an
// elision must be a verbatim run of some ground, and at least one segment must
// carry something, so a citation made of nothing but ellipses grounds against
// nothing.
//
// Whitespace is normalised on both sides and nothing else is: a model that
// re-wraps a quoted line has still quoted it, and a model that invents a
// requirement has still invented it. A citation with no elision in it is one
// segment, and is judged byte for byte as it always was.
//
// Segments are grounded independently rather than against one ground each,
// because a review reading a request beside a working method may legitimately
// quote both in one breath, and a rule that demanded one source would refuse
// the more careful of the two answers.
func quotationGrounded(citation string, keyed []string) bool {
	quoted := 0
	for _, segment := range elision.Split(citation, -1) {
		key := citationKey(segment)
		if key == "" {
			continue
		}
		grounded := false
		for _, ground := range keyed {
			if strings.Contains(ground, key) {
				grounded = true
				break
			}
		}
		if !grounded {
			return false
		}
		quoted++
	}
	return quoted > 0
}

// symbolToken is the widest thing that could be a name: letters, digits and the
// punctuation a name is built out of, with no space in it.
var symbolToken = regexp.MustCompile(`[\p{L}\p{N}_][\p{L}\p{N}_./#-]*`)

// symbolsIn lists, lowercased and without repeats, the distinctively-spelled
// names a piece of text uses.
func symbolsIn(text string) []string {
	var symbols []string
	seen := map[string]bool{}
	for _, match := range symbolToken.FindAllString(text, -1) {
		symbol := strings.ToLower(strings.Trim(match, "._-/#"))
		if !symbolShaped(match) || symbol == "" || seen[symbol] {
			continue
		}
		seen[symbol] = true
		symbols = append(symbols, symbol)
	}
	return symbols
}

// symbolShaped is the entailment door's whole line, and where it is drawn is
// the reason the door is safe to open.
//
// A SYMBOL IS A NAME SOMEBODY SPELLED DISTINCTIVELY. It carries an interior
// separator (`dataset.features`, `is_following_end`, `examples/rich_log.py`,
// `#follow-log`), or an interior capital (`RichLog`, `IntersectionObserver`,
// `gridTemplateColumns`), or letters beside digits (`base16`, `HTTP400`). Those
// are recognised by shape, the way NamedFiles and enumerationItem already are
// in this package — never by a list of words, which is the failure FAILSAFE's
// first clause is about.
//
// A PLAIN ENGLISH WORD IS NOT A SYMBOL, and that is the half that keeps invented
// scope out. The measured failure this whole invariant exists for is a gate that
// held a worker to "March refers to any calendar year present in the data",
// bought a round against it, and got back a worse deliverable than the one it
// rejected. "March" is the person's word; "calendar year present in the data" is
// the run's own. Under this rule that citation names no symbol at all, the
// entailment door never opens for it, and it is refused exactly as it was before
// this door existed.
func symbolShaped(token string) bool {
	core := strings.Trim(token, "._-/#")
	if len([]rune(core)) < 2 {
		return false
	}
	interior := []rune(core)
	var letters, digits, lower, interiorUpper, separators int
	for position, run := range interior {
		switch {
		case unicode.IsLetter(run):
			letters++
			if unicode.IsLower(run) {
				lower++
			} else if position > 0 {
				interiorUpper++
			}
		case unicode.IsDigit(run):
			digits++
		default:
			separators++
		}
	}
	if separators > 0 {
		return true
	}
	// An interior capital only names something when the word also has small
	// letters in it: "IMPORTANT" is a person shouting, "RichLog" is a class.
	if interiorUpper > 0 && lower > 0 {
		return true
	}
	return letters > 0 && digits > 0
}

// symbolsGrounded is the entailment door: a finding that quotes nothing and
// names no file is grounded when everything it NAMES is something a ground
// names.
//
// This is what admits "the deliverable does not contain the code that writes
// feature_schema.joblib and validates dataset.features" — a finding that is
// true, that is about exactly what the request asked for, and that quotes not
// one contiguous clause of it. A finding whose subject matter is entirely the
// run's own is refused here as everywhere else: it either names a symbol no
// ground names, or it names none at all, and a citation that names nothing
// distinctive is prose, which is what the quotation door is for.
func symbolsGrounded(citation string, held map[string]bool) bool {
	symbols := symbolsIn(citation)
	if len(symbols) == 0 {
		return false
	}
	for _, symbol := range symbols {
		if !held[symbol] {
			return false
		}
	}
	return true
}
