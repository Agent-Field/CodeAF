package factory

import (
	"strconv"
	"strings"
	"time"
)

// ── WHO MAY PASS AN APPROVE STEP ─────────────────────────────────────────────
//
// An approve step says who may pass it, in one field (Stage.Pass, `pass:` in
// .codeaf/factory.yaml), spelled one of three ways:
//
//   - person: only a person. Auto and the manager never pass it; it always
//     waits in Needs you. A repository-fixed approve step is this, whatever
//     its field says.
//   - manager: a person or the manager. In Auto the manager passes it, as Auto
//     passed routine approvals before the field existed.
//   - manager when <condition>: the manager passes it in Auto only when it
//     judges the plain-words condition true of the work it approves; else a
//     person is asked, with the manager's reason.
//
// An approve step that says nothing (every pipeline written before the field)
// reads as manager, so nothing that ran yesterday changes. A NEWLY ADDED
// APPROVE STEP SAYS person: a pipeline edit that adds one without a word
// writes `pass: person` ([PassForNewApprove]).

// The three spellings of [Stage.Pass].
const (
	PassPerson      = "person"
	PassManager     = "manager"
	PassManagerWhen = "manager when"
)

// PassWhenMost is how long a manager-when condition may be, in runes: one
// plain sentence a person writes, never an essay.
const PassWhenMost = 300

// PassForNewApprove is what a newly added approve step says when the edit
// that adds it says nothing: only a person passes it.
const PassForNewApprove = PassPerson

// PassWord reads one pass word as it is kept: "" (unset), person, manager, or
// `manager when <condition>` with the condition trimmed and folded to one
// line. ok is false for anything else, and for a manager-when with no
// condition or a condition longer than [PassWhenMost].
func PassWord(s string) (string, bool) {
	w := strings.Join(strings.Fields(s), " ")
	low := strings.ToLower(w)
	switch {
	case w == "":
		return "", true
	case low == PassPerson || low == "a person" || low == "only a person":
		return PassPerson, true
	case low == PassManager || low == "a person or the manager" || low == "the manager":
		return PassManager, true
	case strings.HasPrefix(low, PassManagerWhen+" "):
		cond := strings.TrimSpace(w[len(PassManagerWhen):])
		if cond == "" || len([]rune(cond)) > PassWhenMost {
			return "", false
		}
		return PassManagerWhen + " " + cond, true
	}
	return "", false
}

// PassOf is who may pass the approve step st, as its kind (person, manager or
// manager when) and, for manager when, the condition. A fixed step is person;
// a step that says nothing is manager.
func PassOf(st Stage) (kind, cond string) {
	if st.Fixed {
		return PassPerson, ""
	}
	w, ok := PassWord(st.Pass)
	switch {
	case !ok || w == "" || w == PassManager:
		// An unreadable word never widens who may pass: a hand-edited record
		// that says something else is a person's.
		if !ok {
			return PassPerson, ""
		}
		return PassManager, ""
	case w == PassPerson:
		return PassPerson, ""
	}
	return PassManagerWhen, strings.TrimSpace(w[len(PassManagerWhen):])
}

// PersonOnly says only a person passes the approve step st: fixed by the
// repository, or `pass: person`.
func PersonOnly(st Stage) bool {
	k, _ := PassOf(st)
	return k == PassPerson
}

// Passed is who passed an approve step, kept on its phase once it is done:
// the manager (in Auto) or a person, by handle when known, and the manager's
// one sentence when it judged a condition.
type Passed struct {
	By     string `json:"By"` // PassedByManager or PassedByPerson
	Handle string `json:",omitempty"`
	Why    string `json:",omitempty"`
}

// Who passed an approve step ([Passed.By]).
const (
	PassedByManager = "manager"
	PassedByPerson  = "person"
)

// PassJudgement is the manager's reading of a manager-when condition for one
// approval: Key names the approval it was for (the step, its round and the
// head it read), so a judgement never outlives the work it judged.
type PassJudgement struct {
	Key   string
	Yes   bool
	Why   string
	Model string `json:",omitempty"`
	At    time.Time
}

// WaitingGateStage is the approve step the item's approval waits at, and its
// stage: the current step while it waits on an approve question, or, on a
// landed job, the approve step it landed at, else its last switched-on step
// when that is an approve step. ok is false when no approve step waits.
func WaitingGateStage(it Item) (Stage, int, bool) {
	if it.Stream == nil {
		return Stage{}, -1, false
	}
	if it.State == StateLanded {
		if g := LandingGate(it); g >= 0 {
			if i := StageIndex(it.Stages, it.Stream.Phases[g].Name); i >= 0 {
				return it.Stages[i], g, true
			}
		}
		for i := len(it.Stages) - 1; i >= 0; i-- {
			if !it.Stages[i].On {
				continue
			}
			if IsApprove(it.Stages[i]) {
				return it.Stages[i], -1, true
			}
			break
		}
		return Stage{}, -1, false
	}
	if it.QKind != QKindApprove {
		return Stage{}, -1, false
	}
	cur := it.Stream.Cur
	if cur < 0 || cur >= len(it.Stream.Phases) {
		return Stage{}, -1, false
	}
	name := it.Stream.Phases[cur].Name
	if i := StageIndex(it.Stages, name); i >= 0 && IsApprove(it.Stages[i]) {
		return it.Stages[i], cur, true
	}
	return Stage{}, -1, false
}

