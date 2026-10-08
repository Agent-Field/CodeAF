package factory

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Adaptation is a bounded power of the plan stage, never a free mode. Three
// layers change what an item runs: a stage's `when`, read off the item's
// triage with no judgment at all ([Fits]); the plan stage, which may add,
// switch on and skip stages within the bounds below; and the person, whose
// keys change anything. The recipe gives each kind ONE WORD saying how much
// freedom plan has, written after the kind in its section heading:
//
//	## issue · adapt   plan may change the stages within the bounds (the default)
//	## issue · ask     plan may, and the person ratifies before anything after plan runs
//	## issue · fixed   plan may not change the stages at all
//
// THE BOUNDS ARE HELD IN CODE, here, and never in a prompt: a model told not
// to skip proof is a model that may, and [Adapt] is the only door a plan's edit
// reaches an item through.

// AdaptMode is the recipe's one word per kind for how much plan may change the
// stages.
type AdaptMode string

const (
	AdaptFree  AdaptMode = "adapt" // within the bounds; the default
	AdaptAsk   AdaptMode = "ask"   // within the bounds, and the person ratifies
	AdaptFixed AdaptMode = "fixed" // no change at all
)

// AdaptWords are the three words in the order the manual names them.
var AdaptWords = []string{string(AdaptFree), string(AdaptAsk), string(AdaptFixed)}

// AdaptFor is the word a kind runs under: the recipe's own, or [AdaptFree]
// when it says none. An empty kind is an issue, as everywhere else.
func (r Recipe) AdaptFor(k Kind) AdaptMode {
	if k == "" {
		k = KindIssue
	}
	if m, ok := r.Adapt[k]; ok && m != "" {
		return m
	}
	return AdaptFree
}

// PlanEdit is what the plan stage may ask to change about an item's stages,
// and THE WHOLE OF IT: there is no field for the cap, the gate, the effort or
// the rounds, so a plan can never raise what an item may spend.
//
//   - Add is new conversation stages, each with an ask; a time word at the
//     start of the ask places it (`after test, …`), as [AddStageWords] does,
//     and with none it goes after review.
//   - On switches a stage of the item's recipe on by name, an off one included.
//   - Skip switches a stage that has not run yet off by name.
//   - Why is one sentence saying why, recorded last.
type PlanEdit struct {
	Add  []Stage
	On   []string
	Skip []string
	Why  string
}

// Empty says the edit changes nothing. A reason with no change is nothing to
// record.
func (e PlanEdit) Empty() bool {
	return len(e.Add) == 0 && len(e.On) == 0 && len(e.Skip) == 0
}

