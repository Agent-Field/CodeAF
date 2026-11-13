package model

import (
	"encoding/json"
	"reflect"
	"time"

	"github.com/anishathalye/porcupine"
)

// Operation keeps the real invocation and response ordering. Unknown means a
// timeout, not a failed mutation: it may take effect anywhere before history end.
type Operation struct {
	ID      int
	Call    int64
	Return  int64
	Input   Call
	Output  Outcome
	Unknown bool
}

// Check never partitions an identity: vault, revocation and session state can
// couple cells. A timeout is inconclusive and callers must treat it as failure.
func Check(history []Operation) porcupine.CheckResult {
	return CheckWithin(history, time.Second)
}

func CheckWithin(history []Operation, budget time.Duration) porcupine.CheckResult {
	if budget <= 0 || budget > time.Second || len(history) > 100 {
		return porcupine.Unknown
	}
	end := int64(1)
	for _, op := range history {
		end = max(end, op.Call+1, op.Return+1)
	}
	operations := make([]porcupine.Operation, 0, len(history))
	for _, op := range history {
		if op.Call < 0 || !op.Unknown && op.Return <= op.Call {
			return porcupine.Illegal
		}
		until := op.Return
		if op.Unknown {
			until = end
		}
		operations = append(operations, porcupine.Operation{ClientId: op.ID, Input: op.Input, Output: op, Call: op.Call, Return: until})
	}
	nondeterministic := porcupine.NondeterministicModel{
		Init:  func() []any { return []any{State{}} },
		Equal: func(a, b any) bool { return reflect.DeepEqual(a, b) },
		Step: func(state, input, output any) []any {
			s := state.(State)
			call := input.(Call)
			observed := output.(Operation)
			next, result := Step(s, call)
			if observed.Unknown {
				return []any{s, next}
			}
			if result == observed.Output {
				return []any{next}
			}
			return nil
		},
	}
	return porcupine.CheckOperationsTimeout(nondeterministic.ToModel(), operations, budget)
}

// Minimize preserves invocation order and only removes a group when the
// remaining history is still definitely illegal. Inconclusive probes keep it.
func Minimize(history []Operation) []Operation {
	out := append([]Operation(nil), history...)
	if Check(out) != porcupine.Illegal {
		return out
	}
	for width := len(out) / 2; width > 0; width /= 2 {
		// Keep the state-establishing first operation; dropping it can turn
		// a race into an irrelevant standalone read of an absent cell.
		for start := 1; start+width <= len(out); {
			candidate := append(append([]Operation(nil), out[:start]...), out[start+width:]...)
			if Check(candidate) == porcupine.Illegal {
				out = candidate
			} else {
				start++
			}
		}
	}
	return out
}

// Digest is a diagnostic state encoding, independent of production row bytes.
func Digest(s State) string { b, _ := json.Marshal(s); return string(b) }
