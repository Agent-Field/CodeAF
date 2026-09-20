package fuzzy

import (
	"strings"
	"testing"
)

// terms is [Terms] for a one-word query, spelled out for the table-driven
// tests below.
func terms(query string) []Term { return Terms(query) }

// THE NUCLEO OPTIMALITY REPRO. fzf's single-matrix approximation answers "foo"
// against "xf foo" with the span "xf_oo" — f at 1, then a gap, then the two
// o's — because its gap state and its consecutive-bonus state share one
// matrix and the wrong one wins the tie. The two-matrix program finds
// "x__foo": the f after the space is worth a white boundary (10, doubled for
// the first character) and its o's run consecutive carrying that bonus, for
// 36 + 26 + 26 = 88, while fzf's span is worth 36 − 4 + 26 = 58 by the same
// constants. This test is the reason the port is nucleo's and not fzf's.
func TestTheOptimalityReproFooAgainstXfFoo(t *testing.T) {
	got, ok := Score("xf foo", terms("foo"))
	if !ok || got != 88 {
		t.Fatalf("Score(\"xf foo\", \"foo\") = %d, %v; want 88, true", got, ok)
	}
	// And the same constants under fzf's chosen span score strictly less,
	// which is what "not optimal under its own scoring" means.
	if got <= 64 {
		t.Fatalf("the optimum %d did not clear fzf's span (64)", got)
	}
}

// THE GAP PRICE, exactly. A one-character gap costs start (3) and nothing
// more — the extension is charged for each additional skipped character, not
// for the first. This pins the off-by-one in the gap matrix: the fresh-gap arm
// reads the diagonal TWO columns back, one further left than the consecutive
// diagonal, and a loop that reads only one column back overcharges every
// short gap.
func TestAGapOfOneCostsStartAndNothingMore(t *testing.T) {
	// a at the white boundary (16 + 2·10), one skipped character, then b with
	// no bonus of its own: 36 − 3 + 16.
	if got, _ := Score("axb", terms("ab")); got != 49 {
		t.Fatalf("Score(\"axb\", \"ab\") = %d; want 49", got)
	}
	// Two skipped characters add one extension: 36 − 3 − 1 + 16.
	if got, _ := Score("axxb", terms("ab")); got != 48 {
		t.Fatalf("Score(\"axxb\", \"ab\") = %d; want 48", got)
	}
}

// RANKING LAW: the rungs. A prefix outranks a scattered subsequence — "can"
// at the head of "cancel" collects the doubled boundary on its first
// character and consecutive carries after it, while the same letters inside
// "american" start mid-word and carry only the consecutive floor.
func TestAPrefixBeatsAScatteredMatch(t *testing.T) {
	prefix, _ := Score("cancel", terms("can"))
	scattered, _ := Score("american", terms("can"))
	if prefix != 88 || scattered != 56 {
		t.Fatalf("cancel = %d, american = %d; want 88 and 56", prefix, scattered)
	}
}

// RANKING LAW: consecutive beats gapped, and the floor is 4. Two letters
// side by side mid-word score 16 + (16 + 4): the second character carries at
// least [bonusConsecutive] even when its own bonus is zero, which is what
// keeps a tight run ahead of the same letters with a hole between them.
func TestAConsecutiveChunkCarriesTheFloor(t *testing.T) {
	if got, _ := Score("zab", terms("ab")); got != 36 {
		t.Fatalf("Score(\"zab\", \"ab\") = %d; want 36", got)
	}
	if got, _ := Score("zaxb", terms("ab")); got != 29 {
		t.Fatalf("Score(\"zaxb\", \"ab\") = %d; want 29", got)
	}
}

// RANKING LAW: hyphenated initials over their unhyphenated twin. "ff" over
// "fuzzy-finder" pays one gap of five characters (3 + 4) but lands its second
// f right after the hyphen — a word boundary, 8, since '-' is a non-word
// byte and not one of the delimiters "/,:;|"; over "fuzzyfinder" the gap is
// one shorter and the landing is mid-word (0). The boundary is worth more
// than the shorter gap: 53 against 46.
func TestInitialsRankTheHyphenatedWordOverItsTwin(t *testing.T) {
	hyphen, _ := Score("fuzzy-finder", terms("ff"))
	plain, _ := Score("fuzzyfinder", terms("ff"))
	if hyphen != 53 || plain != 46 {
		t.Fatalf("fuzzy-finder = %d, fuzzyfinder = %d; want 53 and 46", hyphen, plain)
	}
	if hyphen <= plain {
		t.Fatal("the hyphenated word must rank above its unhyphenated twin")
	}
}

