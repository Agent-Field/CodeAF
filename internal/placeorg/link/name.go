package link

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// wordRe keeps a token the reference namer would keep: a lower-case word of
// at least three letters, digits and hyphens allowed after the first letter.
var wordRe = regexp.MustCompile(`[a-z][a-z0-9-]{2,}`)

// functionWords are the reference stoplist. They are grammar, not subjects,
// so a view is not named "the".
var functionWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "are": true,
	"was": true, "were": true, "been": true, "its": true, "this": true,
	"that": true, "these": true, "those": true, "you": true, "your": true,
}

// nameStop is the name-quality backstop: words that say nothing about what
// the chats are. It is not a list of subjects. A word that fails it is
// skipped; when every word fails, the view is named nameFallback.
var nameStop = map[string]bool{
	"only": true, "just": true, "new": true, "chat": true, "chats": true,
	"group": true, "misc": true, "things": true, "stuff": true, "place": true,
	"general": true, "other": true, "task": true, "tasks": true,
}

// groupName is the first c-TF-IDF word of these chats that qualifies, or
// nameFallback. The reference names a demoted group the same way and does
// not ask a model: the word is already in the chats.
func groupName(corpus *placeorg.Corpus, members []string) string {
	if corpus == nil || len(members) == 0 {
		return nameFallback
	}
	// n is every chat in the corpus, empty ones included, matching the
	// reference IDF. A word in more than 60% of chats is not a name.
	n := len(corpus.Chats)
	if n == 0 {
		return nameFallback
	}
	docs := make([][]string, n)
	df := map[string]int{}
	for i, chat := range corpus.Chats {
		toks := tokens(chat.Text + " " + chat.Title)
		docs[i] = toks
		seen := map[string]bool{}
		for _, w := range toks {
			if seen[w] {
				continue
			}
			seen[w] = true
			df[w]++
		}
	}
	want := map[string]bool{}
	for _, id := range members {
		want[id] = true
	}
	tf := map[string]int{}
	sup := map[string]int{}
	for i, chat := range corpus.Chats {
		if !want[chat.ID] {
			continue
		}
		seen := map[string]bool{}
		for _, w := range docs[i] {
			tf[w]++
			if !seen[w] {
				seen[w] = true
				sup[w]++
			}
		}
	}
	type cand struct {
		word  string
		score float64
	}
	var cands []cand
	limit := 0.6 * float64(n)
	for w, f := range tf {
		if nameStop[w] || sup[w] < 2 || float64(df[w]) > limit {
			continue
		}
		cands = append(cands, cand{word: w, score: float64(f) * math.Log(float64(n)/(1+float64(df[w])))})
	}
	if len(cands) == 0 {
		return nameFallback
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].word < cands[j].word
	})
	return cands[0].word
}

func tokens(text string) []string {
	found := wordRe.FindAllString(strings.ToLower(text), -1)
	out := make([]string, 0, len(found))
	for _, w := range found {
		if functionWords[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}
