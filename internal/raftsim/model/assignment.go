package model

import (
	"github.com/anishathalye/porcupine"
	"reflect"
	"time"
)

// AssignmentState is an independent small run/lease model. Literal protocol
// values here intentionally do not import the production rules or constants.
type AssignmentState struct {
	Holder      string
	Fence       uint64
	Status      string
	Revision    uint64
	Incarnation uint64
	Available   bool
	Revoked     bool
	ParentFence uint64
}
type AssignmentCall struct{ Kind string }
type AssignmentOperation struct {
	ID           int
	Call, Return int64
	Input        AssignmentCall
	Code         string
	Unknown      bool
}

func AssignmentStep(s AssignmentState, c AssignmentCall) (AssignmentState, string) {
	switch c.Kind {
	case "claim":
		if s.Revoked {
			return s, "revoked"
		}
		if s.Incarnation != 1 || !s.Available || s.Revision != 1 || s.Status != "pending" {
			return s, "conflict"
		}
		if s.Holder != "owner" || s.Fence != 1 {
			return s, "lease_held"
		}
		s.Holder = "worker"
		s.Fence++
		s.Status = "claimed"
	case "acquire":
		if s.Holder != "owner" || s.Fence != 1 {
			return s, "lease_held"
		}
		s.Holder = "other"
		s.Fence++
	case "cancel":
		if s.Revision != 1 {
			return s, "conflict"
		}
		s.Status = "cancelled"
	case "reassign":
		if s.Status == "cancelled" || s.Status == "finished" || s.Revision != 1 {
			return s, "conflict"
		}
		if s.Holder != "owner" || s.Fence != 1 {
			return s, "lease_held"
		}
		s.Revision++
		s.Status = "pending"
	case "incarnation":
		s.Incarnation++
		s.Available = false
	case "drain":
		s.Available = false
	case "revoke":
		s.Revoked = true
	case "parent_take":
		s.ParentFence++
	case "finish":
		if s.Revoked {
			return s, "revoked"
		}
		if s.Incarnation != 1 || s.Revision != 1 || s.Status != "claimed" {
			return s, "conflict"
		}
		if s.Holder != "worker" || s.Fence != 2 {
			return s, "fence_stale"
		}
		s.Status = "finished"
	default:
		return s, "unsupported"
	}
	return s, "ok"
}
func CheckAssignments(initial AssignmentState, history []AssignmentOperation) porcupine.CheckResult {
	if len(history) > 100 {
		return porcupine.Unknown
	}
	end := int64(1)
	for _, o := range history {
		end = max(end, o.Call+1, o.Return+1)
	}
	ops := make([]porcupine.Operation, 0, len(history))
	for _, o := range history {
		if o.Call < 0 || !o.Unknown && o.Return <= o.Call {
			return porcupine.Illegal
		}
		until := o.Return
		if o.Unknown {
			until = end
		}
		ops = append(ops, porcupine.Operation{ClientId: o.ID, Call: o.Call, Return: until, Input: o.Input, Output: o})
	}
	m := porcupine.NondeterministicModel{Init: func() []any { return []any{initial} }, Equal: func(a, b any) bool { return reflect.DeepEqual(a, b) }, Step: func(state, input, output any) []any {
		s := state.(AssignmentState)
		next, code := AssignmentStep(s, input.(AssignmentCall))
		o := output.(AssignmentOperation)
		if o.Unknown {
			return []any{s, next}
		}
		if code == o.Code {
			return []any{next}
		}
		return nil
	}}
	return porcupine.CheckOperationsTimeout(m.ToModel(), ops, time.Second)
}
