package session

// WHAT A DESIGN IS HELD TO, AND WHAT IT COSTS TO GET IT WRONG.
//
// harness_build_test.go has the job and the card. This file has the ladder
// underneath them — salvage, the repair turn, the retry that hands a refused
// design its own error back, and the two ways an attempt can be spent for
// nothing. Every case here was a real failure first: a design that kept failing
// on a slow reasoning model, chased down to a reply that ran out of budget, a
// review thrown away by its own draft's homework, and a guide that told the
// designer there was no filesystem while handing it seven filesystem tools.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// endedResponse is a completion that says how it ended. It is the one thing
// textResponse cannot say, and the whole of what tells a truncated page apart
// from a stray brace.
func endedResponse(text, finishReason string) *ai.Response {
	response := textResponse(text)
	response.Choices[0].FinishReason = finishReason
	return response
}

// ── a reply that ran out of room ────────────────────────────────────────────

// A CUT-OFF REPLY IS NOT A PUNCTUATION PROBLEM, and it does not buy a repair
// turn. The repairer is told never to change content; handed half an object it
// closes the braces and invents the rest, which is a page nobody wrote reaching
// a card somebody approves.
func TestACutOffDesignSkipsTheRepairTurn(t *testing.T) {
	const half = `{"cues": ["a", "b"], "justification": "x", "harness": {"id": {"name": "half`
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return endedResponse(half, "length"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return endedResponse(half, "length"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return endedResponse(half, "length"), nil },
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())

	_, _, err := agent.designPage(context.Background(), "anything", "test/model", designSeat{})
	if err == nil {
		t.Fatal("a design that never closed its object was accepted")
	}
	if !strings.Contains(err.Error(), "ran out of completion budget") {
		t.Fatalf("the failure blamed something else: %v", err)
	}
	// THREE ATTEMPTS AND NOT SIX. One call per attempt is the whole point: a
	// repair turn on each would double the bill to fix a length problem with a
	// delimiter.
	if calls := completer.requests(); calls != harnessDesignRetries+1 {
		t.Fatalf("a truncated reply cost %d calls, not %d", calls, harnessDesignRetries+1)
	}
}

// THE MODEL IS TOLD THE ONE THING THAT WOULD HELP. A model shown only "that did
// not parse" answers by sending the same page again, at the same length, into
// the same ceiling.
func TestACutOffDesignIsAskedForASmallerOne(t *testing.T) {
	const half = `{"cues": ["a", "b"], "justification": "x", "harness": {"id": {"name": "half`
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return endedResponse(half, "length"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(designReply), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(reviewReply), nil },
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())

	page, _, err := agent.designPage(context.Background(), "anything", "test/model", designSeat{})
	if err != nil {
		t.Fatalf("the second attempt should have landed: %v", err)
	}
	if page.Id.Name != "flake-triage" {
		t.Fatalf("the page that came back is %q", page.Id.Name)
	}
	second := completer.request(1)
	last := second[len(second)-1]
	told := textOf(last)
	if !strings.Contains(told, "SMALLER harness") {
		t.Fatalf("the retry was not asked for a smaller design; it was told: %s", told)
	}
	if !strings.Contains(told, "ran out of completion budget") {
		t.Fatalf("the retry was not told why; it was told: %s", told)
	}
	// The refused reply itself is in the history, or the model is repairing a
	// page it cannot see.
	if !strings.Contains(textOf(second[len(second)-2]), "half") {
		t.Fatal("the retry was not shown the reply it is being asked to fix")
	}
}

// ── a reply that broke the law ──────────────────────────────────────────────

// A REFUSED DESIGN IS SHOWN ITS OWN PAGE AND THE VALIDATOR'S OWN SENTENCE. That
// is the difference between asking a model to repair what it wrote and asking it
// to guess again — and the second attempt is the one that usually lands.
func TestARefusedDesignIsHandedTheValidatorsSentence(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(ladderWithoutVerify), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(designReply), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(reviewReply), nil },
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())

	if _, _, err := agent.designPage(context.Background(), "anything", "test/model", designSeat{}); err != nil {
		t.Fatalf("the repaired design was refused too: %v", err)
	}
	second := completer.request(1)
	told := textOf(second[len(second)-1])
	if !strings.Contains(told, "REFUSED") || !strings.Contains(told, "no verify node") {
		t.Fatalf("the retry was told: %s", told)
	}
}

