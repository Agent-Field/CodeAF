package plan

// The acceptance checklist: what the request asked for, read once, before the
// work.
//
// This is the fact nothing in the system held. `Done` is the criterion the
// PLANNER states about what the work will produce — at most six conditions, in
// the planner's own words, aimed at outputs. A request's forty bullets of stated
// behaviour never became structure anywhere, so at the moment of judgement there
// was no checklist for a deliverable to be short against, and the gate weighed
// the only account of coverage it had: the worker's own sentence about the tests
// the worker had itself written. Two graded runs shipped at exit 0 that way, one
// of them a single hidden test short of a solve (docs/design/gate/ACCEPTANCE.md).
//
// It reads the request and nothing else, and it runs before any work exists, so
// nothing it says can have moved in response to what the work turned out to be —
// which is the same property Grounds protects and for the same reason.
//
// THE WORKER IS NEVER SHOWN THIS LIST. Spec.Render deliberately omits it. A
// worker handed the list of behaviours it will be checked on writes checks for
// the list and nothing else, which is the failure being fixed one level up.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Point is one behaviour the request states, and the words of the request it is
// a reading of.
//
// Two fields because the two are used by different readers and neither can do
// the other's job. Behaviour is what a repair round is aimed at and what a
// person reads in the finding; Quote is what the grounding rule weighs, and a
// point whose quote is not the person's own words is a requirement this system
// invented for itself and may not hold anybody to.
type Point struct {
	Behaviour string `json:"behaviour"`
	Quote     string `json:"quote"`
}

// Empty reports that this point says nothing that could be checked or grounded.
func (p Point) Empty() bool {
	return strings.TrimSpace(p.Behaviour) == "" || strings.TrimSpace(p.Quote) == ""
}

// acceptancePrompt asks for the checklist and nothing else.
//
// The last two paragraphs are the ones that earn their place, and they are the
// same two the criterion block already spells, because they guard against the
// same two failures. A model asked to list what a request asks for will
// generalise — it will write "the implementation is correct and well tested",
// which is checkable against nothing — and it will invent, which turns the
// harness's own taste into a requirement the person never made.
const acceptancePrompt = `You read one request and list the behaviours it states.

A behaviour is something that must be observably true of the finished work: a
rule it must follow, a case it must handle, a transition it must make, an input
it must accept, an outcome it must produce. One point per behaviour the request
states, in the request's own vocabulary, short enough to read in one breath.

Every point carries the words of the request it comes from. Quote them verbatim
— you may skip a middle with "..." and quote both halves, but every character
either side of an elision must be the request's own. A point you cannot quote is
a point the request did not make.

Write no point the request did not state. Do not add what a careful engineer
would also do, what the domain usually requires, or what would make the result
better. Every point you invent becomes a requirement nobody asked for, and the
work will be sent back to satisfy it.

Do not restate the request's summary, its title, or what the work is broadly
about. Those are not behaviours and nothing can check them.

Answer with one bare JSON object and nothing else — no code fence around it and
no sentence before or after it:
{"points": [{"behaviour": "<what must be observably true>", "quote": "<the request's own words>"}]}`

var acceptanceSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "points": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "behaviour": {"type": "string"},
          "quote": {"type": "string"}
        },
        "required": ["behaviour", "quote"],
        "additionalProperties": false
      }
    }
  },
  "required": ["points"],
  "additionalProperties": false
}`)

type acceptanceReply struct {
	Points []Point `json:"points"`
}

// Acceptance reads a request and returns the behaviours it states.
//
// It is one call, on the request alone, and it is deliberately not folded into
// the brief or the criterion pass: those two are written for the WORKER and this
// is written for the gate, and a checklist assembled in the same breath as the
// instruction is a checklist the instruction has already seen.
//
// An empty list is a legitimate reading. A request that states no checkable
// behaviour — a question, a lookup, a piece of prose — has no acceptance
// checklist, and everything downstream of this behaves exactly as it did before
// this existed. A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN.
func Acceptance(ctx context.Context, client Completer, request string) ([]Point, Usage, error) {
	var usage Usage
	request = strings.TrimSpace(request)
	if client == nil || request == "" {
		return nil, usage, nil
	}
	ctx = provider.WithCall(ctx, provider.ClassPlanGround)
	messages := []ai.Message{
		systemMessage(acceptancePrompt),
		userMessage("The request:\n" + request),
	}
	var decoded acceptanceReply
	response, err := structured(ctx, client, messages, acceptanceSchema, &decoded)
	usage.Add(usageOf(response))
	if err != nil {
		return nil, usage, fmt.Errorf("acceptance: %w", err)
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return NormalizeAcceptance(request, decoded.Points), usage, nil
}

// NormalizeAcceptance bounds and cleans what a model returned.
//
// THE CAP IS DERIVED FROM THE REQUEST ITSELF AND NOT TYPED. A request cannot
// state more behaviours than it has lines: past one point per non-empty line the
// model has stopped describing the request and started describing the domain,
// and the checklist has become the thing this whole invariant exists to keep out.
// It needs no constant, it scales with the ask — the ofetch circuit-breaker
// request is forty-two lines and states about that many behaviours; a one-line
// request states one — and there is no number for a later wave to tune wrongly.
//
// Points are deduplicated on their quote, because two readings of one sentence
// are one behaviour said twice, and a checklist that counted them twice would
// buy two repair rounds for one gap.
func NormalizeAcceptance(request string, points []Point) []Point {
	ceiling := statedLines(request)
	if ceiling == 0 {
		return nil
	}
	clean := make([]Point, 0, len(points))
	seen := map[string]bool{}
	for _, point := range points {
		if len(clean) >= ceiling {
			break
		}
		point = Point{
			Behaviour: strings.TrimSpace(point.Behaviour),
			Quote:     strings.TrimSpace(point.Quote),
		}
		if point.Empty() {
			continue
		}
		key := strings.ToLower(strings.Join(strings.Fields(point.Quote), " "))
		if seen[key] {
			continue
		}
		seen[key] = true
		clean = append(clean, point)
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

// statedLines counts the lines of a request that say anything. It is the whole
// of the cap above, and it is a count of the person's own text rather than of
// anything this system produced.
func statedLines(request string) int {
	lines := 0
	for _, line := range strings.Split(request, "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	return lines
}

// AcceptanceQuotes is the checklist's citations, in the form every grounding
// rule in this program already reads: one string per point, the request's own
// words. It is here rather than at each caller so the checklist has one
// rendering into the shape the invariant weighs.
func AcceptanceQuotes(points []Point) []string {
	quotes := make([]string, 0, len(points))
	for _, point := range points {
		if quote := strings.TrimSpace(point.Quote); quote != "" {
			quotes = append(quotes, quote)
		}
	}
	return quotes
}

// SetAcceptance stamps the checklist on the node that DELIVERS, and on no other.
//
// The list is the request's, so it belongs to whoever hands the finished thing
// over. Every other node in a plan contributes material to that node and was
// never asked for the whole request's behaviours; holding one of them to the
// list would be failing a worker for work that was never its. The delivery gate
// draws the same line from the other end — it judges the node whose parent is
// the root and nothing else — so the two agree by construction rather than by
// two readings of one rule.
//
// It answers to deliverableOwner, which is where "who produces the finished
// thing" is already decided, and it does nothing when the plan has no single
// owner: a graph with several sinks has not gathered yet, and stamping the list
// on all of them would buy one repair round per sink for one gap.
func (g *Graph) SetAcceptance(points []Point) {
	if g == nil || len(points) == 0 {
		return
	}
	owner, _ := g.deliverableOwner()
	if owner == 0 {
		return
	}
	node := g.Node(owner)
	if node == nil {
		return
	}
	node.Spec.Accept = points
}
