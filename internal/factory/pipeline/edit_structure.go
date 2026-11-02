package pipeline

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── ADDING, REMOVING AND MOVING A STEP IN ONE PIPELINE ──────────────────────
//
// The three structural edits touch only what the file format already holds: the
// ordered list under pipeline.<kind>, and for a new step one definition under
// steps. Every YAML-reading factory parses both, so the edits need no new key,
// no new runtime word and no attestation of their own; a step added with when,
// effort or fanout spells those keys and meets the runtime seal like any other
// edit that does.
//
// THE ROWS A PERSON DEPENDS ON STAY WHERE THEY ARE. approve, a post's forge
// verb, a gate's person and any name under fixed are locked: none is removed
// or moved here, so the order of those rows among themselves never changes.
// A NEW PERSON MAY BE ADDED: add_step of kind gate puts an approval in one
// pipeline, waiting on whom its who names ("@handle", operator, or "" for the
// job's person), with an optional prompt the person reads. A gate is a person,
// so it takes no until, rounds, effort or fanout, and once in the file it is
// locked like every other person. A CHECK IS NOT ONE OF THEM: its prompt is a shell command whose exit
// code is the evidence, and add_step may add one (kind check) as it adds a
// chat, through the same reviewed pull request that would add it by hand. A
// check takes no effort or fanout (no model reads it) and stops only on done,
// clean, green or proven, the words its runner checks. That is this editor's
// policy, matching the board's fixed rows; a reviewed pull request to the file
// is the file changing, which is how the law changes.
//
// A CHAT STEP MAY CROSS A PERSON, AND THE PREVIEW SAYS SO. Moving, adding or
// removing a chat step before a gate changes what runs unattended before that
// person answers. It is allowed (the pull request is reviewed, and a running
// job keeps the stage order it started with), and every gate whose run-up
// changed is named in Edited.Gates with the steps before it, before and after.
// No barrier is invented and no role is asked for. A person is not proof, so
// crossing one is never refused by the proof rule below.
//
// PROOF STAYS, AND NO CHAT RUNS AFTER IT. A step is proof the way the runner
// says ([factory.IsProofStage] on the stage the file compiles it to): named
// proof, named prove*, or running until proven. Such a row is never removed or
// moved here, whether or not fixed lists it, because a job without one lands
// with no sheet; nor does a set_step in a structural preview end the proof of
// a pipeline that preview touched (a set_step alone, or one beside a structural
// edit of another pipeline, is a field edit, as it always was).
// And a pipeline a structural edit touched may not end with a
// movable chat after its first proof: the sheet is written before that work
// runs, so the work would land unproved. That holds for a row the edit placed
// and for one the base already had there; a base in that shape is repaired by
// removing that row or moving it before proof. A check, a post and a person
// after proof keep the locks they had and are not refused for standing there:
// a post after proof is how a proved change is published.
//
// A REMOVED ROW KEEPS ITS DEFINITION. Another kind may list the same step, and a
// shipped step whose definition went would come back with its shipped words.
// An added step either writes a new definition or names one that already says
// exactly what the edit says: another kind's step is never rewritten through
// this door.

// OrderChange is one pipeline whose list an edit changed, before and after.
type OrderChange struct {
	Pipeline string
	Before   []string
	After    []string
}

// KindCount is how many steps one pipeline lists after the edit.
type KindCount struct {
	Pipeline string
	Steps    int
}

// GateChange is one person (approve, or a step whose kind is gate) whose
// run-up an edit changed: Before and After are the steps that run before that
// person answers, in order, gates excluded. The person itself never moves.
type GateChange struct {
	Pipeline string
	Gate     string
	Before   []string
	After    []string
}