// A DESIGN THAT NEVER COMES RIGHT STOPS, and it says how many attempts it took
// to give up — a person watching a design that failed is owed the count.
func TestADesignThatKeepsBreakingTheLawStops(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(ladderWithoutVerify), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(ladderWithoutVerify), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(ladderWithoutVerify), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(ladderWithoutVerify), nil
		},
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())

	_, _, err := agent.designPage(context.Background(), "anything", "test/model", designSeat{})
	if err == nil {
		t.Fatal("a page that never validated was accepted")
	}
	if !strings.Contains(err.Error(), "no valid design in 3 attempts") {
		t.Fatalf("the failure said %v", err)
	}
	if calls := completer.requests(); calls != harnessDesignRetries+1 {
		t.Fatalf("%d attempts were spent, not %d", calls, harnessDesignRetries+1)
	}
}

// SALVAGE COSTS NOTHING, so a fenced reply is one call and not two. The ladder's
// whole shape is cheapest-first, and a design lost to a code fence would be a
// good design lost to punctuation.
func TestAFencedDesignCostsNoSecondCall(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("Here you go:\n\n```json\n" + designReply + "\n```\n"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(reviewReply), nil },
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())

	page, _, err := agent.designPage(context.Background(), "anything", "test/model", designSeat{})
	if err != nil {
		t.Fatalf("a fenced envelope was refused: %v", err)
	}
	if page.Id.Name != "flake-triage" {
		t.Fatalf("the page that came back is %q", page.Id.Name)
	}
	// Two calls: the design and the review. A third would be a repair turn
	// nobody needed.
	if calls := completer.requests(); calls != 2 {
		t.Fatalf("a fenced reply cost %d calls, not 2", calls)
	}
}

// ── the review, and the draft's stale homework ──────────────────────────────

// A CRITIC THAT DROPS A NODE KEEPS ITS REVIEW. The draft's derivation table is
// what the patched page is checked against — the critic never restates it — so a
// table naming the node the patch deleted would refuse the page and throw the
// whole second pass away. That is the one patch a critic most often writes.
func TestAReviewThatDropsANodeIsNotThrownAwayByTheDraftsOwnTable(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(threeNodeDesign), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"findings": [{"pass": "cost", "text": "the middle step re-reads what the first one already said"}],
				"ops": [{"op": "drop_node", "node": "restate"}, {"op": "add_edge", "node": "look", "text": "check"}],
				"calls": {"draft": 3, "revised": 2}}`), nil
		},
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())

	page, _, err := agent.designPage(context.Background(), "anything", "test/model", designSeat{})
	if err != nil {
		t.Fatalf("the design failed: %v", err)
	}
	if _, found := page.Program.Node("restate"); found {
		t.Fatal("the review was thrown away: the node the critic dropped is still on the page")
	}
	if len(page.Program.Nodes) != 2 {
		t.Fatalf("the patched page has %d nodes", len(page.Program.Nodes))
	}
}

// THE PRUNING IS NOT A WAY AROUND THE CHECK. Every pair over nodes the patch
// LEFT STANDING is still held to the edges, so a critic that wires two lanes
// into a line still loses its turn to the draft's own table.
func TestAReviewThatContradictsTheSurvivingTableIsStillRefused(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(twoLaneDesign), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"findings": [{"pass": "speed", "text": "run them in a line"}],
				"ops": [{"op": "add_edge", "node": "left", "text": "right"}],
				"calls": {"draft": 3, "revised": 3}}`), nil
		},
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())

	page, _, err := agent.designPage(context.Background(), "anything", "test/model", designSeat{})
	if err != nil {
		t.Fatalf("the design failed: %v", err)
	}
	// The draft goes forward: the review is an improvement pass and not a gate.
	if page.Program.Successors("left") != nil && len(page.Program.Successors("left")) > 1 {
		t.Fatalf("the contradicting patch landed: left now runs into %v", page.Program.Successors("left"))
	}
	if _, err := subharness.Encode(page); err != nil {
		t.Fatalf("the draft that went forward will not encode: %v", err)
	}
}

