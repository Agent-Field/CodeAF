package pipeline

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"gopkg.in/yaml.v3"
)

// ── STRUCTURED EDITS OF A REPOSITORY'S PIPELINE FILE ────────────────────────
//
// An editor never writes YAML. It names an edit, and this file turns the file
// that was really read plus that edit into the exact bytes the pull request
// route will commit. The edits are the ones the file format can hold today: a
// step's prompt, kind, when, effort, fanout, until and max, the default budget,
// a chat step added to, removed from or moved within one pipeline's list
// (edit_structure.go), and the file's own rules, ask_first and never lines
// replaced whole (edit_lists.go). Everything else in the file (the rest of
// start, size, fixed, every other pipeline and every other step) rides through
// the same parser and the same encoder the route uses, so what is previewed is
// what is committed.
//
// A REPOSITORY WITH NO FILE IS EDITED FROM THE SHIPPED DEFAULTS. An empty base
// is not a blank file (that is still base_invalid): it is "there is no file
// yet", and the edits apply to [Defaults], the same File [Load] runs when the
// default branch has none. The answer is then the whole canonical file the pull
// request creates, every shipped step written out, so the repository runs what
// it ran before plus the edit and nothing else.
//
// THE COMMITTED FILE IS THE CANONICAL ENCODING, AND ITS COMMENTS ARE GONE. The
// preview encodes the edited file with [Encode], and the pull request route
// parses and encodes whatever text it is sent again, so a YAML comment or a
// layout of the base cannot survive either door. Edited.CommentsDropped and
// Edited.FormatRewritten say so on every changed preview. Keeping them needs a
// patch of the YAML nodes AND a route that commits validated text verbatim;
// until both exist, a structural edit is no exception. A key the parser does not
// know is never stripped to make an edit fit: it is a problem, and a base with
// any problem is base_invalid.
//
// THE PREVIEW NEVER WRITES. It is a pure function of the base text and the
// edits: no file, no network, no clock.

// The edit verbs.
const (
	OpSetStep    = "set_step"
	OpSetBudget  = "set_budget"
	OpAddStep    = "add_step"
	OpRemoveStep = "remove_step"
	OpMoveStep   = "move_step"
	OpSetList    = "set_list"
)

// The bounds of an edit. They are this preview's own: the file format has no
// cap on a prompt, a round count or a budget, and a value over one of these is
// refused here rather than written. Only a field an edit names is bounded; a
// larger value already in the file is carried through untouched.
const (
	EditTextMax   = 256 << 10 // the pull request route's own cap on the file
	EditPromptMax = 8 << 10   // bytes
	EditUntilMax  = 200       // characters
	EditRoundsMax = 99
	EditBudgetMax = 10000.0
	EditOpsMax    = 64
)

// Why an edit was refused. A caller reads the code, never the words.
const (
	EditBaseInvalid      = "base_invalid"
	EditBadOp            = "bad_op"
	EditLocked           = "locked"
	EditUnknownStep      = "unknown_step"
	EditBadValue         = "bad_value"
	EditTooLarge         = "too_large"
	EditNotRepresentable = "not_representable"
)

// EditError is a refusal. Op is the zero-based index of the edit that caused it,
// or -1 when the base or the whole request did.
type EditError struct {
	Code string
	Op   int
	Why  string
}

func (e *EditError) Error() string { return e.Why }

func refuseEdit(code string, op int, format string, args ...any) *EditError {
	return &EditError{Code: code, Op: op, Why: fmt.Sprintf(format, args...)}
}

// EditOp is one edit. Pointers tell a field that was not sent from one that was
// sent empty. set_step names a step and at least one of prompt, kind, when,
// effort, fanout, until and max; an empty kind, when, effort or fanout is the
// default, written as the key left out. Kind may only be chat (or empty), and
// only a chat step is edited at all ([lockedStep]); set_budget names only Value.
// Pipeline and After belong to the three structural verbs alone: Pipeline is the
// kind whose list changes, and After is the step the named one goes after, ""
// for first ([checkStructure]). List and Lines belong to set_list alone: List is
// rules, ask_first or never, and Lines is that list's every line, never nil (an
// empty list clears it) ([checkSetList]).
type EditOp struct {
	Op       string
	Name     string
	Pipeline string
	After    *string
	Prompt   *string
	Kind     *string
	When     *string
	Effort   *string
	Fanout   *string
	Until    *string
	Max      *int
	Value    *float64
	List     string
	Lines    []string
	// Adapt belongs to set_adapt, Fixed (with Name) to set_fixed, and MaxSize
	// and MaxCost to set_start (edit_settings.go).
	Adapt   *string
	Fixed   *bool
	MaxSize *string
	MaxCost *float64
	// Who belongs to set_approver, with Name.
	Who *string
	// To belongs to rename_step, with Name: the step's new name.
	To string
}