// checkStructure bounds add_step, remove_step and move_step without the file.
// Each names a pipeline and a step; add_step and move_step name After, the step
// it goes after, with "" for first; add_step alone takes a prompt (required
// but for a gate) and until, max, when, effort and fanout, and a kind: chat
// (the default), check or gate, and who for a gate. No other structural edit
// takes a kind, and none takes a value.
func checkStructure(i int, op EditOp) (EditOp, *EditError) {
	if op.Value != nil || (op.Kind != nil && op.Op != OpAddStep) {
		return op, refuseEdit(EditBadOp, i, "%s takes no kind and no value; only add_step names a kind", op.Op)
	}
	if op.Pipeline == "" {
		return op, refuseEdit(EditBadOp, i, "%s names a pipeline", op.Op)
	}
	if !stepNamed(op.Pipeline) {
		return op, refuseEdit(EditUnknownStep, i, "that is not a pipeline name")
	}
	if !stepNamed(op.Name) {
		return op, refuseEdit(EditUnknownStep, i, "that is not a step name")
	}
	if factory.ApproveWord(op.Name) {
		return op, refuseEdit(EditLocked, i, "%s is a person and is not added, removed or moved here", op.Name)
	}
	if op.Op == OpRemoveStep {
		if op.After != nil || stepFieldsNamed(op) {
			return op, refuseEdit(EditBadOp, i, "remove_step takes only a pipeline and a name")
		}
		return op, nil
	}
	if op.After == nil {
		return op, refuseEdit(EditBadOp, i, "%s names the step it goes after, or \"\" for first", op.Op)
	}
	if bad := checkAnchor(i, op); bad != nil {
		return op, bad
	}
	if op.Op == OpMoveStep {
		if stepFieldsNamed(op) {
			return op, refuseEdit(EditBadOp, i, "move_step takes only a pipeline, a name and the step it goes after")
		}
		return op, nil
	}
	return checkAddStep(i, op)
}

// checkAnchor bounds the After of add_step and move_step: "" or a step name
// other than the step's own.
func checkAnchor(i int, op EditOp) *EditError {
	if *op.After != "" && !stepNamed(*op.After) {
		return refuseEdit(EditUnknownStep, i, "that is not a step to go after")
	}
	if *op.After == op.Name {
		return refuseEdit(EditBadValue, i, "a step does not go after itself")
	}
	return nil
}

func checkAddStep(i int, op EditOp) (EditOp, *EditError) {
	if op.Kind != nil {
		k, ok := kindWord(*op.Kind)
		if !ok || (k != "" && k != string(factory.StageCheck) && k != string(factory.StageGate)) {
			return op, refuseEdit(EditBadValue, i, "a step added here is a chat, a check or a gate")
		}
		op.Kind = &k
	}
	if op.Prompt == nil && !addsGate(op) {
		return op, refuseEdit(EditBadValue, i, "add_step needs a prompt")
	}
	if bad := checkCheckWords(i, op); bad != nil {
		return op, bad
	}
	op, bad := checkGateWords(i, op)
	if bad != nil {
		return op, bad
	}
	word, err := factory.StageWord(strings.ReplaceAll(op.Name, "-", ""))
	if err != nil {
		return op, refuseEdit(EditBadValue, i, "%s is not a name the runner can run (2 to 12 letters or digits once hyphens are removed)", op.Name)
	}
	if factory.ApproveWord(word) {
		return op, refuseEdit(EditBadValue, i, "%s would read as a person's name", op.Name)
	}
	return checkStepFields(i, op)
}

// checkCheckWords refuses what a check cannot hold: effort and fanout (no
// model reads it), and an until its runner does not check.
func checkCheckWords(i int, op EditOp) *EditError {
	if op.Kind == nil || *op.Kind != string(factory.StageCheck) {
		return nil
	}
	if (op.Effort != nil && *op.Effort != "") || (op.Fanout != nil && *op.Fanout != "") {
		return refuseEdit(EditBadValue, i, "a check runs a command, not a model; it takes no effort or fanout")
	}
	if op.Until != nil {
		if _, free := mechanicalUntil(*op.Until); free {
			return refuseEdit(EditBadValue, i, "a check stops on done, clean, green or proven")
		}
	}
	return nil
}

// addsGate says whether an add_step adds a person.
func addsGate(op EditOp) bool {
	return op.Kind != nil && *op.Kind == string(factory.StageGate)
}

// checkGateWords refuses who on any added step but a gate, and on a gate what
// a person cannot hold: until and rounds (it stops when they answer), effort
// and fanout (no model reads it). It answers who as the file spells it.
func checkGateWords(i int, op EditOp) (EditOp, *EditError) {
	if !addsGate(op) {
		if op.Who != nil {
			return op, refuseEdit(EditBadOp, i, "only a gate added here names who it waits on")
		}
		return op, nil
	}
	if op.Until != nil || op.Max != nil || (op.Effort != nil && *op.Effort != "") || (op.Fanout != nil && *op.Fanout != "") {
		return op, refuseEdit(EditBadValue, i, "a gate is a person; it takes no until, rounds, effort or fanout")
	}
	if op.Who == nil {
		return op, nil
	}
	w, ok := factory.ApproverWord(*op.Who)
	if !ok {
		return op, refuseEdit(EditBadValue, i, "who is @handle, operator, or \"\" for the job's person")
	}
	op.Who = &w
	return op, nil
}

