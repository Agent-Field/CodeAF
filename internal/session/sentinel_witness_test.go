package session

// sentinel_witness_test.go pins the authority half of the readiness-witness
// repair: the person's own sentence is the sentinel's criterion, and what the
// proposer guessed a yes would look like is a guess with provenance, never the
// criterion itself. It is a mechanical reading of the composed prompt, not
// evidence about a live model: the native proof is root's.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// THE CRITERION AND THE GUESS ARE NAMED APART. The failing watch's hint said
// "yes when the output is 200" while the person asked for readiness; the
// question must hand the sentence to the sentinel as the thing to judge and the
// hint as the proposer's own note, so a match to the note is not a match to the
// ask.
func TestTheSentinelIsGivenThePersonAsTheCriterionNotTheHint(t *testing.T) {
	judgment := standing.Judgment{
		Item: standing.Item{
			Words: "Notify me once when the health endpoint at http://127.0.0.1:18777/ready becomes ready.",
			When:  standing.When{Kind: standing.WhenProbe, Hint: "yes when the output is 200"},
		},
		Evidence: "200",
	}
	question := standingSentinelQuestion(judgment)
	if !strings.Contains(question, "the criterion, their own words") {
		t.Fatalf("the person's sentence is not named as the criterion:\n%s", question)
	}
	if !strings.Contains(question, "Notify me once when the health endpoint") {
		t.Fatalf("the person's own words are missing from the judgment:\n%s", question)
	}
	if strings.Contains(question, "WHAT A YES LOOKS LIKE:") {
		t.Fatalf("the hint still reads as the criterion:\n%s", question)
	}
	if !strings.Contains(question, "the proposer's guess") {
		t.Fatalf("the hint carries no provenance:\n%s", question)
	}
	// The observed evidence travels whole, whatever it is.
	if !strings.Contains(question, "WHAT THE CHECK FOUND:\n200") {
		t.Fatalf("the evidence did not reach the sentinel:\n%s", question)
	}
}

// AND THE INSTRUCTION SAYS WHAT TO DO WITH A WEAKER FACT. A transport reading
// where the person asked about readiness is the observed failure, and the
// prompt names it: a match to the guess does not settle their condition.
func TestTheSentinelPromptRefusesAMatchWeakerThanTheAsk(t *testing.T) {
	if !strings.Contains(standingSentinelPrompt, "IS THE ONLY CRITERION") {
		t.Fatalf("the prompt does not make the person's sentence the criterion:\n%s", standingSentinelPrompt)
	}
	if !strings.Contains(standingSentinelPrompt, "the proposer's own guess, not their words") {
		t.Fatalf("the prompt does not distinguish the guess from their words:\n%s", standingSentinelPrompt)
	}
	if !strings.Contains(standingSentinelPrompt, "an application is ready") {
		t.Fatalf("the prompt does not name the reachability-versus-readiness case:\n%s", standingSentinelPrompt)
	}
}

// CLIPPING IS CARRIED, NOT ABSORBED. The reading a sentinel is handed says it
// was cut, so no partial view can be compared with a whole one.
func TestAProbeReadingKeepsTheClipItConfirmed(t *testing.T) {
	text := strings.Repeat("x", standing.ProbeClip+64) + "\ntail line"
	reading := standingProbeReading(text)
	if !reading.Clipped {
		t.Fatalf("an oversize reading was not marked clipped: %+v", reading)
	}
	if !strings.Contains(reading.Text, "tail line") {
		t.Fatalf("the clipped reading lost the tail:\n%s", reading.Text)
	}
	short := standingProbeReading("{\"ready\": true}")
	if short.Clipped {
		t.Fatalf("a small reading was marked clipped: %+v", short)
	}
}
