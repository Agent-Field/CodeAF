package pipeline

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE FILE'S SETTINGS: ADAPT, REQUIRED STEPS AND WHAT STARTS ON ITS OWN ───
//
// Three edits set what the file says beside its steps and its lists:
//
//   - set_adapt sets `adapt:`, how much a job's plan may reshape the steps it
//     runs: adapt (within the bounds, the default, written as no key), ask (a
//     person approves a reshaped plan) or fixed (the steps as written).
//   - set_fixed marks one step required, or not: a step under `fixed:` is one a
//     plan may never drop or change, and Auto never passes an approve step
//     that is. Proof is always required, so it is never unmarked here.
//   - set_start sets start.max_size (XS to XL; "" is the default, M) and
//     start.max_cost (dollars, zero or more), the limits a job must be within
//     to start on its own.
//
//   - set_approver names who one person step waits on: approve (or a gate)
//     waits on the job's person ("") unless it names "@handle" or operator.
//
// start.labels, the labels that let an issue start on its own, is a list like
// the rules, and is replaced whole by set_list with the list labels.
//
// Each is set once per preview, and each is checked as the parser reads it back
// ([settingsDiffer]), so what is previewed is what is committed.

// The settings verbs, and the list of start labels.
const (
	OpSetAdapt    = "set_adapt"
	OpSetFixed    = "set_fixed"
	OpSetStart    = "set_start"
	OpSetApprover = "set_approver"
	ListLabels    = "labels"
)

// The settings a preview reports, in order.
const (
	SettingAdapt   = "adapt"
	SettingFixed   = "fixed"
	SettingMaxSize = "max_size"
	SettingMaxCost = "max_cost"
	// SettingApprovers is every person step that names who it waits on, as
	// "approve: @sam", joined ", ".
	SettingApprovers = "approvers"
)

var settingNames = []string{SettingAdapt, SettingFixed, SettingMaxSize, SettingMaxCost, SettingApprovers}

// SettingChange is one setting an edit changed, before and after, in the words
// the file reads: adapt's word, the required steps joined ", ", the size letter
// and the dollars.
type SettingChange struct {
	Setting string
	Before  string
	After   string
}

// settingsOp says whether verb is one of the three settings edits.
func settingsOp(verb string) bool {
	return verb == OpSetAdapt || verb == OpSetFixed || verb == OpSetStart || verb == OpSetApprover
}

// settingFieldsNamed says whether an edit names a field only a settings edit
// takes.
func settingFieldsNamed(op EditOp) bool {
	return op.Adapt != nil || op.Fixed != nil || op.MaxSize != nil || op.MaxCost != nil || op.Who != nil
}

// checkSettings bounds a settings edit without the file.
func checkSettings(i int, op EditOp) (EditOp, *EditError) {
	if op.Pipeline != "" || op.After != nil || op.Kind != nil || op.Value != nil || stepFieldsNamed(op) {
		return op, refuseEdit(EditBadOp, i, "%s takes no step words, pipeline, position or value", op.Op)
	}
	switch op.Op {
	case OpSetAdapt:
		if op.Name != "" || op.Fixed != nil || op.MaxSize != nil || op.MaxCost != nil || op.Who != nil {
			return op, refuseEdit(EditBadOp, i, "set_adapt takes only adapt")
		}
		if op.Adapt == nil {
			return op, refuseEdit(EditBadValue, i, "set_adapt needs adapt")
		}
		word, ok := stepWord(*op.Adapt, factory.AdaptWords)
		if !ok {
			return op, refuseEdit(EditBadValue, i, "adapt is adapt, ask or fixed")
		}
		// THE DEFAULT IS NO KEY, so setting it back never writes a line the
		// file did not have.
		if word == string(factory.AdaptFree) {
			word = ""
		}
		op.Adapt = &word
	case OpSetFixed:
		if op.Adapt != nil || op.MaxSize != nil || op.MaxCost != nil || op.Who != nil {
			return op, refuseEdit(EditBadOp, i, "set_fixed takes only a step name and fixed")
		}
		if op.Fixed == nil {
			return op, refuseEdit(EditBadValue, i, "set_fixed needs fixed, true or false")
		}
		if !stepNamed(op.Name) {
			return op, refuseEdit(EditUnknownStep, i, "that is not a step name")
		}
		if op.Name == "proof" && !*op.Fixed {
			return op, refuseEdit(EditLocked, i, "proof is always required")
		}
	case OpSetApprover:
		if op.Adapt != nil || op.Fixed != nil || op.MaxSize != nil || op.MaxCost != nil {
			return op, refuseEdit(EditBadOp, i, "set_approver takes only a step name and who")
		}
		if !stepNamed(op.Name) {
			return op, refuseEdit(EditUnknownStep, i, "that is not a step name")
		}
		if op.Who == nil {
			return op, refuseEdit(EditBadValue, i, "set_approver needs who: @handle, operator, or \"\" for the job's person")
		}
		w, ok := factory.ApproverWord(*op.Who)
		if !ok {
			return op, refuseEdit(EditBadValue, i, "who is @handle, operator, or \"\" for the job's person")
		}
		op.Who = &w
	case OpSetStart:
		if op.Name != "" || op.Adapt != nil || op.Fixed != nil || op.Who != nil {
			return op, refuseEdit(EditBadOp, i, "set_start takes only max_size and max_cost")
		}
		if op.MaxSize == nil && op.MaxCost == nil {
			return op, refuseEdit(EditBadValue, i, "set_start needs max_size or max_cost")
		}
		if op.MaxSize != nil {
			size := DefaultMaxSize
			if strings.TrimSpace(*op.MaxSize) != "" {
				var ok bool
				if size, ok = normSize(*op.MaxSize); !ok {
					return op, refuseEdit(EditBadValue, i, "max_size is XS, S, M, L or XL")
				}
			}
			op.MaxSize = &size
		}
		if op.MaxCost != nil {
			v := *op.MaxCost
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > EditBudgetMax {
				return op, refuseEdit(EditBadValue, i, "max_cost is a number of dollars, zero or more and at most %g", EditBudgetMax)
			}
		}
	}
	return op, nil
}

