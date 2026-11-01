package factory

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Adaptation is a bounded power of the plan stage, never a free mode. Three
// layers change what an item runs: a stage's `when`, read off the item's
// triage with no judgment at all ([Fits]); the plan stage, which may add,
// switch on and skip stages within the bounds below; and the person, whose
// keys change anything. The manager and the person may also remove a stage
// from one job and move one, within the same bounds. The recipe gives each kind ONE WORD saying how much
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

// Moved is one stage an edit moves, by name, and the stage it now goes after:
// After is a stage's name, and "" puts it first. A move is of ONE JOB'S steps;
// the repository's pipeline moves through its own editor
// (internal/factory/pipeline/edit_structure.go), whose rules this one mirrors.
type Moved struct {
	Stage string
	After string
}

// RunEdit is THE ONE EDIT to an item's stages, whoever makes it: the manager
// before a run, the plan stage during one, the person from settings or in
// words. A run is a short program of one-word stages, and this is the whole
// of what may change about it; there is no field for the cap, the
// fanout, and its rounds only as Max below: the person's, at most
// [StepTriesMost], so an edit can never raise what an item may spend past a
// bound the person chose.
//
//   - Add is new conversation stages, each placed after a named stage, or
//     an approve step (named approve, or of kind gate), where the run holds
//     for the person.
//   - Ask is a new ask for a stage, by name.
//   - Thinking is a stage's thinking, by name: "", cheap or strong.
//   - Max is how many tries a conversation stage gets, by name: 1 to
//     [StepTriesMost]. Only the person sets it.
//   - On switches a stage on by name, an off one included.
//   - Skip switches a stage off by name; it stays in the job, off.
//   - Remove takes a stage out of this job by name: it is gone from the job's
//     steps, not off, and nothing else about the job or its pipeline changes.
//   - Move puts a stage after another by name, or first.
//   - Why is one line saying why, recorded last and kept on every stage the
//     edit touches.
//   - By is who edits: manager, plan or you; "" is plan.
type RunEdit struct {
	Add      []Added
	Ask      map[string]string
	Thinking map[string]string
	Max      map[string]int
	On       []string
	Skip     []string
	Remove   []string
	Move     []Moved
	Why      string
	By       string
}

