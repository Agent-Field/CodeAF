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
//
// The grain paragraph is the guard against the opposite failure, which is what
// the width-2 incident was: a stage whose material already enumerated its units
// was halved into two parts that were each handed the entire enumeration, so
// both did the whole job, the person paid for it twice, and the wait was longer
// than one agent's. Coarse halving is what a model reaches for when nothing
// tells it where the seams are; the seams were already in the material. So the
// rule is stated as a reading of the inputs rather than as a number — follow the
// enumeration the material presents, and leave whole what it does not present
// as standing apart.
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

Let the material set the width. Where the work to be done is already enumerated —
the goal names the units, a finished dependency's result names them, the sources
name them — that enumeration is the split, and the parts follow it: one unit
each, or one batch of them each when the units outnumber the parts you are
allowed below, batched evenly so no part carries the set. Do not replace an
enumeration the material already made with a coarser
one of your own, and never write two parts that would each cover the same set:
both would do all of it, so the person pays for the whole job twice and waits
longer than for one agent. Every part names the units it owns and no others,
which is also what keeps its context small — each part is told about its own
units and not the rest, so units taken many-wide cost close to what one agent's
single pass over all of them would have cost, and the person waits for one
unit's work instead of all of them in turn.

The same reading is the counterweight, and it points the other way just as
often: units the material does not present as standing apart are not made
independent by being separated. Where finishing one piece needs what another
piece would have found, or where the answer depends on holding the whole set
together, the enumeration is not a split and the work stays whole.

Default to fewer parts. Only split out a part when you can say what makes it
doable by an agent that knows nothing about the others. Give 1 to 5 parts, and
return a single part when the stage is genuinely one piece of work — that is a
correct answer, not a failure to decompose.

Every part costs a worker, and a worker is not free: each one pays its own
orientation, setup and delivery before it produces anything, whatever the size
of the unit it was handed. Weigh the split against that fixed cost. Units one
agent could finish in a single short sitting — a few small files, a handful of
short pieces, one pass over material that fits in one context — do not earn
their own workers: batch them into one part, or leave the stage whole. Split
only when the parts are each substantial enough that doing them at the same
time repays several workers' fixed costs in the time it saves.

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

` + proportionRule + `

