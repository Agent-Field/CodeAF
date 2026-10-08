package factory

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
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
// to skip proof is a model that may, and [Edit] is the only door an edit to an
// item's stages reaches it through, whoever makes it ([RunEdit]).

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

// The words for who changed an item's stages, the first word of every record
// line and the value of [Stage.By].
const (
	ByRecipe  = "recipe"
	ByManager = "manager"
	ByPlan    = "plan"
	ByYou     = "you"
)

// Added is one stage an edit adds, and the stage it goes after: After is a
// stage's name, and "" places it as a time word at the start of its ask says
// (`after test, …`), or just before proof when the ask says none. The
// stage's own Why, when it has one, wins over the edit's.
type Added struct {
	Stage Stage
	After string
}

// RunEdit is THE ONE EDIT to an item's stages, whoever makes it: the manager
// before a run, the plan stage during one, the person from settings or in
// words. A run is a short program of one-word stages, and this is the whole
// of what may change about it; there is no field for the cap, the gate, the
// rounds or the fanout, so an edit can never raise what an item may spend.
//
//   - Add is new conversation stages, each placed after a named stage.
//   - Ask is a new ask for a stage, by name.
//   - Thinking is a stage's thinking, by name: "", cheap or strong.
//   - On switches a stage on by name, an off one included.
//   - Skip switches a stage off by name.
//   - Why is one line saying why, recorded last and kept on every stage the
//     edit touches.
//   - By is who edits: manager, plan or you; "" is plan.
type RunEdit struct {
	Add      []Added
	Ask      map[string]string
	Thinking map[string]string
	On       []string
	Skip     []string
	Why      string
	By       string
}

// Empty says the edit changes nothing. A reason with no change is nothing to
// record.
func (e RunEdit) Empty() bool {
	return len(e.Add) == 0 && len(e.Ask) == 0 && len(e.Thinking) == 0 && len(e.On) == 0 && len(e.Skip) == 0
}

// editors are the words for who may edit a run's stages.
var editors = []string{ByManager, ByPlan, ByYou}

// who is the edit's author word, plan when it says none.
func (e RunEdit) who() string {
	if by := strings.TrimSpace(e.By); by != "" {
		return by
	}
	return ByPlan
}

// PlanEdit is the plan stage's edit as it was before [RunEdit]: added stages
// as bare stages whose ask carries its own time word. It stays, with
// [Adapt], for one release so code written against it keeps compiling; it
// is a RunEdit by plan ([PlanEdit.RunEdit]).
type PlanEdit struct {
	Add  []Stage
	On   []string
	Skip []string
	Why  string
}

// Empty says the edit changes nothing.
func (e PlanEdit) Empty() bool { return e.RunEdit().Empty() }

// RunEdit is the same edit in the one shape, by plan.
func (e PlanEdit) RunEdit() RunEdit {
	out := RunEdit{On: e.On, Skip: e.Skip, Why: e.Why, By: ByPlan}
	for _, st := range e.Add {
		out.Add = append(out.Add, Added{Stage: st})
	}
	return out
}

// EditMode is when an edit is made, which decides its bounds.
type EditMode string

const (
	// EditBeforeRun is an edit before anything has run: the manager's
	// program for the item, or the person's. Any stage's ask and thinking
	// may change, a stage may be added after any other, switched on, or
	// skipped unless it is proof, a gate or a stage the policy names.
	EditBeforeRun EditMode = "before run"
	// EditInRun is an edit while the item runs: the same, but a stage that
	// is done or running is never touched and nothing is added before one,
	// and the recipe's adapt word holds (fixed refuses, ask sets the plan
	// gate). The zero mode is this one.
	EditInRun EditMode = "in run"
)

// Adapt is [Edit] of a plan's edit during a run, kept for the runner's fold
// and the code written before [RunEdit].
func Adapt(it Item, edit PlanEdit, recipe Recipe) (Item, []string, error) {
	return Edit(it, edit.RunEdit(), recipe, EditInRun)
}

