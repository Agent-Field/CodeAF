package main

// The two duties of PART FOUR are prompt work, and prompt work is only proved by
// a real model reading it. This file is that proof, held as a test so the next
// person changing the reviewer brief can re-run the evidence instead of trusting
// a paragraph in a commit message.
//
// It is OPT-IN — `AFORGE_LIVE_REVIEW=1 go test ./cmd/harness-design -run Live -v`
// — because it spends money and takes a minute, and a suite that reaches the
// network by default is a suite people stop running.
//
// The draft it hands the critic is the one the rig really produced, with both
// defects in it at once: three approach-evaluations collapsed into ONE
// agent.loop, and a verify with nowhere to fail to. Neither is visible to
// Validate and neither is visible to the derivation check — a table drawn over
// these four nodes is true, which is exactly why the duties had to be written.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
)

// collapsedC2 is the C2 defect, as a page: one worker told to price three
// approaches across four dimensions, and a re-derivation whose disagreement has
// nowhere to go.
func collapsedC2() (design, subharness.Harness) {
	h := subharness.Harness{
		Id: subharness.Id{
			Name:   "voice-input-compare",
			Desc:   "Compares three ways to add voice input to a Go TUI and re-derives the winner's score.",
			Author: "designer",
		},
		Program: subharness.Program{
			Nodes: []subharness.Node{
				{Id: "evaluate", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{
					"brief": "Evaluate each of the three approaches to adding voice input to a Go TUI — " +
						"local whisper.cpp, a cloud STT API, and push-to-talk via an external app — " +
						"scoring all three on latency, cost, privacy and implementation risk. " +
						"Output one table with the twelve scores and a sentence of reasoning per cell.",
					"max_turns": "3",
				}},
				{Id: "decide", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{
					"brief": "Read the table and name the winner, with the scores that decided it.",
				}},
				{Id: "rederive", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{
					"brief": "Score the named winner again on the same four dimensions, from the goal's own " +
						"statement, ignoring the numbers above.",
				}},
				{Id: "check-scores", Kind: subharness.KindVerify, Fields: subharness.Fields{
					"check": "Do the re-derived scores match the original scores for the winner on all four dimensions?",
				}},
			},
			Edges: []subharness.Edge{
				{"evaluate", "decide"}, {"decide", "rederive"}, {"rederive", "check-scores"},
			},
		},
		Verify: subharness.Verify{Ladder: subharness.VerifyRederive},
		Dyn:    subharness.Dyn{Ladder: subharness.DynFixed},
	}
	page, err := subharness.Encode(h)
	if err != nil {
		panic(err)
	}
	return design{
		Cues:          []string{"voice input", "compare approaches"},
		Justification: "Four nodes in a line: price the options, decide, re-derive the winner, check the two derivations agree.",
		Harness:       page,
	}, h
}

// The critic must not walk past either defect. What it does about them is its
// own judgement — a split, or one line saying why serial is right; a branch, or
// one line saying the gate is the answer — but SILENCE is the outcome the duties
// exist to forbid, so silence is what this asserts against.
func TestLiveReviewAnswersBothDutiesOnTheCollapsedDraft(t *testing.T) {
	if os.Getenv("AFORGE_LIVE_REVIEW") == "" {
		t.Skip("set AFORGE_LIVE_REVIEW=1 to spend a real review turn on this")
	}
	key := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if key == "" {
		t.Skip("OPENROUTER_API_KEY is not set")
	}
	model := os.Getenv("AFORGE_LIVE_MODEL")
	if model == "" {
		model = "deepseek/deepseek-v4-flash"
	}

	reviewer, err := reviewSystem(availableTools)
	if err != nil {
		t.Fatal(err)
	}
	draft, draftHarness := collapsedC2()
	goal := goals[4].text // c2, the goal this draft was drawn for
	history := []message{
		{Role: "system", Content: reviewer},
		{Role: "user", Content: strings.Join([]string{
			"THE GOAL:\n\n" + goal,
			"THE DRAFT'S CUES:\n\n" + strings.Join(draft.Cues, " · "),
			"THE DRAFT'S JUSTIFICATION:\n\n" + draft.Justification,
			"THE DRAFT HARNESS:\n\n" + string(draft.Harness),
			"Review it and reply with your findings and the ops that answer them.",
		}, "\n\n")},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	revised, harness, applied, _, err := reviewOnce(ctx, newChatClient(key, model), history, 10000, draft, draftHarness)
	if err != nil {
		t.Fatalf("the review produced nothing usable: %v", err)
	}

	said := ""
	for _, found := range revised.Findings {
		t.Logf("%-8s %s", found.Pass, found.Text)
		said += " " + strings.ToLower(found.Text)
	}
	for _, result := range applied {
		t.Logf("op %s (applied %v)", result.Op.String(), result.Applied())
		said += " " + strings.ToLower(result.Op.String())
	}

	// DUTY ONE: the collapsed node is named — by a split that takes it apart, or
	// by a line arguing it stays whole.
	fanned := false
	for _, node := range harness.Program.Nodes {
		if node.Kind == subharness.KindParallelSplit {
			fanned = true
		}
	}
	if !fanned && !strings.Contains(said, "evaluate") {
		t.Errorf("duty one went unanswered: no split, and nothing said about the node that prices all three approaches")
	}

	// DUTY TWO: the verify is named — by a branch on its outcome, or by a line
	// arguing that dying on a failed check is the answer here.
	forked := strings.Contains(verifyPaths(harness), "forked at")
	if !forked && !strings.Contains(said, "check-scores") && !strings.Contains(said, "verify") {
		t.Errorf("duty two went unanswered: the failed check still goes nowhere and nothing was said about it")
	}
	t.Logf("where a failed check goes:\n%s", verifyPaths(harness))
}
