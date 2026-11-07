package pipeline

import (
	"maps"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── MAKING, RENAMING AND REMOVING A NAMED PIPELINE ──────────────────────────
//
// A file holds one list of steps per pipeline under `pipeline:`, keyed by a
// name: issue is the one a job runs when nothing picks another, pr and ci are
// the lists of pull request and CI jobs, and any other name is a pipeline a
// person picks at Start or a routing rule names (then.pipeline). The three
// verbs here change WHICH lists the file holds, never a step's words:
//
//   - add_pipeline {name, steps} writes a new list, each step one the file or
//     the shipped steps already define (or approve), in the order given;
//     add_pipeline {name, from} starts it as a copy of a pipeline the file
//     already holds. A step that does not exist yet is added afterwards with
//     add_step naming the new pipeline, in the same preview.
//   - remove_pipeline {name} takes a named pipeline out of the file. Its
//     steps' definitions stay, as a removed row's do: another pipeline may
//     list them.
//   - rename_pipeline {name, to} gives a named pipeline another name, its
//     list and its adapt word unchanged.
//
// A NAME IS SPELLED AS A STEP'S IS: 2 to 32 lowercase letters, digits and
// inner hyphens ([KindNamed]), the spelling the parser reads under
// `pipeline:`. PR AND CI ARE NOT A NAME OF ONE'S OWN: they are the lists the
// factory gives pull request and CI jobs, so a pipeline is never made under,
// renamed into or removed from either, and issue is neither made (it is
// always there), renamed nor removed, because a job that picks nothing runs
// it. A pipeline is the subject of one of these verbs per preview.
//
// THE PROOF LAW HOLDS FOR A NEW LIST TOO. A pipeline made here is touched by
// this preview, so a chat listed after its proof is refused exactly as an
// add_step there would be ([editState.chatAfterProof]).

// The verbs that make, remove and rename a whole pipeline.
const (
	OpAddPipeline    = "add_pipeline"
	OpRemovePipeline = "remove_pipeline"
	OpRenamePipeline = "rename_pipeline"
)

// PipelineChange is one pipeline a pipeline verb made, removed or renamed:
// Op is the verb, Pipeline its name (the old one for a rename), To the new
// name of a rename, and Steps the list it holds afterwards (nil once removed).
type PipelineChange struct {
	Op       string
	Pipeline string
	To       string
	Steps    []string
}

// pipelineOp says whether a verb is one of the three here.
func pipelineOp(verb string) bool {
	return verb == OpAddPipeline || verb == OpRemovePipeline || verb == OpRenamePipeline
}

// ReservedPipeline is why name is not a pipeline a person makes, renames or
// removes, or "": pr and ci are the factory's own lists for pull request and
// CI jobs, and issue is the list a job runs when nothing picks another.
func ReservedPipeline(name string) string {
	switch factory.Kind(name) {
	case factory.KindPR, factory.KindCI:
		return name + " is the pipeline the factory runs for " + strings.ToUpper(name) + " jobs; a pipeline of your own takes another name"
	case factory.KindIssue:
		return "issue is the pipeline a job runs when nothing picks another; it is always there, under that name"
	}
	return ""
}

// checkPipelineOp bounds the three verbs without the file: a pipeline name
// in name, never a reserved one; add_pipeline's steps or from (one of them);
// rename_pipeline's to. None takes a step's fields, a position or a value.
func checkPipelineOp(i int, op EditOp) (EditOp, *EditError) {
	if op.Pipeline != "" || op.After != nil || op.Kind != nil || op.Value != nil || stepFieldsNamed(op) {
		return op, refuseEdit(EditBadOp, i, "%s takes the pipeline's name in name, and no step fields", op.Op)
	}
	if op.Op != OpAddPipeline && (op.Steps != nil || op.From != "") {
		return op, refuseEdit(EditBadOp, i, "only add_pipeline takes steps or from")
	}
	if op.Op != OpRenamePipeline && op.To != "" {
		return op, refuseEdit(EditBadOp, i, "only rename_step and rename_pipeline take to")
	}
	if !KindNamed(op.Name) {
		return op, refusePipeline(EditBadValue, i, "a pipeline name is 2 to 32 lowercase letters, digits and inner hyphens")
	}
	if why := ReservedPipeline(op.Name); why != "" {
		return op, refusePipeline(EditLocked, i, "%s", why)
	}
	switch op.Op {
	case OpRenamePipeline:
		if !KindNamed(op.To) {
			return op, refusePipeline(EditBadValue, i, "to is 2 to 32 lowercase letters, digits and inner hyphens")
		}
		if op.To == op.Name {
			return op, refusePipeline(EditBadValue, i, "a pipeline is not renamed to its own name")
		}
		if why := ReservedPipeline(op.To); why != "" {
			return op, refusePipeline(EditLocked, i, "%s", why)
		}
	case OpAddPipeline:
		switch {
		case op.Steps != nil && op.From != "":
			return op, refuseEdit(EditBadOp, i, "add_pipeline takes steps or from, not both")
		case op.Steps == nil && op.From == "":
			return op, refuseEdit(EditBadOp, i, "add_pipeline lists its steps, or names in from the pipeline it starts as a copy of")
		case op.From != "" && !KindNamed(op.From):
			return op, refusePipeline(EditUnknownStep, i, "from is not a pipeline name")
		case op.Steps != nil && len(op.Steps) == 0:
			// AN EMPTY PIPELINE IS NOT AN EMPTY RUN: the recipe leaves an
			// empty list out, and a job that picked it would run issue.
			return op, refuseEdit(EditBadValue, i, "a pipeline lists at least one step")
		case len(op.Steps) > factory.StageMost:
			return op, refuseEdit(EditTooLarge, i, "a pipeline runs at most %d steps", factory.StageMost)
		}
		seen := map[string]bool{}
		for _, name := range op.Steps {
			if !stepNamed(name) {
				return op, refuseEdit(EditUnknownStep, i, "that is not a step name")
			}
			if seen[name] {
				return op, refuseEdit(EditBadValue, i, "%s is listed twice; a pipeline runs a step once", name)
			}
			seen[name] = true
		}
		op.Steps = slices.Clone(op.Steps)
	}
	return op, nil
}

// pipelines applies one pipeline verb.
func (st *editState) pipelines(i int, op EditOp) *EditError {
	st.structural = true
	for _, name := range []string{op.Name, op.To} {
		if name == "" {
			continue
		}
		if st.kindSeen[name] {
			return refuseEdit(EditBadOp, i, "%s is made, renamed or removed once in one preview", name)
		}
		st.kindSeen[name] = true
	}
	kind := factory.Kind(op.Name)
	list, exists := st.out.Pipeline[kind]
	switch op.Op {
	case OpAddPipeline:
		if exists {
			return refusePipeline(EditBadValue, i, "%s is already a pipeline in this file; pick another name", op.Name)
		}
		steps, bad := st.newPipeline(i, op)
		if bad != nil {
			return bad
		}
		st.out.Pipeline[kind] = steps
		if _, seen := st.firstOp[kind]; !seen {
			st.firstOp[kind] = i
		}
		for _, name := range steps {
			st.placedAt[op.Name+"\x00"+name] = i
			st.inPipeline[name] = true
		}
		st.kinds = append(st.kinds, PipelineChange{Op: op.Op, Pipeline: op.Name, Steps: slices.Clone(steps)})
	case OpRemovePipeline:
		if !exists {
			return refusePipeline(EditUnknownStep, i, "%s is not a pipeline in this file", op.Name)
		}
		delete(st.out.Pipeline, kind)
		st.moveAdapt(kind, "")
		st.kinds = append(st.kinds, PipelineChange{Op: op.Op, Pipeline: op.Name})
	case OpRenamePipeline:
		if !exists {
			return refusePipeline(EditUnknownStep, i, "%s is not a pipeline in this file", op.Name)
		}
		to := factory.Kind(op.To)
		if _, taken := st.out.Pipeline[to]; taken {
			return refusePipeline(EditBadValue, i, "%s is already a pipeline in this file; pick another name", op.To)
		}
		st.out.Pipeline[to] = list
		delete(st.out.Pipeline, kind)
		st.moveAdapt(kind, to)
		st.renamedKinds[op.To] = op.Name
		st.kinds = append(st.kinds, PipelineChange{Op: op.Op, Pipeline: op.Name, To: op.To, Steps: slices.Clone(list)})
	}
	return nil
}

// newPipeline is the list an add_pipeline writes: a copy of from's, or steps,
// each a step the runner would run under the new name. A step the file does
// not define is refused rather than written as a row the runner would skip.
func (st *editState) newPipeline(i int, op EditOp) ([]string, *EditError) {
	if op.From != "" {
		list, ok := st.out.Pipeline[factory.Kind(op.From)]
		if !ok {
			return nil, refusePipeline(EditUnknownStep, i, "%s is not a pipeline in this file", op.From)
		}
		return slices.Clone(list), nil
	}
	out := make([]string, 0, len(op.Steps))
	for _, name := range op.Steps {
		if !factory.ApproveWord(name) {
			step, ok := st.out.Steps[name]
			if !ok {
				return nil, refuseEdit(EditUnknownStep, i, "%s is not a step this file or codeaf defines; make the pipeline first, then add_step it there", name)
			}
			if !step.isGate() && strings.TrimSpace(step.Prompt) == "" {
				return nil, refuseEdit(EditBadValue, i, "%s has no prompt, so the runner would skip it", name)
			}
			if _, err := factory.StageWord(strings.ReplaceAll(name, "-", "")); err != nil {
				return nil, refuseEdit(EditBadValue, i, "%s is not a name the runner can run (2 to 12 letters or digits once hyphens are removed)", name)
			}
		}
		if other := sameStage(out, name); other != "" {
			return nil, refuseEdit(EditBadValue, i, "%s would run as the same stage as %s in the %s pipeline", name, other, op.Name)
		}
		out = append(out, name)
	}
	return out, nil
}

// moveAdapt carries one pipeline's own adapt word to its new name, or drops
// it ("" for a removed pipeline). The map is the edited file's own copy.
func (st *editState) moveAdapt(from, to factory.Kind) {
	mode, ok := st.out.AdaptKinds[from]
	if !ok {
		return
	}
	st.out.AdaptKinds = maps.Clone(st.out.AdaptKinds)
	delete(st.out.AdaptKinds, from)
	if to != "" {
		st.out.AdaptKinds[to] = mode
	}
	if len(st.out.AdaptKinds) == 0 {
		st.out.AdaptKinds = nil
	}
}
