// The only loop craft has is not a loop in the graph. Fan-out and repair
// rounds both happen the way the revision sentinel already works: a node lands,
// its result is read, and the shape that was implicit in the workflow file
// becomes real nodes spliced into a live job. Nothing about a run lives in this
// process — every decision here is re-derivable from the store plus the craft
// repository, which is what makes a craft run resumable after a crash rather
// than merely restartable.
package resident

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// CraftSource loads one learned workflow by name at the craft repository's
// current version. The repository is the durable home of workflows; the
// sentinel re-reads a run's workflow by name@commit rather than caching a copy,
// so a run that survives a restart is running the same version it started on
// or it refuses to continue.
type CraftSource interface {
	Load(name string) (*craft.Workflow, error)
}

const (
	// craftBudgetQuestionPrefix marks the consent stop a craft run posts,
	// exactly as the daily rail's own prefix marks its. It is stable because the
	// journaled question is also the durable record of what was asked.
	craftBudgetQuestionPrefix = "Craft budget reached -- "
	// craftContinueOption and craftStopOption are the two things a person may
	// say to that stop, in the wire form the head resolves the question with.
	// The run reads the answer back off the question row rather than being
	// handed it: the row is durable, it survives being answered, and it is the
	// same record that proves the question was asked at all. A question the user
	// already answered is not a question, and a run whose consent evaporated
	// when the answer landed would ask forever and never continue.
	craftContinueOption = "craft:continue:"
	craftStopOption     = "craft:stop:"
	// craftBudgetQuestionScan bounds the consent read. One run's money stops are
	// a handful by construction — each one costs a whole bound's worth of spend
	// to reach — and the read is newest-first, so a scan that ran out would
	// under-count consent and ask again rather than spend on consent it never
	// got.
	craftBudgetQuestionScan = 20
	// craftSweepLimit bounds one resume sweep. A craft run is routine work of a
	// couple of dozen leaves; anything past this is not a sweep, it is a
	// migration, and it should not run inside a tick.
	craftSweepLimit = 500
	// craftNodeMessageScan bounds the say-it-once check. One leaf's anchored
	// thread is steering plus a handful of receipts, never a transcript.
	craftNodeMessageScan = 50
)

// CraftRun is what admitting one craft run produced.
type CraftRun struct {
	// Prefix is the run's job root id and id namespace.
	Prefix string
	Nodes  int
	Steps  int
	// Receipt names the craft and its version in the user's own thread.
	Receipt string
}

// CraftAdvance is what one landed craft node caused. Every field is something
// that happened; Stopped is the honest reason nothing more was opened.
type CraftAdvance struct {
	Unrolled int
	Round    int
	Spliced  int
	Receipt  string
	Stopped  string
}

// CraftRunner is the craft half of the sentinel: it admits runs and it
// advances them. It holds no run state — the store and the craft repository
// hold all of it.
type CraftRunner struct {
	graph  *store.Store
	source CraftSource
	dir    string
	now    func() time.Time
}

// NewCraftRunner builds the runner over one store and one craft repository.
// dir makes verifier scripts absolute; it is the repository the source reads.
func NewCraftRunner(graph *store.Store, source CraftSource, dir string) *CraftRunner {
	return &CraftRunner{graph: graph, source: source, dir: dir, now: time.Now}
}

// WithClock replaces the wall clock. Production uses time.Now; the wall-clock
// bound is otherwise untestable without waiting out a real half hour.
func (c *CraftRunner) WithClock(now func() time.Time) *CraftRunner {
	if now != nil {
		c.now = now
	}
	return c
}

// RunCraft compiles one named workflow and admits it under the spine as
// ordinary user-origin work. From here on nothing distinguishes it from a
// planned job except the craft its provenance names.
func (c *CraftRunner) RunCraft(name string, params map[string]string, sessionID, intent string) (CraftRun, error) {
	if c == nil || c.graph == nil || c.source == nil {
		return CraftRun{}, fmt.Errorf("run craft: %w: no craft repository", store.ErrInvalid)
	}
	workflow, err := c.source.Load(name)
	if err != nil {
		return CraftRun{}, fmt.Errorf("run craft %q: %w", name, err)
	}
	if workflow == nil {
		return CraftRun{}, fmt.Errorf("run craft %q: %w: no such workflow", name, store.ErrNotFound)
	}
	if strings.TrimSpace(intent) == "" {
		intent = craftIntent(workflow, params)
	}
	prefix, err := c.nextPrefix(workflow)
	if err != nil {
		return CraftRun{}, fmt.Errorf("run craft %q: %w", name, err)
	}
	provenance := store.Provenance{
		Origin:    store.OriginUser,
		SessionID: sessionID,
		Intent:    intent,
		Craft:     CraftRef(workflow),
	}
	subtree, err := CompileCraftAs(prefix, c.dir, workflow, params, provenance)
	if err != nil {
		return CraftRun{}, err
	}
	if err := c.graph.Splice(store.RootID, subtree, provenance); err != nil {
		return CraftRun{}, fmt.Errorf("run craft %q: %w", name, err)
	}
	return CraftRun{
		Prefix: prefix, Nodes: len(subtree.Nodes), Steps: len(workflow.Steps),
		Receipt: craftCompileReceipt(workflow),
	}, nil
}

