package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// sizeAnchors is the calibration point of the whole system, and the only place
// a number about harness capacity is written down.
//
// A fixed threshold — "split above six sources" — mis-sizes everything, because
// a research node is source-heavy while a writing node touches almost nothing
// and still takes an hour. But free judgment is worse: asked whether something
// can be decomposed, a model always says yes, which is exactly the runaway we
// spent the spine prompt fighting.
//
// So the model is given a reference frame instead of a rule. Placing a task
// against three concrete examples is a comparison, which models are good at;
// estimating it in the abstract is not. And it makes recalibration tractable —
// a different model or a different harness means rewriting these three examples
// and nothing else. They are a prior today. Once an executor exists and we can
// measure how many steps a leaf really costs, they get replaced by measured
// ones, and every judgment downstream moves with them.
const sizeAnchors = `Use these three reference tasks to judge scale. They are the ruler; place the
node against them rather than estimating it on its own.

TOO SMALL — one action and one answer. Look up a single fact; rename one thing;
  correct one sentence. An agent finishes before it has finished reading the
  instruction. Work this small should have stayed part of something larger,
  because handing it over costs more than doing it.

RIGHT — one self-contained subject taken from start to finished, in one sitting.
  Profile a single named vendor from its own material and write it up; add one
  capability along with the tests that prove it works; draft one section of a
  document. Perhaps ten steps, all on one subject, one deliverable, nothing
  waiting on a decision being made elsewhere. This is the target, and most work
  should land here.

TOO BIG — several unrelated threads tangled together, or more than one
  deliverable hiding inside one instruction. Survey a whole market and recommend
  a product; rebuild a component and migrate everything that uses it and update
  the documentation. Open-ended, no natural stopping point, and an agent doing it
  alone spends most of its time on work that could have been happening at once.

Judge by the breadth of the subject, not the volume of material. A great deal of
material about one subject is RIGHT. A little material about five unrelated
subjects, or one job that quietly contains three different deliverables, is TOO
BIG. A long list of things to touch is not by itself a reason to call something
oversized.`

// sizePrompt asks where a node sits against the anchors, and states the trade-off
// in the currency that actually applies. The economics matter as much as the
// ruler: told only "is this the right size", a model optimises for tidiness,
// but told what splitting costs and what not splitting costs, it optimises for
// the thing we care about. This is the same lever that made the spine stop
// inventing stages.
//
// LinearSubharness names the baseline worker. It is written here rather than
// imported because the ruler is a fact about a subharness, and the package that
// registers subharnesses is the one that already imports this one; a constant
// repeated in two packages is cheaper than a cycle.
const LinearSubharness = "linear"

// Subharness is one specialist offered to the sizing pass: a different way of
// working with a capacity of its own, not a smaller agent. Registration is what
// puts it in front of the sizing model — one nobody registered is one the
// planner cannot choose, which is exactly the additive rule.
type Subharness struct {
	Name    string
	Purpose string
}

// The rulers in force, one per subharness. They are mutable because a measured
// profile can replace a built-in prior, and guarded because chat can
// recalibrate one subharness while a model change or another plan reads
// another.
//
// Linear is seeded with the prior above and is always present: with nothing
// else registered every read, every prompt and every schema below is exactly
// what it was before subharnesses existed.
var (
	anchorMutex     sync.RWMutex
	anchorPrior     = map[string]string{LinearSubharness: sizeAnchors}
	anchorInForce   = map[string]string{LinearSubharness: sizeAnchors}
	subharnessOrder []string
	subharnessBy    = map[string]Subharness{}
)

// Anchors returns one stable snapshot of the linear ruler in force.
func Anchors() string { return AnchorsFor(LinearSubharness) }

// UseAnchors installs a calibrated linear ruler. An empty string restores the
// prior, which is the right fallback whenever a profile is missing or
// unreadable.
func UseAnchors(anchors string) { UseAnchorsFor(LinearSubharness, anchors) }

