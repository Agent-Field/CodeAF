package plan

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// AN OVERSIZED NODE THAT CANNOT BE DIVIDED INTO SIMULTANEOUS PARTS IS DIVIDED
// INTO ORDERED STAGES.
//
// The sizing pass has always named two things that discharge the burden of
// dividing: pieces that genuinely run at the same time, or a node that cannot be
// brought to an end inside what one worker can hold. Only the first ever had
// machinery behind it. A node whose inside is a sequence was asked the
// simultaneity question twice — once by the fan-out, once again by the expansion
// underneath it — and when it truthfully came back as one part both times, the
// second answer was journaled as "the division gave back one piece, which is the
// node again" and the node ran whole against the very budget it had already been
// measured past.
//
// This file is the second question, and it is asked only of the nodes the first
// one could not answer: after a fan-out has returned one part for a node the
// ruler put beyond one worker's reach. The division it looks for is in time
// rather than in width — the stages the piece passes through, each finishable on
// its own and each within reach — and the chain it draws is a shape the graph
// already has, so nothing downstream has to learn a new one.

// sequenceDepth is the most stages one piece may be drawn as, and it is stated
// once for two readers: the prompt below interpolates it, so the model is told
// the number, and the reply is given room for that many stages. A count a model
// is asked for and a count it is given room for are one fact — see fanOutWidth,
// where the two spellings drifting apart killed a whole run.
const sequenceDepth = 4

// sequenceDepthWord is sequenceDepth as the prompt spells it.
var sequenceDepthWord = strconv.Itoa(sequenceDepth)

// sequencePrompt asks for the ordered stages of one piece of work.
//
// It is the size prompt's second discharge, written as a question. Everything it
// states about why it is being asked comes from there in the same terms — the
// piece cannot be carried to an end inside what one worker can hold — because a
// model told only "break this into stages" draws the tidy phase list that the
// fan-out prompt spends four paragraphs refusing, and every one of those phases
// would be a barrier bought for nothing.
//
// So the premise it opens with is the one thing that makes an ordered division
// legitimate here, and it is stated as already settled: the simultaneity
// question has been asked and the answer was the node itself. What is left is
// the only division that remains, and the guards on it are the spine's own —
// fewest stages, no merge stage, and one stage is a correct answer.
//
// It names no domain and carries no example, for the reason the expansion burden
// beside it carries none: a domain named in a prompt that every sequence in
// every job passes through is a shape the model will find again whether or not
// it is there.
var sequencePrompt = `You break one piece of work into the ordered stages it is made of.

` + agentPremise + `

This piece cannot be brought to an end inside what one worker can hold: however
it is written, the worker would be stopped part-way through and resumed. Nothing
inside it runs at the same time as anything else — that has already been asked,
and the answer was the piece itself — so the only division left is in time.

Draw the ordered stages the piece passes through. Each one is finishable and
checkable on its own, produces a deliverable of its own, and is within one
worker's reach. What a stage leaves behind is what the next stage starts from,
so a stage that has to see the whole piece at once is not a stage.

For every stage list in "needs" the number of the stage whose output it starts
from. The first stage starts from the piece itself and lists none: []. Two
stages that could be done in either order are one stage, not two.

Use the fewest stages that are genuinely ordered, and never more than ` + sequenceDepthWord + `. Do
not add a final merge, synthesis or summary stage; that is added after you.

Return a single stage when the piece has no order inside it. That leaves the
piece whole, and it is a correct answer rather than a failure to divide.

` + titleRule + `

` + proportionRule + `

` + verdictRule