func stepFieldsNamed(op EditOp) bool {
	return op.Prompt != nil || op.Until != nil || op.Max != nil || op.When != nil || op.Effort != nil || op.Fanout != nil
}

// structure applies one structural edit to its pipeline's list. A pipeline row
// is the subject of one structural edit per preview.
func (st *editState) structure(i int, op EditOp) *EditError {
	st.structural = true
	kind := factory.Kind(op.Pipeline)
	list, ok := st.out.Pipeline[kind]
	if !ok {
		return refuseEdit(EditUnknownStep, i, "%s is not a pipeline in this file", op.Pipeline)
	}
	row := op.Pipeline + "\x00" + op.Name
	if st.placed[row] {
		return refuseEdit(EditBadOp, i, "a step is added, removed or moved once in one pipeline")
	}
	st.placed[row] = true
	if _, seen := st.firstOp[kind]; !seen {
		st.firstOp[kind] = i
	}
	if op.Op != OpRemoveStep {
		st.placedAt[row] = i
	}
	var bad *EditError
	switch op.Op {
	case OpAddStep:
		list, bad = st.addStep(i, list, op)
	case OpRemoveStep:
		list, bad = st.removeStep(i, list, op)
	default:
		list, bad = st.moveStep(i, list, op)
	}
	if bad != nil {
		return bad
	}
	st.out.Pipeline[kind] = list
	return nil
}

func (st *editState) addStep(i int, list []string, op EditOp) ([]string, *EditError) {
	if st.named[op.Name] {
		return nil, refuseEdit(EditBadOp, i, "%s is edited by a set_step too; a step added here takes its fields on the add_step", op.Name)
	}
	// A GATE THE FILE ALREADY DEFINES may be listed in another pipeline: one
	// more place a person answers takes nothing away from anyone ([define]
	// still holds it to its own words).
	gate := addsGate(op) && st.out.Steps[op.Name].Kind == string(factory.StageGate)
	if lockedStep(st.out, op.Name) && !gate {
		return nil, refuseEdit(EditLocked, i, "%s is locked and is not added here", op.Name)
	}
	if other := sameStage(list, op.Name); other != "" {
		return nil, refuseEdit(EditBadValue, i, "%s would run as the same stage as %s in the %s pipeline", op.Name, other, op.Pipeline)
	}
	if len(list) >= factory.StageMost {
		return nil, refuseEdit(EditTooLarge, i, "a pipeline runs at most %d steps", factory.StageMost)
	}
	at, bad := anchorAt(i, list, op)
	if bad != nil {
		return nil, bad
	}
	if bad := st.define(i, op); bad != nil {
		return nil, bad
	}
	st.added[op.Name] = true
	return insertAt(list, at, op.Name), nil
}

// define gives an added step its definition. A new name is written as the edit
// says. A name the file already defines is listed only when every field the
// edit names already says the same; its other fields are the definition's own.
func (st *editState) define(i int, op EditOp) *EditError {
	have, exists := st.out.Steps[op.Name]
	want := have
	if !exists {
		want = Step{}
	}
	if op.Prompt != nil {
		want.Prompt, want.wrotePrompt = *op.Prompt, true
	}
	if op.Kind != nil {
		want.Kind = *op.Kind
	}
	if op.Who != nil {
		want.Who = *op.Who
	}
	setStepWords(&want, op)
	if !exists {
		st.out.Steps[op.Name] = want
		st.newSteps = append(st.newSteps, op.Name)
		return nil
	}
	if want.Kind != have.Kind || want.Prompt != have.Prompt || want.Until != have.Until || want.Max != have.Max ||
		want.When != have.When || want.Effort != have.Effort || want.Fanout != have.Fanout || want.Who != have.Who {
		return refuseEdit(EditBadValue, i, "%s is already a step with other words; pick another name or add it as it is", op.Name)
	}
	return nil
}

func (st *editState) removeStep(i int, list []string, op EditOp) ([]string, *EditError) {
	at := indexOf(list, op.Name)
	if at < 0 {
		return nil, refuseEdit(EditUnknownStep, i, "%s is not in the %s pipeline", op.Name, op.Pipeline)
	}
	if lockedStep(st.out, op.Name) {
		return nil, refuseEdit(EditLocked, i, "%s is locked and cannot be removed", op.Name)
	}
	if st.proofRow(op.Name) {
		return nil, refuseEdit(EditLocked, i, "%s is proof, which vouches for the work before it, and is not removed here", op.Name)
	}
	// AN EMPTY PIPELINE IS NOT AN EMPTY RUN: the recipe leaves an empty kind
	// out, and a job of that kind then runs the issue steps.
	if len(list) == 1 {
		return nil, refuseEdit(EditBadValue, i, "the %s pipeline keeps at least one step", op.Pipeline)
	}
	return append(append([]string{}, list[:at]...), list[at+1:]...), nil
}