// ── the window ──────────────────────────────────────────────────────────────

// harnessSlowestAttempt is one design turn at the slowest pace this pipeline was
// measured at: deepseek-v4-pro, writing the whole envelope against the whole
// guide, took a hundred seconds a turn. It is here rather than in the package
// because nothing in the code may branch on it — it is the number the window
// below has to be big enough for.
const harnessSlowestAttempt = 100 * time.Second

// THE WINDOW IS FOR THE PERSON, NOT FOR THE WRITING, and this is what keeps it
// that way. It bounds the whole job — every attempt, the review, and the wait
// for an answer to the card — so a ladder that grew a rung or a model that got
// slower could quietly leave nobody any time to answer. A card that expires
// while somebody is reading it is the failure this pins.
func TestTheDesignWindowIsMostlyForThePerson(t *testing.T) {
	// Every attempt the ladder allows, plus the review pass, all at the slowest
	// measured pace.
	writing := time.Duration(harnessDesignRetries+2) * harnessSlowestAttempt
	if writing >= harnessDesignWindow/2 {
		t.Fatalf("the writing can take %s of a %s window, so the person is left %s to answer the card",
			writing, harnessDesignWindow, harnessDesignWindow-writing)
	}
}

// ── fixtures ────────────────────────────────────────────────────────────────

// textOf is one message's prose, for a test asserting what a model was told.
func textOf(message ai.Message) string {
	var out strings.Builder
	for _, part := range message.Content {
		out.WriteString(part.Text)
	}
	return out.String()
}

// ladderWithoutVerify is the refusal this pipeline sees most often on a live
// model: a rung claimed above `accept` by a program with nothing in it that
// could do the verifying.
const ladderWithoutVerify = `{
  "cues": ["flaky test", "triage the flake"],
  "justification": "One job, and a promise it cannot keep.",
  "harness": {
    "id": {"name": "flake-triage", "desc": "chase a flaky test to a fix"},
    "program": {
      "nodes": [
        {"id": "look", "kind": "agent.loop", "fields": {"brief": "read the failing test and say what it does", "max_turns": "3"}}
      ],
      "edges": []
    },
    "whitelist": [],
    "verify": {"ladder": "invariants"},
    "dyn": {"ladder": "fixed"}
  }
}`

// threeNodeDesign carries a FULL pair table over three nodes in a line, which is
// what the guide asks for — and what makes a critic's drop_node into a table
// that names a job the page no longer has.
const threeNodeDesign = `{
  "cues": ["flaky test", "triage the flake"],
  "justification": "Three jobs in a line: read it, restate it, check the report.",
  "derivation": [
    {"a": "look", "b": "restate", "rel": "depends", "why": "the restatement is of what the read found"},
    {"a": "look", "b": "check", "rel": "depends", "why": "the check is downstream of the read"},
    {"a": "restate", "b": "check", "rel": "depends", "why": "the check reads the restatement"}
  ],
  "harness": {
    "id": {"name": "flake-triage", "desc": "chase a flaky test to a fix"},
    "program": {
      "nodes": [
        {"id": "look", "kind": "agent.loop", "fields": {"brief": "read the failing test and say what it does", "max_turns": "3"}},
        {"id": "restate", "kind": "agent.loop", "fields": {"brief": "say the same thing again in one line", "max_turns": "1"}},
        {"id": "check", "kind": "verify", "fields": {"ladder": "accept", "check": "the report names the failing test"}}
      ],
      "edges": [["look", "restate"], ["restate", "check"]]
    },
    "whitelist": [],
    "verify": {"ladder": "accept"},
    "dyn": {"ladder": "fixed"}
  }
}`

