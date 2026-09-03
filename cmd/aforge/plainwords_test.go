package main

import (
	"strings"
	"testing"
)

// NO GO ERROR CHAIN REACHES A PERSON.
//
// The worked example is the one this rule was written from: a run against a
// model id that does not exist printed, as its deliverable,
//
//	I couldn't apply that request: splice failed: compile request: compile
//	intent: API error (400): nosuch/model-xyz is not a valid model ID
//
// Three internal package verbs stand in front of the one fact that matters, and
// nothing at all says what to do about it.
func TestNoGoErrorChainReachesAPerson(t *testing.T) {
	chain := "I couldn't apply that request: splice failed: compile request: compile intent: " +
		"API error (400): nosuch/model-xyz is not a valid model ID"
	said := plainWords(chain)

	for _, machinery := range []string{"splice failed", "compile request", "compile intent", "API error"} {
		if strings.Contains(said, machinery) {
			t.Fatalf("the reader is still shown the internal verb %q:\n%s", machinery, said)
		}
	}
	if !strings.Contains(said, "nosuch/model-xyz is not a valid model ID") {
		t.Fatalf("the one fact a person can act on was lost:\n%s", said)
	}
	if !strings.Contains(said, "aforge models") || !strings.Contains(said, "--model") {
		t.Fatalf("the message says what went wrong and never says what to do about it:\n%s", said)
	}
	if !strings.Contains(said, "I couldn't apply that request") {
		t.Fatalf("the sentence written for a person was thrown away with the machinery:\n%s", said)
	}
}

// A cause aforge does not recognise is printed as it is. A remedy that is a
// guess is worse than none: it sends somebody to the wrong place with
// confidence.
func TestAnUnrecognisedCauseIsSaidPlainlyAndNothingIsInvented(t *testing.T) {
	said := plainWords("compile request: the planner is unreachable")
	if said != "the planner is unreachable" {
		t.Fatalf("plain words = %q, want %q", said, "the planner is unreachable")
	}
	if strings.Contains(said, "\n") {
		t.Fatalf("a remedy was invented for a cause nothing knows what to do about:\n%s", said)
	}
}

// The parts of a chain that name something a person can act on are KEPT. A rule
// that took only the last segment would answer "no such file or directory" and
// throw away the path that says which file.
func TestPlainWordsKeepsWhatAPersonCanActOn(t *testing.T) {
	said := plainWords("open /nope/graph.json: no such file or directory")
	if !strings.Contains(said, "/nope/graph.json") {
		t.Fatalf("the only actionable part of the chain was dropped:\n%s", said)
	}
}

// A chain that is machinery all the way down is still better said than
// swallowed: nothing at all is the one answer a person cannot work with.
func TestAChainOfNothingButVerbsIsStillSaid(t *testing.T) {
	if said := plainWords("splice failed: compile intent"); strings.TrimSpace(said) == "" {
		t.Fatal("a failure made of nothing but machinery was reported as silence")
	}
}