// StepView is one step as the file reads. Gate is a step that is a person
// (approve) and so has no prompt. Locked steps cannot be edited: approve, any
// step whose kind is check, gate or post, and any name the file lists under fixed. InFile is false for a step the file does not
// define itself, whose wording is a shipped default.
type StepView struct {
	Name string
	Gate bool
	// Who is who a person step waits on; "" the job's person.
	Who    string
	Prompt string
	Kind   string
	When   string
	Effort string
	Fanout string
	Until  string
	Max    int
	Locked bool
	InFile bool
}

// KindView is one pipeline: the steps of one kind of work, in order. InFile is
// false for a kind the file does not list, whose steps are the shipped default.
type KindView struct {
	Kind   string
	Steps  []string
	InFile bool
}

// Source is a parsed pipeline file, for a person to edit. BudgetInFile is false
// when the file does not state a default budget and Budget is the shipped one.
// Rules, AskFirst and Never are the file's OWN lines under rules,
// start.ask_first and start.never, never nil: this is the repository's file,
// and the factory's house rules ([WithHouseRules]) are never in it.
type Source struct {
	Budget       float64
	BudgetInFile bool
	Kinds        []KindView
	Steps        []StepView
	Fixed        []string
	Rules        []string
	AskFirst     []string
	Never        []string
	// Adapt is the file's adapt word ("" when it says none, which is adapt),
	// Labels its start labels, and MaxSize and MaxCost its start limits as the
	// parser reads them (the defaults when the file says none).
	Adapt   string
	Labels  []string
	MaxSize string
	MaxCost float64
}

// StepChange is one step an edit really changed. Fields names what changed, in
// the order prompt, kind, when, effort, fanout, until, max.
type StepChange struct {
	Name   string
	Fields []string
	Before StepView
	After  StepView
}

// BudgetChange is the default budget before and after an edit.
type BudgetChange struct {
	Before float64
	After  float64
}

// Edited is the answer to [ApplyEdits].
//
// Text is the exact canonical file when Changed, and the base text, byte for
// byte, when not: a preview that changes nothing offers nothing to write.
// Changed is a SEMANTIC comparison against the base as the parser reads it, so
// reformatting alone never makes it true. FormatRewritten says the committed
// bytes differ from the base beyond the edit (the encoder's own layout, keys the
// file left out written in), and CommentsDropped says a YAML comment in the base
// is not in the committed file. Both are false for an unchanged preview.
//
// Structural is true when any edit was add_step, remove_step or move_step, and
// only then are Order, Added, Counts and Gates set (non-nil, empty when nothing
// moved): a preview of field edits alone answers exactly what it always did.
// Order is every pipeline whose list changed, Added every step definition the
// edits wrote that the file did not hold, Counts every pipeline's length after
// the edit, and Gates every person whose unattended run-up changed.
//
// Listed is true when any edit was set_list, and only then is Lists set
// (non-nil, empty when no line changed): every list whose lines changed, in the
// order rules, ask_first, never.
type Edited struct {
	Text            string
	Changed         bool
	FormatRewritten bool
	CommentsDropped bool
	Steps           []StepChange
	Budget          *BudgetChange

	Structural bool
	Order      []OrderChange
	// Renamed is every rename_step, in edit order; set only with Structural.
	Renamed []Rename
	Added   []string
	Counts  []KindCount
	Gates   []GateChange

	Listed bool
	Lists  []ListChange

	// SettingsEdited is true when any edit was set_adapt, set_fixed or
	// set_start, and only then is Settings set (non-nil, empty when nothing
	// changed): every setting whose words changed (edit_settings.go).
	SettingsEdited bool
	Settings       []SettingChange
}