// craftCompileReceipt names what is about to run and which version of it.
func craftCompileReceipt(workflow *craft.Workflow) string {
	steps := len(workflow.Steps)
	receipt := fmt.Sprintf("using your %s craft", strings.TrimSpace(workflow.Name))
	if commit := strings.TrimSpace(workflow.Commit); commit != "" {
		receipt += " v " + craftShortCommit(commit)
	}
	return fmt.Sprintf("%s — %d %s", receipt, steps, plural(steps, "step", "steps"))
}

// craftShortCommit is a version as a person reads it. An uncommitted edit
// carries its content hash so the guard can tell two edits apart; the receipt
// keeps the word and drops the hash, because what the reader needs to know is
// that this run is on a file nobody saved, not which unsaved file it is.
func craftShortCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	short, marked, dirty := strings.Cut(commit, "+")
	if len(short) > 7 {
		short = short[:7]
	}
	if dirty {
		if word, _, _ := strings.Cut(marked, "-"); word != "" {
			return short + "+" + word
		}
	}
	return short
}

func craftIntent(workflow *craft.Workflow, params map[string]string) string {
	intent := "run the " + strings.TrimSpace(workflow.Name) + " craft"
	named := make([]string, 0, len(params))
	for _, param := range workflow.Params {
		if value := strings.TrimSpace(params[strings.TrimSpace(param.Name)]); value != "" {
			named = append(named, strings.TrimSpace(param.Name)+"="+value)
		}
	}
	if len(named) > 0 {
		intent += " (" + strings.Join(named, ", ") + ")"
	}
	return intent
}

// nextPrefix mints a fresh id namespace for this craft. The store is the
// counter: asking the same craft for twice must not collide with the run
// already on the graph.
func (c *CraftRunner) nextPrefix(workflow *craft.Workflow) (string, error) {
	nodes, err := c.graph.Nodes()
	if err != nil {
		return "", err
	}
	base := "craft-" + craftSlug(workflow.Name)
	taken := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		taken[node.ID] = true
	}
	for attempt := 1; ; attempt++ {
		candidate := fmt.Sprintf("%s-%d", base, attempt)
		if !taken[candidate] {
			return candidate, nil
		}
	}
}

// Settle advances the run one landed node belongs to. It is called with the
// result the node is about to complete with, before that completion settles:
// splicing while the landed node is still open keeps the job's root open too,
// so nothing downstream can start on a plan that is one splice out of date.
// Best effort by construction — the sweep re-derives anything this missed.
//
// landing is what this leaf just spent and has not journaled yet. It is the
// same idiom the daily rail's WithAdditionalSpend is, and it is load-bearing
// here: the leaf whose landing opens a fan-out is precisely the leaf whose cost
// the usage table does not have yet, so a money gate that read the table alone
// would be exactly one leaf behind at the one moment it decides whether to open
// two dozen more.
func (c *CraftRunner) Settle(node store.Node, result string, landing float64) (CraftAdvance, error) {
	if c == nil || c.graph == nil || c.source == nil {
		return CraftAdvance{}, nil
	}
	if strings.TrimSpace(node.Provenance.Craft) == "" {
		return CraftAdvance{}, nil
	}
	workflow, err := c.load(node.Provenance.Craft, nil)
	if err != nil {
		return CraftAdvance{}, err
	}
	return c.advance(node, result, workflow, landing)
}