// RANKING LAW: a boundary match survives a long gap, and past it the gap
// outbills the bonus. The white bonus is 10 and a gap of g skipped characters
// costs 3 + (g−1), so a 'b' eight characters past its 'a', opening a word,
// still holds a lead over the same letters one hole apart mid-word (32
// against 29). The crossover the constants pick: past eleven skipped
// characters the gap costs more than the bonus pays, and at twelve the plain
// one-hole match wins. fzf frames this same calibration as "the bonus is
// cancelled when the gap between the acronyms grows over 8 characters" — the
// exact seat of the crossover depends on the shape of the two matches being
// compared; what the test pins is that the seat exists and sits where the
// constants put it.
func TestABoundaryMatchSurvivesALongGapAndDiesPastIt(t *testing.T) {
	rival, _ := Score("zaxb", terms("ab"))
	if rival != 29 {
		t.Fatalf("the rival scored %d; want 29", rival)
	}
	survives, _ := Score("za"+strings.Repeat("x", 7)+" b", terms("ab"))
	if survives != 32 || survives <= rival {
		t.Fatalf("the 8-gap boundary match scored %d; want 32, above the rival's 29", survives)
	}
	dies, _ := Score("za"+strings.Repeat("x", 11)+" b", terms("ab"))
	if dies != 28 || dies >= rival {
		t.Fatalf("the 12-gap boundary match scored %d; want 28, below the rival's 29", dies)
	}
}

// SMART-CASE, BOTH DIRECTIONS. A word typed lowercase matches any casing; a
// word typed with an uppercase letter in it must be found as typed.
func TestSmartCaseBothWays(t *testing.T) {
	if _, ok := Score("DeepSeek", terms("ds")); !ok {
		t.Fatal("a lowercase word must match uppercase text")
	}
	if _, ok := Score("deepseek", terms("DS")); ok {
		t.Fatal("an uppercase word must not match lowercase text")
	}
	if _, ok := Score("DeepSeek", terms("DeepSeek")); !ok {
		t.Fatal("an uppercase word must match the text that carries it")
	}
}

// THE FOLD KEEPS THE CLASSES. Matching case-insensitively must not flatten
// the haystack before the bonus model reads it: a camelCase turn inside the
// text is worth its bonus to a lowercase query. "fb" against "fooBar" buys
// the camel turn on the B (5) that "foobar" cannot offer.
func TestTheFoldKeepsTheCamelClasses(t *testing.T) {
	camel, _ := Score("fooBar", terms("fb"))
	plain, _ := Score("foobar", terms("fb"))
	if camel != 53 || plain != 48 {
		t.Fatalf("fooBar = %d, foobar = %d; want 53 and 48", camel, plain)
	}
}

// NON-ASCII: folded once, and its bytes carry no class. A lowercase word
// reaches uppercase accented text through the fold, and a non-ASCII byte
// between two letters grants no boundary bonus — the '·' in "a·b" is not the
// '-' in "a-b", though a '-' would be worth 9 to the b that follows it.
func TestNonAsciiFoldsOnceAndCarriesNoClass(t *testing.T) {
	if _, ok := Score("Élan Vital", terms("élan")); !ok {
		t.Fatal("a lowercase word must reach accented text through the fold")
	}
	dot, _ := Score("a·b", terms("ab"))
	dash, _ := Score("a-b", terms("ab"))
	if dot >= dash {
		t.Fatalf("\"a·b\" (%d) must not outrank \"a-b\" (%d): non-ASCII bytes carry no class", dot, dash)
	}
}

// MULTI-TERM IS AN AND, summed. Every word has to match; the order of the
// words is nothing; a missing word empties the result.
func TestEveryTermMustMatch(t *testing.T) {
	hay := "deepseek/deepseek-v4-flash"
	if got, ok := Score(hay, terms("ds v4")); !ok || got == 0 {
		t.Fatalf("\"ds v4\" over %q = %d, %v; want a positive sum", hay, got, ok)
	}
	if got, ok := Score(hay, Terms("ds v4")); !ok || got != scoreOf(t, hay, "v4 ds") {
		t.Fatalf("word order changed the score: %d against %d", got, scoreOf(t, hay, "v4 ds"))
	}
	if _, ok := Score(hay, terms("ds zzz")); ok {
		t.Fatal("a word nothing carries must empty the result")
	}
}