// Inspect reads a repository file for editing. ok is false, with the reasons,
// for a file that is blank, does not parse or has any problem: a file the pull
// request route would refuse is never offered for editing.
func Inspect(text string) (src Source, problems []string, ok bool) {
	f, err := parseBase(text)
	if err != nil {
		return Source{}, err.problems, false
	}
	return describe(f), nil, true
}

// InspectDefaults is the shipped default pipeline described the way [Inspect]
// describes a file: the File a repository with no file runs ([Defaults]), for
// a person to edit into the file that repository does not have yet. Nothing in
// it is the repository's own, so the budget, every pipeline and every step say
// they are not in the file, and the three lists are empty.
func InspectDefaults() Source {
	src := describe(Defaults())
	for i := range src.Steps {
		src.Steps[i].InFile = false
	}
	return src
}

type baseError struct{ problems []string }

// baseFile is the File an edit starts from: the shipped defaults for "", which
// is no file at all, and the parse of the text for anything else.
func baseFile(text string) (File, *baseError) {
	if text == "" {
		return Defaults(), nil
	}
	return parseBase(text)
}

func parseBase(text string) (File, *baseError) {
	switch {
	case strings.TrimSpace(text) == "":
		return File{}, &baseError{[]string{RepoFile + ": the file is empty"}}
	case !utf8.ValidString(text):
		return File{}, &baseError{[]string{RepoFile + ": the file is not valid UTF-8"}}
	case len(text) > EditTextMax:
		return File{}, &baseError{[]string{RepoFile + ": the file is too large to edit here"}}
	}
	f, err := Parse(text)
	if err != nil {
		return File{}, &baseError{[]string{RepoFile + ": " + err.Error()}}
	}
	if len(f.Problems) > 0 {
		out := make([]string, 0, len(f.Problems))
		for _, p := range f.Problems {
			out = append(out, p.Error())
			if len(out) == 20 {
				break
			}
		}
		return File{}, &baseError{out}
	}
	if out := runtimeDrops(f); len(out) > 0 {
		return File{}, &baseError{out}
	}
	return f, nil
}

// runtimeDrops names the steps the runtime would silently leave out of a kind.
// File.stages (which Parse and the runner keep as they are) skips a step whose
// name is not a stage word once its hyphens are removed (13 or more letters, say),
// and a second step whose normalized name an earlier one already took
// (lint-pass and lintpass). Parse accepts both, so an editor built on it would
// offer an edit of a step that never runs. THE EDITOR REFUSES SUCH A BASE; the
// existing parse and execution are untouched. It follows stages() step for step:
// a gate is keyed by its own name, any other step by its normalized word, in one
// map per kind. Kinds are walked in sorted order so the problems are stable.
func runtimeDrops(f File) []string {
	kinds := make([]string, 0, len(f.Pipeline))
	for kind := range f.Pipeline {
		kinds = append(kinds, string(kind))
	}
	sort.Strings(kinds)
	var out []string
	add := func(format string, args ...any) {
		if len(out) < 20 {
			out = append(out, RepoFile+": "+fmt.Sprintf(format, args...))
		}
	}
	for _, kind := range kinds {
		used := map[string]bool{}
		for _, raw := range f.Pipeline[factory.Kind(kind)] {
			if factory.ApproveWord(raw) {
				if used[raw] {
					add("%s pipeline lists %q twice, and the runner would run it once", kind, raw)
				}
				used[raw] = true
				continue
			}
			if st, ok := f.Steps[raw]; !ok || strings.TrimSpace(st.Prompt) == "" {
				continue // not a name problem; stages() skips it for its own reason
			}
			word, err := factory.StageWord(strings.ReplaceAll(raw, "-", ""))
			switch {
			case err != nil:
				add("%s pipeline step %q is not a name the runner can run (2 to 12 letters or digits once hyphens are removed), so it would be skipped", kind, raw)
			case used[word]:
				add("%s pipeline step %q becomes the same stage %q as an earlier step, so it would be skipped", kind, raw, word)
			default:
				used[word] = true
			}
		}
	}
	return out
}

// lockedStep says whether a step is refused to every edit. A check, gate or
// post step is locked too: its prompt is a shell command, a person or a verb,
// and a preview that names only "prompt" cannot show a person that. They stay
// readable and are written back as they were; until a preview carries the
// kind, they are changed in the file itself.
func lockedStep(f File, name string) bool {
	k := f.Steps[name].Kind
	return factory.ApproveWord(name) || listed(f.Fixed, name) || (k != "" && k != string(factory.StageCheck))
}

