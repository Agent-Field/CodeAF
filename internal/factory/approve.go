package factory

import (
	"encoding/json"
	"strconv"
)

// Approve steps: where a person sits in a run.
//
// AN APPROVE STEP IS A STAGE OF KIND GATE, named `approve` (the owner's one
// word, 2026-10-09). The run holds there until the person answers: yes is
// continue, words are continue with the words handed to the next step as the
// person's note, and no sends the run back to the step before it with the
// words. The default recipe has one between plan and write; a recipe file may
// place one anywhere (a stage line `N. approve`, or a `## approve` line inside
// a kind's section), and the manager may add or remove one within the
// recipe's fixed stages ([Edit]).
//
// APPROVE STEPS REPLACE `ask me at` (the old [Gate]). An item or a recipe
// written with one is read into approve steps and never written with it
// again: plan is an approve after plan, ship an approve before the result
// (before the first post step, or last), and none no approve at all, so an
// item with no approve step ships itself when its proof is green ([HasApprove]).
//
// An approve step with nothing after it is the landing itself: the run lands
// there, and the approval is the proof sheet's.

// ApproveName is an approve step's name; a second one on the same item is
// `approve2`, then `approve3`, because a stage is named by one word.
const ApproveName = "approve"

// Approve is an approve step named for stages: `approve`, or the first of
// `approve2`, `approve3` … the stages do not have yet.
func Approve(stages []Stage) Stage {
	return Stage{Name: approveName(stages), Kind: StageGate, On: true}
}

func approveName(stages []Stage) string {
	name := ApproveName
	for k := 2; StageIndex(stages, name) >= 0; k++ {
		name = ApproveName + strconv.Itoa(k)
	}
	return name
}

// QKindApprove is the question kind of a run holding at an approve step
// ([Item.QKind]).
const QKindApprove = "approve"

// ApproveQuestion is what a run holding at an approve step asks, after the
// step before it: `plan is ready · continue, or send it back?`. A run that
// holds before any step has run asks `ready to start · continue?`.
func ApproveQuestion(prev string) string {
	if prev == "" {
		return "ready to start · continue?"
	}
	return prev + " is ready · continue, or send it back?"
}

// IsApprove says whether a stage is an approve step: a person's gate.
func IsApprove(s Stage) bool { return s.Kind == StageGate }

// HasApprove says whether an item holds for a person anywhere: an approve
// step that is on and fits it. An item without one ships itself when its
// proof is all green, which is what `ask me at never` used to say.
func HasApprove(it Item) bool {
	for _, s := range it.Stages {
		if IsApprove(s) && s.On && Fits(s, it) {
			return true
		}
	}
	return false
}

// ExpandGates reads the stages' old gate knobs into approve steps and clears
// them: `gate ship` is an approve after its stage; `gate plan` is an approve
// after the stage named plan, and before any other stage that carries it
// (where it used to stop), with the gate's condition as the approve's when;
// `gate none` is nothing. An approve already standing at the place is not
// doubled. Stages without an old gate come back as they were.
func ExpandGates(stages []Stage) []Stage {
	old := false
	for _, s := range stages {
		if s.OldGate != "" || s.OldGateWhen != "" {
			old = true
			break
		}
	}
	if !old {
		return stages
	}
	out := make([]Stage, 0, len(stages)+2)
	for _, s := range stages {
		g, when := s.OldGate, s.OldGateWhen
		s.OldGate, s.OldGateWhen = "", ""
		before := g == GatePlan && s.Name != "plan"
		after := g == GateShip || (g == GatePlan && s.Name == "plan")
		if before && (len(out) == 0 || !IsApprove(out[len(out)-1])) {
			a := Approve(append(append([]Stage(nil), out...), stages...))
			a.When = when
			out = append(out, a)
		}
		out = append(out, s)
		if after {
			a := Approve(append(append([]Stage(nil), out...), stages...))
			a.When = when
			out = append(out, a)
		}
	}
	return dedupeApprove(out)
}

