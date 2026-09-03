package revision

// A run ends when the request is satisfied, not when the plan runs out.

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider/pool"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// theErrand is the request that measured this: one command run, one line
// reported, nothing changed. Every leaf of it was finished two minutes in.
func theErrand() Grounds {
	return Grounds{Intent: "Run the command 'go test ./internal/subharness/ -count=1' in this " +
		"workspace and report the final line it prints. Change no files."}
}

func errandNode() store.Node {
	return store.Node{ID: "task-1", Provenance: store.Provenance{Intent: theErrand().Intent}}
}

func TestAStatedRuleIsNeverPutToTheRequestQuestion(t *testing.T) {
	judgment := Judgment{Constraint: []string{"Change no files."}}
	if RequestQuestionable(judgment) {
		t.Fatal("a broken stated rule was offered to the request question")
	}

	evidence := Evidence{Constraints: []plan.Constraint{
		{Text: "Change no files.", Kind: plan.ConstraintNoWrites},
		{Text: "Do not use the network.", Kind: plan.ConstraintOther},
	}}
	want := []string{"Change no files.", "Do not use the network."}
	got := statedRules(Grounds{}, evidence)
	if len(got) != len(want) {
		t.Fatalf("stated rules: got %q, want %q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("stated rule %d: got %q, want %q", index, got[index], want[index])
		}
	}
}

// THE ONE QUESTION, ANSWERED YES. The deliverable is the final line, wrapped in
// a sentence and a code fence — which is the exact shape the model judge failed
// as "a report about the output, not the output itself" — and the receipt it
// earns is the one sentence every reader of this spells.
func TestARequestAlreadySatisfiedComesBackWithItsReceipt(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	answer := &scriptedJudge{replies: []*ai.Response{said(`{"met":true,"missing":""}`)}}

	met, words, asked := RequestMet(context.Background(), settings,
		pool.Adopt(settings, answer.Model(), answer), errandNode(), theErrand(),
		"The command completed. The final line printed was:\n\n```\nok  \tinternal/subharness\t0.412s\n```",
		Evidence{Observed: true})

	if !asked {
		t.Fatal("the question was never put")
	}
	if !met {
		t.Fatalf("a satisfied request was read as short: %q", words)
	}
	if words != RequestMetWords {
		t.Fatalf("the receipt is not the words stated once: %q", words)
	}
}

// AND ANSWERED NO, WHICH LEAVES THE RUN EXACTLY WHERE IT WAS. What the question
// found absent comes back in the request's own words, so it can be journaled
// beside the gap rather than folded into it.
func TestARequestStillShortNamesWhatIsAbsent(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	answer := &scriptedJudge{replies: []*ai.Response{
		said(`{"met":false,"missing":"report the final line it prints"}`)}}

	met, words, asked := RequestMet(context.Background(), settings,
		pool.Adopt(settings, answer.Model(), answer), errandNode(), theErrand(),
		"I ran the command.", Evidence{Observed: true})

	if !asked || met {
		t.Fatalf("a short request was read as satisfied: asked=%v met=%v", asked, met)
	}
	if words != "report the final line it prints" {
		t.Fatalf("what is missing did not come back in the request's words: %q", words)
	}
}

// A YES THAT NAMES SOMETHING MISSING IS A DISAGREEMENT WITH ITSELF, and the safe
// reading of a disagreement about whether to stop is that it did not say stop.
func TestAYesThatStillNamesSomethingMissingDoesNotStopTheRun(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	answer := &scriptedJudge{replies: []*ai.Response{
		said(`{"met":true,"missing":"report the final line it prints"}`)}}

	met, _, asked := RequestMet(context.Background(), settings,
		pool.Adopt(settings, answer.Model(), answer), errandNode(), theErrand(),
		"I ran the command.", Evidence{Observed: true})

	if !asked || met {
		t.Fatalf("the run stopped on an answer that contradicted itself: asked=%v met=%v", asked, met)
	}
}