// Sweep re-derives every craft run's next move from the store alone. It is the
// resume path: after a crash, a restart, or a Rebuild, the runs that were
// mid-flight are exactly the landed control nodes whose consequences are not
// on the graph yet, and nothing else is needed to find them.
func (c *CraftRunner) Sweep(ctx context.Context) (int, error) {
	if c == nil || c.graph == nil || c.source == nil {
		return 0, nil
	}
	nodes, err := c.graph.ActiveNodes()
	if err != nil {
		return 0, err
	}
	cache := make(map[string]*craft.Workflow)
	advanced, scanned := 0, 0
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return advanced, err
		}
		if node.Status != store.Done || strings.TrimSpace(node.Provenance.Craft) == "" {
			continue
		}
		if scanned++; scanned > craftSweepLimit {
			break
		}
		workflow, err := c.load(node.Provenance.Craft, cache)
		if err != nil {
			continue
		}
		// Nothing is in flight here: every node this pass reads has already
		// journaled whatever it spent, so the gate needs no additional spend.
		result, err := c.advance(node, node.Summary, workflow, 0)
		if err != nil {
			continue
		}
		advanced += result.Spliced
	}
	return advanced, nil
}

// load reads a run's workflow at the version its provenance names. A craft
// repository that moved under a live run is a refusal, not a silent swap: the
// nodes already on the graph were compiled from a version that no longer
// exists, and finishing the run with a different one would make the receipt a
// lie and the survival statistic worthless.
func (c *CraftRunner) load(reference string, cache map[string]*craft.Workflow) (*craft.Workflow, error) {
	if cached, ok := cache[reference]; ok {
		if cached == nil {
			return nil, fmt.Errorf("craft %q is not loadable", reference)
		}
		return cached, nil
	}
	name := reference
	if cut := strings.LastIndex(reference, "@"); cut > 0 {
		name = reference[:cut]
	}
	workflow, err := c.source.Load(name)
	if err == nil && workflow != nil && CraftRef(workflow) != reference {
		err = fmt.Errorf("craft %q moved to %q since this run started", reference, CraftRef(workflow))
		workflow = nil
	}
	if err == nil && workflow == nil {
		err = fmt.Errorf("craft %q is no longer in the repository", reference)
	}
	if cache != nil {
		cache[reference] = workflow
	}
	if err != nil {
		return nil, err
	}
	return workflow, nil
}

// advance is the whole sentinel: what kind of node landed, whether the run is
// still allowed to open work, and the one splice that follows.
func (c *CraftRunner) advance(node store.Node, result string, workflow *craft.Workflow, landing float64) (CraftAdvance, error) {
	prefix, stepID, generation, index, ok := craftNodeParts(node.ID)
	if !ok {
		return CraftAdvance{}, nil
	}
	var step craft.Step
	found := false
	for _, candidate := range workflow.Steps {
		if craftSlug(candidate.ID) == stepID {
			step, found = candidate, true
			break
		}
	}
	if !found {
		return CraftAdvance{}, nil
	}
	switch {
	// A repair copy of a fan-out step is still a fan-out step. The generation
	// says which attempt this is, never what kind of work it is — and a repair
	// that listed its items again and had nobody unroll them is a round that
	// re-listed instead of redoing the work.
	case step.ForEach != nil && (generation == "" || generation == craftRoundGeneration):
		return c.unroll(prefix, node, step, workflow, result, landing)
	case step.Verify != nil && (generation == "" || generation == craftRoundGeneration):
		round := 1
		if generation == craftRoundGeneration {
			round = index
		}
		return c.round(prefix, node, step, workflow, result, round, landing)
	default:
		return CraftAdvance{}, nil
	}
}