// AnchorsFor returns the ruler in force for one subharness. An empty name is
// linear, as it is everywhere else; one nobody registered has no ruler at all,
// and saying so with an empty string is more honest than handing back linear's
// — the whole point of a second subharness is that it measures differently.
func AnchorsFor(subharness string) string {
	subharness = normalizeSubharness(subharness)
	anchorMutex.RLock()
	defer anchorMutex.RUnlock()
	return anchorInForce[subharness]
}

// UseAnchorsFor installs a calibrated ruler for one subharness. An empty string
// restores that subharness's own prior — the built-in three examples for
// linear, whatever it shipped with for a specialist.
func UseAnchorsFor(subharness, anchors string) {
	subharness = normalizeSubharness(subharness)
	anchorMutex.Lock()
	defer anchorMutex.Unlock()
	if strings.TrimSpace(anchors) == "" {
		anchors = anchorPrior[subharness]
	}
	anchorInForce[subharness] = anchors
}

// UseSubharness registers a specialist with the sizing pass: its purpose, so
// the model can tell whether a node's essence matches it, and its prior
// anchors, so the node can be placed against that subharness's own ruler rather
// than linear's. Registering linear again only re-seats its prior.
func UseSubharness(subharness Subharness, priorAnchors string) {
	name := strings.TrimSpace(subharness.Name)
	if name == "" {
		return
	}
	subharness.Name = name
	anchorMutex.Lock()
	defer anchorMutex.Unlock()
	if _, known := subharnessBy[name]; !known && name != LinearSubharness {
		subharnessOrder = append(subharnessOrder, name)
		sort.Strings(subharnessOrder)
	}
	if name != LinearSubharness {
		subharnessBy[name] = subharness
	}
	anchorPrior[name] = priorAnchors
	if _, seated := anchorInForce[name]; !seated {
		anchorInForce[name] = priorAnchors
	}
}

// Subharnesses returns the registered specialists in a stable order. Linear is
// never among them: it is the baseline every node already sits against, and a
// menu with one entry is no menu.
func Subharnesses() []Subharness {
	anchorMutex.RLock()
	defer anchorMutex.RUnlock()
	list := make([]Subharness, 0, len(subharnessOrder))
	for _, name := range subharnessOrder {
		list = append(list, subharnessBy[name])
	}
	return list
}

// PurposeFor is what a registered specialist is for, in the words it registered
// with. Linear, empty and anything nobody registered answer nothing at all —
// which is the same "there is no specialist here" every other reader gets, and
// what keeps a prompt that asks for it byte-identical in a process without one.
func PurposeFor(subharness string) string {
	anchorMutex.RLock()
	defer anchorMutex.RUnlock()
	return subharnessBy[strings.TrimSpace(subharness)].Purpose
}

// KnownSubharness reports whether a name reaches a registered specialist. Empty
// and "linear" are the baseline rather than a specialist, so both answer no.
func KnownSubharness(name string) bool {
	anchorMutex.RLock()
	defer anchorMutex.RUnlock()
	_, ok := subharnessBy[strings.TrimSpace(name)]
	return ok
}

// ForgetSubharnesses restores the registry to its linear-only state. Tests own
// it: registration is process-global by design, and a test that adds a
// specialist must be able to put the process back.
func ForgetSubharnesses() {
	anchorMutex.Lock()
	defer anchorMutex.Unlock()
	subharnessOrder = nil
	subharnessBy = map[string]Subharness{}
	anchorPrior = map[string]string{LinearSubharness: sizeAnchors}
	anchorInForce = map[string]string{LinearSubharness: anchorInForce[LinearSubharness]}
}

func normalizeSubharness(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return LinearSubharness
	}
	return name
}

func sizePromptWith(anchors string) string {
	return sizePromptFor(anchors, Subharnesses())
}