func stepView(f File, name string) StepView {
	if factory.ApproveWord(name) {
		return StepView{Name: name, Gate: true, Locked: true, InFile: true, Who: f.Steps[name].Who}
	}
	s := f.Steps[name]
	return StepView{Name: name, Prompt: s.Prompt, Kind: s.Kind, When: s.When, Effort: s.Effort, Fanout: s.Fanout, Who: s.Who,
		Until: s.Until, Max: s.Max, Locked: lockedStep(f, name),
		InFile: s.wrotePrompt || s.wroteUntil || s.wroteMax || s.Kind != "" || s.When != "" || s.Effort != "" || s.Fanout != ""}
}

func describe(f File) Source {
	src := Source{Budget: f.DefaultBudget, BudgetInFile: f.set.defaultBudget, Fixed: append([]string{}, f.Fixed...)}
	seen := map[string]bool{}
	for _, kind := range sortedKinds(f) {
		names := append([]string{}, f.Pipeline[factory.Kind(kind)]...)
		src.Kinds = append(src.Kinds, KindView{Kind: kind, Steps: names, InFile: f.set.pipeline[factory.Kind(kind)]})
		for _, name := range names {
			if !seen[name] {
				seen[name] = true
				src.Steps = append(src.Steps, stepView(f, name))
			}
		}
	}
	if src.Kinds == nil {
		src.Kinds = []KindView{}
	}
	if src.Steps == nil {
		src.Steps = []StepView{}
	}
	src.Rules = append([]string{}, f.Rules...)
	src.AskFirst = append([]string{}, f.Start.AskFirst...)
	src.Never = append([]string{}, f.Start.Never...)
	src.Adapt = string(f.Adapt)
	src.Labels = append([]string{}, f.Start.Labels...)
	src.MaxSize, src.MaxCost = normOr(f.Start.MaxSize, DefaultMaxSize), f.Start.MaxCost
	return src
}

// sortedKinds is the file's kinds in the order a person reads them: the three
// kinds the factory knows first, in its order, then any other kind the file
// names, alphabetically.
func sortedKinds(f File) []string {
	kinds := make([]string, 0, len(f.Pipeline))
	for kind := range f.Pipeline {
		kinds = append(kinds, string(kind))
	}
	rank := map[string]int{string(factory.KindIssue): 0, string(factory.KindPR): 1, string(factory.KindCI): 2}
	sort.Slice(kinds, func(i, j int) bool {
		ri, iok := rank[kinds[i]]
		rj, jok := rank[kinds[j]]
		switch {
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		}
		return kinds[i] < kinds[j]
	})
	return kinds
}

// ApplyEdits applies edits to the base file the way a person would have, and
// answers the file the pull request route will commit.
//
// Every edit is checked before any is applied, and one refusal refuses all. A
// step must be named exactly, must sit in one of the file's pipelines, and must
// not be locked. The result is parsed again and encoded again: if that does not
// reproduce the same bytes and the same values, the format cannot hold the edit
// and it is refused as not_representable rather than committed as something else.
//
// A base of "" is a repository with no file: the edits apply to [Defaults], and
// a changed answer is the whole file the pull request creates. Only "" means
// that; a base of blanks is a blank file, and is refused as it always was.
func ApplyEdits(base string, ops []EditOp) (Edited, error) {
	f, bad := baseFile(base)
	if bad != nil {
		return Edited{}, refuseEdit(EditBaseInvalid, -1, "%s", bad.problems[0])
	}
	if len(ops) == 0 {
		return Edited{}, refuseEdit(EditBadOp, -1, "there is no edit to apply")
	}
	if len(ops) > EditOpsMax {
		return Edited{}, refuseEdit(EditBadOp, -1, "at most %d edits at once", EditOpsMax)
	}
	canonical, err := Encode(f)
	if err != nil {
		return Edited{}, refuseEdit(EditBaseInvalid, -1, "the file could not be read back")
	}
	// Every edit is checked on its own before any is applied.
	checked := make([]EditOp, len(ops))
	for i, op := range ops {
		c, bad := CheckEdit(i, op)
		if bad != nil {
			return Edited{}, bad
		}
		checked[i] = c
	}
	if bad := renameAlone(checked); bad != nil {
		return Edited{}, bad
	}
	st := newEditState(f)
	for i, op := range checked {
		if bad := st.apply(i, op); bad != nil {
			return Edited{}, bad
		}
	}
	if bad := st.chatAfterProof(); bad != nil {
		return Edited{}, bad
	}
	return st.result(base, canonical)
}

