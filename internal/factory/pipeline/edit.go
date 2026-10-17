package pipeline

import (
	"bytes"
	"fmt"
	"math"
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
// route will commit. The only edits are the two the file format can hold today:
// a step's prompt, kind, when, effort, fanout, until and max, and the default budget. Everything else in
// the file (start, size, fixed, rules, the pipelines and their order, every
// other step) rides through the same parser and the same encoder the route
// uses, so what is previewed is what is committed.
//
// THE PREVIEW NEVER WRITES. It is a pure function of the base text and the
// edits: no file, no network, no clock.

// The edit verbs.
const (
	OpSetStep   = "set_step"
	OpSetBudget = "set_budget"
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
type EditOp struct {
	Op     string
	Name   string
	Prompt *string
	Kind   *string
	When   *string
	Effort *string
	Fanout *string
	Until  *string
	Max    *int
	Value  *float64
}

// StepView is one step as the file reads. Gate is a step that is a person
// (approve) and so has no prompt. Locked steps cannot be edited: approve, any
// step whose kind is check, gate or post, and any name the file lists under fixed. InFile is false for a step the file does not
// define itself, whose wording is a shipped default.
type StepView struct {
	Name   string
	Gate   bool
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
type Source struct {
	Budget       float64
	BudgetInFile bool
	Kinds        []KindView
	Steps        []StepView
	Fixed        []string
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
type Edited struct {
	Text            string
	Changed         bool
	FormatRewritten bool
	CommentsDropped bool
	Steps           []StepChange
	Budget          *BudgetChange
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

type baseError struct{ problems []string }

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
	return factory.ApproveWord(name) || listed(f.Fixed, name) || f.Steps[name].Kind != ""
}

func stepView(f File, name string) StepView {
	if factory.ApproveWord(name) {
		return StepView{Name: name, Gate: true, Locked: true, InFile: true}
	}
	s := f.Steps[name]
	return StepView{Name: name, Prompt: s.Prompt, Kind: s.Kind, When: s.When, Effort: s.Effort, Fanout: s.Fanout,
		Until: s.Until, Max: s.Max, Locked: lockedStep(f, name),
		InFile: s.wrotePrompt || s.wroteUntil || s.wroteMax || s.Kind != "" || s.When != "" || s.Effort != "" || s.Fanout != ""}
}

func describe(f File) Source {
	src := Source{Budget: f.DefaultBudget, BudgetInFile: f.set.defaultBudget, Fixed: append([]string{}, f.Fixed...)}
	kinds := make([]string, 0, len(f.Pipeline))
	for kind := range f.Pipeline {
		kinds = append(kinds, string(kind))
	}
	// The three kinds the factory knows come first, in its order; any other
	// kind a file names follows alphabetically.
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
	seen := map[string]bool{}
	for _, kind := range kinds {
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
	return src
}

// ApplyEdits applies edits to the base file the way a person would have, and
// answers the file the pull request route will commit.
//
// Every edit is checked before any is applied, and one refusal refuses all. A
// step must be named exactly, must sit in one of the file's pipelines, and must
// not be locked. The result is parsed again and encoded again: if that does not
// reproduce the same bytes and the same values, the format cannot hold the edit
// and it is refused as not_representable rather than committed as something else.
func ApplyEdits(base string, ops []EditOp) (Edited, error) {
	f, bad := parseBase(base)
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
	inPipeline := map[string]bool{}
	for _, names := range f.Pipeline {
		for _, name := range names {
			inPipeline[name] = true
		}
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

	before := describe(f)
	out := f
	out.Steps = make(map[string]Step, len(f.Steps))
	for name, s := range f.Steps {
		out.Steps[name] = s
	}
	named := map[string]bool{}
	budgetSeen := false
	for i, op := range checked {
		if op.Op == OpSetBudget {
			if budgetSeen {
				return Edited{}, refuseEdit(EditBadOp, i, "the budget is set once")
			}
			budgetSeen = true
			out.DefaultBudget = *op.Value
			continue
		}
		if named[op.Name] {
			return Edited{}, refuseEdit(EditBadOp, i, "a step is edited once")
		}
		named[op.Name] = true
		if !inPipeline[op.Name] {
			return Edited{}, refuseEdit(EditUnknownStep, i, "%s is not a step in this pipeline", op.Name)
		}
		if k := f.Steps[op.Name].Kind; k != "" && !factory.ApproveWord(op.Name) {
			return Edited{}, refuseEdit(EditLocked, i, "%s is a %s step and is changed only in the file itself", op.Name, k)
		}
		if lockedStep(f, op.Name) {
			return Edited{}, refuseEdit(EditLocked, i, "%s is locked and cannot be edited", op.Name)
		}
		s := out.Steps[op.Name]
		if op.Prompt != nil {
			s.Prompt, s.wrotePrompt = *op.Prompt, true
		}
		if op.Kind != nil {
			s.Kind = *op.Kind
		}
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
		out.Steps[op.Name] = s
	}

	text, err := Encode(out)
	if err != nil {
		return Edited{}, refuseEdit(EditNotRepresentable, -1, "the file could not be written")
	}
	if len(text) > EditTextMax {
		return Edited{}, refuseEdit(EditTooLarge, -1, "the file would be larger than %d bytes", EditTextMax)
	}
	res := Edited{Text: base}
	if bytes.Equal(text, canonical) {
		return res, nil
	}
	if why := notRoundTrip(text, out); why != "" {
		return Edited{}, refuseEdit(EditNotRepresentable, -1, "%s", why)
	}
	after := describe(out)
	res.Text, res.Changed = string(text), true
	res.FormatRewritten = base != string(canonical)
	res.CommentsDropped = hasComments(base)
	for _, b := range before.Steps {
		a := stepView(out, b.Name)
		var fields []string
		if a.Prompt != b.Prompt {
			fields = append(fields, "prompt")
		}
		if a.Kind != b.Kind {
			fields = append(fields, "kind")
		}
		if a.When != b.When {
			fields = append(fields, "when")
		}
		if a.Effort != b.Effort {
			fields = append(fields, "effort")
		}
		if a.Fanout != b.Fanout {
			fields = append(fields, "fanout")
		}
		if a.Until != b.Until {
			fields = append(fields, "until")
		}
		if a.Max != b.Max {
			fields = append(fields, "max")
		}
		if len(fields) > 0 {
			res.Steps = append(res.Steps, StepChange{Name: b.Name, Fields: fields, Before: b, After: a})
		}
	}
	if before.Budget != after.Budget {
		res.Budget = &BudgetChange{Before: before.Budget, After: after.Budget}
	}
	return res, nil
}

// CheckEdit refuses an edit that is wrong whatever the file holds: an unknown
// verb, a field the verb does not take, a name that cannot be a step, a value
// out of bounds. It answers the edit with its text trimmed, as it will be
// written. Whether the step exists, or is locked, depends on the file and is
// [ApplyEdits]'s to say. i is the edit's position, for the refusal.
func CheckEdit(i int, op EditOp) (EditOp, *EditError) {
	switch op.Op {
	case OpSetBudget:
		if op.Name != "" || op.Prompt != nil || op.Until != nil || op.Max != nil || op.Kind != nil || op.When != nil || op.Effort != nil || op.Fanout != nil {
			return op, refuseEdit(EditBadOp, i, "set_budget takes only a value")
		}
		if op.Value == nil {
			return op, refuseEdit(EditBadValue, i, "set_budget needs a value")
		}
		v := *op.Value
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > EditBudgetMax {
			return op, refuseEdit(EditBadValue, i, "the budget is a number of dollars above zero and at most %g", EditBudgetMax)
		}
	case OpSetStep:
		if op.Value != nil {
			return op, refuseEdit(EditBadOp, i, "set_step does not take a value")
		}
		if op.Prompt == nil && op.Until == nil && op.Max == nil && op.Kind == nil && op.When == nil && op.Effort == nil && op.Fanout == nil {
			return op, refuseEdit(EditBadOp, i, "set_step needs a prompt, kind, when, effort, fanout, until or max")
		}
		if op.Name == "" || op.Name != stepToken(op.Name) || !validStepName(op.Name) {
			return op, refuseEdit(EditUnknownStep, i, "that is not a step name")
		}
		if op.Prompt != nil {
			p, why := cleanPrompt(*op.Prompt)
			if why != "" {
				return op, refuseEdit(EditBadValue, i, "%s", why)
			}
			op.Prompt = &p
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
	default:
		return op, refuseEdit(EditBadOp, i, "that edit is not one of set_step and set_budget")
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