// sizePromptFor is the sizing prompt as it has always been, plus one section
// per registered specialist. With none registered the returned bytes are
// exactly the bytes this prompt had before subharnesses existed — that is the
// law, and there is a test that reads it byte for byte.
//
// Its second paragraph restates the worker premise in its own words, and keeps
// them deliberately — workerPremise is the source of truth for the shared fact,
// but this pass is the one that measures capacity, and capacity is precisely
// what the shared fact leaves out. One tool at a time, one deliverable, and a
// context that fills as it goes are the terms every judgment below is made in;
// a prompt that said only "works alone, in order, with tools" would be asking
// for a size against nothing.
func sizePromptFor(anchors string, specialists []Subharness) string {
	prompt := `You judge whether each node is the right size to hand to a single agent.

That agent works alone and in order: it thinks, uses one tool, sees the result,
thinks again. It cannot do two things at once. It produces one deliverable and
stops. Everything it reads stays in front of it, so it slows down as it goes.

` + anchors + `

Judge each node against those anchors:

- atomic      — close to RIGHT, or smaller. One agent should just do it.
- borderline  — larger than RIGHT, but it is not obvious that breaking it up
                would produce parts that could genuinely run at the same time.
- oversized   — clearly toward TOO BIG. Splitting it would let real work happen
                simultaneously that is currently stuck behind other work.

Weigh both costs honestly. Splitting a node costs a round of planning and an
extra result to reassemble, and buys nothing if the parts would just run one
after another anyway. Leaving a node too big costs an agent grinding serially
through work that had no reason to be sequential.

Judge the nodes relative to each other as well as to the anchors — you are
seeing all of them, and the largest few are what matter.

Most nodes in a well-built plan are atomic. Say so when they are.

Also return split_into for every node:

- For an atomic node, return an empty list. Always.
- Otherwise, name the pieces the node would break into that could genuinely run
  at the same time. Each is a short label of two or three words — "Berlin
  permits", "competitor pricing" — a name, not a sentence, and never a copy of
  the node's own summary or of the things it must touch.
- Name a piece only if it is real: something an agent could work on knowing
  nothing about the others, producing a result of its own. If the inside of the
  node is a sequence, or if you would only be restating it in smaller words,
  there are no pieces.
- If you cannot name at least two, return an empty list. That is the answer that
  says to leave the node whole, and it is a common and correct one.`
	if len(specialists) == 0 {
		return prompt
	}
	return prompt + subharnessSection(specialists)
}

// subharnessSection puts the specialists beside the ruler the nodes were just
// judged against. It is written as an inversion rather than an alternative: the
// interesting case is the node that is oversized for one agent working alone
// and is nevertheless one job for a subharness built for exactly that job, and
// naming it is what stops the decomposition.
func subharnessSection(specialists []Subharness) string {
	var section strings.Builder
	section.WriteString("\n\nSome nodes can be taken WHOLE by a specialist subharness. A specialist is not\n" +
		"a smaller agent and not a better one: it is a different way of working, with a\n" +
		"capacity of its own. A node that is oversized for one agent working alone may\n" +
		"be exactly one job for one of these.\n")
	for _, specialist := range specialists {
		fmt.Fprintf(&section, "\n%s — %s\n", specialist.Name, strings.TrimSpace(specialist.Purpose))
		if ruler := strings.TrimSpace(AnchorsFor(specialist.Name)); ruler != "" {
			section.WriteString("\nIts ruler, which replaces the one above for this subharness only:\n\n")
			section.WriteString(ruler)
			section.WriteString("\n")
		}
	}
	section.WriteString("\nAlso return subharness for every node. Name one only when that subharness's\n" +
		"purpose is the essence of the node's own work AND the node sits inside that\n" +
		"subharness's ruler. Naming it says the node is atomic FOR IT and must not be\n" +
		"split, so return an empty split_into with it. Leave subharness empty for mixed\n" +
		"work, for work whose essence is something else, and whenever you are in doubt\n" +
		"— that is the answer for most nodes, and it is never wrong, only slower.")
	return section.String()
}

var sizeSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "sizes": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "node":  { "type": "integer" },
          "size":  { "type": "string", "enum": ["atomic", "borderline", "oversized"] },
          "split_into": { "type": "array", "items": { "type": "string" } }
        },
        "required": ["node", "size", "split_into"],
        "additionalProperties": false
      }
    }
  },
  "required": ["sizes"],
  "additionalProperties": false
}`)

// sizeSchemaFor extends the verdict with the subharness question when there is
// one to ask. The enum carries the registered names and the empty string, so a
// subharness the process does not have cannot be named at all — the degradation
// is structural rather than a check downstream.
func sizeSchemaFor(specialists []Subharness) json.RawMessage {
	if len(specialists) == 0 {
		return sizeSchema
	}
	names := make([]string, 0, len(specialists)+1)
	names = append(names, "")
	for _, specialist := range specialists {
		names = append(names, specialist.Name)
	}
	enum, err := json.Marshal(names)
	if err != nil {
		return sizeSchema
	}
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "sizes": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "node":  { "type": "integer" },
          "size":  { "type": "string", "enum": ["atomic", "borderline", "oversized"] },
          "split_into": { "type": "array", "items": { "type": "string" } },
          "subharness": { "type": "string", "enum": ` + string(enum) + ` }
        },
        "required": ["node", "size", "split_into", "subharness"],
        "additionalProperties": false
      }
    }
  },
  "required": ["sizes"],
  "additionalProperties": false
}`)
}

type sizeVerdict struct {
	Node  int      `json:"node"`
	Size  string   `json:"size"`
	Parts []string `json:"split_into"`
	// Subharness is the specialist that can take this node whole. Empty is the
	// baseline and the overwhelmingly common answer; a name that reaches no
	// registered subharness is dropped, because a plan that asks for a worker
	// we do not have should still get its work done by the generalist.
	Subharness string `json:"subharness,omitempty"`
}

// sizeResult is one stage's verdicts.
type sizeResult struct {
	verdicts []sizeVerdict
	usage    *ai.Usage
	err      error
}

// SizeNodes judges every node in the graph, one call per stage, all at once.
// It reads nothing that bind writes, so it is run concurrently with binding
// rather than as a pass of its own — the judgment is free in wall clock.
//
// Like Bind, it is split into gather and apply: the calls run while another
// pass reads the same graph, so no write may happen until the builder runs
// sizeApply serially.
func SizeNodes(ctx context.Context, client Completer, graph *Graph) (Usage, error) {
	return sizeApply(graph, sizeGather(ctx, client, graph, graph.planBlock()))
}

// sizeGather runs every stage's call against a catalog block the caller has
// already rendered. It never writes to the graph.
func sizeGather(ctx context.Context, client Completer, graph *Graph, shared string) []sizeResult {
	stages := len(graph.Stages)
	if stages == 0 {
		stages = 1
	}

	results := make([]sizeResult, stages)
	var group sync.WaitGroup
	for stage := 1; stage <= stages; stage++ {
		group.Add(1)
		go func(stage int) {
			defer group.Done()
			// A faulted stage lands as a failed one, and sizeApply's default —
			// unjudged work nodes are atomic — carries its nodes the rest of
			// the way, exactly as it does for a stage the model skipped.
			defer func() {
				if recovered := recover(); recovered != nil {
					results[stage-1] = sizeResult{err: guard.Note(fmt.Sprintf("plan/size stage %d", stage), recovered)}
				}
			}()
			verdicts, usage, err := sizeStage(ctx, client, shared, graph, stage)
			results[stage-1] = sizeResult{verdicts: verdicts, usage: usage, err: err}
		}(stage)
	}
	group.Wait()
	return results
}