// editState is one preview while its edits apply: the base as read, the file
// being edited (its own copies of the step map and every pipeline list), and
// what the edits have already named, so a second edit of the same thing is
// refused rather than silently winning.
type editState struct {
	base       File
	out        File
	inPipeline map[string]bool
	// named is every step a set_step edited, added every step an add_step
	// listed, and placed every pipeline row a structural edit touched.
	named  map[string]bool
	added  map[string]bool
	placed map[string]bool
	// placedAt is the edit that added or moved each pipeline row, setAt the
	// set_step that edited each step, and firstOp each pipeline's first
	// structural edit: what a refusal after the last edit names as its cause.
	placedAt   map[string]int
	setAt      map[string]int
	firstOp    map[factory.Kind]int
	newSteps   []string
	renames    []Rename
	budgetSeen bool
	structural bool
	// listSeen is every list a set_list replaced, and listed says one did.
	listSeen map[string]bool
	listed   bool
	// settingSeen is every setting (and each step's required mark) a settings
	// edit set, and settingsEdited says one did.
	settingSeen    map[string]bool
	settingsEdited bool
}

func newEditState(f File) *editState {
	st := &editState{base: f, out: f, inPipeline: map[string]bool{}, named: map[string]bool{}, added: map[string]bool{}, placed: map[string]bool{},
		placedAt: map[string]int{}, setAt: map[string]int{}, firstOp: map[factory.Kind]int{}, listSeen: map[string]bool{}, settingSeen: map[string]bool{}}
	st.out.Steps = make(map[string]Step, len(f.Steps))
	for name, s := range f.Steps {
		st.out.Steps[name] = s
	}
	st.out.Pipeline = make(map[factory.Kind][]string, len(f.Pipeline))
	for kind, names := range f.Pipeline {
		st.out.Pipeline[kind] = append([]string{}, names...)
		for _, name := range names {
			st.inPipeline[name] = true
		}
	}
	return st
}

func (st *editState) apply(i int, op EditOp) *EditError {
	switch op.Op {
	case OpSetBudget:
		if st.budgetSeen {
			return refuseEdit(EditBadOp, i, "the budget is set once")
		}
		st.budgetSeen = true
		st.out.DefaultBudget = *op.Value
		return nil
	case OpSetStep:
		return st.setStep(i, op)
	case OpSetList:
		return st.setList(i, op)
	case OpSetAdapt, OpSetFixed, OpSetStart, OpSetApprover:
		return st.settings(i, op)
	case OpRenameStep:
		return st.rename(i, op)
	}
	return st.structure(i, op)
}

func (st *editState) setStep(i int, op EditOp) *EditError {
	if st.named[op.Name] {
		return refuseEdit(EditBadOp, i, "a step is edited once")
	}
	st.named[op.Name] = true
	st.setAt[op.Name] = i
	if st.added[op.Name] {
		return refuseEdit(EditBadOp, i, "%s is added by this edit; its fields go on the add_step", op.Name)
	}
	if !st.inPipeline[op.Name] {
		return refuseEdit(EditUnknownStep, i, "%s is not a step in this pipeline", op.Name)
	}
	k := st.base.Steps[op.Name].Kind
	if k != "" && k != string(factory.StageCheck) && !factory.ApproveWord(op.Name) {
		return refuseEdit(EditLocked, i, "%s is a %s step and is changed only in the file itself", op.Name, k)
	}
	// A CHECK'S PROMPT IS ITS COMMAND, edited like any prompt; it keeps its kind.
	if k == string(factory.StageCheck) {
		if op.Kind != nil && *op.Kind != k {
			return refuseEdit(EditLocked, i, "%s is a check; a step's kind is changed only in the file itself", op.Name)
		}
		check := op
		check.Kind = &k
		if bad := checkCheckWords(i, check); bad != nil {
			return bad
		}
	}
	if lockedStep(st.base, op.Name) {
		return refuseEdit(EditLocked, i, "%s is locked and cannot be edited", op.Name)
	}
	s := st.out.Steps[op.Name]
	if op.Prompt != nil {
		s.Prompt, s.wrotePrompt = *op.Prompt, true
	}
	if op.Kind != nil {
		s.Kind = *op.Kind
	}
	setStepWords(&s, op)
	st.out.Steps[op.Name] = s
	return nil
}