// Empty says the edit changes nothing. A reason with no change is nothing to
// record.
func (e RunEdit) Empty() bool {
	return len(e.Add) == 0 && len(e.Ask) == 0 && len(e.Thinking) == 0 && len(e.Max) == 0 && len(e.On) == 0 && len(e.Skip) == 0 &&
		len(e.Remove) == 0 && len(e.Move) == 0
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
// is a RunEdit by plan ([PlanEdit.RunEdit]). Remove and Move are the item
// card's (`factory_item`); the plan stage's own `plan_edit` never names them.
type PlanEdit struct {
	Add    []Stage
	On     []string
	Skip   []string
	Remove []string
	Move   []Moved
	Why    string
}

// Empty says the edit changes nothing.
func (e PlanEdit) Empty() bool { return e.RunEdit().Empty() }

// RunEdit is the same edit in the one shape, by plan.
func (e PlanEdit) RunEdit() RunEdit {
	out := RunEdit{On: e.On, Skip: e.Skip, Remove: e.Remove, Move: e.Move, Why: e.Why, By: ByPlan}
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
	// skipped, removed or moved unless it is proof or a stage the policy
	// names.
	EditBeforeRun EditMode = "before run"
	// EditInRun is an edit while the item runs: the same, but a stage that
	// is done or running is never touched and nothing is added before one,
	// and the recipe's adapt word holds (fixed refuses, ask puts an approve
	// step after plan). The zero mode is this one.
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
//   - a dropped job is never edited ([RefuseDropped]);
//   - a name is one word ([StageWord]) the item does not have yet, in any
//     case, and an item has at most [StageMost] stages;
//   - a stage is found by its name without case and is kept in its stored
//     spelling; a name two stages answer to refuses the whole edit;
//   - an added stage is a conversation with an ask;
//   - an ask is at most [AskMost] cells, and thinking is "", cheap or strong;
//   - the stage named proof and a stage a policy line names are never
//     skipped, removed or moved; an approve step ([IsApprove]) may be added
//     or skipped like any stage that is not fixed;
//   - A PERSON'S ROW STAYS WHERE THE PERSON PUT IT: only you remove or move
//     an approve step, as only you skip one, so nobody else reorders the
//     places the run holds for you. A conversation may be moved across one,
//     as the repository's pipeline editor allows
//     (internal/factory/pipeline/edit_structure.go);
//   - only you remove or move a post, which posts outward, as that editor
//     locks one; and a post is never moved before the proof or the approve
//     step it stood after, so nothing posts before what holds it;
//   - a stage that is done or running (or waiting on the person, or failed)
//     is never changed, removed or moved, and nothing is added or moved
//     before one;
//   - nothing is moved after proof but a person's approve step, which does no
//     work, and a post, which publishes what proof saw, for the reason
//     nothing is added there: proof has to see it;
//   - a stage the edit removes is not also changed, placed after, or moved by
//     the same edit, a stage is moved once in one edit, a step does not go
//     after itself, and a job keeps at least one step;
//   - A FIXED STAGE ([Stage.Fixed], [StageFixed]) BINDS EVERYONE, YOU TOO:
//     its ask and thinking are never changed and it is never skipped,
//     removed or moved, and the refusal is [FixedRefusal]'s sentence.
//     Switching it on is allowed.
//
// In a run ([EditInRun]) the recipe's adapt word holds for everyone but you:
// under `fixed` every change is refused, and under `ask` an approve step
// stands after plan, so the person ratifies the change before anything after
// plan runs. Before a run `fixed` holds for everyone but you too.
//
// The lines start with who (`manager`, `plan`, `you`) and say `set <name>:
// <ask>`, `set <name> thinking <word>`, `added <name>` (`added <name> after
// <stage>` when the edit named the stage), `switched on <name>`, `skipped
// <name>`, `removed <name>`, `moved <name> after <stage>` (`moved <name>
// first`), in that order, each in the order the edit names them, and `why:
// <why>` last when the edit says why. A move to where the stage already
// stands is no change and says nothing. They are appended to [Item.Adapted]
// on the item answered, and returned so a caller may log them too; a caller
// never appends them a second time. Every stage added, changed or moved
// carries By and Why; a removed stage is gone, and its line is its record.
func Edit(it Item, e RunEdit, recipe Recipe, mode EditMode) (out Item, outLines []string, err error) {
	// The HTTP door needs to distinguish a bounded refusal from a failed store write.
	defer func() {
		if err != nil {
			err = EditRefusal{Cause: err}
		}
	}()
	if e.Empty() {
		return it, nil, nil
	}
	// A DROPPED JOB'S STEPS ARE NOT EDITED by anyone: nothing of it runs, and
	// Bring back sets its steps to the recipe again ([RefuseDropped]).
	if err := RefuseDropped(it); err != nil {
		return it, nil, err
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
	if who != ByYou {
		word := recipe.AdaptFor(kind)
		// THE FACTORY'S OWN WORD TRAVELS WITH THE JOB ([Item.House]) and can
		// only tighten the repository's, whichever machine runs the edit.
		if it.House != nil {
			word, _ = StricterAdapt(word, it.House.Adapt)
		}
		// BEFORE A RUN ONLY `fixed` BINDS: steps that may never change are
		// not the manager's to change ahead of the run either.
		if mode == EditInRun || word == AdaptFixed {
			adapt = word
		}
	}
	if adapt == AdaptFixed {
		return it, nil, fmt.Errorf("the recipe for %s is fixed; %s may not change the stages", kind, who)
	}
	stages := CopyStages(it.Stages)
	if len(stages) == 0 {
		stages = CopyStages(recipe.For(kind))
	}
	started := startedStages(it)
	// Edits through this shared door cannot change a landed job, even when
	// its recipe omitted or switched off proof. No new work may bypass the
	// person about to publish an already-landed job.
	if it.State == StateLanded {
		return it, nil, errors.New("the job has landed, so the steps can no longer be edited")
	}
	// ONCE PROOF HAS STARTED THE STAGES ARE FROZEN through this door. The sheet
	// was written for the stages as they stood; a stage added, switched on or
	// reworded afterwards would be approved by a sheet that never saw it, and a
	// landed job would wait on the person with that stage still pending.
	if proofStarted(it, stages) {
		return it, nil, errors.New(ProofFrozenRefusal)
	}
	fixed := func(i int) bool { return StageFixed(stages, i, recipe, kind) }
	// workAfterProof says whether stage i is work the proof sheet cannot have
	// seen: a stage past proof that is not a person's approve step, which is a
	// hold and does no work. Skipping such a stage is always fine; nothing in
	// the edit may start or reshape one.
	workAfterProof := func(i int) bool {
		p := firstProof(stages)
		return p >= 0 && i > p && !IsApprove(stages[i])
	}
	why := oneLine(e.Why)
	var errs []error
	refuse := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	var lines []string
	say := func(l string) { lines = append(lines, who+" "+l) }
	// touched is every stage, by stored name, that an earlier part of this
	// edit changed, placed a stage after, or moved: a removal of one of them
	// would leave a line about a stage the job no longer has.
	touched := map[string]bool{}
	mark := func(i int, stageWhy string) {
		touched[stages[i].Name] = true
		stages[i].By = who
		if stageWhy = oneLine(stageWhy); stageWhy == "" {
			stageWhy = why
		}
		stages[i].Why = stageWhy
	}
	// find is the index of a named stage, or a refusal for an edit's part. THE
	// NAME IS LOOKED UP WITHOUT CASE ([stageMatches]) and the stored spelling is
	// kept; a name that two stages answer to refuses, even when the request
	// spells one of them exactly, because choosing one would be a guess and
	// leaving the other would be half a change. A refusal fails the whole edit.
	find := func(name, doing string) int {
		switch hits := stageMatches(stages, name); len(hits) {
		case 0:
			refuse("there is no stage named %s to %s", strings.TrimSpace(name), doing)
		case 1:
			return hits[0]
		default:
			refuse("more than one stage is named %s, so it cannot be changed by name until the steps have one name each", strings.TrimSpace(name))
		}
		return -1
	}

	for _, name := range sortedKeys(e.Ask) {
		i := find(name, "set")
		if i < 0 {
			continue
		}
		ask := oneLine(e.Ask[name])
		switch {
		// A CHECK'S ASK IS A SHELL COMMAND: only the person changes what runs
		// on their checkout, never a model that read the issue.
		case stages[i].Kind == StageCheck && who != ByYou && ask != stages[i].Ask:
			refuse("only the person may change the command of %s, a check", stages[i].Name)
		case fixed(i) && ask != stages[i].Ask:
			errs = append(errs, FixedRefusal(stages[i].Name))
		case started[stages[i].Name]:
			refuse("%s may not change %s, which has already run", who, stages[i].Name)
		case workAfterProof(i) && ask != stages[i].Ask:
			refuse("%s may not change %s, which comes after proof", who, stages[i].Name)
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
		case workAfterProof(i) && word != stages[i].Effort:
			refuse("%s may not change %s, which comes after proof", who, stages[i].Name)
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
	for _, name := range sortedKeys(e.Max) {
		i := find(name, "set")
		if i < 0 {
			continue
		}
		tries := e.Max[name]
		switch {
		// HOW MANY TRIES A STEP GETS IS WHAT IT MAY SPEND, so it is the
		// person's alone, and bounded.
		case who != ByYou:
			refuse("only the person sets how many tries a step gets")
		case tries < 1 || tries > StepTriesMost:
			refuse("%s", ErrStepTries.Error())
		case stages[i].Kind == StageCheck || IsApprove(stages[i]):
			refuse("%s is a check or an approval; only a conversation step's tries change here", stages[i].Name)
		case fixed(i) && tries != max(stages[i].Max, 1):
			errs = append(errs, FixedRefusal(stages[i].Name))
		case started[stages[i].Name]:
			refuse("%s may not change %s, which has already run", who, stages[i].Name)
		case workAfterProof(i) && tries != max(stages[i].Max, 1):
			refuse("%s may not change %s, which comes after proof", who, stages[i].Name)
		case tries != max(stages[i].Max, 1):
			stages[i].Max = tries
			mark(i, "")
			say("set " + stages[i].Name + " tries " + strconv.Itoa(tries))
		}
	}
	for _, add := range e.Add {
		// A CHECK RUNS A SHELL COMMAND in the job's checkout, so only the
		// person adds one; the manager and the plan add conversations.
		if add.Stage.Kind == StageCheck && who != ByYou {
			refuse("only the person may add a check to a job; %s adds conversations", who)
			continue
		}
		st, when, bad := plannedStage(add.Stage)
		if bad != nil {
			refuse("%s", bad.Error())
			continue
		}
		// THE PLACE IS RESOLVED THROUGH THE SAME UNIQUE LOOKUP AS EVERY OTHER
		// NAME, and [PlaceStage] is handed the STORED spelling: its own lookup is
		// byte-exact, so a lowercased `tidy` would miss a legacy `Tidy` and drop
		// the stage somewhere the person never asked for. Explicit After and the
		// ask's own `after X` / `before X` both go through it; `first`, `last`
		// and no place are not anchored and pass untouched.
		anchor := ""
		if after := strings.TrimSpace(add.After); after != "" {
			i := find(after, "add after")
			if i < 0 {
				continue
			}
			anchor = stages[i].Name
			when = "after " + anchor
		} else if verb, name, ok := strings.Cut(when, " "); ok && (verb == "after" || verb == "before") {
			// A textual anchor no stage answers to keeps [PlaceStage]'s own
			// meaning (just before proof); one two stages answer to refuses.
			switch hits := stageMatches(stages, name); len(hits) {
			case 1:
				when = verb + " " + stages[hits[0]].Name
			case 0:
			default:
				refuse("more than one stage is named %s, so %s cannot be added %s it until the steps have one name each", name, st.Name, verb)
				continue
			}
		}
		if IsApprove(st) {
			// A SECOND APPROVE IS ITS OWN STEP, never a clash of names.
			st.Name = approveName(stages)
		}
		if len(stageMatches(stages, st.Name)) > 0 {
			refuse("there is already a stage named %s; %s may switch it on instead", st.Name, who)
			continue
		}
		if len(stages) >= StageMost {
			refuse("%s", ErrNineStages.Error())
			continue
		}
		next := PlaceStage(stages, st, when)
		at := StageIndex(next, st.Name)
		if p := firstProof(next); p >= 0 && at > p && !IsApprove(st) {
			refuse("%s may not add %s after proof; proof has to see it", who, st.Name)
			continue
		}
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
		if anchor != "" {
			touched[anchor] = true
		}
		if anchor != "" {
			say("added " + st.Name + " after " + anchor)
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
		case !stages[i].On && workAfterProof(i):
			refuse("%s may not switch on %s, which comes after proof", who, stages[i].Name)
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
		case IsProofStage(st):
			refuse("%s may not skip proof", who)
		case IsApprove(st) && who != ByYou:
			refuse("only you may choose automatic approval for %s", st.Name)
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
	for _, name := range e.Remove {
		i := find(name, "remove")
		if i < 0 {
			continue
		}
		st := stages[i]
		switch {
		case fixed(i):
			errs = append(errs, FixedRefusal(st.Name))
		case IsProofStage(st):
			refuse("%s may not remove %s; proof vouches for the work before it", who, st.Name)
		case IsApprove(st) && who != ByYou:
			refuse("only you may remove %s, where the run holds for you", st.Name)
		case st.Kind == StagePost && who != ByYou:
			refuse("only you may remove %s, which posts outward", st.Name)
		case policyNames(recipe.Policy, st.Name):
			refuse("%s may not remove %s; the policy names it", who, st.Name)
		case started[st.Name]:
			refuse("%s may not remove %s, which has already run", who, st.Name)
		case touched[st.Name]:
			refuse("%s is changed by the same edit, so it is not removed", st.Name)
		// AN EMPTY JOB IS NOT AN EMPTY RUN: a job with no steps of its own
		// is given its recipe's again (above), so its last step stays.
		case len(stages) == 1:
			refuse("a job keeps at least one step; %s is its last", st.Name)
		default:
			stages = append(append([]Stage(nil), stages[:i]...), stages[i+1:]...)
			say("removed " + st.Name)
		}
	}
	moved := map[string]bool{}
	for _, mv := range e.Move {
		i := find(mv.Stage, "move")
		if i < 0 {
			continue
		}
		st := stages[i]
		// THE PLACE IS FOUND BY THE SAME UNIQUE LOOKUP AS EVERY NAME, and kept
		// in its stored spelling; "" is first.
		anchor := ""
		if after := strings.TrimSpace(mv.After); after != "" {
			j := find(after, "move after")
			if j < 0 {
				continue
			}
			if j == i {
				refuse("a step does not go after itself; %s stays where it is", st.Name)
				continue
			}
			anchor = stages[j].Name
		}
		switch {
		case fixed(i):
			errs = append(errs, FixedRefusal(st.Name))
			continue
		case IsProofStage(st):
			refuse("%s may not move %s; proof vouches for the work before it", who, st.Name)
			continue
		case IsApprove(st) && who != ByYou:
			refuse("only you may move %s, where the run holds for you", st.Name)
			continue
		case st.Kind == StagePost && who != ByYou:
			refuse("only you may move %s, which posts outward", st.Name)
			continue
		case policyNames(recipe.Policy, st.Name):
			refuse("%s may not move %s; the policy names it", who, st.Name)
			continue
		case started[st.Name]:
			refuse("%s may not move %s, which has already run", who, st.Name)
			continue
		case moved[st.Name]:
			refuse("%s is moved once in one edit", st.Name)
			continue
		}
		moved[st.Name] = true
		rest := append(append([]Stage(nil), stages[:i]...), stages[i+1:]...)
		at := 0
		if anchor != "" {
			at = StageIndex(rest, anchor) + 1
		}
		if at == i {
			// Where it already stands: no change, and nothing to say.
			continue
		}
		next := insertStage(rest, at, st)
		// A POST STANDS AFTER PROOF, where a proved change is published, so it
		// is not refused there; it is never moved before the proof or the
		// approve step it stood after: nothing posts before what holds it.
		if p := firstProof(next); p >= 0 && at > p && !IsApprove(st) && st.Kind != StagePost {
			refuse("%s may not move %s after proof; proof has to see it", who, st.Name)
			continue
		}
		// NOR IS WHAT HOLDS A POST MOVED AFTER IT: an approval (or proof)
		// moved after a post it stood before lets the post go first as surely
		// as moving the post would.
		if post := passedPost(stages[i+1:], next[:at]); post != "" && (IsApprove(st) || IsProofStage(st)) {
			refuse("%s may not move %s after %s; nothing posts before what holds it", who, st.Name, post)
			continue
		}
		blocked := false
		for _, later := range next[at+1:] {
			if started[later.Name] {
				refuse("%s may not move %s before %s, which has already run", who, st.Name, later.Name)
				blocked = true
				break
			}
			if st.Kind == StagePost && (IsProofStage(later) || IsApprove(later)) && StageIndex(stages[:i], later.Name) >= 0 {
				refuse("%s may not move %s before %s; nothing posts before what holds it", who, st.Name, later.Name)
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		stages = next
		mark(at, "")
		if anchor != "" {
			touched[anchor] = true
			say("moved " + st.Name + " after " + anchor)
		} else {
			say("moved " + st.Name + " first")
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
	if adapt == AdaptAsk {
		// UNDER ASK THE PERSON RATIFIES THE CHANGE before anything after plan
		// runs: an approve step after plan, unless one stands there already or
		// what follows plan has begun. PLAN IS FOUND BY THE SAME UNIQUE LOOKUP
		// AS EVERY NAME THE EDIT REACHED: a legacy `Plan` is plan here too, or
		// an edit that reached its `Write` would land with no one to ratify it.
		// Two stages that both answer to plan refuse the whole edit, because
		// ratifying after one of them is a guess and skipping is no ratification.
		hits := stageMatches(stages, "plan")
		if len(hits) > 1 {
			return it, nil, errors.New("more than one stage is named plan, so there is no one place to ask for approval until the steps have one name each")
		}
		if len(hits) == 1 {
			p := hits[0]
			next := p + 1
			switch {
			case next < len(stages) && IsApprove(stages[next]):
				stages[next].On = true
			case next < len(stages) && started[stages[next].Name]:
			case len(stages) >= StageMost:
			default:
				stages = insertStage(stages, next, Approve(stages))
			}
		}
	}
	it.Stages = stages
	it.Adapted = append(append([]string(nil), it.Adapted...), lines...)
	return it, lines, nil
}

// ProofName is the stage whose sheet vouches for the work before it.
const ProofName = "proof"

// IsProofStage says whether a stage is a proof conversation: the one named
// proof, a send-back's prove*, or any stage that runs until proven, whatever
// it is called. Checks and posts are NOT proof here, though their claims reach
// the sheet: they can precede legitimate work, so they never freeze an edit.
// The run loop's sheet maker asks this same question so the two cannot drift.
func IsProofStage(st Stage) bool {
	name := strings.ToLower(strings.TrimSpace(st.Name))
	return name == ProofName || strings.HasPrefix(name, "prove") ||
		strings.TrimSpace(st.Until) == UntilProven
}

// firstProof is the index of the earliest proof stage, or -1.
func firstProof(stages []Stage) int {
	for i, st := range stages {
		if IsProofStage(st) {
			return i
		}
	}
	return -1
}

// passedPost is the first post among ahead (the steps a moved step now
// follows) that stood among after (the steps it followed before), "" when
// there is none: the post the move would let go before it.
func passedPost(after, ahead []Stage) string {
	for _, st := range ahead {
		if st.Kind == StagePost && StageIndex(after, st.Name) >= 0 {
			return st.Name
		}
	}
	return ""
}

// proofStarted says whether any proof phase has begun. A phase counts when its
// own name reads as proof, or when it names a configured stage that is proof,
// so a renamed until-proven stage and a historical phase both freeze the edit.
func proofStarted(it Item, stages []Stage) bool {
	if it.Stream == nil {
		return false
	}
	for _, ph := range it.Stream.Phases {
		if (ph.State == "" || ph.State == PhasePending) && ph.Round == 0 {
			continue
		}
		if IsProofStage(Stage{Name: ph.Name}) {
			return true
		}
		for _, st := range stages {
			if strings.EqualFold(strings.TrimSpace(st.Name), strings.TrimSpace(ph.Name)) && IsProofStage(st) {
				return true
			}
		}
	}
	return false
}

// ProofFrozenRefusal is the sentence Edit says to an edit of the stages
// once proof has started.
const ProofFrozenRefusal = "proof has started, so the steps can no longer be edited"

// FixedRefusal is the one sentence every road says when it is asked to change
// a stage the recipe file fixes ([Stage.Fixed]), whoever asked:
// `security is fixed by the recipe · change .codeaf/factory.yaml to change it`.
func FixedRefusal(name string) error {
	return errors.New(strings.TrimSpace(name) + " is fixed by the recipe · change " + PipelineFile + " to change it")
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
	// THE RECIPE'S INTENT IS READ CONSERVATIVELY: an edit now reaches a legacy
	// `Review` by any case, so a recipe stage that is fixed protects every stage
	// that is the same name by that fold, whichever spelling either side holds.
	// Nothing is migrated and the exact [StageIndex] stays what the runner uses.
	for _, r := range rs {
		if r.Fixed && strings.EqualFold(strings.TrimSpace(r.Name), strings.TrimSpace(stages[i].Name)) {
			return true
		}
	}
	return false
}

// sortedKeys is a map's keys in order, so an edit's lines read the same on
// every run.
func sortedKeys[V any](m map[string]V) []string {
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
// plannedCheck is a check added to one run: its ask is the command, run as
// written; it stops on done, clean, green or proven (green when it says none)
// and takes no effort or fanout, since no model reads it. Max is its rounds,
// zero for one.
func plannedCheck(add Stage, place string) (Stage, string, error) {
	st := add
	st.Ask = strings.TrimSpace(oneLine(add.Ask))
	switch {
	case strings.TrimSpace(add.Name) == "":
		return Stage{}, "", errors.New("a check added to a run needs a name")
	case st.Ask == "":
		return Stage{}, "", errors.New("a check added to a run needs the command it runs")
	case askCells(st.Ask) > AskMost:
		return Stage{}, "", ErrAskTooLong
	case st.Effort != "" || st.Fanout != "":
		return Stage{}, "", errors.New("a check runs a command, not a model; it takes no effort or fanout")
	case st.Max < 0 || st.Max > CheckRoundsMost:
		return Stage{}, "", fmt.Errorf("a check runs from 1 to %d rounds", CheckRoundsMost)
	}
	switch strings.ToLower(strings.TrimSpace(st.Until)) {
	case "":
		st.Until = UntilGreen
	case UntilDone, UntilClean, UntilGreen, UntilProven:
		st.Until = strings.ToLower(strings.TrimSpace(st.Until))
	default:
		return Stage{}, "", errors.New("a check stops on done, clean, green or proven")
	}
	st.Name = strings.ToLower(strings.TrimSpace(add.Name))
	st.Kind, st.On = StageCheck, true
	st.OldGate, st.OldGateWhen = "", ""
	st.Proof = nil
	return st, place, nil
}

// CheckRoundsMost is the most rounds a check added to one run may take.
const CheckRoundsMost = 9

func plannedStage(add Stage) (st Stage, when string, bad error) {
	// AN APPROVE STEP IS ADDED BY ITS NAME OR ITS KIND, and has no ask: the
	// person is the step. Its place is the ask's time word or the edit's
	// after, as any stage's is.
	place, rest := stagePlacementOf(add.Ask)
	rest = strings.ToLower(strings.Trim(rest, " .,;:!"))
	rest = strings.TrimPrefix(strings.TrimPrefix(rest, "an "), "a ")
	rest = strings.TrimSuffix(rest, " step")
	if add.Kind == StageGate || ApproveWord(strings.ToLower(strings.TrimSpace(add.Name))) || ApproveWord(rest) {
		a := Approve(nil)
		if w := strings.ToLower(strings.TrimSpace(add.When)); w != "" && oneOf(w, WhenWords) {
			a.When = w
		}
		a.Why, a.By = add.Why, add.By
		return a, place, nil
	}
	if add.Kind == StageCheck {
		return plannedCheck(add, place)
	}
	if add.Kind != "" && add.Kind != StageChat {
		return Stage{}, "", errors.New("a stage added to a run is a conversation or a check, not a " + string(add.Kind) + " stage")
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
	st.OldGate, st.OldGateWhen = "", ""
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
		if (ph.State != "" && ph.State != PhasePending) || ph.Round > 0 {
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

// EditRefusal retains the exact bounded-edit sentence while identifying a conflict.
type EditRefusal struct{ Cause error }

func (e EditRefusal) Error() string { return e.Cause.Error() }
func (e EditRefusal) Unwrap() error { return e.Cause }

// adaptRank orders the adapt words from the most freedom to the least: "" and
// adapt let the plan reshape within the bounds, ask makes a person ratify it,
// fixed lets nobody but the person change the steps.
func adaptRank(m AdaptMode) int {
	switch m {
	case AdaptAsk:
		return 1
	case AdaptFixed:
		return 2
	}
	return 0
}

// StricterAdapt is want when it gives the plan no more freedom than have, and
// have (false) when want would loosen it: a personal file or the factory may
// tighten a repository's adapt word, never loosen it.
func StricterAdapt(have, want AdaptMode) (AdaptMode, bool) {
	if adaptRank(want) < adaptRank(have) {
		return have, false
	}
	return want, true
}