// twoLaneDesign is two jobs the designer derived as independent and drew as
// lanes. Both survive any patch, so the table over them is still a live claim.
const twoLaneDesign = `{
  "cues": ["flaky test", "triage the flake"],
  "justification": "Two independent readings, then a check over both.",
  "derivation": [
    {"a": "left", "b": "right", "rel": "independent", "why": "each reads the failure report from the goal; neither reads the other"}
  ],
  "harness": {
    "id": {"name": "flake-triage", "desc": "chase a flaky test to a fix"},
    "program": {
      "nodes": [
        {"id": "open", "kind": "parallel.split", "fields": {"width": "2"}},
        {"id": "left", "kind": "agent.loop", "fields": {"brief": "read the failure as a timing problem", "max_turns": "2"}},
        {"id": "right", "kind": "agent.loop", "fields": {"brief": "read the failure as a state problem", "max_turns": "2"}},
        {"id": "shut", "kind": "parallel.join", "fields": {}},
        {"id": "check", "kind": "verify", "fields": {"ladder": "accept", "check": "the report names the failing test"}}
      ],
      "edges": [["open", "left"], ["open", "right"], ["left", "shut"], ["right", "shut"], ["shut", "check"]]
    },
    "whitelist": [],
    "verify": {"ladder": "accept"},
    "dyn": {"ladder": "width", "cap": 2}
  }
}`

// ── the brief, against the belt it hands over ───────────────────────────────

// THE GUIDE MAY NOT DENY A TOOL IT IS ABOUT TO HAND OVER. It used to: the tool
// section said "There is no web, no filesystem, no shell" — true of the
// development rig's three toy tools, and flatly false here, where the belt IS
// the filesystem and the shell. A designer told both things at once does the
// safe thing and whitelists nothing, and the harness that comes back cannot
// touch the work it was designed for. The closed list is the whole claim now.
func TestTheDesignerBriefDoesNotDenyItsOwnBelt(t *testing.T) {
	agent, _ := buildAgent(t, &scriptedCompleter{}, t.TempDir())
	designer, reviewer, err := agent.harnessBriefs()
	if err != nil {
		t.Fatalf("the brief will not render: %v", err)
	}
	for _, brief := range []struct {
		name string
		text string
	}{{"designer", designer}, {"reviewer", reviewer}} {
		for tool := range agent.harnessToolNames() {
			if !strings.Contains(brief.text, tool) {
				t.Errorf("the %s brief never names the tool %q that this surface hands a harness", brief.name, tool)
			}
		}
		if strings.Contains(brief.text, "no filesystem") || strings.Contains(brief.text, "no shell") {
			t.Errorf("the %s brief denies a tool it hands over", brief.name)
		}
	}
}

// A REASONING MODEL THAT RAN OUT OF ROOM ANSWERS WITH NOTHING, and that is a
// different failure from a page that stopped halfway. It did not write too much;
// it thought too long — so it is told so, and the empty reply is kept out of the
// history rather than sent back as an assistant turn with no content in it.
func TestADesignThatThoughtItselfEmptyIsToldThat(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return endedResponse("", "length"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(designReply), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(reviewReply), nil },
	}}
	agent, _ := buildAgent(t, completer, t.TempDir())

	if _, _, err := agent.designPage(context.Background(), "anything", "test/model", designSeat{}); err != nil {
		t.Fatalf("the second attempt should have landed: %v", err)
	}
	second := completer.request(1)
	told := textOf(second[len(second)-1])
	if !strings.Contains(told, "came back empty") {
		t.Fatalf("the retry was told: %s", told)
	}
	// System, goal, then the refusal. No empty assistant turn between them.
	for _, message := range second {
		if message.Role == "assistant" && strings.TrimSpace(textOf(message)) == "" {
			t.Fatal("an assistant turn with nothing in it went back to the model")
		}
	}
}