// Edit applies an edit to an item's stages within the bounds of its mode, and
// answers the item, the lines it recorded, and an error naming every part the
// bounds refused. AN EDIT IS APPLIED WHOLE OR NOT AT ALL: when any part of it
// is refused the item comes back unchanged, because half a change is a stage
// list nobody decided on.
//
// The bounds held in both modes:
//
//   - a name is one word ([StageWord]) the item does not have yet, and an
//     item has at most [StageMost] stages;
//   - an added stage is a conversation with an ask;
//   - an ask is at most [AskMost] cells, and thinking is "", cheap or strong;
//   - a stage of kind gate, the stage named proof, and a stage a policy line
//     names are never skipped;
//   - a stage that is done or running (or waiting on the person, or failed)
//     is never changed, and nothing is added before one;
//   - A FIXED STAGE ([Stage.Fixed], [StageFixed]) BINDS EVERYONE, YOU TOO:
//     its ask and thinking are never changed and it is never skipped, and
//     the refusal is [FixedRefusal]'s sentence. Switching it on is allowed.
//
// In a run ([EditInRun]) the recipe's adapt word holds for everyone but you:
// under `fixed` every change is refused, and under `ask` the item's gate
// becomes plan, so the person ratifies the change before anything after plan
// runs.
//
// The lines start with who (`manager`, `plan`, `you`) and say `set <name>:
// <ask>`, `set <name> thinking <word>`, `added <name>` (`added <name> after
// <stage>` when the edit named the stage), `switched on <name>`, `skipped
// <name>`, in that order, each in the order the edit names them, and `why:
// <why>` last when the edit says why. They are appended to [Item.Adapted] on
// the item answered, and returned so a caller may log them too; a caller
// never appends them a second time. Every stage added or changed carries By
// and Why.
func Edit(it Item, e RunEdit, recipe Recipe, mode EditMode) (Item, []string, error) {
	if e.Empty() {
		return it, nil, nil
	}
	if mode == "" {
		mode = EditInRun
	}
	who := e.who()
	if !oneOf(who, editors) {
		return it, nil, fmt.Errorf("an edit to the stages is by the manager, plan or you, not %q", who)
	}
	kind := it.Kind
	if kind == "" {
		kind = KindIssue
	}
	adapt := AdaptFree
	if mode == EditInRun && who != ByYou {
		adapt = recipe.AdaptFor(kind)
	}
	if adapt == AdaptFixed {
		return it, nil, fmt.Errorf("the recipe for %s is fixed; %s may not change the stages", kind, who)
	}
	stages := CopyStages(it.Stages)
	if len(stages) == 0 {
		stages = CopyStages(recipe.For(kind))
	}
	started := startedStages(it)
	fixed := func(i int) bool { return StageFixed(stages, i, recipe, kind) }
	why := oneLine(e.Why)
	var errs []error
	refuse := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	var lines []string
	say := func(l string) { lines = append(lines, who+" "+l) }
	mark := func(i int, stageWhy string) {
		stages[i].By = who
		if stageWhy = oneLine(stageWhy); stageWhy == "" {
			stageWhy = why
		}
		stages[i].Why = stageWhy
	}
	// find is the index of a named stage, or a refusal for an edit's part.
	find := func(name, doing string) int {
		i := StageIndex(stages, strings.ToLower(strings.TrimSpace(name)))
		if i < 0 {
			refuse("there is no stage named %s to %s", strings.TrimSpace(name), doing)
		}
		return i
	}

	for _, name := range sortedKeys(e.Ask) {
		i := find(name, "set")
		if i < 0 {
			continue
		}
		ask := oneLine(e.Ask[name])
		switch {
		case fixed(i) && ask != stages[i].Ask:
			errs = append(errs, FixedRefusal(stages[i].Name))
		case started[stages[i].Name]:
			refuse("%s may not change %s, which has already run", who, stages[i].Name)
		case ask == "":
			refuse("a stage needs an ask; %s has none", stages[i].Name)
		case askCells(ask) > AskMost:
			refuse("%s", ErrAskTooLong.Error())
		case ask != stages[i].Ask:
			stages[i].Ask = ask
			mark(i, "")
			say("set " + stages[i].Name + ": " + ask)
		}
	}
	for _, name := range sortedKeys(e.Thinking) {
		i := find(name, "set")
		if i < 0 {
			continue
		}
		word := strings.ToLower(strings.TrimSpace(e.Thinking[name]))
		switch {
		case word != "" && !oneOf(word, EffortWords):
			refuse("%s", ErrThinking.Error())
		case fixed(i) && word != stages[i].Effort:
			errs = append(errs, FixedRefusal(stages[i].Name))
		case started[stages[i].Name]:
			refuse("%s may not change %s, which has already run", who, stages[i].Name)
		case word != stages[i].Effort:
			stages[i].Effort = word
			mark(i, "")
			if word == "" {
				say("set " + stages[i].Name + " thinking usual")
			} else {
				say("set " + stages[i].Name + " thinking " + word)
			}
		}
	}
	for _, add := range e.Add {
		st, when, bad := plannedStage(add.Stage)
		if bad != nil {
			refuse("%s", bad.Error())
			continue
		}
		if after := strings.TrimSpace(add.After); after != "" {
			if find(after, "add after") < 0 {
				continue
			}
			when = "after " + strings.ToLower(after)
		}
		if StageIndex(stages, st.Name) >= 0 {
			refuse("there is already a stage named %s; %s may switch it on instead", st.Name, who)
			continue
		}
		if len(stages) >= StageMost {
			refuse("%s", ErrNineStages.Error())
			continue
		}
		next := PlaceStage(stages, st, when)
		at := StageIndex(next, st.Name)
		blocked := false
		for _, later := range next[at+1:] {
			if started[later.Name] {
				refuse("%s may not add %s before %s, which has already run", who, st.Name, later.Name)
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		stages = next
		mark(at, add.Stage.Why)
		if after := strings.TrimSpace(add.After); after != "" {
			say("added " + st.Name + " after " + strings.ToLower(after))
		} else {
			say("added " + st.Name)
		}
	}
	for _, name := range e.On {
		i := find(name, "switch on")
		switch {
		case i < 0:
		case started[stages[i].Name]:
			refuse("%s may not switch %s, which has already run", who, stages[i].Name)
		case !stages[i].On:
			stages[i].On = true
			mark(i, "")
			say("switched on " + stages[i].Name)
		}
	}
	for _, name := range e.Skip {
		i := find(name, "skip")
		if i < 0 {
			continue
		}
		st := stages[i]
		switch {
		case fixed(i):
			errs = append(errs, FixedRefusal(st.Name))
		case st.Kind == StageGate:
			refuse("%s may not skip %s; it is a person's gate", who, st.Name)
		case st.Name == "proof":
			refuse("%s may not skip proof", who)
		case policyNames(recipe.Policy, st.Name):
			refuse("%s may not skip %s; the policy names it", who, st.Name)
		case started[st.Name]:
			refuse("%s may not skip %s, which has already run", who, st.Name)
		case st.On:
			stages[i].On = false
			mark(i, "")
			say("skipped " + st.Name)
		}
	}
	if len(errs) > 0 {
		return it, nil, errors.Join(errs...)
	}
	if len(lines) == 0 {
		return it, nil, nil
	}
	if why != "" {
		lines = append(lines, "why: "+why)
	}
	it.Stages = stages
	if adapt == AdaptAsk {
		it.Gate = GatePlan
	}
	it.Adapted = append(append([]string(nil), it.Adapted...), lines...)
	return it, lines, nil
}

// FixedRefusal is the one sentence every road says when it is asked to change
// a stage the recipe file fixes ([Stage.Fixed]), whoever asked:
// `security is fixed by the recipe · change .codeaf/factory.md to change it`.
func FixedRefusal(name string) error {
	return errors.New(strings.TrimSpace(name) + " is fixed by the recipe · change " + RecipeFile + " to change it")
}

// StageFixed says whether the stage at i of an item's stages is fixed: the
// item's copy says so, or the recipe's stage of the same name for the item's
// kind does, so an item that took its copy before the file said fixed is
// bound all the same.
func StageFixed(stages []Stage, i int, recipe Recipe, kind Kind) bool {
	if i < 0 || i >= len(stages) {
		return false
	}
	if stages[i].Fixed {
		return true
	}
	if kind == "" {
		kind = KindIssue
	}
	rs := recipe.For(kind)
	j := StageIndex(rs, stages[i].Name)
	return j >= 0 && rs[j].Fixed
}

// sortedKeys is a map's keys in order, so an edit's lines read the same on
// every run.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// plannedStage is a stage an edit asked to add, as the item will run it: a
// conversation, on, waiting until done unless it said a word the runner
// knows, with no gate of its own, named by one word. when is where its ask's
// time word puts it. bad says what is wrong with it, and is nil when nothing
// is.
func plannedStage(add Stage) (st Stage, when string, bad error) {
	if add.Kind != "" && add.Kind != StageChat {
		return Stage{}, "", errors.New("a stage added to a run is a conversation, not a " + string(add.Kind) + " stage")
	}
	parsed, when, err := StageSentence(strings.TrimSpace(add.Ask))
	if err != nil && strings.TrimSpace(add.Name) == "" {
		if err.Error() == "say what the stage should do" {
			return Stage{}, "", errors.New("a stage added to a run needs an ask")
		}
		return Stage{}, "", err
	}
	if err != nil {
		// A named stage whose ask the sentence reader refused: the ask may
		// still be good words on their own (a colon of its own, say).
		if parsed.Ask = strings.Trim(oneLine(add.Ask), " ,:;."); parsed.Ask == "" {
			return Stage{}, "", errors.New("a stage added to a run needs an ask")
		}
		if askCells(parsed.Ask) > AskMost {
			return Stage{}, "", ErrAskTooLong
		}
		when = StageWhen(add.Ask)
	}
	st = add
	st.Kind, st.Ask, st.On = StageChat, parsed.Ask, true
	st.Gate, st.GateWhen = "", ""
	st.Proof = append([]string(nil), add.Proof...)
	st.Name = parsed.Name
	if strings.TrimSpace(add.Name) != "" {
		name, err := StageWord(add.Name)
		if err != nil {
			return Stage{}, "", err
		}
		st.Name = name
	}
	if st.Effort != "" && !oneOf(st.Effort, EffortWords) {
		return Stage{}, "", ErrThinking
	}
	if !oneOf(st.Until, untilWords) {
		st.Until = UntilDone
	}
	return st, when, nil
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
// the who word dropped from a line when the line before said the same who,
// so `manager set review: thorough on security · added arch after review ·
// skipped neaten · why: touches the call row`, and `… · you skipped neaten`
// when the person changed it after. Nothing when the item has no record (the
// emptiness law). The line is not cut; the caller fits it.
func AdaptedLine(it Item) string {
	var parts []string
	last := ""
	for _, l := range it.Adapted {
		if l = oneLine(l); l == "" {
			continue
		}
		if who, rest, ok := strings.Cut(l, " "); ok && oneOf(who, editors) {
			if who == last {
				l = rest
			}
			last = who
		} else {
			last = ""
		}
		parts = append(parts, l)
	}
	return strings.Join(parts, " · ")
}
