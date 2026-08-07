// A learned workflow is not a second engine. It compiles here into the same
// admission shape a planned graph produces — steps are leaves, needs are
// feeds_into edges — so persistence, resume, cost accounting, steering,
// surgery and the load governor are all inherited rather than reimplemented.
// Everything this file knows how to plant is an ordinary node; the two shapes
// craft adds beyond a flat graph (fan-out and repair rounds) are planted as
// single leaves whose landed results the sentinel expands at runtime.
package resident

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/craft"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	// craftIDMark separates a craft run's prefix, its step, and a runtime
	// generation inside one node id. Step ids are slugged to lowercase
	// alphanumerics and hyphens, so this character cannot occur inside one and
	// the id parses back to (prefix, step, generation) without ambiguity — the
	// property the sentinel depends on to know what a landed node was.
	craftIDMark = "~"
	// craftItemGeneration and craftRoundGeneration prefix the two runtime
	// splices. "i" is one unrolled for_each item, "r" is one repair round.
	craftItemGeneration  = "i"
	craftRoundGeneration = "r"

	// craftItemsMarker ends a for_each planner's answer. A bare "one item per
	// line" instruction loses to preamble every time; an explicit final marker
	// costs one line and makes the parse exact.
	craftItemsMarker = "ITEMS:"
	// craftVerdictMarker opens a verify leaf's answer. Exit status lives inside
	// the worker's shell, so the verdict has to cross back as text, and it has
	// to be unmistakable: a round is spent on it.
	craftVerdictMarker = "VERDICT:"
	craftVerdictPass   = "pass"
	craftVerdictFail   = "fail"

	// craftItemPlaceholder is the one reference a for_each step's brief may
	// leave unresolved at compile: it is filled per item at unroll.
	craftItemPlaceholder = "item"
	// craftAssignmentMarker ends a fan-out planner's brief and opens the
	// per-item assignment. The unroll reads the assignment back off the landed
	// node rather than recompiling it, because the node's brief is the only
	// durable copy of what the run's parameters actually filled in.
	craftAssignmentMarker = "--- item assignment ---"
)

// craftReference matches {{name}} with optional inner padding. Anything that
// still matches after substitution is a hole nobody filled, and a step brief
// with a hole in it is worse than no run at all.
var craftReference = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)

// CraftRef is the durable version identity of one loaded workflow: the name
// survival stats group by, and the commit that says which draft of it ran.
func CraftRef(workflow *craft.Workflow) string {
	if workflow == nil {
		return ""
	}
	name := strings.TrimSpace(workflow.Name)
	commit := strings.TrimSpace(workflow.Commit)
	if commit == "" {
		return name
	}
	return name + "@" + commit
}

// CraftCommit returns the commit half of a craft reference.
func CraftCommit(reference string) string {
	if cut := strings.LastIndex(reference, "@"); cut >= 0 {
		return reference[cut+1:]
	}
	return ""
}

// CompileCraft turns one loaded workflow into the store's admission shape.
// The id namespace is derived from the run's provenance so a compile is
// reproducible; RunCraft uses CompileCraftAs to guarantee a fresh namespace
// when the same craft is asked for twice.
func CompileCraft(workflow *craft.Workflow, params map[string]string, provenance store.Provenance) (store.Subtree, error) {
	return CompileCraftAs(craftPrefix(workflow, provenance), "", workflow, params, provenance)
}