// AND AN ANSWER NOBODY COULD READ LEAVES THE EXISTING PATH ALONE. asked is
// false, so the caller buys the round it was going to buy: the alternative is a
// delivery ended as satisfied on the strength of a provider failure.
func TestAnUnreadableAnswerBuysTheRoundItWasGoingToBuy(t *testing.T) {
	settings := config.Config{Model: "worker/model"}
	answer := &scriptedJudge{replies: []*ai.Response{said("I think it is fine, honestly.")}}

	met, _, asked := RequestMet(context.Background(), settings,
		pool.Adopt(settings, answer.Model(), answer), errandNode(), theErrand(),
		"I ran the command.", Evidence{Observed: true})

	if asked || met {
		t.Fatalf("prose was read as a verdict: asked=%v met=%v", asked, met)
	}
	// And a caller holding no client asks nothing at all, at no cost.
	if _, _, put := RequestMet(context.Background(), settings, nil, errandNode(),
		theErrand(), "I ran the command.", Evidence{}); put {
		t.Fatal("a gate with no client claimed to have asked")
	}
}

// THE SECOND DOOR: an extension that would have planned a remainder asks the
// same question first, and a yes ends the job with the receipt rather than with
// a spliced node. Nothing is planned — the planner here would panic if it were
// called, which is the assertion.
func TestAnExtensionRefusedByASatisfiedRequestPlansNothing(t *testing.T) {
	graph := gateStore(t)
	node, ok, err := graph.Node("task-2")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	unmet := Judgment{Gaps: "src/circuit-breaker.ts — it never opens the circuit",
		Quote: "persist the feature schema", Citations: []string{"persist the feature schema"},
		Checked: true, Grounds: Grounds{Intent: node.Provenance.Intent},
		Request: func(context.Context) (bool, string, bool) {
			return true, RequestMetWords, true
		}}

	extension := ExtendForGap(context.Background(), graph, node, "done", unmet, nil, 0,
		func(context.Context, string, string) (store.Subtree, error) {
			t.Fatal("a satisfied request bought a remainder anyway")
			return store.Subtree{}, nil
		})

	if !extension.Met {
		t.Fatalf("the extension did not carry the answer: %+v", extension)
	}
	if extension.Unclosed || extension.Spliced != 0 {
		t.Fatalf("a request that was met left a gap open: %+v", extension)
	}
	if !strings.Contains(extension.Refused, RequestMetWords) {
		t.Fatalf("the receipt did not reach the person: %q", extension.Refused)
	}
}

// AND THE SAME QUESTION IS NOT PAID FOR TWICE. A gate that asked one door
// earlier and was told no carries the answer on the judgement, and the
// extension door reads it rather than putting the question again.
func TestAQuestionAlreadyPutIsNotPutAgainAtTheSecondDoor(t *testing.T) {
	graph := gateStore(t)
	node, _, err := graph.Node("task-2")
	if err != nil {
		t.Fatal(err)
	}
	unmet := Judgment{Gaps: "it never opens the circuit", RequestAsked: true,
		Quote: "persist the feature schema", Citations: []string{"persist the feature schema"},
		Checked: true, Grounds: Grounds{Intent: node.Provenance.Intent},
		Request: func(context.Context) (bool, string, bool) {
			t.Fatal("the same question was paid for twice")
			return true, RequestMetWords, true
		}}

	if extension := ExtendForGap(context.Background(), graph, node, "done", unmet, nil, 0,
		func(context.Context, string, string) (store.Subtree, error) {
			return store.Subtree{Nodes: nil}, nil
		}); extension.Met {
		t.Fatalf("a verdict already told no came back met: %+v", extension)
	}
}