// unroll turns one landed list into real siblings. Everything that was waiting
// on the fan-out step now waits on every item of it — the same rewiring a
// planned container's needs get when the container expands into leaves.
func (c *CraftRunner) unroll(prefix string, node store.Node, step craft.Step, workflow *craft.Workflow, result string, landing float64) (CraftAdvance, error) {
	items := craftListItems(result, craftFanCap(step.ForEach))
	if len(items) == 0 {
		advance := CraftAdvance{Stopped: "no items",
			Receipt: fmt.Sprintf("the %q step named no items to fan out over — taking it no further",
				strings.TrimSpace(step.ID))}
		c.post(node, advance.Receipt)
		return advance, nil
	}
	// The ids are a function of the landed list AND of the node that listed it,
	// so a run that died halfway through its own unroll comes back to exactly
	// the same batch and admits only what is missing from it — and a repair
	// round's items sit under the repair rather than on top of the previous
	// round's.
	ids := make([]string, len(items))
	missing := make([]bool, len(items))
	pending := 0
	for index := range items {
		ids[index] = craftChildID(node.ID, craftItemGeneration, index+1)
		exists, err := c.exists(ids[index])
		if err != nil {
			return CraftAdvance{}, err
		}
		missing[index] = !exists
		if !exists {
			pending++
		}
	}
	dependents, err := c.pendingDependents(node.ID, ids)
	if err != nil {
		return CraftAdvance{}, err
	}
	if pending == 0 {
		// Nothing left to admit, but the rewiring may be what did not finish.
		return CraftAdvance{}, c.attachSources(dependents, ids)
	}
	if stop, err := c.checkLimits(prefix, node, workflow, landing); err != nil || stop != "" {
		return CraftAdvance{Stopped: stop}, err
	}

	// The compiled brief taught the planner how to produce a list; the item
	// leaves carry the assignment that brief already spells out, filled with
	// this run's own parameters. The workflow file no longer knows them.
	assignment := craftAssignment(node.Brief)

	upstream := make([]store.Need, 0, len(step.Needs)+1)
	upstream = append(upstream, store.Need{NodeID: node.ID, Kind: store.FeedsInto})
	for _, need := range step.Needs {
		if id := craftNodeID(prefix, need); id != node.ID {
			upstream = append(upstream, store.Need{NodeID: id, Kind: store.FeedsInto})
		}
	}

	spliced := 0
	for index, item := range items {
		if !missing[index] {
			continue
		}
		spec := store.NodeSpec{
			ID:    ids[index],
			Brief: craftItemBrief(assignment, item, index+1, len(items)),
			Title: clipLabel(item, 48),
			Group: strings.TrimSpace(workflow.Name),
			Stage: node.Stage + 1,
			Needs: append([]store.Need(nil), upstream...),
		}
		if err := c.graph.Splice(prefix, store.Subtree{Nodes: []store.NodeSpec{spec}},
			c.provenance(node, workflow)); err != nil {
			return CraftAdvance{Unrolled: spliced, Spliced: spliced}, err
		}
		spliced++
	}
	if err := c.attachSources(dependents, ids); err != nil {
		return CraftAdvance{Unrolled: spliced, Spliced: spliced}, err
	}
	advance := CraftAdvance{
		Unrolled: spliced, Spliced: spliced,
		Receipt: fmt.Sprintf("%s craft — %q fans out over %d %s",
			strings.TrimSpace(workflow.Name), strings.TrimSpace(step.ID),
			len(ids), plural(len(ids), "item", "items")),
	}
	c.post(node, advance.Receipt)
	return advance, nil
}