// Adapt applies a plan's edit to an item within the recipe's bounds, and
// answers the item, the lines it recorded, and an error naming every bound the
// edit crossed. AN EDIT IS APPLIED WHOLE OR NOT AT ALL: when any part of it is
// refused the item comes back unchanged, because half a plan's change is a
// stage list nobody decided on.
//
// The bounds, every one checked here:
//
//   - under `fixed` every edit that changes something is refused;
//   - a stage of kind gate, the stage named proof, and a stage a policy line
//     names are never skipped;
//   - a stage that is done or running (or waiting on the person, or failed) is
//     never switched or skipped, and nothing is added before one;
//   - an added stage is a conversation with an ask, and its name is not one the
//     item already has;
//   - under `ask` the item's gate becomes plan, so the person ratifies the
//     change before anything after plan runs.
//
// The lines are `plan added <name>`, `plan switched on <name>`, `plan skipped
// <name>` in that order, each in the order the edit names them, and `why:
// <why>` last when the edit says why. They are appended to [Item.Adapted] on
// the item answered, and returned so a caller may log them too; a caller never
// appends them a second time.
func Adapt(it Item, edit PlanEdit, recipe Recipe) (Item, []string, error) {
	if edit.Empty() {
		return it, nil, nil
	}
	kind := it.Kind
	if kind == "" {
		kind = KindIssue
	}
	mode := recipe.AdaptFor(kind)
	if mode == AdaptFixed {
		return it, nil, fmt.Errorf("the recipe for %s is fixed; plan may not change the stages", kind)
	}
	stages := CopyStages(it.Stages)
	if len(stages) == 0 {
		stages = CopyStages(recipe.For(kind))
	}
	started := startedStages(it)
	var errs []error
	refuse := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	var lines []string

	for _, add := range edit.Add {
		st, when, why := plannedStage(add)
		if why != "" {
			refuse("%s", why)
			continue
		}
		if StageIndex(stages, st.Name) >= 0 {
			refuse("there is already a stage named %s; plan may switch it on instead", st.Name)
			continue
		}
		next := PlaceStage(stages, st, when)
		at := StageIndex(next, st.Name)
		for _, later := range next[at+1:] {
			if started[later.Name] {
				refuse("plan may not add %s before %s, which has already run", st.Name, later.Name)
				next = nil
				break
			}
		}
		if next == nil {
			continue
		}
		stages = next
		lines = append(lines, "plan added "+st.Name)
	}
	for _, name := range edit.On {
		i := StageIndex(stages, strings.TrimSpace(name))
		switch {
		case i < 0:
			refuse("there is no stage named %s to switch on", name)
		case started[stages[i].Name]:
			refuse("plan may not switch %s, which has already run", stages[i].Name)
		case !stages[i].On:
			stages[i].On = true
			lines = append(lines, "plan switched on "+stages[i].Name)
		}
	}
	for _, name := range edit.Skip {
		i := StageIndex(stages, strings.TrimSpace(name))
		if i < 0 {
			refuse("there is no stage named %s to skip", name)
			continue
		}
		st := stages[i]
		switch {
		case st.Kind == StageGate:
			refuse("plan may not skip %s; it is a person's gate", st.Name)
		case st.Name == "proof":
			refuse("plan may not skip proof")
		case policyNames(recipe.Policy, st.Name):
			refuse("plan may not skip %s; the policy names it", st.Name)
		case started[st.Name]:
			refuse("plan may not skip %s, which has already run", st.Name)
		case st.On:
			stages[i].On = false
			lines = append(lines, "plan skipped "+st.Name)
		}
	}
	if len(errs) > 0 {
		return it, nil, errors.Join(errs...)
	}
	if len(lines) == 0 {
		return it, nil, nil
	}
	if why := oneLine(edit.Why); why != "" {
		lines = append(lines, "why: "+why)
	}
	it.Stages = stages
	if mode == AdaptAsk {
		it.Gate = GatePlan
	}
	it.Adapted = append(append([]string(nil), it.Adapted...), lines...)
	return it, lines, nil
}

// plannedStage is a stage a plan asked to add, as the item will run it: a
// conversation, on, waiting until done unless it said a word the runner knows,
// with no gate of its own. when is where it goes. why says what is wrong with
// it, and is empty when nothing is.
func plannedStage(add Stage) (st Stage, when, why string) {
	if add.Kind != "" && add.Kind != StageChat {
		return Stage{}, "", "plan may add only a conversation stage, not a " + string(add.Kind) + " stage"
	}
	words := strings.TrimSpace(add.Ask)
	parsed := ParseStage(words)
	if parsed.Ask == "" {
		return Stage{}, "", "a stage plan adds needs an ask"
	}
	st = add
	st.Kind, st.Ask, st.On = StageChat, parsed.Ask, true
	st.Gate, st.GateWhen = "", ""
	st.Proof = append([]string(nil), add.Proof...)
	if st.Name = oneLine(st.Name); st.Name == "" {
		st.Name = parsed.Name
	}
	if !oneOf(st.Until, untilWords) {
		st.Until = UntilDone
	}
	return st, StageWhen(words), ""
}

// startedStages is the stages an item's stream has begun, by name: anything
// not still pending. Plan touches only what has not run.
func startedStages(it Item) map[string]bool {
	out := map[string]bool{}
	if it.Stream == nil {
		return out
	}
	for _, ph := range it.Stream.Phases {
		if ph.State != "" && ph.State != PhasePending {
			out[ph.Name] = true
		}
	}
	return out
}

// policyNames says whether any policy line names the stage, as a whole word,
// in any case, with a plural ending allowed: `tests pass before anything posts`
// names test.
func policyNames(policy []string, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `(?:s|es)?\b`)
	for _, line := range policy {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// AdaptedLine is an item's record as one line: its lines joined with ` · `,
// every `plan ` after the first dropped because the first already said who,
// so `plan added security · skipped neaten · why: touches billing`. Nothing
// when the item has no record (the emptiness law).
func AdaptedLine(it Item) string {
	var parts []string
	for _, l := range it.Adapted {
		if l = oneLine(l); l == "" {
			continue
		}
		if len(parts) > 0 {
			l = strings.TrimPrefix(l, "plan ")
		}
		parts = append(parts, l)
	}
	return strings.Join(parts, " · ")
}