// sizeApply writes the gathered verdicts into the graph.
func sizeApply(graph *Graph, results []sizeResult) (Usage, error) {
	var usage Usage
	var failures []error
	for _, item := range results {
		usage.Add(item.usage)
		if item.err != nil {
			failures = append(failures, item.err)
			continue
		}
		for _, verdict := range item.verdicts {
			node := graph.Node(verdict.Node)
			if node == nil || node.Kind == KindSynthesis {
				continue
			}
			switch Size(verdict.Size) {
			case SizeAtomic, SizeBorderline, SizeOversized:
				node.Size = Size(verdict.Size)
			}
			node.Parts = shortLabels(verdict.Parts)
			// A named subharness is the other half of the verdict: this node is
			// atomic for it, so the size judgment made against the baseline
			// ruler no longer applies and there is nothing left to split. This
			// is the inversion the whole section exists for — one specialist
			// leaf instead of eight generalist ones.
			if KnownSubharness(verdict.Subharness) {
				node.Subharness = strings.TrimSpace(verdict.Subharness)
				node.Size = SizeAtomic
				node.Parts = nil
			}
		}
	}
	// A node the model skipped is treated as atomic. Defaulting the unknown
	// case toward not expanding is the safe direction: an unnecessary split
	// wastes calls and invites the runaway, while an unsplit node still gets
	// done, only more slowly.
	for index := range graph.Nodes {
		if graph.Nodes[index].Kind == KindWork && graph.Nodes[index].Size == SizeUnknown {
			graph.Nodes[index].Size = SizeAtomic
		}
	}
	return usage, joinErrors(failures)
}

// shortLabels keeps only things that look like names. Asked for parts, a model
// will sometimes hand back the lines it was given — the node's own summary, its
// list of sources — which would make the pre-check pass for every node and stop
// checking anything. A part is a label; anything sentence-length is an echo.
func shortLabels(values []string) []string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		value = trim(value)
		if value == "" || len(strings.Fields(value)) > 5 || strings.ContainsAny(value, ";—") {
			continue
		}
		kept = append(kept, value)
	}
	return kept
}

func sizeStage(ctx context.Context, client Completer, shared string, graph *Graph, stage int) ([]sizeVerdict, *ai.Usage, error) {
	ctx = provider.WithCall(ctx, provider.ClassPlanSize)
	var targets strings.Builder
	found := false
	for _, node := range graph.Nodes {
		if node.Stage != stage || node.Kind == KindSynthesis {
			continue
		}
		found = true
		fmt.Fprintf(&targets, "%d. %s — %s\n", node.ID, node.Title, node.Summary)
		if len(node.Sources) > 0 {
			fmt.Fprintf(&targets, "   must touch: %s\n", strings.Join(node.Sources, "; "))
		}
	}
	if !found {
		return nil, nil, nil
	}
	specialists := Subharnesses()
	messages := []ai.Message{
		systemMessage(sizePromptFor(Anchors(), specialists)),
		userMessage(shared),
		userMessage(fmt.Sprintf("Judge the size of each of these stage %d nodes:\n%s", stage, targets.String())),
	}
	var decoded struct {
		Sizes []sizeVerdict `json:"sizes"`
	}
	response, err := structured(ctx, client, messages, sizeSchemaFor(specialists), &decoded)
	if err != nil {
		return nil, usageOf(response), fmt.Errorf("size stage %d: %w", stage, err)
	}
	// The pass was asked about a named set of nodes; an answer that judges none
	// of them, or judges one that does not exist, did not do the job. Sizes
	// themselves are opinions and are not checkable here.
	if len(decoded.Sizes) == 0 {
		provider.Report(ctx, provider.VerdictSemanticFailure)
		return decoded.Sizes, usageOf(response), nil
	}
	for _, verdict := range decoded.Sizes {
		if graph.Node(verdict.Node) == nil {
			provider.Report(ctx, provider.VerdictSemanticFailure)
			return decoded.Sizes, usageOf(response), nil
		}
	}
	provider.Report(ctx, provider.VerdictVerifiedSuccess)
	return decoded.Sizes, usageOf(response), nil
}