// CompileCraftAs is CompileCraft with the caller's id namespace and craft
// repository directory. dir makes verifier script paths absolute; empty leaves
// them named relative to the craft repository, which is honest but weaker.
//
// provenance must already carry this workflow's reference: Splice stamps one
// provenance onto every admitted node, so requiring it here is what makes
// "every node of a craft run names its version" a compile-time invariant
// rather than a convention a caller can forget.
func CompileCraftAs(prefix, dir string, workflow *craft.Workflow, params map[string]string, provenance store.Provenance) (store.Subtree, error) {
	if workflow == nil {
		return store.Subtree{}, fmt.Errorf("compile craft: %w: nil workflow", store.ErrInvalid)
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return store.Subtree{}, fmt.Errorf("compile craft %q: %w: empty id namespace", workflow.Name, store.ErrInvalid)
	}
	if strings.TrimSpace(provenance.Craft) != CraftRef(workflow) {
		return store.Subtree{}, fmt.Errorf("compile craft %q: %w: provenance names craft %q, not %q",
			workflow.Name, store.ErrInvalid, provenance.Craft, CraftRef(workflow))
	}
	if len(workflow.Steps) == 0 {
		return store.Subtree{}, fmt.Errorf("compile craft %q: %w: no steps", workflow.Name, store.ErrInvalid)
	}
	if len(workflow.Steps) > craft.MaxSteps {
		return store.Subtree{}, fmt.Errorf("compile craft %q: %w: %d steps exceeds the ceiling of %d",
			workflow.Name, store.ErrInvalid, len(workflow.Steps), craft.MaxSteps)
	}

	filled, err := craftParams(workflow, params)
	if err != nil {
		return store.Subtree{}, err
	}
	steps, err := craftStepIndex(workflow)
	if err != nil {
		return store.Subtree{}, err
	}
	stages, err := craftStages(workflow, steps)
	if err != nil {
		return store.Subtree{}, err
	}

	specs := make([]store.NodeSpec, 0, len(workflow.Steps)+1)
	consumed := make(map[string]bool, len(workflow.Steps))
	maxStage := 0
	for _, step := range workflow.Steps {
		brief, err := craftStepBrief(workflow, step, filled, dir)
		if err != nil {
			return store.Subtree{}, err
		}
		spec := store.NodeSpec{
			ID:     craftNodeID(prefix, step.ID),
			Parent: prefix,
			Brief:  brief,
			Title:  strings.TrimSpace(step.ID),
			Group:  strings.TrimSpace(workflow.Name),
			Stage:  stages[step.ID],
		}
		for _, need := range craftStepNeeds(step) {
			consumed[need] = true
			spec.Needs = append(spec.Needs, store.Need{
				NodeID: craftNodeID(prefix, need), Kind: store.FeedsInto,
			})
		}
		if stages[step.ID] > maxStage {
			maxStage = stages[step.ID]
		}
		specs = append(specs, spec)
	}

	// The run always gets its own root, even when a single step would already
	// be the sink. The root is the deliverable owner the way a planned job's
	// synthesis node is — and, load-bearing here, it is the one node that stays
	// open for the whole run, which is what gives every runtime splice a legal
	// parent to attach to.
	root := store.NodeSpec{
		ID:    prefix,
		Brief: craftRootBrief(workflow, filled),
		Title: strings.TrimSpace(workflow.Name),
		Group: strings.TrimSpace(workflow.Name),
		Stage: maxStage + 1,
	}
	for _, step := range workflow.Steps {
		if consumed[step.ID] {
			continue
		}
		root.Needs = append(root.Needs, store.Need{
			NodeID: craftNodeID(prefix, step.ID), Kind: store.FeedsInto,
		})
	}
	return store.Subtree{Nodes: append([]store.NodeSpec{root}, specs...)}, nil
}

// craftStepNeeds is a step's complete input set. A for_each step also consumes
// the step its item list comes from, whether or not the file said so twice.
func craftStepNeeds(step craft.Step) []string {
	needs := make([]string, 0, len(step.Needs)+1)
	seen := make(map[string]bool, len(step.Needs)+1)
	for _, need := range step.Needs {
		need = strings.TrimSpace(need)
		if need == "" || seen[need] {
			continue
		}
		seen[need] = true
		needs = append(needs, need)
	}
	if step.ForEach != nil {
		source := strings.TrimSpace(step.ForEach.Source)
		if source != "" && !seen[source] {
			needs = append(needs, source)
		}
	}
	return needs
}

func craftStepIndex(workflow *craft.Workflow) (map[string]craft.Step, error) {
	steps := make(map[string]craft.Step, len(workflow.Steps))
	for _, step := range workflow.Steps {
		id := strings.TrimSpace(step.ID)
		// One id law, asked here in the same words the validator refuses a file
		// in. Anything this rejects the parser has already rejected at save
		// time; the check stays because CompileCraftAs is reachable with a
		// Workflow that never passed through a file.
		if !craft.ValidStepID(id) {
			return nil, fmt.Errorf("compile craft %q: %w: step id %q is not a plain slug — %q would be",
				workflow.Name, store.ErrInvalid, step.ID, craft.Slug(id))
		}
		if _, duplicate := steps[id]; duplicate {
			return nil, fmt.Errorf("compile craft %q: %w: step %q is duplicated",
				workflow.Name, store.ErrInvalid, id)
		}
		steps[id] = step
	}
	for _, step := range workflow.Steps {
		for _, need := range craftStepNeeds(step) {
			if _, ok := steps[need]; !ok {
				return nil, fmt.Errorf("compile craft %q: %w: step %q needs unknown step %q",
					workflow.Name, store.ErrInvalid, step.ID, need)
			}
		}
	}
	return steps, nil
}

