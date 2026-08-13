// The positive stopping condition, asked as a question.
//
// Growth used to be bounded only negatively: rounds spent, nodes spliced,
// dollars burned. Every one of those answers "have we done too much", and none
// of them answers "is there anything left to do" — which is why a job could
// spend three legitimate rounds inventing verification of the round before it
// and be refused only by arithmetic, long after the money was gone.
//
// A criterion makes the positive question askable. Given what the finished job
// is judged against, what has already landed, and what is in flight together
// with what each piece is committed to producing, a reader can say whether
// every condition is covered. That reader is this call.
package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Landed is one result a job already has in hand.
//
// It is a title and the result's own words rather than the store's dependency
// row, because this package must not learn the journal's shape to ask a
// question about text. The caller that has the graph fills it in.
type Landed struct {
	Title  string
	Result string
}

// Uncovered names one condition of the criterion nothing covers, and what is
// missing from it. Naming the gap is the whole answer: the call never proposes
// work, because proposing work is what the planner is for and what the round
// counter exists to bound.
type Uncovered struct {
	Condition string `json:"condition"`
	Missing   string `json:"missing"`
}

// Satisfaction is the verdict. Complete means every condition is met by
// something landed or committed; the default direction of the prompt is the
// other one, because a gate that wrongly says complete truncates work that was
// genuinely unfinished.
type Satisfaction struct {
	Complete  bool        `json:"complete"`
	Uncovered []Uncovered `json:"uncovered,omitempty"`
}

// satisfiedPrompt is general: no examples, no domain vocabulary, consumed
// identically by a prose job and a code job.
//
// The last paragraph is the 27-round spiral doctrine restated where a gate can
// enforce it. The comments in resident/overrun.go say what a cap alone cannot
// do: a round that invents verification of the round before it is a round whose
// premise the next round inherits, and the set of things left to fix is then
// unbounded by construction. "Checking it again is not coverage, it is a second
// job" is that, said to the only reader positioned to refuse it.
const satisfiedPrompt = `You decide whether a job still needs work added to it.

You are given the criterion the finished job is judged against, what has already landed, and what is still in flight together with what each piece in flight is committed to producing. Assume every piece in flight lands exactly as committed.

Under that assumption, take each condition of the criterion in turn and ask: is it already met, or will it be met by something in flight? A condition that would merely be better served is met. Being richer is not the test. Being met is.

Answer that the job is complete when every condition is covered. For each condition you cannot cover, name the condition and say what is missing from it — the thing no landed and no in-flight work produces.

Do not propose work. Naming the gap is the whole answer. Do not invent conditions the criterion does not contain, and do not treat verification of work already done as a gap: a condition met by a result is met, and checking it again is not coverage, it is a second job.`

var satisfiedSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "complete": { "type": "boolean" },
    "uncovered": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "condition": { "type": "string" },
          "missing": { "type": "string" }
        },
        "required": ["condition", "missing"],
        "additionalProperties": false
      }
    }
  },
  "required": ["complete", "uncovered"],
  "additionalProperties": false
}`)

// satisfiedLimits bound the two tables. A landed result's own summary is the
// expensive half and the least load-bearing — the question is whether a
// condition is covered, not how well — so each entry is clipped hard and the
// list is capped.
//
// satisfiedResultBytes is the fallback clip, for the gate that could not be told
// what window it is being asked through. satisfiedBudget is the rest of the law.
const (
	satisfiedResultBytes = 600
	satisfiedLanded      = 24
	satisfiedInFlight    = 24
)

// The gate's share of its own prompt. Both tables together are what it reads;
// the criterion and the static prompt are the rest, and they are short.
const (
	satisfiedTablesShare = 2
	satisfiedShares      = 3

	// satisfiedFloorTokens is the prompt, the schema and the criterion.
	satisfiedFloorTokens = 4 << 10

	// satisfiedEntries is how many rows the pot is cut into. The two caps above
	// bound the tables at 24 rows each, and the fallback pair already sat at
	// this ratio — 48 × 600 is the pot the old numbers implied — so an unknown
	// window clips exactly where it always did.
	satisfiedEntries = satisfiedLanded + satisfiedInFlight
)

// satisfiedBudget is what one row of either table may carry, sized from the
// window of the model being asked. Computed once per call and passed down, so
// the two tables — which are the cache-shaped part of this prompt — are clipped
// by the same number every time the same rows are rendered.
func satisfiedBudget(contextTokens int) int {
	pot := ctxbudget.For(contextTokens).WithFloor(satisfiedFloorTokens).
		Share(satisfiedTablesShare, satisfiedShares, satisfiedResultBytes*satisfiedEntries)
	if room := pot / satisfiedEntries; room > satisfiedResultBytes {
		return room
	}
	return satisfiedResultBytes
}

// Satisfied asks whether a criterion is already covered by work that exists.
//
// The message order is the cache shape and is deliberate: the static prompt,
// then the criterion — fixed for the job's lifetime — then the landed table in
// admission order, which is append-only so the prefix only ever grows at the
// tail, and last the in-flight commitments, which are the only part that
// changes when a job's shape does. This is the one recurring call the growth
// governor makes, and it is the most cache-friendly shape available to it.
//
// An empty criterion is not a question: nothing is known about what done means,
// so nothing can be said about whether it is reached, and the caller is told the
// job is not complete — which admits growth, the direction that cannot truncate.
// contextTokens is the window of the client being asked; zero is unknown and
// clips the two tables exactly where they were always clipped.
func Satisfied(ctx context.Context, client Completer, contextTokens int, criterion Done, landed []Landed, inflight []Spec) (Satisfaction, Usage, error) {
	if client == nil || criterion.Empty() {
		return Satisfaction{}, Usage{}, nil
	}
	ctx = provider.WithCall(ctx, provider.ClassPlanAudit)
	room := satisfiedBudget(contextTokens)
	messages := []ai.Message{
		systemMessage(satisfiedPrompt),
		userMessage("The criterion this job is judged against:\n" + Spec{Done: criterion}.Render(0)),
		userMessage(landedBlock(landed, room)),
		userMessage(inFlightBlock(inflight, room)),
	}
	var verdict Satisfaction
	response, err := structured(ctx, client, messages, satisfiedSchema, &verdict)
	usage := Usage{}
	usage.Add(usageOf(response))
	if err != nil {
		return Satisfaction{}, usage, fmt.Errorf("ask whether the job is complete: %w", err)
	}
	// A verdict of complete with named gaps is the model disagreeing with
	// itself, and the safe reading of a disagreement about whether to stop is
	// that it did not say stop.
	if verdict.Complete && len(verdict.Uncovered) > 0 {
		verdict.Complete = false
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return verdict, usage, nil
}

func landedBlock(landed []Landed, room int) string {
	if len(landed) == 0 {
		return "Nothing has landed yet."
	}
	if len(landed) > satisfiedLanded {
		landed = landed[len(landed)-satisfiedLanded:]
	}
	var out strings.Builder
	out.WriteString("What has already landed:\n")
	for _, item := range landed {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = "(untitled)"
		}
		out.WriteString("- ")
		out.WriteString(title)
		if result := strings.TrimSpace(item.Result); result != "" {
			out.WriteString(": ")
			out.WriteString(clipSpec(result, room))
		}
		out.WriteString("\n")
	}
	return out.String()
}

func inFlightBlock(inflight []Spec, room int) string {
	if len(inflight) == 0 {
		return "Nothing is in flight."
	}
	if len(inflight) > satisfiedInFlight {
		inflight = inflight[:satisfiedInFlight]
	}
	var out strings.Builder
	out.WriteString("Still in flight, and what each is committed to producing:\n")
	for _, spec := range inflight {
		out.WriteString("- ")
		out.WriteString(firstLineOf(spec.Instruction))
		if !spec.Done.Empty() {
			out.WriteString("\n  commits to: ")
			out.WriteString(strings.ReplaceAll(Spec{Done: spec.Done}.Render(room), "\n", "\n  "))
		}
		out.WriteString("\n")
	}
	return out.String()
}

func firstLineOf(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	if text == "" {
		return "(unstated work)"
	}
	return clipSpec(text, 200)
}