// sequenceOnce asks the stage question of one piece and returns what it drew,
// levelled.
//
// It reuses the spine's schema and the spine's levelling rather than growing a
// second grammar for stages: what comes back is a list, and a list has an order
// whether or not the work does. Levelling reads the stated needs instead, so
// stages that wait for nothing are one stage — and a "sequence" whose stages
// wait for nothing is not one, which the acceptance check then says in words.
//
// The class is named here rather than left to the caller because a call should
// say what it is. Inside an expansion the outer override makes it plan.expand
// either way, which is where every stage question is asked from today.
func sequenceOnce(ctx context.Context, client Completer, goal string) ([]Stage, *ai.Usage, error) {
	ctx = provider.WithCall(ctx, provider.ClassPlanExpand)
	messages := []ai.Message{
		systemMessage(sequencePrompt),
		userMessage(goal),
	}
	var decoded struct {
		Stages []Stage `json:"stages"`
	}
	response, err := structuredParts(ctx, client, messages, spineSchema, sequenceDepth, &decoded)
	if err != nil {
		return nil, usageOf(response), fmt.Errorf("stages: %w", err)
	}
	stages := keptStages(decoded.Stages)
	if len(stages) == 0 {
		// Schema-valid and useless, exactly as the spine reads it: the reply
		// parsed, so nothing upstream could have caught it.
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return nil, usageOf(response), annotate(errors.New("stages: no stages returned"), response)
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return orderedStages(stages), usageOf(response), nil
}

// orderedStages reads a drawn stage list as the sequence it was asked for.
//
// The spine's levelling is the right reading of every stage list that says what
// it waits for, and it is the wrong one for a list that says nothing at all. A
// spine's order is the order the work was SPOKEN in, so a spine whose stages
// name no needs is seven things that can all start now; this question asked for
// the stages in the order they happen, and the schema makes the needs field
// mandatory, so a model that answered the ask and left the bookkeeping empty
// hands back exactly that list. Levelling it would fold the whole sequence into
// one stage and refuse it as the node again — the old dead end reached by a new
// road, and it was seen happening on the second of two draws of the same brief.
//
// So: where any stage states what it waits for, the stated needs are the
// schedule and levelling reads them, folding stages that wait for nothing
// together exactly as it does for a spine. Where none of them states anything,
// the order they were drawn in is the sequence.
func orderedStages(stages []Stage) []Stage {
	for _, stage := range stages {
		if len(stage.Needs) > 0 {
			return levelled(stages)
		}
	}
	return clearNeeds(stages)
}

// keptStages drops what a stage list says nothing with. It is shared with the
// spine because it is one rule about one schema, and two copies of it would be
// two answers to "what counts as a stage".
func keptStages(drawn []Stage) []Stage {
	stages := make([]Stage, 0, len(drawn))
	for _, stage := range drawn {
		stage.Title = trim(stage.Title)
		stage.Summary = trim(stage.Summary)
		if stage.Title == "" && stage.Summary == "" {
			continue
		}
		stages = append(stages, stage)
	}
	return stages
}

// expandAsStages is the second move: the stage question asked of one node, and
// the chain it draws returned as this node's expansion.
//
// It is reached only when the fan-out has already come back with one part, so
// the ordinary node — the one whose inside really is several subjects — pays
// nothing new for it, and the expansion of a sequence still costs the two calls
// the flat path costs. The sizing pass those two calls would have spent on the
// single restated part is spent here instead, on the chain, where the answer
// changes something.
//
// What comes back is accepted through the acceptance check the parallel case
// already uses (WorthKeeping), because its three tests are exactly the chain's
// tests, read in the chain's terms: one link is the node again, two links that
// finish on the same condition are one link with the answer bought twice, and
// links that came back no smaller than the node are a sequence nobody shortened
// — the piece would be stopped part-way in each of them as it was in the node.
func expandAsStages(ctx context.Context, client Completer, sub *Graph, node *Node, goal string, spent Usage) expansion {
	usage := spent
	stages, stageUsage, err := sequenceOnce(ctx, client, goal)
	usage.Add(stageUsage)
	if err != nil {
		return expansion{nodeID: node.ID, usage: usage, err: fmt.Errorf("expand %q: %w", node.Title, err)}
	}
	chainInto(sub, stages)
	// A chain of one link is the node in different words, and the acceptance
	// check already has the words for that. Sizing it would be paying a call to
	// be told what the single link's own title says. A chain the ruler never
	// reached is atomic link by link (see sizeApply), so a failed sizing call
	// leaves the chain standing as drawn rather than throwing away a division
	// that has already been paid for.
	if workNodes(sub) >= 2 {
		sizeUsage, _ := SizeNodes(ctx, client, sub)
		usage.merge(sizeUsage)
	}
	sub.Goal = node.Title
	return expansion{nodeID: node.ID, sub: sub, usage: usage}
}

// chainInto lays the drawn stages into the sub-graph the expansion already
// assembled, as the ordered work nodes the splice will carry: each link waits on
// the one before it, and the first waits on whatever the node itself was waiting
// on, which Splice fills in.
//
// It writes into that sub-graph rather than building its own so that what a
// sub-planner is given — the settled points it may not rebind, the evidence
// standard, the terrain it must not re-imagine, the prices its sizing pass
// weighs the links against — is assembled in exactly one place, whichever of the
// two questions the expansion ended up asking.
//
// Every link is put in one stage of that sub-graph rather than one stage each.
// The order is carried by the needs — Splice rewrites Stage to the parent's and
// remaps the needs into real edges — and a sub-graph of four stages would buy
// four sizing calls to judge four links that one call judges together, against a
// catalog that is stronger for holding all of them at once.
func chainInto(sub *Graph, stages []Stage) {
	previous := 0
	for _, stage := range stages {
		link := Node{Kind: KindWork, Stage: 1, Title: stage.Title, Summary: stage.Summary}
		if previous != 0 {
			link.Needs = []int{previous}
		}
		previous = sub.Add(link)
	}
}

// dividesInTime reports whether the burden's second discharge is even available
// to a node.
//
// Only a node the ruler put past one worker's reach may be cut into stages,
// because being stopped part-way is the whole of the reason to cut a sequence:
// a node already within reach would pay a briefing per stage and a reassembly at
// the end for a sequence it was going to run anyway. That is the null hypothesis
// holding, and it is the same boundary JudgeSplit draws.
func dividesInTime(node *Node) bool {
	return node.Size == SizeOversized || node.Size == SizeBorderline
}