// A FIELD NOTHING ASSIGNS IS DEAD WIRING. The extension door could only ask
// through a closure on the judgement, and nothing in production put one there —
// so for every caller that is not the chat surface the door was decoration, and
// a remainder could be bought over a request already satisfied. It is bound at
// the one exit every verdict in this program leaves through.
func TestEveryVerdictCarriesTheQuestionAndTheSecondDoorCanPutIt(t *testing.T) {
	graph := gateStore(t)
	node, ok, err := graph.Node("task-2")
	if err != nil || !ok {
		t.Fatalf("node: ok=%v err=%v", ok, err)
	}
	settings := config.Config{Model: "worker/model"}
	// The gate fails on its own prose, and the question that follows says the
	// request is satisfied anyway — the shape #428 measured, where a judge
	// convicted a correct answer for how it was wrapped.
	judge := &scriptedJudge{replies: []*ai.Response{
		said(`{"pass":false,"gaps":"that is a report about the output, not the output itself","quote":"persist the feature schema"}`),
		said(`{"met":true,"missing":""}`),
	}}
	client := pool.Adopt(settings, judge.Model(), judge)

	verdict := JudgeDeliverable(context.Background(), settings, client, graph, node,
		"The command completed. The final line printed was:\n\n```\nok\t0.4s\n```", "",
		Evidence{Observed: true}, "worker/model")
	if verdict.Pass {
		t.Fatalf("the fixture did not fail its gate: %+v", verdict)
	}
	if verdict.Request == nil {
		t.Fatal("a verdict left the gate with no way to ask whether the request was met")
	}

	extension := ExtendForGap(context.Background(), graph, node, "done", verdict, nil, 0,
		func(context.Context, string, string) (store.Subtree, error) {
			t.Fatal("a remainder was bought over a request already satisfied")
			return store.Subtree{}, nil
		})

	if !extension.Met || extension.Unclosed || extension.Spliced != 0 {
		t.Fatalf("the second door could not put the question: %+v", extension)
	}
	if !strings.Contains(extension.Refused, RequestMetWords) {
		t.Fatalf("the receipt did not reach the person: %q", extension.Refused)
	}
}

// AND THE DOOR IS SHUT ON A MEASUREMENT. A model's reading may not overturn a
// file the plan promised and the disk does not hold, a check this work turned
// red, or a behaviour nothing exercises — the two world-doors are forbidden to
// do it on evidence, and this must not do it on a sentence.
func TestAMeasuredFindingIsNeverPutToTheRequestQuestion(t *testing.T) {
	graph := gateStore(t)
	node, _, err := graph.Node("task-2")
	if err != nil {
		t.Fatal(err)
	}
	asked := func(context.Context) (bool, string, bool) {
		t.Fatal("a measurement was offered to a model to overturn")
		return true, RequestMetWords, true
	}
	for name, unmet := range map[string]Judgment{
		"a file the plan promised": {Gaps: "the file is not on disk", Mechanical: true,
			Citations: []string{"persist the feature schema"}, Request: asked},
		"a check this work broke": {Gaps: "this work broke checks that were passing", Sourced: true,
			Citations: []string{"persist the feature schema"}, Request: asked},
		"a behaviour nothing exercises": {Gaps: "no check exercises it",
			Unexercised: []string{"a probe holds its slot"},
			Citations:   []string{"persist the feature schema"}, Request: asked},
		"a check that names it and asserts nothing": {Gaps: "asserted by no check",
			Unasserted: []string{"a probe holds its slot"},
			Citations:  []string{"persist the feature schema"}, Request: asked},
		"a name the tree no longer binds": {Gaps: "nothing binds it",
			Unbound:   []string{"temp_post_req_data_path"},
			Citations: []string{"persist the feature schema"}, Request: asked},
	} {
		if RequestQuestionable(unmet) {
			t.Errorf("%s was offered to the request question", name)
		}
		if extension, met := metExtension(context.Background(), Extension{}, unmet); met {
			t.Errorf("%s was overturned by a reading: %+v", name, extension)
		}
	}
	// And the judge's own prose still is, which is the whole point of the door.
	prose := Judgment{Gaps: "that is a report about the output, not the output itself",
		Citations: []string{"report the final line it prints"},
		Grounds:   Grounds{Intent: node.Provenance.Intent},
		Request: func(context.Context) (bool, string, bool) {
			return true, RequestMetWords, true
		}}
	if !RequestQuestionable(prose) {
		t.Fatal("the judge's own reading of the request stopped being answerable by one")
	}
	if _, met := metExtension(context.Background(), Extension{}, prose); !met {
		t.Fatal("a satisfied request could not end the run")
	}
}