// scoreOf is Score with the failure turned into a fatal, for callers that
// have already asserted the match.
func scoreOf(t *testing.T, hay, query string) int {
	t.Helper()
	got, ok := Score(hay, terms(query))
	if !ok {
		t.Fatalf("%q did not match %q", query, hay)
	}
	return got
}

// THE EMPTY QUERY IS NOT A FILTER. No terms, everything matches at zero.
func TestAnEmptyQueryMatchesEverything(t *testing.T) {
	if got := terms(""); got != nil {
		t.Fatalf("Terms(\"\") = %v; want nil", got)
	}
	if got, ok := Score("anything at all", nil); !ok || got != 0 {
		t.Fatalf("Score(no terms) = %d, %v; want 0, true", got, ok)
	}
	if got, ok := ScoreFields(nil, nil); !ok || got != 0 {
		t.Fatalf("ScoreFields(no terms) = %d, %v; want 0, true", got, ok)
	}
}

// NO MATCH, FAST. A word whose first character never appears is refused by
// the walk, in one pass, before any matrix is built — and a word whose first
// character appears but whose rest does not follow it is refused by the same
// walk.
func TestNoMatchRefusedFast(t *testing.T) {
	if _, ok := Score("banana", terms("zzz")); ok {
		t.Fatal("a first character that never appears must not match")
	}
	if _, ok := Score("banana", terms("anez")); ok {
		t.Fatal("a word the text cannot complete in order must not match")
	}
	if _, ok := Score("short", terms("longer than the text")); ok {
		t.Fatal("a word longer than the text must not match")
	}
}

// A TERM PAST THE GUARD STILL ANSWERS. A thousand-plus character word is not
// something anybody types, but the walk's subsequence answer stands for it at
// score zero rather than a refusal that would misreport the row.
func TestPastTheNeedleGuardTheWalkStillAnswers(t *testing.T) {
	long := strings.Repeat("a", 1001)
	if got, ok := Score(strings.Repeat("a", 1200), terms(long)); !ok || got != 0 {
		t.Fatalf("a matched over-long term = %d, %v; want 0, true", got, ok)
	}
	if _, ok := Score(strings.Repeat("b", 1200), terms(long)); ok {
		t.Fatal("an over-long term the text cannot complete in order must not match")
	}
}

// PER TERM, THE BEST FIELD WINS. A row that answers in several fields is
// found by whichever field carries the word — the value a settings row holds
// is as searchable as its label — and the total is the sum over the terms,
// each taken from its own best field.
func TestScoreFieldsTakesTheBestFieldPerTerm(t *testing.T) {
	fields := []string{"ask before running", "tools.approvalMode", "what happens when the model asks", "yolo"}
	if _, ok := ScoreFields(fields, terms("yolo")); !ok {
		t.Fatal("the value field must carry the match")
	}
	got, ok := ScoreFields(fields, terms("ask yolo"))
	if !ok || got == 0 {
		t.Fatalf("\"ask yolo\" = %d, %v; want the sum of one term off the label and one off the value", got, ok)
	}
	if _, ok := ScoreFields(fields, terms("ask nowhere")); ok {
		t.Fatal("a term no field carries must empty the result")
	}
	if _, ok := ScoreFields(nil, terms("ask")); ok {
		t.Fatal("no fields must match nothing but the empty query")
	}
}

// THE SLAB IS THE SAME ANSWER EVERY TIME. Pooled buffers must not leak state
// between calls: the same inputs score the same in any order, and a big field
// followed by a small one is not influenced by the big one's leftovers.
func TestTheSlabCarriesNoStateBetweenCalls(t *testing.T) {
	first, _ := Score("claude-sonnet-4.5", terms("sonnet"))
	for i := 0; i < 3; i++ {
		Score(strings.Repeat("x y z ", 400), terms("xyz"))
		if again, _ := Score("claude-sonnet-4.5", terms("sonnet")); again != first {
			t.Fatalf("score changed across calls: %d then %d", first, again)
		}
	}
}
