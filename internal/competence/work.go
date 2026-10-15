package competence

import (
	"math"
	"strings"
)

// THE KIND OF WORK IS THE ENGINE'S ONE NAME FOR IT.
//
// A task's kind is the lower-case words of its title, in order, capped — the
// key internal/session's task ledger rates models on (taskgrade.go) and the key
// this package groups checked results by. It lives here, and the session reads
// it from here, because two spellings of "which work was this" would drift the
// first time either was tuned, and then a scope on the competence map and a
// class in the ledger would disagree about the same task.

const (
	// WorkWords bounds a kind. A title is already capped where tasks are named;
	// this is the guard for the other doors a node can be named through — a
	// person's own `/task` sentence, a shaper that ran long — so that one
	// over-long name cannot become a class nothing else ever meets.
	WorkWords = 6

	// WorkOverlap is when two kinds are the same kind-shaped work: half the
	// words of the smaller one, or more.
	//
	// HALF, because a name is three words and two of three in common is a
	// genuine family — "tests for the rail" beside "tests for the composer" —
	// while one word in common is a shared verb and nothing else. A threshold
	// that pooled on one word would have every "write" in the project rating
	// one model, which is the exec.leaf mistake arm B paid for.
	WorkOverlap = 0.5
)

// WorkOf is the kind of a piece of work, from its title: lower case, cut at
// anything that is not a letter or a digit, duplicates dropped, capped at
// [WorkWords].
//
// THE WORDS KEEP THEIR ORDER, so the class reads as the name somebody wrote. A
// ledger nobody can read is a ledger nobody checks; the set comparison that
// pools two kinds happens at read time and does not need the key sorted.
func WorkOf(title string) string {
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(title)), func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return false
		}
		return true
	})
	seen := make(map[string]bool, len(fields))
	words := make([]string, 0, WorkWords)
	for _, field := range fields {
		if seen[field] {
			continue
		}
		seen[field] = true
		words = append(words, field)
		if len(words) == WorkWords {
			break
		}
	}
	return strings.Join(words, " ")
}

// WorkMeets reports whether two kinds are the same kind-shaped work: their
// words overlap by [WorkOverlap] of the smaller one or more.
//
// The smaller side is the denominator on purpose. A three-word name that is
// wholly inside a six-word one is the same work described at two lengths, and
// measuring against the longer side would call that a stranger.
func WorkMeets(one, other string) bool {
	first, second := workWordSet(one), workWordSet(other)
	if len(first) == 0 || len(second) == 0 {
		return false
	}
	if len(first) > len(second) {
		first, second = second, first
	}
	shared := 0
	for word := range first {
		if second[word] {
			shared++
		}
	}
	return float64(shared) >= math.Ceil(WorkOverlap*float64(len(first)))
}

func workWordSet(kind string) map[string]bool {
	words := strings.Fields(kind)
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	return set
}