func (st *editState) moveStep(i int, list []string, op EditOp) ([]string, *EditError) {
	at := indexOf(list, op.Name)
	if at < 0 {
		return nil, refuseEdit(EditUnknownStep, i, "%s is not in the %s pipeline", op.Name, op.Pipeline)
	}
	if lockedStep(st.out, op.Name) {
		return nil, refuseEdit(EditLocked, i, "%s is locked and cannot be moved", op.Name)
	}
	if st.proofRow(op.Name) {
		return nil, refuseEdit(EditLocked, i, "%s is proof, which vouches for the work before it, and is not moved here", op.Name)
	}
	rest := append(append([]string{}, list[:at]...), list[at+1:]...)
	to, bad := anchorAt(i, rest, op)
	if bad != nil {
		return nil, bad
	}
	return insertAt(rest, to, op.Name), nil
}

// proofStep says whether the runner would run a listed step of f as proof. It
// compiles the one step as [File.Recipe] does and asks [factory.IsProofStage],
// so the editor and the runner never disagree about which row is proof.
func proofStep(f File, name string) bool {
	stages := f.stages([]string{name})
	return len(stages) == 1 && factory.IsProofStage(stages[0])
}

// proofRow says whether a row is proof in the base or in the file as edited so
// far, so neither an existing proof nor one a set_step just made can be moved.
func (st *editState) proofRow(name string) bool {
	return proofStep(st.base, name) || proofStep(st.out, name)
}

// chatAfterProof refuses the first pipeline a structural edit touched whose
// edited list runs a movable chat after its first proof. It runs once every
// edit has applied, so a preview that adds a step and removes a stray one in
// the same list is judged by the list it would commit. The refusal names the
// edit that brought the shape: the add or move that placed the stray row, else
// the edit that made the earlier row proof, else the first structural edit of
// that pipeline, which kept a stray row the base already had.
func (st *editState) chatAfterProof() *EditError {
	for _, kind := range sortedKinds(st.out) {
		first, touched := st.firstOp[factory.Kind(kind)]
		if !touched {
			continue
		}
		list := st.out.Pipeline[factory.Kind(kind)]
		if bad := st.proofEnded(kind, first, list); bad != nil {
			return bad
		}
		proof := -1
		for at, name := range list {
			if proofStep(st.out, name) {
				proof = at
				break
			}
		}
		if proof < 0 {
			continue
		}
		for _, name := range list[proof+1:] {
			if !st.strayAfterProof(kind, name) {
				continue
			}
			at := first
			if op, ok := st.placedAt[kind+"\x00"+name]; ok {
				at = op
			} else if op, ok := st.placedAt[kind+"\x00"+list[proof]]; ok {
				at = op
			} else if op, ok := st.setAt[list[proof]]; ok && !proofStep(st.base, list[proof]) {
				at = op
			} else if op, ok := st.setAt[name]; ok && proofStep(st.base, name) && !proofStep(st.out, name) {
				at = op
			}
			return refuseEdit(EditLocked, at, "%s would run after %s in the %s pipeline; proof has to see the work, so a step goes before it", name, list[proof], kind)
		}
	}
	return nil
}

// proofEnded refuses a touched pipeline whose base ran proof and whose edited
// list runs none. remove_step and move_step cannot take a proof row away, but a
// set_step can change until on a row that was proof only by until, and the same
// preview's structural edit would then commit a pipeline that lands with no
// sheet. A set_step alone is a field edit and is never judged here, so that
// legacy behaviour is unchanged. The refusal names the set_step that ended the
// row's proof, else the pipeline's first structural edit.
func (st *editState) proofEnded(kind string, first int, list []string) *EditError {
	for _, name := range list {
		if proofStep(st.out, name) {
			return nil
		}
	}
	ended, at := "", first
	for _, name := range st.base.Pipeline[factory.Kind(kind)] {
		if !proofStep(st.base, name) {
			continue
		}
		if op, set := st.setAt[name]; set {
			ended, at = name, op
			break
		}
		if ended == "" {
			ended = name
		}
	}
	if ended == "" {
		return nil
	}
	return refuseEdit(EditLocked, at, "%s would no longer be proof, and the %s pipeline would land with no sheet", ended, kind)
}

