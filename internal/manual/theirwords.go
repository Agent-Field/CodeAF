package manual

// THE MODEL DOES NOT SEARCH THE PERSON'S WORDS (#307).
//
// A lookup on this manual is composed by a model, not typed by the person who
// wanted to know. Asked "who can see my files", `deepseek/deepseek-v4-flash`
// was measured sending "who can see my files privacy file access", "privacy
// files who can see my workspace", "who can see my files when I use aforge" —
// and this corpus is a few dozen short sections, so two words nobody said move
// the ranking off the page. Those were the numbers then; the permissions
// section has since been reworded to carry the privacy and file-access terms,
// so only one of the three still misses and the rows in the test file carry
// stand-ins for the two that reach the page on their own now:
//
//	who can see my files                        → permissions, 2nd of 4
//	who can see my files when I use aforge      → MISS
//	who can view my files in aforge             → MISS
//	visibility of files in the workspace        → MISS
//
// The person's own words are the better question and the harness is holding
// them, so the search reads BOTH: the question as the model composed it, and
// the sentence the person actually typed. Nothing is asked of the model, no
// description tells it to quote anybody, and the retrieval the free tests
// measure on the person's exact words becomes the floor the live surface
// stands on rather than an ideal it drifts away from.
//
// THE RANKING HAS ONE RULE ABOVE SCORE: a section BOTH questions returned comes
// first. Two questions about the same thing agreeing on a section is stronger
// evidence than either one's number, and the numbers themselves are not
// comparable across questions — a longer question scores everything higher — so
// each side's scores are read against its own best before they are compared.

import (
	"math"
	"strings"
)

// theirWordsCap is how long the person's last message may be and still be read
// as a question.
//
// A question is a sentence. A message can also be a pasted document with a
// sentence somewhere in it, and BM25 over a thousand words returns whatever
// shares the most vocabulary with a document rather than what answers anything
// — the exact dilution this file exists to undo, arriving from the other side.
// Cutting a long message down would only choose which half to be diluted by,
// because the sentence sits above the paste as often as below it. So a message
// past this length is not read as a question at all: the model's query stands
// alone, which is what a lookup did before this file existed. No help, and no
// harm either.
//
// Four hundred bytes is about four lines of typing and seven times the longest
// question either set in internal/manual/asked puts, so every question those
// floors measure — and every question these pages were written for — is a long
// way inside it.
const theirWordsCap = 400

// SearchBoth ranks this corpus against the question a model composed AND the
// words the person themselves used, and answers the best k of the two together.
// It is [Corpus.Search] when the person's words are empty or are not a question
// at all, so a caller with nobody to quote loses nothing by asking for both.
func (c *Corpus) SearchBoth(query, personsWords string, k int) []Section {
	c.load()
	asked := c.score(query)
	theirs := c.score(theirQuestion(personsWords))
	switch {
	case theirs == nil:
		return c.Search(query, k)
	case asked == nil:
		return c.sectionsAt(bestOf(rankedBy(theirs), theirs, k))
	}
	mine, yours := bestOf(rankedBy(asked), asked, k), bestOf(rankedBy(theirs), theirs, k)
	together := agreementOf(asked, theirs, mine, yours)
	return c.sectionsAt(bestOf(union(mine, yours), together, k))
}

// union is the two questions' own answers, together and each section once.
//
// What it is handed is each side ALREADY CUT TO k, so what is merged is what
// each question would have been answered from on its own — a section neither
// search returned is a section neither question answered with, and merging both
// scored tails whole would make this a third ranking rather than the two it is
// putting together. The result is section indexes, so the corpus itself is the
// identity and there is nothing to de-duplicate by page and heading afterwards.
func union(mine, yours []int) []int {
	kept := make([]int, 0, len(mine)+len(yours))
	seen := make(map[int]bool, len(mine)+len(yours))
	for _, side := range [][]int{mine, yours} {
		for _, at := range side {
			if !seen[at] {
				seen[at] = true
				kept = append(kept, at)
			}
		}
	}
	return kept
}

// agreementOf is the number the union is ordered by: how well a section answers
// the better of the two questions, plus a whole point when BOTH of them returned
// it.
//
// The point is what puts agreement above score, and it is a whole one because
// each side is read against its own best first — a longer question scores every
// section higher, so the raw numbers are not comparable across the two — which
// leaves every single question's best at exactly 1 and no confidence able to
// outrank a section the two of them agree on.
func agreementOf(asked, theirs []float64, mine, yours []int) []float64 {
	returned := make(map[int]int, len(mine)+len(yours))
	for _, side := range [][]int{mine, yours} {
		for _, at := range side {
			returned[at]++
		}
	}
	askedPeak, theirsPeak := peak(asked), peak(theirs)
	merged := make([]float64, len(asked))
	for i := range merged {
		merged[i] = math.Max(asked[i]/askedPeak, theirs[i]/theirsPeak)
		if returned[i] == 2 {
			merged[i]++
		}
	}
	return merged
}

// peak is the best score one question reached, and never zero: a question that
// touched nothing is answered before this is called, and dividing by its zero
// would make every section of the corpus equally good.
func peak(scores []float64) float64 {
	best := 0.0
	for _, score := range scores {
		if score > best {
			best = score
		}
	}
	if best == 0 {
		return 1
	}
	return best
}

// theirQuestion is the person's last message when it is short enough to be a
// question, and nothing at all when it is longer than one ([theirWordsCap] says
// why nothing rather than a cut).
func theirQuestion(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > theirWordsCap {
		return ""
	}
	return message
}