// round buys one more bounded attempt at a failed check. The repair steps are
// fresh copies carrying the verifier's own output, the check is re-spliced
// after them, and whatever was waiting on the old check now waits on the new
// one. Rounds are bounded by the file, and the bound is enforced from the
// graph itself: the attempt number is in the node id, so it survives every
// crash and every rebuild without a counter anywhere in this process.
func (c *CraftRunner) round(prefix string, node store.Node, step craft.Step, workflow *craft.Workflow, result string, round int, landing float64) (CraftAdvance, error) {
	pass, known := craftVerdict(result)
	if !known {
		advance := CraftAdvance{Stopped: "no verdict",
			Receipt: fmt.Sprintf("the %q check reported no verdict — taking it no further", strings.TrimSpace(step.ID))}
		c.post(node, advance.Receipt)
		return advance, nil
	}
	if pass {
		return CraftAdvance{}, nil
	}
	maxRounds := craftMaxRounds(step.Verify.UntilPass)
	if step.Verify.UntilPass == nil || round >= maxRounds {
		advance := CraftAdvance{Stopped: "rounds exhausted",
			Receipt: craftExhaustedReceipt(step, round, maxRounds)}
		c.post(node, advance.Receipt)
		return advance, nil
	}
	next := round + 1
	verifyID := craftGenerationID(prefix, step.ID, craftRoundGeneration, next)
	if exists, err := c.exists(verifyID); err != nil || exists {
		return CraftAdvance{}, err
	}
	if stop, err := c.checkLimits(prefix, node, workflow, landing); err != nil || stop != "" {
		return CraftAdvance{Stopped: stop}, err
	}

	revise := craftReviseSteps(step)
	live, err := c.graph.ActiveNodes()
	if err != nil {
		return CraftAdvance{}, err
	}
	// The repair copies consume the failed check, so they are consumers of it
	// too; the round's own nodes are excluded from the rewiring for the same
	// reason a fan-out's items are.
	own := []string{verifyID}
	for _, reviseID := range revise {
		if target, ok := craftFindStep(workflow, reviseID); ok {
			own = append(own, craftGenerationID(prefix, target.ID, craftRoundGeneration, next))
		}
	}
	dependents, err := c.pendingDependents(node.ID, own)
	if err != nil {
		return CraftAdvance{}, err
	}
	repaired := make([]store.Need, 0, len(revise))
	spliced := 0
	for _, reviseID := range revise {
		target, ok := craftFindStep(workflow, reviseID)
		if !ok {
			continue
		}
		id := craftGenerationID(prefix, target.ID, craftRoundGeneration, next)
		exists, err := c.exists(id)
		if err != nil {
			return CraftAdvance{Spliced: spliced}, err
		}
		if exists {
			repaired = append(repaired, store.Need{NodeID: id, Kind: store.FeedsInto})
			continue
		}
		// The repair re-issues the step's own compiled brief — the filled one
		// the first attempt was given — with the verifier's words attached. A
		// step whose node is gone (surgery removed it mid-run) cannot be
		// repaired, and the new check must not be promised a dependency this
		// round is never going to plant: Splice would refuse the check for
		// needing an unknown node, and the sweep would re-derive that same
		// refusal on every tick forever.
		original, ok, err := c.graph.Node(craftNodeID(prefix, target.ID))
		if err != nil {
			return CraftAdvance{Spliced: spliced}, err
		}
		if !ok {
			continue
		}
		repaired = append(repaired, store.Need{NodeID: id, Kind: store.FeedsInto})
		brief := original.Brief
		spec := store.NodeSpec{
			ID:    id,
			Brief: craftRoundBrief(brief, result, next, maxRounds),
			Title: strings.TrimSpace(target.ID),
			Group: strings.TrimSpace(workflow.Name),
			Stage: node.Stage,
			Needs: []store.Need{{NodeID: node.ID, Kind: store.FeedsInto}},
		}
		// A repair consumes what the original consumed, including the items a
		// fan-out step unrolled into — the list node's own result is a list,
		// and the work the repair has to build on is in the items.
		for _, need := range craftStepNeeds(target) {
			for _, source := range craftNeedNodes(live, prefix, need) {
				spec.Needs = append(spec.Needs, store.Need{NodeID: source, Kind: store.FeedsInto})
			}
		}
		if err := c.graph.Splice(prefix, store.Subtree{Nodes: []store.NodeSpec{spec}},
			c.provenance(node, workflow)); err != nil {
			return CraftAdvance{Spliced: spliced}, err
		}
		spliced++
	}
	if len(repaired) == 0 {
		advance := CraftAdvance{Stopped: "nothing to revise",
			Receipt: craftExhaustedReceipt(step, round, maxRounds)}
		c.post(node, advance.Receipt)
		return advance, nil
	}

	verify := store.NodeSpec{
		ID: verifyID,
		// The same check, unchanged: a round that softened its own verifier
		// would be a round that always passes.
		Brief: node.Brief,
		Title: strings.TrimSpace(step.ID),
		Group: strings.TrimSpace(workflow.Name),
		Stage: node.Stage + 1,
		Needs: repaired,
	}
	if err := c.graph.Splice(prefix, store.Subtree{Nodes: []store.NodeSpec{verify}},
		c.provenance(node, workflow)); err != nil {
		return CraftAdvance{Spliced: spliced}, err
	}
	spliced++
	if err := c.attachSources(dependents, []string{verifyID}); err != nil {
		return CraftAdvance{Round: next, Spliced: spliced}, err
	}
	advance := CraftAdvance{
		Round: next, Spliced: spliced,
		Receipt: fmt.Sprintf("check failed — one more round (%d of %d)", next, maxRounds),
	}
	c.post(node, advance.Receipt)
	return advance, nil
}

func craftExhaustedReceipt(step craft.Step, round, maxRounds int) string {
	if step.Verify.UntilPass == nil {
		return fmt.Sprintf("the %q check failed and the craft buys no rounds for it — delivering what landed, unverified",
			strings.TrimSpace(step.ID))
	}
	return fmt.Sprintf("the %q check still failed after %d %s — delivering what landed, unverified",
		strings.TrimSpace(step.ID), maxRounds, plural(maxRounds, "round", "rounds"))
}

// craftReviseSteps names what a failed check re-runs: the file's own list, or
// the check's direct inputs when it named none.
func craftReviseSteps(step craft.Step) []string {
	if step.Verify.UntilPass != nil && len(step.Verify.UntilPass.Revise) > 0 {
		return step.Verify.UntilPass.Revise
	}
	return step.Needs
}