// strayAfterProof says whether a row standing after proof is chat work the
// sheet cannot have seen. A person, a check and a post are not, and neither is
// a row the editor cannot move (fixed, or proof the base already had and the
// edit kept proof), unless this preview placed it there. A base proof row a
// set_step made ordinary chat is work like any other.
func (st *editState) strayAfterProof(kind, name string) bool {
	_, placed := st.placedAt[kind+"\x00"+name]
	// A CHECK THIS EDIT PUT AFTER PROOF runs a command on work the sheet has
	// already vouched for; one the file already had there keeps its place.
	if st.out.Steps[name].Kind == string(factory.StageCheck) {
		return placed
	}
	if isPerson(st.out, name) || st.out.Steps[name].Kind != "" {
		return false
	}
	if placed {
		return true
	}
	return !lockedStep(st.out, name) && !(proofStep(st.base, name) && proofStep(st.out, name))
}

// anchorAt is the position just after op.After in list, or 0 for "".
func anchorAt(i int, list []string, op EditOp) (int, *EditError) {
	if *op.After == "" {
		return 0, nil
	}
	at := indexOf(list, *op.After)
	if at < 0 {
		return 0, refuseEdit(EditUnknownStep, i, "%s is not in the %s pipeline", *op.After, op.Pipeline)
	}
	return at + 1, nil
}

// sameStage is the step in list the runner would key as the same stage as
// name, or "". It keys a step the way [File.stages] does: approve by its own
// name, every other step by its word once hyphens are removed.
func sameStage(list []string, name string) string {
	key := func(n string) string {
		if factory.ApproveWord(n) {
			return n
		}
		w, err := factory.StageWord(strings.ReplaceAll(n, "-", ""))
		if err != nil {
			return "\x00" + n
		}
		return w
	}
	want := key(name)
	for _, n := range list {
		if n == name || key(n) == want {
			return n
		}
	}
	return ""
}

func indexOf(list []string, name string) int {
	for i, n := range list {
		if n == name {
			return i
		}
	}
	return -1
}

func insertAt(list []string, at int, name string) []string {
	out := make([]string, 0, len(list)+1)
	out = append(out, list[:at]...)
	out = append(out, name)
	return append(out, list[at:]...)
}

// structureFacts fills the structural half of a preview from the base and the
// edited file, every kind in the order a person reads them.
func (st *editState) structureFacts(res *Edited) {
	res.Structural = true
	res.Order, res.Counts, res.Gates = []OrderChange{}, []KindCount{}, []GateChange{}
	res.Added = append([]string{}, st.newSteps...)
	for _, kind := range sortedKinds(st.out) {
		before, after := st.base.Pipeline[factory.Kind(kind)], st.out.Pipeline[factory.Kind(kind)]
		res.Counts = append(res.Counts, KindCount{Pipeline: kind, Steps: len(after)})
		if strings.Join(before, "\n") == strings.Join(after, "\n") {
			continue
		}
		res.Order = append(res.Order, OrderChange{Pipeline: kind, Before: append([]string{}, before...), After: append([]string{}, after...)})
		res.Gates = append(res.Gates, gateChanges(st.out, kind, before, after)...)
	}
}

// gateChanges names every person in after whose unattended run-up is a
// different set of steps than in before.
func gateChanges(f File, kind string, before, after []string) []GateChange {
	var out []GateChange
	for _, gate := range after {
		if !isPerson(f, gate) {
			continue
		}
		b, a := runUp(f, before, gate), runUp(f, after, gate)
		if !sameSet(b, a) {
			out = append(out, GateChange{Pipeline: kind, Gate: gate, Before: b, After: a})
		}
	}
	return out
}

// isPerson says whether a listed step is a person: approve, or a gate by kind.
func isPerson(f File, name string) bool {
	return factory.ApproveWord(name) || f.Steps[name].isGate()
}

// runUp is the steps that run before gate in list, people excluded.
func runUp(f File, list []string, gate string) []string {
	out := []string{}
	for _, n := range list {
		if n == gate {
			break
		}
		if !isPerson(f, n) {
			out = append(out, n)
		}
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	in := make(map[string]bool, len(a))
	for _, n := range a {
		in[n] = true
	}
	for _, n := range b {
		if !in[n] {
			return false
		}
	}
	return true
}