// craftStages assigns each step a depth one past its deepest input, so the
// rail renders the run as the waves it actually executes in.
func craftStages(workflow *craft.Workflow, steps map[string]craft.Step) (map[string]int, error) {
	stages := make(map[string]int, len(steps))
	var resolve func(id string, path map[string]bool) (int, error)
	resolve = func(id string, path map[string]bool) (int, error) {
		if stage, done := stages[id]; done {
			return stage, nil
		}
		if path[id] {
			return 0, fmt.Errorf("compile craft %q: %w: step cycle at %q",
				workflow.Name, store.ErrInvalid, id)
		}
		path[id] = true
		stage := 1
		for _, need := range craftStepNeeds(steps[id]) {
			upstream, err := resolve(need, path)
			if err != nil {
				return 0, err
			}
			if upstream+1 > stage {
				stage = upstream + 1
			}
		}
		delete(path, id)
		stages[id] = stage
		return stage, nil
	}
	ids := make([]string, 0, len(steps))
	for id := range steps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := resolve(id, map[string]bool{}); err != nil {
			return nil, err
		}
	}
	return stages, nil
}

// craftParams fills the declared holes. The filling itself is the workflow's
// own law — Fill — rather than a second copy of it here: recognition runs a
// request through Fill before it ever reaches this compiler, and a param that
// is optional with no default resolves to the empty string there while a second
// implementation would leave the key absent and turn an optional param into an
// unresolved reference. Two entry points, one law.
func craftParams(workflow *craft.Workflow, params map[string]string) (map[string]string, error) {
	filled, err := workflow.Fill(params)
	if err != nil {
		return nil, fmt.Errorf("compile craft %q: %w: %s", workflow.Name, store.ErrInvalid, err)
	}
	return filled, nil
}

// substituteCraft fills {{name}} references and refuses anything left over.
// allow names the references this text is permitted to carry forward — only
// {{item}}, and only inside a for_each step, which fills at unroll.
func substituteCraft(text string, params map[string]string, allow map[string]bool) (string, error) {
	var unresolved []string
	filled := craftReference.ReplaceAllStringFunc(text, func(match string) string {
		name := craftReference.FindStringSubmatch(match)[1]
		if value, ok := params[name]; ok {
			return value
		}
		if allow[name] {
			return match
		}
		unresolved = append(unresolved, name)
		return match
	})
	if len(unresolved) > 0 {
		return "", fmt.Errorf("%w: unresolved reference {{%s}}", store.ErrInvalid, unresolved[0])
	}
	return filled, nil
}

func craftStepBrief(workflow *craft.Workflow, step craft.Step, params map[string]string, dir string) (string, error) {
	allow := map[string]bool{}
	if step.ForEach != nil {
		allow[craftItemPlaceholder] = true
	}
	brief, err := substituteCraft(step.Brief, params, allow)
	if err != nil {
		return "", fmt.Errorf("compile craft %q step %q: %w", workflow.Name, step.ID, err)
	}
	brief = strings.TrimSpace(brief)

	switch {
	case step.Verify != nil:
		brief = craftVerifyBrief(brief, step, dir)
	case step.ForEach != nil:
		brief = craftFanBrief(brief, step)
	}
	if advice := craftSkillSentence(step.Skill); advice != "" {
		brief += "\n\n" + advice
	}
	if advice := craftModelSentence(step.Model); advice != "" {
		brief += "\n\n" + advice
	}
	if strings.TrimSpace(brief) == "" {
		return "", fmt.Errorf("compile craft %q step %q: %w: empty brief",
			workflow.Name, step.ID, store.ErrInvalid)
	}
	return brief, nil
}

// craftVerifyBrief turns a verifier declaration into an ordinary leaf's work:
// run this executable, then report its verdict in a shape the sentinel can
// read without a model call.
func craftVerifyBrief(brief string, step craft.Step, dir string) string {
	script := craftVerifierPath(step.Verify.Script, dir)
	var out strings.Builder
	if brief != "" {
		out.WriteString(brief)
		out.WriteString("\n\n")
	}
	fmt.Fprintf(&out, "Check the work above by running this verifier from the workspace:\n%s\n\n", script)
	fmt.Fprintf(&out, "Exit status 0 is a pass; anything else is a fail. Your answer's FIRST line must be exactly %s %s or %s %s, and nothing else on that line. Everything after it must be the verifier's own output, verbatim, because a failure's output is what the next round is given to work from. Do not fix the work yourself and do not soften the verdict.",
		craftVerdictMarker, craftVerdictPass, craftVerdictMarker, craftVerdictFail)
	return out.String()
}