// PassJudgeKey is the key a manager-when judgement of the item's waiting
// approval is kept under: the step, the round of the work before it, the head
// and the condition, so new work or a reworded condition is judged afresh.
func PassJudgeKey(it Item, st Stage, cond string) string {
	round := 0
	if it.Stream != nil {
		if _, g, ok := WaitingGateStage(it); ok && g >= 0 {
			if prev := PreviousWork(it.Stream.Phases, g); prev >= 0 {
				round = it.Stream.Phases[prev].Round
			}
		}
	}
	return strings.Join([]string{st.Name, strconv.Itoa(round), it.HeadCommit, cond}, "\x1f")
}

// PassJudged is the manager's standing judgement of the item's waiting
// manager-when approval, when there is one for exactly this approval.
func PassJudged(it Item) (PassJudgement, bool) {
	st, _, ok := WaitingGateStage(it)
	if !ok || it.PassJudge == nil {
		return PassJudgement{}, false
	}
	kind, cond := PassOf(st)
	if kind != PassManagerWhen || it.PassJudge.Key != PassJudgeKey(it, st, cond) {
		return PassJudgement{}, false
	}
	return *it.PassJudge, true
}

// PassBlockReason is why Auto leaves the item's waiting approval to a person
// for its step's pass word, or "": only a person may pass it, or the manager
// judged its condition false. A manager-when approval not judged yet has no
// reason: it is in flight, as a routine approval is.
func PassBlockReason(it Item) string {
	st, _, ok := WaitingGateStage(it)
	if !ok {
		return ""
	}
	kind, cond := PassOf(st)
	switch kind {
	case PassPerson:
		if st.Fixed {
			return ""
		}
		return "Only a person can pass this approval. Review it, then press Approve."
	case PassManagerWhen:
		if j, ok := PassJudged(it); ok && !j.Yes {
			why := strings.TrimSuffix(strings.TrimSpace(j.Why), ".")
			if why == "" {
				why = "it could not tell"
			}
			return "The manager passes this only when " + strings.TrimSuffix(cond, ".") + ", and judged it does not hold: " + why + ". Review it, then press Approve."
		}
	}
	return ""
}

// ManagerMayPass says the manager may pass the item's waiting approval now in
// Auto, by its step's pass word: manager, or manager when with a yes judged
// for exactly this approval.
func ManagerMayPass(it Item) bool {
	st, _, ok := WaitingGateStage(it)
	if !ok {
		return true
	}
	switch kind, _ := PassOf(st); kind {
	case PassPerson:
		return false
	case PassManagerWhen:
		j, ok := PassJudged(it)
		return ok && j.Yes
	}
	return true
}

// NeedsPassJudgement says the item's waiting approval is a manager-when one
// the manager has not judged yet, with the condition it asks.
func NeedsPassJudgement(it Item) (string, bool) {
	st, _, ok := WaitingGateStage(it)
	if !ok {
		return "", false
	}
	kind, cond := PassOf(st)
	if kind != PassManagerWhen {
		return "", false
	}
	if _, judged := PassJudged(it); judged {
		return "", false
	}
	return cond, true
}

// StampPassed records who passed phase g, from the item's pending pass
// (Item.Passing, set by the door that answered) or else a person, and takes
// the pending pass down. It is called by every write that passes an approve
// step.
func StampPassed(it *Item, g int) {
	if it.Stream == nil || g < 0 || g >= len(it.Stream.Phases) {
		return
	}
	p := Passed{By: PassedByPerson}
	if it.Passing != nil {
		p = *it.Passing
	}
	it.Stream.Phases[g].Passed = &p
	it.Passing = nil
}

// PassEvidence is what the manager reads when it judges a manager-when
// condition: the item the judge is handed, its body the work the approval
// approves. For a plan, the plan's own words and the files it names; for a
// landed job, the files it changed with their line counts and its claims;
// for both, the paths the repository protects. The judge reads the body as
// untrusted (a model wrote the plan), and a yes is only ever its reading.
func PassEvidence(it Item) Item {
	st, g, _ := WaitingGateStage(it)
	var b strings.Builder
	b.WriteString("This is the work an approval waits on. The question is about this work.\n")
	if rules := it.StartRules; rules != nil {
		var guarded []string
		guarded = append(guarded, rules.Never...)
		guarded = append(guarded, rules.AskFirst...)
		if len(guarded) > 0 {
			b.WriteString("protected paths and ask-first rules of the repository: " + strings.Join(guarded, "; ") + "\n")
		} else {
			b.WriteString("protected paths of the repository: none\n")
		}
	}
	if it.State == StateLanded {
		if c := it.Change; c != nil && len(c.Files) > 0 {
			b.WriteString("files changed (" + strconv.Itoa(len(c.Files)) + "):\n")
			for _, f := range c.Files {
				b.WriteString("- " + f.Path + " +" + strconv.Itoa(f.Added) + " -" + strconv.Itoa(f.Removed) + "\n")
			}
		}
		for _, cl := range it.Proof {
			mark := "shown"
			if !cl.OK {
				mark = "not shown"
			}
			b.WriteString("claim (" + mark + "): " + cl.Text + "\n")
		}
	} else if it.Stream != nil && g >= 0 {
		if files := PlanFilesOf(it); files != nil {
			b.WriteString("files the plan names (" + strconv.Itoa(len(files)) + "): " + strings.Join(files, ", ") + "\n")
		}
		if prev := PreviousWork(it.Stream.Phases, g); prev >= 0 {
			ph := it.Stream.Phases[prev]
			b.WriteString("the " + ph.Name + " step's report:\n")
			if ph.Result != nil {
				b.WriteString(strings.TrimSpace(ph.Result.Output) + "\n")
			}
		}
	}
	return Item{ID: it.ID, Repo: it.Repo, Kind: it.Kind, Tier: TierOwner, Title: "approval: " + st.Name + " on " + it.Title, Body: b.String()}
}
