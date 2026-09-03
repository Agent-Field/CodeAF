package main

import "strings"

// NO GO ERROR CHAIN REACHES A PERSON.
//
// An error inside this binary is wrapped on every floor it passes, which is
// right for a stack trace and wrong for a sentence somebody reads. The worked
// example is the one that cost this file: a run against a model id that does
// not exist printed
//
//	I couldn't apply that request: splice failed: compile request: compile
//	intent: API error (400): nosuch/model-xyz is not a valid model ID
//
// as the deliverable — three internal package verbs in front of the one fact
// that matters, and nothing at all about what to do next. Every message a
// person reads must carry THE CAUSE and WHAT TO DO, and may not leak a Go type,
// a wrapped chain, a package name or a path from inside the binary.

// machineryVerbs are the wrapping verbs the packages inside this binary put in
// front of a cause.
//
// They are NAMED ONE BY ONE and matched WHOLE, deliberately not guessed at by
// shape: a rule that dropped every short lower-case segment would also drop
// `open /nope/graph.json` and `splice parent "task-1" is closed`, which are the
// only parts of those chains a person can act on.
var machineryVerbs = []string{
	"splice failed",
	"materialize splice",
	"compile request",
	"compile intent",
}

// plainWords is the whole rule, applied to one sentence: drop the machinery
// verbs wherever they stand in the chain, keep everything a person could act
// on, and add the remedy when the fact is one aforge recognises.
func plainWords(sentence string) string {
	fact := terminalCause(sentence)
	if remedy := remedyFor(fact); remedy != "" {
		return fact + "\n" + remedy
	}
	return fact
}

// terminalCause is the same sentence with the machinery taken out of it.
//
// It removes verbs from ANYWHERE in the chain rather than only from the front,
// because the front is usually the one segment written for a person — "I
// couldn't apply that request" — and the machinery hides between that and the
// fact.
func terminalCause(sentence string) string {
	trimmed := strings.TrimSpace(sentence)
	segments := strings.Split(trimmed, ": ")
	kept := make([]string, 0, len(segments))
	for _, segment := range segments {
		if isMachinery(segment) {
			continue
		}
		kept = append(kept, segment)
	}
	// A chain that is machinery all the way down is still better said than
	// swallowed: nothing at all is the one answer a person cannot work with.
	if len(kept) == 0 {
		return trimmed
	}
	return strings.Join(kept, ": ")
}

func isMachinery(segment string) bool {
	lowered := strings.ToLower(strings.TrimSpace(segment))
	for _, verb := range machineryVerbs {
		if lowered == verb {
			return true
		}
	}
	// The provider's transport verb is the one that carries a number with it —
	// `API error (400)`. Everything after it is the provider's own sentence,
	// which is the fact; the status code is not something a person acts on.
	return strings.HasPrefix(lowered, "api error")
}

// remedies are the failures aforge can name in a person's own words and say
// what to do about. The match is on the fact the provider or the store left
// behind, lower-cased, and the remedy is one line: the gesture that fixes it.
//
// The list is short on purpose. A remedy that is a guess is worse than none —
// it sends somebody to the wrong place with confidence — so a cause that is not
// here is printed as it is and nothing is invented under it.
var remedies = []struct {
	fact   string
	remedy string
}{
	{
		fact:   "is not a valid model id",
		remedy: "run `aforge models` to see the ids this key can reach, or pass --model with one of them.",
	},
	{
		fact:   "no auth credentials found",
		remedy: "export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.",
	},
	{
		// The most common first-run failure there is ([config.ErrNoAPIKey]).
		fact:   "openrouter_api_key (or openai_api_key) is required",
		remedy: "export OPENROUTER_API_KEY (or OPENAI_API_KEY) and run it again.",
	},
	{
		fact:   "insufficient credits",
		remedy: "that key is out of credit at the provider — top it up, or point AFORGE_MODEL at a model it can still reach.",
	},
}

func remedyFor(fact string) string {
	lowered := strings.ToLower(fact)
	for _, known := range remedies {
		if strings.Contains(lowered, known.fact) {
			return known.remedy
		}
	}
	return ""
}