// craftFanBrief plants the fan-out as one leaf. It settles the list and stops
// — the per-item work is spliced as real siblings once the list is known, so
// each item gets its own node, its own spend, and its own transcript.
func craftFanBrief(brief string, step craft.Step) string {
	fan := craftFanCap(step.ForEach)
	var out strings.Builder
	fmt.Fprintf(&out, "Settle the list this step runs over, and do no more than that.\n\nThe items come from the result of step %q, which arrives as one of your inputs. Use its list as given: do not invent items, do not merge them, do not reorder them.\n\n",
		strings.TrimSpace(step.ForEach.Source))
	fmt.Fprintf(&out, "Your answer must END with a line reading exactly %s, followed by the items — one per line, at most %d, no numbering and no commentary after them.\n\n",
		craftItemsMarker, fan)
	fmt.Fprintf(&out, "Each item will then be handed to its own worker. That worker's whole assignment, with {{%s}} replaced by its item, is everything below this line:\n\n%s\n%s",
		craftItemPlaceholder, craftAssignmentMarker, brief)
	return out.String()
}

// craftAssignment reads the per-item assignment back off a landed fan-out
// node. The brief on the graph is the filled one; the workflow file is not.
func craftAssignment(brief string) string {
	if cut := strings.LastIndex(brief, craftAssignmentMarker); cut >= 0 {
		return strings.TrimSpace(brief[cut+len(craftAssignmentMarker):])
	}
	return strings.TrimSpace(brief)
}

// craftItemBrief is one unrolled item's whole assignment: the step's brief
// with {{item}} filled, plus the one line that says which of the batch it is.
func craftItemBrief(brief, item string, index, total int) string {
	filled := craftReference.ReplaceAllStringFunc(brief, func(match string) string {
		if craftReference.FindStringSubmatch(match)[1] == craftItemPlaceholder {
			return item
		}
		return match
	})
	return fmt.Sprintf("%s\n\nThis is item %d of %d in this batch: %s\nDo this item only. Another worker has each of the others.",
		strings.TrimSpace(filled), index, total, item)
}

// craftRoundBrief re-issues a step with the verifier's own words attached. The
// feedback is the whole reason a second round is cheaper than a second job.
func craftRoundBrief(brief, feedback string, round, maxRounds int) string {
	return fmt.Sprintf("%s\n\nThis is repair round %d of %d: an earlier attempt at this step was checked and the check failed. Fix exactly what the check reported and change nothing else.\n\nWhat the check said:\n%s",
		strings.TrimSpace(brief), round, maxRounds, strings.TrimSpace(feedback))
}

func craftRootBrief(workflow *craft.Workflow, params map[string]string) string {
	description, err := substituteCraft(strings.TrimSpace(workflow.Description), params, nil)
	if err != nil || strings.TrimSpace(description) == "" {
		description = "the " + strings.TrimSpace(workflow.Name) + " craft"
	}
	return fmt.Sprintf("Deliver the result of %s.\n\nEvery step of the craft arrives as one of your inputs, including any that were fanned out or repaired. Assemble them into the one answer the person who asked is waiting for, name the files it left behind, and say plainly anything the craft could not finish.",
		description)
}

// craftSkillSentence is the anchoring idiom attached documents and quality
// words already use: one deterministic sentence in the brief, naming what is
// on PATH. It is advice, not a harness change — the worker still decides.
func craftSkillSentence(skill string) string {
	skill = strings.TrimSpace(skill)
	if skill == "" {
		return ""
	}
	return fmt.Sprintf("The %s skill is on PATH for this step. Reach for it rather than rewriting what it already does.", skill)
}

// craftModelSentence records a step's preferred model. Provenance carries one
// model per splice, not one per node, so a per-step preference cannot pin the
// leaf without a second durable field; it rides the brief as advice instead,
// in the same voice the skill line uses, and the job's model does the work.
func craftModelSentence(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	return fmt.Sprintf("This step was written for the %s model; the job's model runs it unless the surface pinned another.", model)
}

func craftVerifierPath(script, dir string) string {
	script = strings.TrimSpace(script)
	if dir = strings.TrimSpace(dir); dir == "" {
		return script
	}
	if strings.HasPrefix(script, "/") {
		return script
	}
	return strings.TrimRight(dir, "/") + "/" + script
}

// craftFanCap applies the package ceiling here rather than trusting the file.
// Clamp runs at parse time, but a compiled run must be able to trust every
// number it reads even when the Workflow reached it another way.
func craftFanCap(fan *craft.ForEach) int {
	if fan == nil {
		return 0
	}
	switch {
	case fan.Fan <= 0:
		return craft.DefaultFanCap
	case fan.Fan > craft.MaxFanCap:
		return craft.MaxFanCap
	default:
		return fan.Fan
	}
}