// setStepWords writes the when, effort, fanout, until and max an edit names.
func setStepWords(s *Step, op EditOp) {
	if op.When != nil {
		s.When = *op.When
	}
	if op.Effort != nil {
		s.Effort = *op.Effort
	}
	if op.Fanout != nil {
		s.Fanout = *op.Fanout
	}
	if op.Until != nil {
		s.Until, s.wroteUntil = *op.Until, *op.Until != ""
	}
	if op.Max != nil {
		s.Max, s.wroteMax = *op.Max, true
	}
}

// result encodes the edited file and answers what changed. An unchanged file
// answers the base byte for byte.
func (st *editState) result(base string, canonical []byte) (Edited, error) {
	out := st.out
	text, err := Encode(out)
	if err != nil {
		return Edited{}, refuseEdit(EditNotRepresentable, -1, "the file could not be written")
	}
	if len(text) > EditTextMax {
		return Edited{}, refuseEdit(EditTooLarge, -1, "the file would be larger than %d bytes", EditTextMax)
	}
	res := Edited{Text: base}
	if st.structural {
		st.structureFacts(&res)
	}
	if st.listed {
		st.listFacts(&res)
	}
	if st.settingsEdited {
		st.settingsFacts(&res)
	}
	if bytes.Equal(text, canonical) {
		return res, nil
	}
	if why := notRoundTrip(text, out); why != "" {
		return Edited{}, refuseEdit(EditNotRepresentable, -1, "%s", why)
	}
	// A STEP THE RUNNER WOULD SKIP IS NEVER PREVIEWED. The edits refuse such a
	// name on their own; this is the same check the base passed, run again.
	if drops := runtimeDrops(out); len(drops) > 0 {
		return Edited{}, refuseEdit(EditNotRepresentable, -1, "%s", drops[0])
	}
	res.Text, res.Changed = string(text), true
	// No file has no layout to rewrite and no comment to drop: the whole file
	// is new, and nothing of the repository's own is lost on the way.
	res.FormatRewritten = base != "" && base != string(canonical)
	res.CommentsDropped = hasComments(base)
	res.Steps = stepChanges(describe(st.base), out, st.renamedTo())
	if before, after := st.base.DefaultBudget, out.DefaultBudget; before != after {
		res.Budget = &BudgetChange{Before: before, After: after}
	}
	return res, nil
}

// stepChanges is every step of the base whose words an edit changed, in the
// base's order, with the fields that differ. A renamed step is compared under
// its new name, so a rename alone changes no words.
func stepChanges(before Source, out File, renamed map[string]string) []StepChange {
	var changes []StepChange
	for _, b := range before.Steps {
		name := b.Name
		if to, ok := renamed[name]; ok {
			name = to
		}
		a := stepView(out, name)
		a.Name = b.Name
		var fields []string
		for _, c := range []struct {
			name string
			diff bool
		}{
			{"prompt", a.Prompt != b.Prompt}, {"kind", a.Kind != b.Kind}, {"when", a.When != b.When},
			{"effort", a.Effort != b.Effort}, {"fanout", a.Fanout != b.Fanout}, {"until", a.Until != b.Until}, {"max", a.Max != b.Max},
		} {
			if c.diff {
				fields = append(fields, c.name)
			}
		}
		if len(fields) > 0 {
			changes = append(changes, StepChange{Name: b.Name, Fields: fields, Before: b, After: a})
		}
	}
	return changes
}

