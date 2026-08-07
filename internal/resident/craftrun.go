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
	// craftBudgetQuestionPrefix marks the one consent stop a craft run may
	// post, exactly as the daily rail's own prefix marks its. It is stable
	// because the journaled message is also the never-ask-twice marker.
	craftBudgetQuestionPrefix = "Craft budget reached -- "
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

func craftShortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
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
func (c *CraftRunner) Settle(node store.Node, result string) (CraftAdvance, error) {
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
	return c.advance(node, result, workflow)
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
		result, err := c.advance(node, node.Summary, workflow)
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
func (c *CraftRunner) advance(node store.Node, result string, workflow *craft.Workflow) (CraftAdvance, error) {
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
	case step.ForEach != nil && generation == "":
		return c.unroll(prefix, node, step, workflow, result)
	case step.Verify != nil && (generation == "" || generation == craftRoundGeneration):
		round := 1
		if generation == craftRoundGeneration {
			round = index
		}
		return c.round(prefix, node, step, workflow, result, round)
	default:
		return CraftAdvance{}, nil
	}
}

// unroll turns one landed list into real siblings. Everything that was waiting
// on the fan-out step now waits on every item of it — the same rewiring a
// planned container's needs get when the container expands into leaves.
func (c *CraftRunner) unroll(prefix string, node store.Node, step craft.Step, workflow *craft.Workflow, result string) (CraftAdvance, error) {
	items := craftListItems(result, craftFanCap(step.ForEach))
	if len(items) == 0 {
		return CraftAdvance{Stopped: "no items"}, nil
	}
	// The ids are a function of the landed list, so a run that died halfway
	// through its own unroll comes back to exactly the same batch and admits
	// only what is missing from it.
	ids := make([]string, len(items))
	missing := make([]bool, len(items))
	pending := 0
	for index := range items {
		ids[index] = craftGenerationID(prefix, step.ID, craftItemGeneration, index+1)
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
	if stop, err := c.checkLimits(prefix, node, workflow); err != nil || stop != "" {
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
func (c *CraftRunner) round(prefix string, node store.Node, step craft.Step, workflow *craft.Workflow, result string, round int) (CraftAdvance, error) {
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
	if stop, err := c.checkLimits(prefix, node, workflow); err != nil || stop != "" {
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
		repaired = append(repaired, store.Need{NodeID: id, Kind: store.FeedsInto})
		exists, err := c.exists(id)
		if err != nil {
			return CraftAdvance{Spliced: spliced}, err
		}
		if exists {
			continue
		}
		// The repair re-issues the step's own compiled brief — the filled one
		// the first attempt was given — with the verifier's words attached.
		original, ok, err := c.graph.Node(craftNodeID(prefix, target.ID))
		if err != nil {
			return CraftAdvance{Spliced: spliced}, err
		}
		if !ok {
			continue
		}
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
// carries its result: the compiled node, plus the item leaves a fan-out
// unrolled it into.
func craftNeedNodes(live []store.Node, prefix, need string) []string {
	base := craftNodeID(prefix, need)
	items := base + craftIDMark + craftItemGeneration
	sources := []string{base}
	for _, node := range live {
		if strings.HasPrefix(node.ID, items) {
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

// checkLimits is the one gate before any runtime splice. Money first, because
// spending past a bound needs a person; then the clock, which needs nobody —
// it simply stops opening work and says so. Returns the reason nothing may be
// opened, or empty when the run may continue.
func (c *CraftRunner) checkLimits(prefix string, node store.Node, workflow *craft.Workflow) (string, error) {
	now := c.now()
	impact, err := c.graph.Impact(prefix, now)
	if err != nil {
		return "", err
	}
	ceiling := craftCostCeiling(workflow.Limits)
	if impact.Cost >= ceiling {
		if err := c.askForBudget(prefix, node, workflow, impact.Cost, ceiling); err != nil {
			return "", err
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

// askForBudget posts the same pause-and-ask the daily rail posts: today's
// spend against the bound, what continuing costs, and two choices. It asks at
// most once per run, because a stop the user already answered is not a
// question, and a question nobody answered is not improved by repetition.
func (c *CraftRunner) askForBudget(prefix string, node store.Node, workflow *craft.Workflow, spend, ceiling float64) error {
	asked, err := c.budgetAlreadyAsked(prefix)
	if err != nil || asked {
		return err
	}
	prompt := fmt.Sprintf("%sthe %s craft has spent $%.2f of its $%.2f bound. Say the word and I'll keep going; otherwise it delivers what has already landed.",
		craftBudgetQuestionPrefix, strings.TrimSpace(workflow.Name), spend, ceiling)
	options := []store.QuestionOption{
		{Label: "keep going", Value: "craft:continue:" + prefix},
		{Label: "deliver what landed", Value: "craft:stop:" + prefix},
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

func (c *CraftRunner) budgetAlreadyAsked(prefix string) (bool, error) {
	questions, err := c.graph.UnresolvedQuestions(200)
	if err != nil {
		return false, err
	}
	for _, question := range questions {
		if question.OriginNodeID == prefix && strings.Contains(question.Text, craftBudgetQuestionPrefix) {
			return true, nil
		}
	}
	return false, nil
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

// craftListItems reads a fan-out planner's result. The marker wins when it is
// present; without it every non-empty line counts, which is the forgiving
// reading a worker that ignored the format deserves.
func craftListItems(result string, fan int) []string {
	lines := strings.Split(result, "\n")
	start := 0
	for index, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), craftItemsMarker) {
			start = index + 1
		}
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

// craftVerdict reads a check's answer. The marker is required: a round costs
// money, and guessing at a verdict is how a run either loops forever or ships
// something nobody checked.
func craftVerdict(result string) (pass bool, known bool) {
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		index := strings.Index(upper, craftVerdictMarker)
		if index < 0 {
			continue
		}
		verdict := strings.ToLower(strings.TrimSpace(line[index+len(craftVerdictMarker):]))
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