// craftNeedNodes resolves one step reference to every node that actually
// carries its result: the compiled node, plus every generation of it the run
// has minted since — the item leaves a fan-out unrolled into, the repair copies
// a round re-issued, and the items those repairs unrolled in their turn. All of
// them are that step's work, and a repair handed only the first generation
// would build on results the run has already replaced. The id mark is what
// makes the test exact: it cannot occur inside a step id, so a sibling step
// whose name merely starts the same way never matches.
func craftNeedNodes(live []store.Node, prefix, need string) []string {
	base := craftNodeID(prefix, need)
	generations := base + craftIDMark
	sources := []string{base}
	for _, node := range live {
		if strings.HasPrefix(node.ID, generations) {
			sources = append(sources, node.ID)
		}
	}
	return sources
}

func craftFindStep(workflow *craft.Workflow, id string) (craft.Step, bool) {
	for _, step := range workflow.Steps {
		if craftSlug(step.ID) == craftSlug(id) {
			return step, true
		}
	}
	return craft.Step{}, false
}

// pendingDependents names the unstarted consumers of a node, excluding the
// nodes this advance owns. The exclusion is load-bearing: what an advance
// plants is itself a consumer of the same landed node, and wiring that batch
// to itself is how a fan-out becomes a deadlock instead of a batch.
func (c *CraftRunner) pendingDependents(from string, own []string) ([]string, error) {
	mine := make(map[string]bool, len(own))
	for _, id := range own {
		mine[id] = true
	}
	edges, err := c.graph.ActiveEdges()
	if err != nil {
		return nil, err
	}
	var waiting []string
	for _, edge := range edges {
		if edge.From != from || edge.Kind == store.Suggests || mine[edge.To] {
			continue
		}
		consumer, ok, err := c.graph.Node(edge.To)
		if err != nil {
			return nil, err
		}
		if ok && consumer.Status == store.Pending {
			waiting = append(waiting, edge.To)
		}
	}
	return waiting, nil
}

// attachSources makes everything that was waiting on a landed control node
// wait on its consequences too. A consumer that started in the meantime is
// left alone rather than failing the advance: it built its transcript from
// what it had, and rewriting history is not on offer.
func (c *CraftRunner) attachSources(dependents, sources []string) error {
	for _, dependent := range dependents {
		for _, source := range sources {
			if source == dependent {
				continue
			}
			if err := c.graph.AddEdge(source, dependent, store.FeedsInto); err != nil &&
				!errors.Is(err, store.ErrInvalid) {
				return err
			}
		}
	}
	return nil
}

// checkLimits is the one gate before any runtime splice. What the person
// already said comes first, because a run they closed must not open work no
// matter what the numbers say; then money, because spending past a bound needs
// a person; then the clock, which needs nobody — it simply stops opening work
// and says so. Returns the reason nothing may be opened, or empty when the run
// may continue.
func (c *CraftRunner) checkLimits(prefix string, node store.Node, workflow *craft.Workflow, landing float64) (string, error) {
	consent, err := c.budgetConsent(prefix)
	if err != nil {
		return "", err
	}
	if consent.stopped {
		if err := c.closeRun(prefix, node, workflow); err != nil {
			return "", err
		}
		return "stopped by you", nil
	}
	now := c.now()
	impact, err := c.graph.Impact(prefix, now)
	if err != nil {
		return "", err
	}
	spend := impact.Cost + landing
	ceiling := craftCeiling(workflow.Limits, consent.grants)
	if spend >= ceiling {
		if !consent.asked {
			if err := c.askForBudget(prefix, node, workflow, spend, ceiling); err != nil {
				return "", err
			}
		}
		return "cost", nil
	}
	bound := craftWallClock(workflow.Limits)
	started, err := c.startedAt(prefix)
	if err != nil {
		return "", err
	}
	if !started.IsZero() && now.Sub(started) >= bound {
		receipt := fmt.Sprintf("%s craft hit its %s bound — delivered what landed",
			strings.TrimSpace(workflow.Name), craftDuration(bound))
		c.post(node, receipt)
		return "wall clock", nil
	}
	return "", nil
}

// startedAt is when the run was admitted: the journal entry that spliced its
// root. The root node itself has no start time — it is the last thing to run —
// so the wall-clock bound is measured from the moment the run became real.
func (c *CraftRunner) startedAt(prefix string) (time.Time, error) {
	root, ok, err := c.graph.Node(prefix)
	if err != nil || !ok {
		return time.Time{}, err
	}
	events, err := c.graph.Events(root.CreatedSeq-1, 1)
	if err != nil || len(events) == 0 {
		return time.Time{}, err
	}
	return events[0].Time, nil
}