// CheckEdit refuses an edit that is wrong whatever the file holds: an unknown
// verb, a field the verb does not take, a name that cannot be a step, a value
// out of bounds. It answers the edit with its text trimmed, as it will be
// written. Whether the step exists, or is locked, depends on the file and is
// [ApplyEdits]'s to say. i is the edit's position, for the refusal.
func CheckEdit(i int, op EditOp) (EditOp, *EditError) {
	if op.Op != OpSetList && (op.List != "" || op.Lines != nil) {
		return op, refuseEdit(EditBadOp, i, "%s takes no list and no lines", op.Op)
	}
	if op.Op != OpRenameStep && op.To != "" {
		return op, refuseEdit(EditBadOp, i, "only rename_step takes to")
	}
	if !settingsOp(op.Op) && settingFieldsNamed(op) {
		return op, refuseEdit(EditBadOp, i, "%s takes no adapt, fixed, max_size or max_cost", op.Op)
	}
	switch op.Op {
	case OpSetAdapt, OpSetFixed, OpSetStart, OpSetApprover:
		return checkSettings(i, op)
	case OpSetList:
		return checkSetList(i, op)
	case OpRenameStep:
		return checkRename(i, op)
	case OpAddStep, OpRemoveStep, OpMoveStep:
		return checkStructure(i, op)
	case OpSetBudget, OpSetStep:
		if op.Pipeline != "" || op.After != nil {
			return op, refuseEdit(EditBadOp, i, "%s takes no pipeline and no position", op.Op)
		}
	default:
		return op, refuseEdit(EditBadOp, i, "that edit is not one of set_step, set_budget, add_step, remove_step, move_step, set_list, set_adapt, set_fixed, set_start, set_approver and rename_step")
	}
	if op.Op == OpSetBudget {
		return checkSetBudget(i, op)
	}
	return checkSetStep(i, op)
}

func checkSetStep(i int, op EditOp) (EditOp, *EditError) {
	if op.Value != nil {
		return op, refuseEdit(EditBadOp, i, "set_step does not take a value")
	}
	if op.Kind == nil && !stepFieldsNamed(op) {
		return op, refuseEdit(EditBadOp, i, "set_step needs a prompt, kind, when, effort, fanout, until or max")
	}
	if !stepNamed(op.Name) {
		return op, refuseEdit(EditUnknownStep, i, "that is not a step name")
	}
	if op.Kind != nil {
		// A KIND IS ONLY EVER SET TO CHAT HERE. A chat made a check would
		// run its prompt in a shell, made a post would act on the forge, and
		// made a gate would stop the run; none of that shows in a preview yet.
		k, ok := kindWord(*op.Kind)
		if !ok {
			return op, refuseEdit(EditBadValue, i, "kind is chat, check, gate or post")
		}
		if k != "" {
			return op, refuseEdit(EditBadValue, i, "a step becomes a %s only in the file itself; here a step's kind is chat", k)
		}
		op.Kind = &k
	}
	return checkStepFields(i, op)
}

func checkSetBudget(i int, op EditOp) (EditOp, *EditError) {
	if op.Name != "" || op.Kind != nil || stepFieldsNamed(op) {
		return op, refuseEdit(EditBadOp, i, "set_budget takes only a value")
	}
	if op.Value == nil {
		return op, refuseEdit(EditBadValue, i, "set_budget needs a value")
	}
	v := *op.Value
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > EditBudgetMax {
		return op, refuseEdit(EditBadValue, i, "the budget is a number of dollars above zero and at most %g", EditBudgetMax)
	}
	return op, nil
}

// stepNamed says whether a name is spelled as the file spells a step: exactly,
// lowercase, 2 to 32 letters, digits and inner hyphens.
func stepNamed(name string) bool {
	return name != "" && name == stepToken(name) && validStepName(name)
}

// checkStepFields bounds and trims the prompt, when, effort, fanout, until and
// max an edit names; it is shared by set_step and add_step.
func checkStepFields(i int, op EditOp) (EditOp, *EditError) {
	if op.Prompt != nil {
		p, why := cleanPrompt(*op.Prompt)
		if why != "" {
			return op, refuseEdit(EditBadValue, i, "%s", why)
		}
		op.Prompt = &p
	}
	for _, w := range []struct {
		got   **string
		words []string
		what  string
	}{
		{&op.When, factory.WhenWords, "when is one of " + strings.Join(factory.WhenWords, ", ")},
		{&op.Effort, factory.EffortWords, "effort is cheap or strong, or empty for the default"},
		{&op.Fanout, StepFanouts, "fanout is one, per-file, per-finding or per-claim"},
	} {
		if *w.got == nil {
			continue
		}
		word, ok := stepWord(**w.got, w.words)
		if !ok {
			return op, refuseEdit(EditBadValue, i, "%s", w.what)
		}
		*w.got = &word
	}
	if op.Until != nil {
		u, why := cleanUntil(*op.Until)
		if why != "" {
			return op, refuseEdit(EditBadValue, i, "%s", why)
		}
		op.Until = &u
	}
	if op.Max != nil && (*op.Max < 1 || *op.Max > EditRoundsMax) {
		return op, refuseEdit(EditBadValue, i, "max is a number of rounds from 1 to %d", EditRoundsMax)
	}
	return op, nil
}