func craftMaxRounds(until *craft.UntilPass) int {
	if until == nil {
		return 0
	}
	switch {
	case until.MaxRounds <= 0:
		return craft.DefaultMaxRounds
	case until.MaxRounds > craft.MaxRounds:
		return craft.MaxRounds
	default:
		return until.MaxRounds
	}
}

func craftCostCeiling(limits craft.Limits) float64 {
	switch {
	case limits.CostUSD <= 0:
		return craft.DefaultRunBudgetUSD
	case limits.CostUSD > craft.MaxRunBudgetUSD:
		return craft.MaxRunBudgetUSD
	default:
		return limits.CostUSD
	}
}

func craftWallClock(limits craft.Limits) time.Duration {
	switch {
	case limits.WallClock <= 0:
		return craft.DefaultWallClock
	case limits.WallClock > craft.MaxWallClock:
		return craft.MaxWallClock
	default:
		return limits.WallClock
	}
}

// craftNodeID is the one place a step's durable node id is minted. Runtime
// generations extend it with a marked suffix so the sentinel can read a landed
// node's step and generation straight off its id.
func craftNodeID(prefix, step string) string {
	return prefix + craftIDMark + craftSlug(step)
}

func craftGenerationID(prefix, step, kind string, index int) string {
	return craftChildID(craftNodeID(prefix, step), kind, index)
}

// craftChildID extends any craft node id with one more generation. Items are
// minted from the node that LISTED them rather than from the compiled step, so
// a repaired fan-out's items sit under the repair copy that produced them
// instead of colliding with the previous round's. For a first-generation
// fan-out the two readings are the same string, which is what keeps every id
// already on a live graph byte-for-byte what it was.
func craftChildID(nodeID, kind string, index int) string {
	return fmt.Sprintf("%s%s%s%d", nodeID, craftIDMark, kind, index)
}

// craftNodeParts reads a craft node id back into its run prefix, step id, and
// runtime generation. A generation of zero is the compiled node itself.
//
// It fails closed, the way an id parser that decides what a landed node MEANT
// has to: a generation nobody mints — an unknown kind letter, a zero or
// leading-zero index, a number too long to be an index — is not a craft node
// this sentinel understands, and treating it as one routes a stranger's node
// into a fan-out or a repair round.
func craftNodeParts(id string) (prefix, step, kind string, index int, ok bool) {
	parts := strings.Split(id, craftIDMark)
	switch len(parts) {
	case 2:
		return parts[0], parts[1], "", 0, parts[0] != "" && parts[1] != ""
	case 3:
		if parts[0] == "" || parts[1] == "" {
			return "", "", "", 0, false
		}
		kind, index, ok := craftGenerationParts(parts[2])
		if !ok {
			return "", "", "", 0, false
		}
		return parts[0], parts[1], kind, index, true
	default:
		return "", "", "", 0, false
	}
}

// craftGenerationDigits bounds an index. A fan cap and a round cap are both
// single digits today; four leaves room for every ceiling this package could
// grow without letting an id carry a number that overflows on the way in.
const craftGenerationDigits = 4

func craftGenerationParts(generation string) (kind string, index int, ok bool) {
	if len(generation) < 2 || len(generation) > 1+craftGenerationDigits {
		return "", 0, false
	}
	switch generation[:1] {
	case craftItemGeneration, craftRoundGeneration:
	default:
		return "", 0, false
	}
	digits := generation[1:]
	if digits[0] == '0' {
		return "", 0, false
	}
	number := 0
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return "", 0, false
		}
		number = number*10 + int(digit-'0')
	}
	return generation[:1], number, true
}

// craftSlug reduces a name to what a node id may hold. The law itself lives in
// the craft package, because the validator that refuses a file at save time and
// the compiler that mints node ids from it have to be the same law.
func craftSlug(name string) string {
	return craft.Slug(name)
}

// craftPrefix derives a reproducible id namespace from what the run is: the
// craft, and the words that asked for it.
func craftPrefix(workflow *craft.Workflow, provenance store.Provenance) string {
	seed := CraftRef(workflow) + "\x00" + provenance.SessionID + "\x00" + provenance.Intent
	hash := uint64(1469598103934665603)
	for index := 0; index < len(seed); index++ {
		hash ^= uint64(seed[index])
		hash *= 1099511628211
	}
	name := "craft"
	if workflow != nil {
		name = craftSlug(workflow.Name)
	}
	return fmt.Sprintf("craft-%s-%x", name, hash&0xffffffffff)
}