// craftConsent is everything the person has already said about one run's
// money, read off the run's own question rows.
type craftConsent struct {
	// grants is how many times they said keep going; each one buys one more of
	// the craft's own bound.
	grants int
	// asked is a money stop that has been put to them and has not bought
	// headroom — still waiting, or answered with something that was not a
	// choice. Either way, asking it again is repetition, not a question.
	asked bool
	// stopped is the run they closed.
	stopped bool
}

// budgetConsent reads the run's money stops back off the graph. It is the
// durable half of asking at most once: the questions survive being answered,
// so the record of "this was already asked" cannot evaporate the moment the
// user answers it — which is what turned a single stop into a stutter that
// re-asked on every sweep and bought nothing when it was answered.
func (c *CraftRunner) budgetConsent(prefix string) (craftConsent, error) {
	questions, err := c.graph.QuestionsForNode(prefix, craftBudgetQuestionScan)
	if err != nil {
		return craftConsent{}, err
	}
	var consent craftConsent
	for _, question := range questions {
		if !strings.Contains(question.Text, craftBudgetQuestionPrefix) {
			continue
		}
		answered, keepGoing, ok := craftBudgetAnswer(question.Resolution)
		switch {
		case question.Status != store.QuestionAnswered || !ok || answered != prefix:
			consent.asked = true
		case keepGoing:
			consent.grants++
		default:
			consent.stopped = true
		}
	}
	return consent, nil
}

// craftBudgetAnswer decodes one answer to a craft's money stop, fail-closed in
// the way every option decoder in this system is: the value the question
// offered is the whole vocabulary, and anything else — free text, another
// run's prefix, a truncated marker — is not consent.
func craftBudgetAnswer(resolution string) (prefix string, keepGoing bool, ok bool) {
	resolution = strings.TrimSpace(resolution)
	switch {
	case strings.HasPrefix(resolution, craftContinueOption):
		prefix, keepGoing = strings.TrimSpace(strings.TrimPrefix(resolution, craftContinueOption)), true
	case strings.HasPrefix(resolution, craftStopOption):
		prefix, keepGoing = strings.TrimSpace(strings.TrimPrefix(resolution, craftStopOption)), false
	default:
		return "", false, false
	}
	return prefix, keepGoing, prefix != ""
}

// craftCeiling is the run's bound after consent: the craft's own bound, plus
// one more of it for every time the person said keep going. One bound per yes
// is the daily rail's shape too — consent is for continuing, not for removing
// the bound, and a run that blows through the extra bound is a second decision
// worth a second question.
func craftCeiling(limits craft.Limits, grants int) float64 {
	if grants < 0 {
		grants = 0
	}
	return craftCostCeiling(limits) * float64(1+grants)
}

// closeRun is what "deliver what landed" means on a graph. Nothing that has not
// started will start; the run's root — the one node that owes the person an
// answer — is left open to assemble whatever did land. A cancelled dependency
// is terminal, so the root becomes ready rather than waiting forever on work
// nobody is going to do.
func (c *CraftRunner) closeRun(prefix string, node store.Node, workflow *craft.Workflow) error {
	nodes, err := c.graph.ActiveNodes()
	if err != nil {
		return err
	}
	member := prefix + craftIDMark
	for _, candidate := range nodes {
		if candidate.Status != store.Pending || !strings.HasPrefix(candidate.ID, member) {
			continue
		}
		if err := c.graph.CancelPending(candidate.ID, "you asked the craft to stop and deliver what landed"); err != nil {
			// A leaf that started while this was being decided keeps running:
			// it built its transcript from what it had, and taking the work
			// away mid-turn buys nothing the next boundary does not.
			if errors.Is(err, store.ErrInvalid) || errors.Is(err, store.ErrNotFound) {
				continue
			}
			return err
		}
	}
	c.post(node, fmt.Sprintf("%s craft — stopping here as you asked; delivering what already landed",
		strings.TrimSpace(workflow.Name)))
	return nil
}

// askForBudget posts the same pause-and-ask the daily rail posts: the run's
// spend against its bound, what continuing buys, and two choices. Whether it
// may be asked at all is budgetConsent's judgment, not this function's.
func (c *CraftRunner) askForBudget(prefix string, node store.Node, workflow *craft.Workflow, spend, ceiling float64) error {
	prompt := fmt.Sprintf("%sthe %s craft has spent $%.2f of its $%.2f bound. Say the word and I'll keep going with another $%.2f; otherwise it delivers what has already landed.",
		craftBudgetQuestionPrefix, strings.TrimSpace(workflow.Name), spend, ceiling,
		craftCostCeiling(workflow.Limits))
	options := []store.QuestionOption{
		{Label: "keep going", Value: craftContinueOption + prefix},
		{Label: "deliver what landed", Value: craftStopOption + prefix},
	}
	allowFree := true
	question, err := c.graph.AskQuestion(store.AgentQuestion{
		SessionID: node.Provenance.SessionID,
		Text: boundMessage(store.QuestionMessageBody(prompt, options, store.QuestionConfig{
			Kind: store.QuestionConfirm, Category: store.QuestionCategoryRailRaise,
			Default: "2", AllowFree: &allowFree,
		})),
		OriginNodeID: prefix, Urgency: store.QuestionBlocking,
		Options: options, Category: store.QuestionCategoryRailRaise, DefaultAnswer: "2",
	})
	if err != nil {
		return err
	}
	_, err = c.graph.SurfaceQuestion(question.Seq)
	return err
}

