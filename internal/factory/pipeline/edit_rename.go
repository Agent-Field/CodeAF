package pipeline

import (
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── RENAMING A STEP ─────────────────────────────────────────────────────────
//
// rename_step {name, to} gives one step a new name everywhere the file says
// it: every pipeline that lists it (in its place), the required list, and its
// definition, whose words (who included) move with it unchanged. The old
// definition goes; a shipped name keeps only its shipped words, which any file
// reads back anyway. The new name is spelled as any step is (2 to 32
// lowercase letters, digits and inner hyphens, 2 to 12 once hyphens are
// removed), is no step the file or the shipped steps already define, and runs
// as no stage another step of the same pipeline already runs as.
//
// A NAME THAT MEANS SOMETHING IS NOT RENAMED. approve is a person by its name,
// and proof is proof by its name (or prove*): neither is renamed from or into,
// so a rename never makes or ends a person or a proof. A job that already
// started keeps the stage names it started with.
//
// A renamed step is the subject of that rename alone: no other edit in the
// same preview names it, by its old name or its new one.

// OpRenameStep is the verb that renames a step.
const OpRenameStep = "rename_step"

// Rename is one step a rename_step gave a new name.
type Rename struct {
	From string
	To   string
}

// checkRename bounds a rename_step without the file: a name and a new name,
// both spelled as steps, the new one a stage the runner can run, and neither a
// person.
func checkRename(i int, op EditOp) (EditOp, *EditError) {
	if op.Pipeline != "" || op.After != nil || op.Kind != nil || op.Value != nil || stepFieldsNamed(op) {
		return op, refuseEdit(EditBadOp, i, "rename_step takes only a name and to")
	}
	if !stepNamed(op.Name) {
		return op, refuseEdit(EditUnknownStep, i, "that is not a step name")
	}
	if !stepNamed(op.To) {
		return op, refuseEdit(EditBadValue, i, "to is 2 to 32 lowercase letters, digits and inner hyphens")
	}
	if op.To == op.Name {
		return op, refuseEdit(EditBadValue, i, "a step is not renamed to its own name")
	}
	word, err := factory.StageWord(strings.ReplaceAll(op.To, "-", ""))
	if err != nil {
		return op, refuseEdit(EditBadValue, i, "%s is not a name the runner can run (2 to 12 letters or digits once hyphens are removed)", op.To)
	}
	if factory.ApproveWord(op.Name) || factory.ApproveWord(word) {
		return op, refuseEdit(EditLocked, i, "approve is a person by its name and is not renamed from or into")
	}
	return op, nil
}

// renameAlone refuses an edit that names a step a rename_step renames, by
// either name, so no edit's meaning depends on whether it runs before or after
// the rename.
func renameAlone(ops []EditOp) *EditError {
	owner := map[string]int{}
	for i, op := range ops {
		if op.Op != OpRenameStep {
			continue
		}
		for _, n := range []string{op.Name, op.To} {
			if _, seen := owner[n]; seen {
				return refuseEdit(EditBadOp, i, "%s is renamed once, and by nothing else in the same preview", n)
			}
			owner[n] = i
		}
	}
	for i, op := range ops {
		for _, n := range []string{op.Name, deref(op.After), op.To} {
			if at, renamed := owner[n]; n != "" && renamed && at != i {
				return refuseEdit(EditBadOp, i, "%s is renamed in this preview, so no other edit names it", n)
			}
		}
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// rename applies one rename_step to every pipeline, the required list and the
// step's definition.
func (st *editState) rename(i int, op EditOp) *EditError {
	if !st.inPipeline[op.Name] {
		return refuseEdit(EditUnknownStep, i, "%s is not a step in this pipeline", op.Name)
	}
	if _, taken := st.out.Steps[op.To]; taken || st.inPipeline[op.To] {
		return refuseEdit(EditBadValue, i, "%s is already a step; pick another name", op.To)
	}
	step := st.out.Steps[op.Name]
	if proofStep(st.out, op.Name) || proofStep(File{Steps: map[string]Step{op.To: step}}, op.To) {
		return refuseEdit(EditLocked, i, "proof is proof by its name and is not renamed from or into")
	}
	for kind, list := range st.out.Pipeline {
		at := indexOf(list, op.Name)
		if at < 0 {
			continue
		}
		rest := slices.Delete(slices.Clone(list), at, at+1)
		if other := sameStage(rest, op.To); other != "" {
			return refuseEdit(EditBadValue, i, "%s would run as the same stage as %s in the %s pipeline", op.To, other, kind)
		}
		list = slices.Clone(list)
		list[at] = op.To
		st.out.Pipeline[kind] = list
	}
	st.out.Steps[op.To] = step
	// THE OLD NAME IS GONE FROM THE FILE: a shipped one reads back as its
	// shipped words, as it would in any file that never defined it.
	if shipped, ok := builtinTable[op.Name]; ok {
		st.out.Steps[op.Name] = shipped
	} else {
		delete(st.out.Steps, op.Name)
	}
	if at := indexOf(st.out.Fixed, op.Name); at >= 0 {
		st.out.Fixed = slices.Clone(st.out.Fixed)
		st.out.Fixed[at] = op.To
	}
	st.structural = true
	st.renames = append(st.renames, Rename{From: op.Name, To: op.To})
	return nil
}

// renamedAs maps a list of the base's names to the names the renames gave
// them, so a person's run-up is compared by step, not by spelling.
func (st *editState) renamedAs(names []string) []string {
	out := slices.Clone(names)
	for _, r := range st.renames {
		if at := indexOf(out, r.From); at >= 0 {
			out[at] = r.To
		}
	}
	return out
}

// renamedTo is each renamed step's new name by its old one.
func (st *editState) renamedTo() map[string]string {
	out := make(map[string]string, len(st.renames))
	for _, r := range st.renames {
		out[r.From] = r.To
	}
	return out
}
