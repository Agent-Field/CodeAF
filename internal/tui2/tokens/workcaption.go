package tokens

import (
	"math/rand/v2"
	"slices"
	"time"
)

// WorkCaptionWidth reserves the entire phrase, not just today's visible words.
const WorkCaptionWidth = 28

// Each action accepts only grammatically compatible objects. The mismatch is
// semantic (an engineering verb meeting a human idea), never broken grammar.
var workCaptionRecipes = []struct {
	action  string
	objects []string
}{
	{"Compiling", []string{"curiosity", "a hunch", "possibilities", "common sense", "fresh perspectives"}},
	{"Debugging", []string{"intuition", "assumptions", "reality", "optimism", "the obvious", "a hunch"}},
	{"Refactoring", []string{"chaos", "the obvious", "expectations", "possibilities", "spaghetti", "common sense"}},
	{"Linting", []string{"assumptions", "optimism", "common sense", "reality", "possibilities"}},
	{"Rebasing", []string{"reality", "expectations", "the universe", "optimism", "assumptions"}},
	{"Caching", []string{"inspiration", "a hunch", "possibilities", "curiosity"}},
	{"Resolving", []string{"plot holes", "loose ends", "a paradox", "the ambiguity"}},
	{"Negotiating", []string{"with null", "edge cases", "with entropy", "with semicolons"}},
	{"Befriending", []string{"the compiler", "uncertainty", "legacy code", "edge cases"}},
	{"Consulting", []string{"rubber ducks", "the logs", "future me", "the compiler"}},
	{"Blaming", []string{"the cache", "cosmic rays", "yesterday", "the semicolon"}},
	{"Coercing", []string{"common sense", "the universe", "reality", "order"}},
	{"Optimizing", []string{"optimism", "serendipity", "curiosity", "a hunch"}},
	{"Indexing", []string{"the unknown", "possibilities", "curiosity", "loose ends"}},
	{"Untangling", []string{"spaghetti", "the plot", "assumptions", "possibilities"}},
	{"Awaiting", []string{"enlightenment", "inspiration", "plot twists", "serendipity"}},
}

// nextWorkCaption runs only at an operation boundary. Enumerating eligible
// combinations avoids an unbounded random retry loop as recent history grows.
func nextWorkCaption(recent []string) string {
	var choices []string
	for _, recipe := range workCaptionRecipes {
		for _, object := range recipe.objects {
			phrase := recipe.action + " " + object
			if !slices.Contains(recent, phrase) {
				choices = append(choices, phrase)
			}
		}
	}
	return choices[rand.IntN(len(choices))]
}

// DecodeWorkCaption sends one quiet decoding ripple through ASCII letters.
// Each letter spends 160 ms as two symbols, then resolves for 80 ms before
// the next letter starts. A readable pause separates passes. Other scripts,
// spaces and punctuation are preserved, so every replacement is one cell.
func DecodeWorkCaption(text string, elapsed time.Duration) string {
	const step = 240 * time.Millisecond
	const pause = 1800 * time.Millisecond
	letters := 0
	for i := 0; i < len(text); i++ {
		if captionLetter(text[i]) {
			letters++
		}
	}
	if letters == 0 || elapsed < 0 {
		return text
	}
	phase := elapsed
	if phase >= pause+time.Duration(letters)*step {
		return text
	}
	if phase < pause {
		return text
	}
	phase -= pause
	if phase%step >= 160*time.Millisecond {
		return text
	}
	target := int(phase / step)
	for i := 0; i < len(text); i++ {
		if !captionLetter(text[i]) {
			continue
		}
		if target == 0 {
			const symbols = "01/+=:"
			symbol := symbols[(i+int(phase/(80*time.Millisecond)))%len(symbols)]
			return text[:i] + string(symbol) + text[i+1:]
		}
		target--
	}
	return text
}

func captionLetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
