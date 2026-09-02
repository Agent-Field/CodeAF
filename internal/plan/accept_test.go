package plan

import (
	"encoding/json"
	"strings"
	"testing"
)

// THE CAP IS THE REQUEST'S OWN SHAPE. A request cannot state more behaviours
// than it has clauses, so there is no number here for a later wave to tune
// wrongly. Past that the model has stopped describing the request and started
// describing the domain, which is exactly the invented scope the whole
// grounding invariant exists to keep out.
func TestAChecklistCannotBeLongerThanTheRequestHasClauses(t *testing.T) {
	request := "Add a circuit breaker.\n\nIt must open after five failures."
	var offered []Point
	for index := 0; index < 12; index++ {
		offered = append(offered, Point{
			Behaviour: "behaviour " + string(rune('a'+index)),
			Quote:     "quote " + string(rune('a'+index)),
		})
	}
	kept := NormalizeAcceptance(request, offered)
	if len(kept) != 2 {
		t.Fatalf("a two-line request afforded %d points, want 2: %#v", len(kept), kept)
	}
	if got := NormalizeAcceptance("", offered); got != nil {
		t.Errorf("a request with nothing in it afforded %#v", got)
	}
}

// A point with no words of the request behind it is a requirement nobody made,
// and a point with no behaviour is a quotation nothing can be checked against.
// Neither is a point.
func TestAHalfWrittenPointIsNotAPoint(t *testing.T) {
	request := strings.Repeat("a line\n", 10)
	kept := NormalizeAcceptance(request, []Point{
		{Behaviour: "opens after five failures", Quote: "threshold = 5"},
		{Behaviour: "  ", Quote: "cooldown = 30000"},
		{Behaviour: "closes on a successful probe", Quote: "   "},
	})
	if len(kept) != 1 || kept[0].Behaviour != "opens after five failures" {
		t.Fatalf("kept %#v, want only the point that carries both halves", kept)
	}
}

// One sentence read twice is one behaviour, not two. A checklist that counted
// it twice would buy two repair rounds for one gap.
func TestOneSentenceReadTwiceIsOnePoint(t *testing.T) {
	request := strings.Repeat("a line\n", 10)
	kept := NormalizeAcceptance(request, []Point{
		{Behaviour: "opens after five failures", Quote: "threshold  = 5"},
		{Behaviour: "opens once five failures are seen", Quote: "threshold = 5"},
	})
	if len(kept) != 1 {
		t.Fatalf("two readings of one sentence made %d points: %#v", len(kept), kept)
	}
}

// The checklist rides plan.Spec, which is the one object carried forward
// verbatim through a retry and round-tripped through the store as JSON. A field
// that did not survive that would leave a repair round judged against an empty
// checklist while its predecessor was judged against a full one.
func TestTheChecklistSurvivesTheSpecsOwnJournalling(t *testing.T) {
	spec := Spec{
		Instruction: "add the circuit breaker",
		Accept: []Point{{
			Behaviour: "A rejected non-listed status does not close half-open state",
			Quote:     "must not close half-open state",
		}},
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var back Spec
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Accept) != 1 || back.Accept[0].Quote != spec.Accept[0].Quote {
		t.Fatalf("the checklist did not survive the round trip: %#v", back.Accept)
	}
	// And a spec that carries only a checklist is not an empty spec, or the
	// encoder drops it before it is ever written.
	if (Spec{Accept: spec.Accept}).Empty() {
		t.Error("a spec holding a checklist reported itself empty, so EncodeSpec discards it")
	}
}

// THE WORKER IS NEVER SHOWN THE LIST IT WILL BE CHECKED ON. A worker handed the
// checklist writes checks for the checklist and nothing else, which is the
// failure this mechanism exists to fix one level up.
func TestTheWorkerIsNotShownTheChecklist(t *testing.T) {
	spec := Spec{
		Instruction: "add the circuit breaker",
		Method:      "write it, then run the project's tests",
		Accept: []Point{{
			Behaviour: "A half-open probe holds its slot across internal retries",
			Quote:     "keeps its slot for the full logical request",
		}},
	}
	rendered := spec.Render(0)
	for _, secret := range []string{
		"A half-open probe holds its slot across internal retries",
		"keeps its slot for the full logical request",
	} {
		if strings.Contains(rendered, secret) {
			t.Errorf("the worker's own instruction carries the checklist: %q", secret)
		}
	}
}

// The checklist belongs to whoever hands the finished thing over, and to nobody
// else. Every other node contributes material to that one and was never asked
// for the whole request's behaviours.
func TestTheChecklistLandsOnTheNodeThatDelivers(t *testing.T) {
	graph := &Graph{Goal: "add the circuit breaker"}
	graph.Add(Node{Kind: KindWork, Summary: "the state machine", Stage: 1})
	graph.Add(Node{Kind: KindWork, Summary: "the hooks", Stage: 1})
	graph.Add(Node{Kind: KindSynthesis, Summary: "the finished change", Stage: 2,
		Needs: []int{1, 2}})
	points := []Point{{Behaviour: "opens after five failures", Quote: "threshold = 5"}}
	graph.SetAcceptance(points)

	sink := graph.Node(3)
	if sink == nil || len(sink.Spec.Accept) != 1 {
		t.Fatalf("the node that delivers holds %#v", sink)
	}
	for _, id := range []int{1, 2} {
		if node := graph.Node(id); node != nil && len(node.Spec.Accept) != 0 {
			t.Errorf("node %d contributes material and was handed the whole request's "+
				"checklist: %#v", id, node.Spec.Accept)
		}
	}
}

