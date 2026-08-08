package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// fanoutPrompt splits one stage into simultaneous parts. It never sees another
// stage's parts and never needs to: ordering was settled by the spine, so the
// only question left is what is independent — a local question. That locality
// is exactly what lets every stage expand at the same time, and it is also the
// design's one accepted blind spot, since two stages can unknowingly produce
// the same node. Bind catches that afterwards.
//
// The subject test is the guard against the failure that locality invites.
// Asked to split anything, a model reaches for the phases of the procedure it
// would follow itself — gather, analyse, write — and those come back as
// siblings, which the harness then runs at the same time. Reviewing a pull
// request produced exactly that: "Apply diff", "Read code", "Run tests" and
// "Find defects" as four dependency-free parts. Phases are not independent;
// they are one node's internal shape, and the only correct answer for them is
// to leave the procedure whole.
const fanoutPrompt = `You list the parts of one stage that all run at the same time.

` + agentPremise + `

Split this stage along whatever axis makes its parts genuinely independent —
separate subjects, sources, options, regions, components, layers, cases,
dimensions, angles, time periods, whatever the work is actually made of. Do not
split it into steps.
Anything that must happen in order belongs to the stage sequence, which is
already fixed and not yours to change.

A part is legitimate only when it is a subject someone can own: one agent takes
it from start to finished while knowing nothing about what the other parts
produced. Test every part that way before you keep it.

Phases of one procedure are never parts. "Gather the material", "analyse it",
"write it up" is one procedure — the analysis has nothing to work on until the
gathering is done, and the write-up needs both — so none of the three is ownable
alone. "Apply the change", "check it", "report on what happened" is the same
shape. A subject split looks different: one part per venue, per component, per
option, per region — each finishable on its own, none reading another's output.

When the split you have in mind comes out phase-shaped, the answer is not a
better set of phases. Keep the whole procedure inside one node — gathering,
analysing and writing up one subject is a single part — and split along a
subject axis instead, or return one part.

One thing does get its own part even though it is not a subject: an action that
changes the material every other part works from — prepare the workspace, apply
the patch, fetch or generate the corpus, install what the others run against.
That is one part on its own, and it comes first; the others are written as
working on the material it leaves behind, not as doing it again. It is also
where a shared orientation summary belongs — it reads the material once and
writes down what the others would otherwise each have to rediscover, so five
siblings do not each re-read the same material. Do not invent one where nothing
shared is actually changed; most stages have no such part.

Default to fewer parts. Only split out a part when you can say what makes it
doable by an agent that knows nothing about the others. Give 1 to 5 parts, and
return a single part when the stage is genuinely one piece of work — that is a
correct answer, not a failure to decompose.

When the goal names one final deliverable — a file, a report, a document — no
part of this stage produces it. The parts produce the material it is made of;
producing it is one job with one owner, and that owner is the plan's last node
or the assembly that is added after you. Two parts that both end in writing the
same document are one part.

Cover the stage with minimal overlap. Do not include a merge or summary part.
Stay inside this stage: do not produce work that belongs to another stage.

` + titleRule + `

For each part also list the distinct things an agent would have to touch to
finish it: pages, documents, datasets, files, components, interfaces, decisions —
whatever this particular work actually involves. Name them concretely: "each of
the four venue websites", "the parser and its tests", not "research" or "the
code". List every one, however many that is. Do not round the list down to look
tidy: it is used to judge how big the part really is, and an honest long list is
more useful than a short one.

` + proportionRule

var fanoutSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "parts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "title":   { "type": "string" },
          "summary": { "type": "string" },
          "sources": { "type": "array", "items": { "type": "string" } }
        },
        "required": ["title", "summary", "sources"],
        "additionalProperties": false
      }
    }
  },
  "required": ["parts"],
  "additionalProperties": false
}`)

type stageResult struct {
	nodes []Node
	usage *ai.Usage
	err   error
}

// FanOut expands every stage at once. Each call carries the same frozen prefix —
// goal plus the whole spine — so the varying part is a single line, which is
// both the cheapest shape to generate and the friendliest to a prefix cache.
func FanOut(ctx context.Context, client Completer, premise string, stages []Stage) ([]Node, Usage, error) {
	shared := premise + "\nThe full stage sequence:\n" + spineBlock(stages)

	results := make([]stageResult, len(stages))
	var group sync.WaitGroup
	for index, stage := range stages {
		group.Add(1)
		go func(index int, stage Stage) {
			defer group.Done()
			// A faulting stage fills its own slot before Done runs, so the
			// collector below reads a failed stage rather than an empty one and
			// the other stages still land.
			defer func() {
				if recovered := recover(); recovered != nil {
					results[index] = stageResult{err: guard.Note(fmt.Sprintf("plan/fanout stage %d", index+1), recovered)}
				}
			}()
			nodes, usage, err := fanOutStage(ctx, client, shared, index+1, stage)
			results[index] = stageResult{nodes: nodes, usage: usage, err: err}
		}(index, stage)
	}
	group.Wait()

	var usage Usage
	var nodes []Node
	var failures []error
	for _, result := range results {
		usage.Add(result.usage)
		if result.err != nil {
			failures = append(failures, result.err)
			continue
		}
		nodes = append(nodes, result.nodes...)
	}
	return nodes, usage, joinErrors(failures)
}

func fanOutStage(ctx context.Context, client Completer, shared string, stage int, definition Stage) ([]Node, *ai.Usage, error) {
	ctx = provider.WithCall(ctx, provider.ClassPlanFanOut)
	messages := []ai.Message{
		systemMessage(fanoutPrompt),
		userMessage(shared),
		userMessage(fmt.Sprintf("List the simultaneous parts of stage %d, %q: %s", stage, definition.Title, definition.Summary)),
	}
	var decoded struct {
		Parts []Node `json:"parts"`
	}
	response, err := structured(ctx, client, messages, fanoutSchema, &decoded)
	if err != nil {
		return nil, usageOf(response), fmt.Errorf("fan-out stage %d: %w", stage, err)
	}
	nodes := make([]Node, 0, len(decoded.Parts))
	for _, node := range decoded.Parts {
		node.Stage = stage
		node.Title = trim(node.Title)
		node.Summary = trim(node.Summary)
		node.State = StatePending
		node.Kind = KindWork
		node.Sources = cleanStrings(node.Sources)
		if node.Title == "" && node.Summary == "" {
			continue
		}
		nodes = append(nodes, node)
	}
	if len(nodes) == 0 {
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return nil, usageOf(response), annotate(fmt.Errorf("fan-out stage %d: no parts returned", stage), response)
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return nodes, usageOf(response), nil
}

func spineBlock(stages []Stage) string {
	var block string
	for index, stage := range stages {
		block += fmt.Sprintf("Stage %d — %s: %s\n", index+1, stage.Title, stage.Summary)
	}
	return block
}

// cleanStrings normalises the source list. An empty list is meaningful — it
// says the node touches nothing external — so it is preserved rather than
// treated as missing data.
func cleanStrings(values []string) []string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if value = trim(value); value != "" {
			kept = append(kept, value)
		}
	}
	return kept
}

var _ = errors.Join