func (c *CraftRunner) provenance(node store.Node, workflow *craft.Workflow) store.Provenance {
	return store.Provenance{
		// A runtime splice is the sentinel's own move, not a second thing the
		// user asked for; the verbatim ask is preserved so the run still reads
		// as one job everywhere provenance is shown.
		Origin:    store.OriginSelf,
		SessionID: node.Provenance.SessionID,
		Intent:    node.Provenance.Intent,
		Craft:     CraftRef(workflow),
	}
}

func (c *CraftRunner) exists(id string) (bool, error) {
	_, ok, err := c.graph.Node(id)
	return ok, err
}

// post says one thing once. The sweep re-derives a landed node's move on every
// tick, and a bound that has been reached stays reached — without this the
// honest single line would become a stutter in the thread.
func (c *CraftRunner) post(node store.Node, body string) {
	body = boundMessage(body)
	if body == "" || strings.TrimSpace(node.Provenance.SessionID) == "" {
		return
	}
	said, err := c.graph.NodeMessages(node.ID, 0, craftNodeMessageScan)
	if err != nil {
		return
	}
	for _, message := range said {
		if message.Body == body {
			return
		}
	}
	_, _ = c.graph.PostMessage(store.Message{
		SessionID: node.Provenance.SessionID,
		Role:      store.RoleSystem,
		NodeID:    node.ID,
		Body:      body,
	})
}

// craftListItems reads a fan-out planner's result. The marker is required, the
// way the verdict marker is: without it there is no way to tell a list from a
// paragraph, and the forgiving reading — every non-empty line is an item —
// turns a worker that said nothing into a batch of leaves titled after its
// apology, each one a real model call against the run's money.
func craftListItems(result string, fan int) []string {
	lines := strings.Split(result, "\n")
	start := -1
	for index, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), craftItemsMarker) {
			start = index + 1
		}
	}
	if start < 0 {
		return nil
	}
	items := make([]string, 0, fan)
	for _, line := range lines[start:] {
		item := craftListItem(line)
		if item == "" {
			continue
		}
		items = append(items, item)
		if fan > 0 && len(items) >= fan {
			break
		}
	}
	return items
}

func craftListItem(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimPrefix(line, "* ")
	for index := 0; index < len(line); index++ {
		if line[index] >= '0' && line[index] <= '9' {
			continue
		}
		if index > 0 && (line[index] == '.' || line[index] == ')') && index+1 < len(line) && line[index+1] == ' ' {
			line = line[index+2:]
		}
		break
	}
	return strings.TrimSpace(line)
}

// craftVerdictLine finds the marker in a line of the verifier's own output.
// The case-insensitive match runs over the ORIGINAL bytes rather than over a
// folded copy: upper-casing is not length-preserving in every language a
// verifier may print in — Turkish ı becomes I and loses a byte, ﬁ becomes FI
// and gains one — so an offset found in the copy indexes somewhere else in the
// line, and the verdict a check actually gave is read as no verdict at all.
var craftVerdictLine = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(craftVerdictMarker))

// craftVerdict reads a check's answer. The marker is required: a round costs
// money, and guessing at a verdict is how a run either loops forever or ships
// something nobody checked.
func craftVerdict(result string) (pass bool, known bool) {
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		found := craftVerdictLine.FindStringIndex(line)
		if found == nil {
			continue
		}
		verdict := strings.ToLower(strings.TrimSpace(line[found[1]:]))
		switch {
		case strings.HasPrefix(verdict, craftVerdictPass):
			return true, true
		case strings.HasPrefix(verdict, craftVerdictFail):
			return false, true
		}
	}
	return false, false
}

func craftDuration(bound time.Duration) string {
	if bound >= time.Hour && bound%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(bound/time.Hour))
	}
	return fmt.Sprintf("%dm", int(bound.Round(time.Minute)/time.Minute))
}