` + verdictRule

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

// stageResult carries the stage's accounting as a plan Usage rather than one
// response's, because a stage may now cost two calls: the acceptance check
// below buys one free retry, and a slot that could only hold one response would
// have billed the second to nobody.
type stageResult struct {
	nodes []Node
	usage Usage
	err   error
}

// FanOut expands every stage at once, with no enumeration in force. It is the
// call a caller with no grounding to hand over makes.
func FanOut(ctx context.Context, client Completer, premise string, stages []Stage) ([]Node, Usage, error) {
	return FanOutWith(ctx, client, premise, stages, nil)
}

// FanOutWith expands every stage at once. Each call carries the same frozen
// prefix — goal plus the whole spine — so the varying part is a single line,
// which is both the cheapest shape to generate and the friendliest to a prefix
// cache.
//
// The settlements travel with it because the acceptance check below is keyed on
// them and on nothing else. A stage split under a bound enumeration owes one
// unit per part; a stage split under none owes nothing of the kind, and the
// check does not run. The list is read, never rendered — the premise already
// carries the settled block, and re-rendering it here would move the prefix.
func FanOutWith(ctx context.Context, client Completer, premise string, stages []Stage, settled []Settlement) ([]Node, Usage, error) {
	shared := premise + "\nThe full stage sequence:\n" + spineBlock(stages)
	enumerated := Enumerated(settled)

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
					// The call is still counted. A stage that faulted may have
					// spent one anyway, and a plan that quietly dropped it from
					// the accounting would understate itself — which is the same
					// reason Usage.Add counts a response the provider was quiet
					// about.
					results[index] = stageResult{
						usage: Usage{Calls: 1},
						err:   guard.Note(fmt.Sprintf("plan/fanout stage %d", index+1), recovered),
					}
				}
			}()
			nodes, usage, err := fanOutStage(ctx, client, shared, index+1, stage, enumerated)
			results[index] = stageResult{nodes: nodes, usage: usage, err: err}
		}(index, stage)
	}
	group.Wait()

	var usage Usage
	var nodes []Node
	var failures []error
	for _, result := range results {
		usage.merge(result.usage)
		if result.err != nil {
			failures = append(failures, result.err)
			continue
		}
		nodes = append(nodes, result.nodes...)
	}
	return nodes, usage, joinErrors(failures)
}

func fanOutStage(ctx context.Context, client Completer, shared string, stage int, definition Stage, enumerated bool) ([]Node, Usage, error) {
	ctx = provider.WithCall(ctx, provider.ClassPlanFanOut)
	messages := []ai.Message{
		systemMessage(fanoutPrompt),
		userMessage(shared),
		userMessage(fmt.Sprintf("List the simultaneous parts of stage %d, %q: %s", stage, definition.Title, definition.Summary)),
	}
	var decoded struct {
		Parts []Node `json:"parts"`
	}
	var usage Usage
	response, err := structured(ctx, client, messages, fanoutSchema, &decoded)
	usage.Add(usageOf(response))
	if err != nil {
		return nil, usage, fmt.Errorf("fan-out stage %d: %w", stage, err)
	}
	nodes := fanOutNodes(stage, decoded.Parts)
	if len(nodes) == 0 {
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return nil, usage, annotate(fmt.Errorf("fan-out stage %d: no parts returned", stage), response)
	}
	if refusal := worthSplitting(nodes, enumerated); refusal != "" {
		// One free retry, on the same bytes, for the same reason the ground pass
		// retries: a split that names no units is a miss rather than a position,
		// and asking the same question twice is cheaper than running the parts.
		var again struct {
			Parts []Node `json:"parts"`
		}
		retry, retryErr := structured(ctx, client, messages, fanoutSchema, &again)
		usage.Add(usageOf(retry))
		if retryErr == nil {
			if retried := fanOutNodes(stage, again.Parts); len(retried) > 0 && worthSplitting(retried, enumerated) == "" {
				provider.Report(ctx, provider.VerdictVerifiedSuccess)
				return retried, usage, nil
			}
		}
		// Refused twice. The parts are collapsed back into the one stage they
		// were always a restatement of, which is the answer the prompt itself
		// calls correct — "return a single part when the stage is genuinely one
		// piece of work". Running them instead is what produced six identical
		// briefs, six workers and one job's worth of work done six times.
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return []Node{wholeStage(stage, definition, nodes)}, usage, nil
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return nodes, usage, nil
}

// fanOutNodes normalises one reply's parts into work nodes.
func fanOutNodes(stage int, parts []Node) []Node {
	nodes := make([]Node, 0, len(parts))
	for _, node := range parts {
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
	return nodes
}

// worthSplitting is worthKeeping's shape at the other place work gets divided,
// and it is here because the two checks were never wired to the same door. The
// expansion path has refused splits that claim no difference since it existed;
// the fan-out path — which produces most of the leaves in most plans — accepted
// anything with a title, and a six-vendor stage came back as six parts that
// named no vendor at all. Three of the six briefs then invented the same vendor.
//
// It is keyed on the enumeration and runs nowhere else. Where the material bound
// a variable to several values, the prompt's own law is already exact — "Every
// part names the units it owns and no others" — so a multi-part split owes a
// reference per part, and two parts referencing the same unit are two parts
// doing the same job. Where nothing was enumerated there is no reference to
// check against and the check is silent, which is why an ordinary goal pays it
// nothing.
//
// It names no unit itself, reads no text and calls no model: it is a predicate
// over the source lists the schema already required.
func worthSplitting(parts []Node, enumerated bool) string {
	if !enumerated || len(parts) < 2 {
		return ""
	}
	seen := map[string]int{}
	for index, part := range parts {
		if len(part.Sources) == 0 {
			return fmt.Sprintf("part %d names no unit it owns", index+1)
		}
		for _, source := range part.Sources {
			key := squash(source)
			if key == "" {
				continue
			}
			if owner, taken := seen[key]; taken && owner != index {
				return fmt.Sprintf("parts %d and %d both own %q", owner+1, index+1, source)
			}
			seen[key] = index
		}
	}
	return ""
}

// wholeStage is the stage left whole: one part, the stage's own title and
// summary, carrying every unit the refused split named between its parts. The
// units are kept because they are the honest touch-list of the work either way,
// and losing them would make the undivided node look smaller than it is.
func wholeStage(stage int, definition Stage, refused []Node) Node {
	sources := make([]string, 0)
	seen := map[string]bool{}
	for _, part := range refused {
		for _, source := range part.Sources {
			if key := squash(source); key != "" && !seen[key] {
				seen[key] = true
				sources = append(sources, source)
			}
		}
	}
	return Node{
		Stage:     stage,
		Title:     trim(definition.Title),
		Summary:   trim(definition.Summary),
		Sources:   sources,
		State:     StatePending,
		Kind:      KindWork,
		Undivided: RefusalSameAnswer,
	}
}

func spineBlock(stages []Stage) string {
	var block string
	for index, stage := range stages {
		block += fmt.Sprintf("Stage %d — %s: %s\n", index+1, stage.Title, stage.Summary)
	}
	return block
}

// cleanStrings normalises a string list, dropping the blanks.
//
// The empty result is preserved rather than reported, because this function
// cannot tell the two things it would mean apart. For a long time the comment
// here said an empty source list "says the node touches nothing external", and
// that rationalisation was load-bearing: it is why six parts of an enumerated
// stage, none of which named a single unit, were accepted as six independent
// subjects. An empty list means either "nothing to touch" or "the reply named
// nothing", and only a caller that knows whether a reference was owed can say
// which — see worthSplitting, which is that caller.
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
