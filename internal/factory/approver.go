package factory

import (
	"regexp"
	"strings"
)

// ── WHO AN APPROVAL WAITS ON ─────────────────────────────────────────────────
//
// An approve step (or any gate) waits on the job's person unless it names
// someone else: Stage.Who is "" for the job's person, "@handle" for one named
// person, or ApproverOperator for any of the factory's operators. The named
// approver answers the approval INSTEAD of the job's person, so a team can say
// "proof waits on the release owner". A solo factory has one person, who
// answers everything. Neither Auto nor the manager passes an approval that
// names someone other than the job's person: that person was asked by name.

// ApproverOperator is the role word for any of the factory's operators.
const ApproverOperator = "operator"

var approverHandle = regexp.MustCompile(`^@[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?$`)

// ApproverWord reads one approver word as it is kept: "" (the job's person),
// "@handle" lowercased, or operator. ok is false for anything else.
func ApproverWord(s string) (string, bool) {
	w := strings.ToLower(strings.TrimSpace(s))
	switch {
	case w == "" || w == ApproverOperator:
		return w, true
	case approverHandle.MatchString(w):
		return w, true
	}
	return "", false
}

// WaitingApprover is who the approval an item waits at is for, by its stage's
// Who: "" when it waits on the job's person, or is not waiting at an approval.
// A landed job's proof is approved by whom its last approve step names, since
// an approve step with nothing after it is the landing.
func WaitingApprover(it Item) string {
	if it.State == StateLanded {
		for i := len(it.Stages) - 1; i >= 0; i-- {
			if it.Stages[i].On {
				if IsApprove(it.Stages[i]) {
					return it.Stages[i].Who
				}
				return ""
			}
		}
		return ""
	}
	if it.QKind != QKindApprove || it.Stream == nil {
		return ""
	}
	cur := it.Stream.Cur
	if cur < 0 || cur >= len(it.Stream.Phases) {
		return ""
	}
	name := it.Stream.Phases[cur].Name
	if i := StageIndex(it.Stages, name); i >= 0 && IsApprove(it.Stages[i]) {
		return it.Stages[i].Who
	}
	return ""
}

// NamesOtherApprover says whether the approval an item waits at is for
// someone other than the job's person: a named person who is not them, or the
// operators. Auto and the manager leave such an approval to that person.
func NamesOtherApprover(it Item) bool {
	who := WaitingApprover(it)
	return who != "" && NormalizeHandle(who) != it.Person()
}

// MayApprove says whether a caller may answer the approval an item waits at:
// the named approver when one is named (any operator for the role), else the
// job's person (anyone while it has none).
func MayApprove(it Item, handle string, operator bool) bool {
	switch who := WaitingApprover(it); {
	case who == ApproverOperator:
		return operator
	case who != "":
		return NormalizeHandle(handle) == NormalizeHandle(who)
	}
	return it.Person() == "" || NormalizeHandle(handle) == it.Person()
}