// notRoundTrip is the reason the committed bytes would not read back as the
// edited file, or "". The route parses and encodes whatever it is sent, so the
// preview must be a fixed point of that or it is not what gets committed.
func notRoundTrip(text []byte, want File) string {
	back, err := Parse(string(text))
	if err != nil || len(back.Problems) > 0 {
		return "the edited file does not read back"
	}
	again, err := Encode(back)
	if err != nil || !bytes.Equal(again, text) {
		return "the edited file does not read back the same"
	}
	if back.DefaultBudget != want.DefaultBudget {
		return "the budget does not survive the file format"
	}
	if why := pipelinesDiffer(back, want); why != "" {
		return why
	}
	if why := listsDiffer(back, want); why != "" {
		return why
	}
	if why := settingsDiffer(back, want); why != "" {
		return why
	}
	return stepsDiffer(back, want)
}

// stepsDiffer is why a step did not read back with the words it was given, or "".
func stepsDiffer(back, want File) string {
	for name, s := range want.Steps {
		if factory.ApproveWord(name) {
			continue
		}
		got := back.Steps[name]
		if got.Prompt != s.Prompt || got.Kind != s.Kind || got.When != s.When || got.Effort != s.Effort ||
			got.Fanout != s.Fanout || got.Until != s.Until || got.Max != s.Max {
			return fmt.Sprintf("the file format cannot hold that change to %s", name)
		}
	}
	return ""
}

// pipelinesDiffer is why the pipelines did not read back as the same lists in
// the same order, or "": a kind gained, lost or reordered on the way is not the
// edit that was previewed.
func pipelinesDiffer(back, want File) string {
	if len(back.Pipeline) != len(want.Pipeline) {
		return "the pipelines do not survive the file format"
	}
	for kind, names := range want.Pipeline {
		if got, ok := back.Pipeline[kind]; !ok || !slices.Equal(got, names) {
			return fmt.Sprintf("the file format cannot hold that order of the %s pipeline", kind)
		}
	}
	return ""
}

func cleanPrompt(s string) (string, string) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", "a prompt cannot be empty"
	case len(s) > EditPromptMax:
		return "", fmt.Sprintf("a prompt is at most %d bytes", EditPromptMax)
	case !utf8.ValidString(s):
		return "", "a prompt must be valid UTF-8"
	}
	for _, r := range s {
		if (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 || r == 0xfeff {
			return "", "a prompt cannot hold control characters"
		}
	}
	return s, ""
}

func cleanUntil(s string) (string, string) {
	s = strings.TrimSpace(s)
	switch {
	case utf8.RuneCountInString(s) > EditUntilMax:
		return "", fmt.Sprintf("until is one line of at most %d characters", EditUntilMax)
	case !utf8.ValidString(s):
		return "", "until must be valid UTF-8"
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 || r == 0xfeff {
			return "", "until is one line"
		}
	}
	return s, ""
}

// hasComments is true when the YAML text holds a comment anywhere. The encoder
// writes none, so a base with one loses it.
func hasComments(text string) bool {
	var doc yaml.Node
	if err := yaml.NewDecoder(strings.NewReader(strings.TrimPrefix(text, "\uFEFF"))).Decode(&doc); err != nil {
		return false
	}
	var walk func(n *yaml.Node) bool
	walk = func(n *yaml.Node) bool {
		if n == nil {
			return false
		}
		if n.HeadComment != "" || n.LineComment != "" || n.FootComment != "" {
			return true
		}
		for _, c := range n.Content {
			if walk(c) {
				return true
			}
		}
		return false
	}
	return walk(&doc)
}
