package session

// sentinel_witness_test.go pins the MECHANICAL half of the watch-grounding fix:
// what the judgment carries, and that the prompt states the rule once. It is a
// string reading of the composed prompt and question, NOT evidence about a live
// model -- the production-seam matrix is TestStandingSentinelGroundingE2E
// (internal/e2e/standing_sentinel_grounding_e2e_test.go), which drives the real
// provider and is where the semantic claim is made.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// statusOnlyWatch is the failing shape's judgment: the person asked for
// readiness, the probe threw the body away, and only the status code came back.
func statusOnlyWatch() standing.Judgment {
	return standing.Judgment{
		Item: standing.Item{
			Words: "Notify me once when the health endpoint at http://127.0.0.1:18777/ready becomes ready.",
			When: standing.When{
				Kind: standing.WhenProbe,
				Hint: "yes when the output is 200",
				Probe: standing.Probe{
					Command: "curl -s -o /dev/null -w '%{http_code}' --max-time 10 http://127.0.0.1:18777/ready",
				},
			},
		},
		Evidence: "200",
	}
}

// THE CRITERION, THE GUESS, AND THE LOOK ARE NAMED APART. The person's sentence
// is the thing judged; the hint is the proposer's note; and the command that
// produced the evidence travels with it, so a status-only look cannot pass for a
// reading of the body.
func TestTheSentinelIsGivenThePersonTheGuessAndTheLook(t *testing.T) {
	question := standingSentinelQuestion(statusOnlyWatch())
	for _, want := range []string{
		"the criterion, their own words",
		"Notify me once when the health endpoint",
		"the proposer's guess",
		"WHAT THE CHECK RAN",
		"curl -s -o /dev/null -w '%{http_code}'",
		"WHAT THE CHECK FOUND:\n200",
	} {
		if !strings.Contains(question, want) {
			t.Fatalf("the judgment is missing %q:\n%s", want, question)
		}
	}
	if strings.Contains(question, "WHAT A YES LOOKS LIKE:") {
		t.Fatalf("the hint still reads as the criterion:\n%s", question)
	}
	// The closing instruction must agree with the prompt's three-way contract.
	if !strings.Contains(question, "Answer yes, no or unknown as the first word") {
		t.Fatalf("the question still asks a two-way contract:\n%s", question)
	}
}

// THE INSTRUCTION SAYS WHAT A WEAKER READING IS. A status-only probe where the
// person asked about the application is unknown, and the prompt states it as a
// property of the look rather than of one endpoint.
func TestTheSentinelPromptRefusesAReadingWeakerThanTheAsk(t *testing.T) {
	for _, want := range []string{
		"IS THE ONLY CRITERION",
		"the proposer's own guess, not their words",
		"proves only that something answered",
		"an application is ready",
		"when the person's own words made that reading their criterion",
	} {
		if !strings.Contains(standingSentinelPrompt, want) {
			t.Fatalf("the prompt is missing %q:\n%s", want, standingSentinelPrompt)
		}
	}
}

// A CHECK WITH NO PROBE OF ITS OWN CARRIES NO LOOK. A file watch or a rhythm has
// its evidence in the file or the clock, so there is nothing to attribute and no
// command is invented for it.
func TestAFileWatchCarriesNoProbeCommand(t *testing.T) {
	item := standing.Item{When: standing.When{Kind: standing.WhenFile, Glob: "*.sql"}}
	if look := standingSentinelLook(item); look != "" {
		t.Fatalf("a file watch invented a look: %q", look)
	}
	tool := standing.Item{When: standing.When{Probe: standing.Probe{Tool: "read", Args: []byte(`{"path":"a"}`)}}}
	if look := standingSentinelLook(tool); look != `read {"path":"a"}` {
		t.Fatalf("a tool probe's call was not carried: %q", look)
	}
}
