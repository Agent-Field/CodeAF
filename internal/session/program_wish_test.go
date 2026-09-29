package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/effort"
)

// HOW HARD A PROGRAM'S WORKING MODEL THINKS, NEAREST HAND FIRST: the rung the
// conversation chose for this hand-off, then the one written on the person's
// working seat, then nothing — which leaves the program on its own default.
func TestAProgramsEffortIsTheHandOffsThenTheSeatsThenItsOwn(t *testing.T) {
	for _, tc := range []struct {
		thinking effort.Rung
		seat     string
		want     string
	}{
		{effort.XHigh, "low", "xhigh"},
		{effort.None, "high", "high"},
		{effort.None, "", ""},
		{effort.None, "nonsense", ""},
		{effort.Low, "", "low"},
	} {
		if got := programEffort(tc.thinking, tc.seat); got != tc.want {
			t.Fatalf("programEffort(%q, %q) = %q, want %q", tc.thinking, tc.seat, got, tc.want)
		}
	}
}

// `thinking` on a proposal is a rung of the one ladder, or left out; a word
// that is not one is refused rather than dropped, and "auto" is not a choice
// to make there, because leaving the field out already is.
func TestAProposalsThinkingIsARungOrNothing(t *testing.T) {
	proposal := func(thinking string) string {
		return `{"title":"port the parser","summary":"s","brief":"b","deliverable":"d","acceptance":"a","via":"senior-dev","thinking":"` + thinking + `"}`
	}
	spec, problem := parseTaskArguments(json.RawMessage(proposal("xhigh")))
	if problem != "" || spec.thinking != effort.XHigh {
		t.Fatalf("thinking xhigh = %q (%q)", spec.thinking, problem)
	}
	for _, word := range []string{"extreme", "auto"} {
		if _, problem := parseTaskArguments(json.RawMessage(proposal(word))); !strings.Contains(problem, "thinking is one of low, medium, high, xhigh, max") {
			t.Fatalf("thinking %q = %q, want it refused", word, problem)
		}
	}
	if spec, problem := parseTaskArguments(json.RawMessage(`{"title":"t","summary":"s","brief":"b","deliverable":"d","acceptance":"a"}`)); problem != "" || spec.thinking != effort.None {
		t.Fatalf("a proposal with no thinking = %q (%q)", spec.thinking, problem)
	}
	if !strings.Contains(taskSchemaJSON, `"thinking":{"type":"string","enum":["low","medium","high","xhigh","max"]`) ||
		strings.Contains(taskSchemaWithoutViaJSON, `"thinking"`) {
		t.Fatal("thinking is not offered beside via, and only there")
	}
}

// THE CREW A PROGRAM IS HANDED CARRIES ITS EFFORT AND THE PERSON'S ONE-TASK
// WORD. The hand-off's `thinking` outranks the working seat's own rung, the
// seat's rung is used when the hand-off chose none, and `/task --best` (or
// "do this one properly") reaches the router that picks an unpinned worker.
func TestAProgramsCrewCarriesItsEffortAndTheOneTaskWord(t *testing.T) {
	program := testPrograms("fake")[0]
	pinned := &Agent{config: Config{RolesSource: tierSettings(map[string]string{"tiers.worker": "vendor/worker:low"})}}
	if crew := pinned.delegateCrew(&beltRun{delegate: &program}); crew.Hands != "vendor/worker" || crew.Effort != "low" {
		t.Fatalf("a seat's own rung = %+v, want the worker at low", crew)
	}
	if crew := pinned.delegateCrew(&beltRun{delegate: &program, thinking: effort.XHigh}); crew.Effort != "xhigh" {
		t.Fatalf("the hand-off's thinking = %+v, want xhigh over the seat's low", crew)
	}
	var asked config.CrewAsk
	routed := &Agent{config: Config{
		RolesSource: tierSettings(map[string]string{}),
		RouteCrew: func(ask config.CrewAsk) (crewroute.Decision, error) {
			asked = ask
			return crewroute.Decision{}, nil
		},
	}}
	routed.delegateCrew(&beltRun{delegate: &program, crewEffort: crewroute.EffortBest})
	if asked.Effort != crewroute.EffortBest {
		t.Fatalf("the router was asked %+v, want the person's best", asked)
	}
}