// settings applies one settings edit. Each setting, and each step's mark, is
// set once per preview.
func (st *editState) settings(i int, op EditOp) *EditError {
	key := op.Op
	if op.Op == OpSetFixed || op.Op == OpSetApprover {
		key += "\x00" + op.Name
	}
	if st.settingSeen[key] {
		return refuseEdit(EditBadOp, i, "%s is set once", strings.TrimPrefix(op.Op, "set_"))
	}
	st.settingSeen[key] = true
	st.settingsEdited = true
	switch op.Op {
	case OpSetAdapt:
		st.out.Adapt = factory.AdaptMode(*op.Adapt)
	case OpSetFixed:
		if !st.inPipeline[op.Name] && !st.added[op.Name] {
			return refuseEdit(EditUnknownStep, i, "%s is not a step in this pipeline", op.Name)
		}
		fixed := slices.DeleteFunc(append([]string{}, st.out.Fixed...), func(n string) bool { return n == op.Name })
		if *op.Fixed {
			fixed = append(fixed, op.Name)
		}
		st.out.Fixed = fixed
	case OpSetApprover:
		// ONLY A PERSON WAITS ON SOMEONE: approve, or a step of kind gate,
		// that a pipeline runs.
		step := st.out.Steps[op.Name]
		if !factory.ApproveWord(op.Name) && step.Kind != string(factory.StageGate) {
			return refuseEdit(EditBadValue, i, "%s is not a person; only approve or a gate waits on someone", op.Name)
		}
		if !st.inPipeline[op.Name] {
			return refuseEdit(EditUnknownStep, i, "%s is not a step in this pipeline", op.Name)
		}
		step.Who = *op.Who
		st.out.Steps[op.Name] = step
	case OpSetStart:
		if op.MaxSize != nil {
			st.out.Start.MaxSize = *op.MaxSize
		}
		if op.MaxCost != nil {
			st.out.Start.MaxCost = *op.MaxCost
		}
	}
	return nil
}

// settingOf is one setting of f in the words a preview reports it in.
func settingOf(f File, name string) string {
	switch name {
	case SettingAdapt:
		if f.Adapt == "" {
			return string(factory.AdaptFree)
		}
		return string(f.Adapt)
	case SettingFixed:
		return strings.Join(f.Fixed, ", ")
	case SettingMaxSize:
		return normOr(f.Start.MaxSize, DefaultMaxSize)
	case SettingApprovers:
		return approversOf(f)
	}
	return strconv.FormatFloat(f.Start.MaxCost, 'f', -1, 64)
}

// settingsFacts fills the settings half of a preview: every setting whose
// words changed, in the order adapt, fixed, max_size, max_cost.
func (st *editState) settingsFacts(res *Edited) {
	res.SettingsEdited = true
	res.Settings = []SettingChange{}
	for _, name := range settingNames {
		before, after := settingOf(st.base, name), settingOf(st.out, name)
		if before != after {
			res.Settings = append(res.Settings, SettingChange{Setting: name, Before: before, After: after})
		}
	}
}

// settingsDiffer is why a setting did not read back as it was set, or "".
func settingsDiffer(back, want File) string {
	for _, name := range settingNames {
		if settingOf(back, name) != settingOf(want, name) {
			return fmt.Sprintf("the file format cannot hold that %s", name)
		}
	}
	return ""
}

// approversOf is every person step a pipeline runs that names who it waits
// on, in name order, as "approve: @sam", joined ", ".
func approversOf(f File) string {
	var out []string
	for _, name := range sortedStepNames(f) {
		if who := f.Steps[name].Who; who != "" {
			out = append(out, name+": "+who)
		}
	}
	return strings.Join(out, ", ")
}

// sortedStepNames is every step the file defines, in name order.
func sortedStepNames(f File) []string {
	names := make([]string, 0, len(f.Steps))
	for name := range f.Steps {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