// A LINE IS NOT A BEHAVIOUR. A person writes "defaults are threshold = 5;
// cooldown = 30000; halfOpenMaxRequests = 1" on one line and has stated three
// things a check either exercises or does not. While the ceiling counted lines,
// the s5 sweep held four points against igel's twenty-four hidden checks and
// five against textual's twenty, and the mapping had nothing fine enough to
// match a check to.
func TestOneLineMayStateSeveralBehaviours(t *testing.T) {
	request := "When circuitBreaker: true, defaults are threshold = 5; " +
		"cooldown = 30000. halfOpenMaxRequests = 1.\n"
	offered := []Point{
		{Behaviour: "the threshold defaults to 5", Quote: "threshold = 5"},
		{Behaviour: "the cooldown defaults to 30000", Quote: "cooldown = 30000"},
		{Behaviour: "halfOpenMaxRequests defaults to 1", Quote: "halfOpenMaxRequests = 1"},
	}
	kept := NormalizeAcceptance(request, offered)
	if len(kept) != 3 {
		t.Fatalf("one line stating three behaviours afforded %d points: %#v", len(kept), kept)
	}
	// And a comma is deliberately not a clause boundary: it sits inside names
	// and numbers as often as it sits between statements, and counting it would
	// raise a ceiling the request never earned.
	commas := "one, two, three, four, five, six, seven, eight\n"
	if got := len(NormalizeAcceptance(commas, offered)); got != 1 {
		t.Errorf("a line of commas afforded %d points, want 1", got)
	}
}

// A BEHAVIOUR IS OF THE FINISHED WORK; AN ACTION IS OF THE RUN. The kind is
// what decides whether the gate goes looking for a check, and an errand that
// states only actions has nothing for a check to be missing from — which is the
// whole of the false positive that ran a satisfied request into its wall.
func TestAnActionIsKeptOnTheChecklistAndFiltersOutOfTheBehaviours(t *testing.T) {
	request := "Run the command 'go test ./x/' in this workspace and report the final line " +
		"it prints. Change no files."
	kept := NormalizeAcceptance(request, []Point{
		{Behaviour: "the command is run in this workspace", Quote: "Run the command 'go test ./x/'",
			Kind: PointAction},
		{Behaviour: "no file in the workspace is changed", Quote: "Change no files.",
			Kind: PointBehaviour},
	})
	if len(kept) != 2 {
		t.Fatalf("kept %d points, want both — an action is still the person's words: %#v", len(kept), kept)
	}
	behaviours := Behaviours(kept)
	if len(behaviours) != 1 || behaviours[0].Behaviour != "no file in the workspace is changed" {
		t.Fatalf("the filter kept %#v, want only the behaviour of the finished work", behaviours)
	}
	if Behaviours([]Point{{Behaviour: "a", Quote: "a", Kind: PointAction}}) != nil {
		t.Error("a checklist of nothing but actions offered something to map")
	}
}

// AN UNKNOWN KIND IS A BEHAVIOUR. A point written before this field existed, or
// answered with a word neither enum spells, is still mapped and still counted:
// reading an action as a behaviour costs one finding, and reading a behaviour as
// an action costs a stated requirement nothing ever checks.
func TestAPointWithNoKindIsReadAsABehaviour(t *testing.T) {
	request := strings.Repeat("a line\n", 4)
	kept := NormalizeAcceptance(request, []Point{
		{Behaviour: "opens after five failures", Quote: "threshold = 5"},
		{Behaviour: "closes on a probe", Quote: "cooldown = 30000", Kind: "whatever"},
		{Behaviour: "the suite is run", Quote: "run the suite", Kind: "ACTION"},
	})
	if len(kept) != 3 {
		t.Fatalf("kept %d points, want 3: %#v", len(kept), kept)
	}
	if kept[0].Kind != PointBehaviour || kept[1].Kind != PointBehaviour {
		t.Fatalf("an unknown kind was not read as a behaviour: %#v", kept)
	}
	if kept[2].Kind != PointAction {
		t.Fatalf("a kind the model spelled loudly was not read as an action: %#v", kept[2])
	}
	if len(Behaviours(kept)) != 2 {
		t.Fatalf("the filter kept %#v", Behaviours(kept))
	}
}

// The schema is what the model is held to, so the enum has to be in it: a kind
// the reply may spell freely is a kind that arrives as prose.
func TestTheAcceptanceSchemaNamesBothKinds(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(acceptanceSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(acceptanceSchema), `"enum": ["behaviour", "action"]`) {
		t.Fatalf("the schema does not bound the kind:\n%s", acceptanceSchema)
	}
	if !strings.Contains(acceptancePrompt, `kind "action"`) {
		t.Fatal("the prompt never tells the model what an action is")
	}
}