// dedupeApprove drops an approve step standing straight after another one.
func dedupeApprove(stages []Stage) []Stage {
	out := stages[:0:0]
	for _, s := range stages {
		if IsApprove(s) && len(out) > 0 && IsApprove(out[len(out)-1]) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// ApproveForGate places what an old `ask me at` said as approve steps on
// stages: plan puts one after plan (after the first stage when there is no
// plan), ship one before the first post step or last, and none takes every
// approve step out. An approve already in the place is kept, never doubled.
// No stages is no place, and comes back empty.
func ApproveForGate(stages []Stage, g Gate) []Stage {
	if len(stages) == 0 {
		return stages
	}
	out := CopyStages(stages)
	switch g {
	case GatePlan:
		at := StageIndex(out, "plan") + 1
		if at == 0 {
			at = 1
		}
		if at < len(out) && IsApprove(out[at]) {
			return out
		}
		return insertStage(out, at, Approve(out))
	case GateShip:
		at := len(out)
		for i, s := range out {
			if s.Kind == StagePost {
				at = i
				break
			}
		}
		if at > 0 && IsApprove(out[at-1]) {
			return out
		}
		return insertStage(out, at, Approve(out))
	case GateNone:
		kept := out[:0]
		for _, s := range out {
			if !IsApprove(s) {
				kept = append(kept, s)
			}
		}
		return kept
	}
	return out
}

func insertStage(stages []Stage, at int, st Stage) []Stage {
	out := make([]Stage, 0, len(stages)+1)
	out = append(out, stages[:at]...)
	out = append(out, st)
	return append(out, stages[at:]...)
}

// MigrateGates reads an item written before approve steps: its stages' gate
// knobs and its own `ask me at` become approve steps, and both are cleared.
// An item with neither comes back as it was.
func MigrateGates(it *Item) {
	it.Stages = ExpandGates(it.Stages)
	if it.OldGate != "" {
		it.Stages = ApproveForGate(it.Stages, it.OldGate)
		it.OldGate = ""
	}
}

// UnmarshalJSON reads an item document, including one written with `ask me
// at` ([Gate]), whose gate is read into approve steps ([MigrateGates]) so
// NOTHING A PERSON SET IS LOST and the next write carries the new form.
func (it *Item) UnmarshalJSON(data []byte) error {
	type plain Item
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*it = Item(p)
	MigrateGates(it)
	RepairShipDefault(it)
	NormalizeStageNames(it)
	return nil
}

// RepairShipDefault puts the approve step of an issue still on the OLD
// DEFAULT back where the recipe puts it, after plan, and answers whether it
// moved one.
//
// WHY THERE IS SUCH AN ITEM: until approve steps (2026-10-09) the floor wrote
// every issue it read from GitHub with `ask me at ship` and the recipe's proof
// stage with `gate ship`, as the default and not as anybody's choice. Read
// into approve steps that became one approve step at the very end (plan write
// test review security proof approve), which the owner met on #1550, where an
// issue's approve step belongs after plan. So an issue that NEVER RAN, whose
// steps nobody edited (no step carries who set it), whose one approve step is
// its last, and whose other steps are the default recipe's in order, is read
// as the default it was: the approve step moves to after plan, everything
// else about every step kept. Any other item is left exactly as it is.
func RepairShipDefault(it *Item) bool {
	if it == nil || it.Stream != nil || (it.Kind != KindIssue && it.Kind != "") {
		return false
	}
	n := len(it.Stages)
	if n < 2 || !IsApprove(it.Stages[n-1]) {
		return false
	}
	var names []string
	for _, s := range it.Stages[:n-1] {
		if IsApprove(s) || s.By != "" {
			return false
		}
		names = append(names, s.Name)
	}
	if it.Stages[n-1].By != "" {
		return false
	}
	def := DefaultRecipe().For(KindIssue)
	var want []string
	at := -1
	for i, s := range def {
		if IsApprove(s) {
			if at >= 0 {
				return false
			}
			at = i
			continue
		}
		want = append(want, s.Name)
	}
	if at < 0 || at == len(def)-1 || len(names) != len(want) {
		return false
	}
	for i := range names {
		if names[i] != want[i] {
			return false
		}
	}
	approve := it.Stages[n-1]
	it.Stages = insertStage(CopyStages(it.Stages[:n-1]), at, approve)
	return true
}
