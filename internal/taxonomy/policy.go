package taxonomy

import (
	"sort"
	"sync"
	"time"
)

// Policy is what ONE class of failure earns. There is exactly one per [Class]
// and it is a value in a registry, never a branch in a caller.
//
// THE REGISTRY IS THE POINT. A switch over the class at each site is three
// answers that start the same and drift the first time somebody fixes one of
// them; a policy object means the answer for a class is written once, in one
// place, and every site that classifies gets that answer whether or not its
// author had heard of it.
type Policy interface {
	// Class is the one class this policy answers for.
	Class() Class
	// Decide is the policy's answer. The limits it is handed have already been
	// floored ([Limits.floor]), so an implementation never has to defend itself
	// against a zero.
	Decide(Evidence, Limits) Verdict
}

var (
	registryMu sync.RWMutex
	policies   = map[Class]Policy{}
)

// Register puts a policy in the registry, replacing whatever answered for that
// class before. It is called from this package's own init for the three classes
// that exist, and it is exported so a test can pin a policy without reaching
// into the map.
func Register(p Policy) {
	if p == nil {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	policies[p.Class()] = p
}

// PolicyFor is the registry read [Classify] makes.
func PolicyFor(class Class) (Policy, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	policy, ok := policies[class]
	return policy, ok
}

// Registered is every class that has a policy, in a stable order. A structural
// test reads it to hold this package to "exactly one policy per class".
func Registered() []Class {
	registryMu.RLock()
	defer registryMu.RUnlock()
	classes := make([]Class, 0, len(policies))
	for class := range policies {
		classes = append(classes, class)
	}
	sort.Slice(classes, func(i, j int) bool { return classes[i] < classes[j] })
	return classes
}

func init() {
	Register(transportPolicy{})
	Register(capabilityPolicy{})
	Register(workPolicy{})
}

// ── transport ───────────────────────────────────────────────────────────────

// transportPolicy retries on the same tier and gets out of the way.
//
// THREE THINGS IT NEVER DOES, and each of them was a measured failure:
//
//   - It never ends a turn. [Verdict.EndsTurn] is false for every verdict this
//     returns, including the one that gives up: a request that ran out of
//     attempts is a request that is over, and a turn is not.
//   - It never counts toward a lift. Nothing about who served a request is
//     evidence about who was asked.
//   - It never moves the tier. The endpoint is the suspect, so the answer is a
//     different endpoint — which is what [Verdict.Rotate] asks the routing
//     underneath for, and which the routing already knows how to do.
type transportPolicy struct{}

func (transportPolicy) Class() Class { return Transport }

func (transportPolicy) Decide(e Evidence, l Limits) Verdict {
	attempt := e.Attempt
	if attempt < 1 {
		attempt = 1
	}
	if attempt < l.TransportAttempts {
		return Verdict{
			Action:   ActionRetry,
			Reason:   transportReason(e),
			Attempts: l.TransportAttempts,
			Backoff:  waitFor(e, attempt, l.TransportBackoff),
			Rotate:   true,
		}
	}
	return Verdict{
		Action:   ActionGiveUp,
		Reason:   transportReason(e),
		Attempts: l.TransportAttempts,
	}
}

// waitFor is how long to wait before asking again, and it is TWO answers
// because there are two kinds of transport failure.
//
// A REFUSAL, A RESET OR A DEADLINE is an endpoint under strain, and the pause is
// the whole point of asking again: 2s, 4s, 8s, doubling off the one knob, which
// is the schedule the turn loop already walked.
//
// AN EMPTY 200 OR A MANGLED TOOL CALL IS NOT STRAIN. The endpoint answered, at
// once, with something that was not an answer — it is up, it is fast, and it is
// broken in a way that eight seconds of waiting does not mend. What mends it is
// being served by somebody else, which is [Verdict.Rotate] and costs no time at
// all. Waiting here would spend fourteen seconds of a person's turn to arrive at
// exactly the same request.
func waitFor(e Evidence, attempt int, base time.Duration) time.Duration {
	if e.Empty || e.Malformed {
		return 0
	}
	if base <= 0 || attempt < 1 {
		return 0
	}
	return base << (attempt - 1)
}

// transportReason is the phrase the journal carries. It names the SHAPE and not
// the payload: an autopsy grouping a thousand lines wants "the reply arrived
// empty" four hundred times, not four hundred distinct sentences.
func transportReason(e Evidence) string {
	switch {
	case e.Empty:
		return "the reply arrived empty"
	case e.Malformed:
		return "the tool call did not parse"
	case e.Timeout:
		return "nobody answered in time"
	case e.Idle:
		return "it went silent and stopped working"
	case e.Cut:
		return "the reply stopped part-way"
	case e.Wire:
		return "the connection did not hold"
	case e.Upstream != "":
		return "the endpoint refused"
	case e.Status > 0:
		return "the provider could not serve it"
	}
	return "the request did not reach anybody"
}

// ── capability ──────────────────────────────────────────────────────────────

// capabilityPolicy is the ONLY policy that may buy a stronger tier, and it buys
// one at a time, late, under a cap, and gives it back.
//
// ── WHY IT COUNTS SEMANTIC FAILURES AND NOTHING ELSE ──
//
// A refutation is a MEASURED failure of the work: a fresh reader ran the check
// and said, with evidence, that the job is not done. That is the only signal in
// this system that is genuinely about the model. A refutation of a round whose
// calls died on the wire is not one — there was nothing for the check to pass —
// and counting it is exactly how four bad responses in a row bought seven times
// the price for the rest of a run. [Tally] is where the two are kept apart, and
// [Evidence.Refuted] is already the filtered count when it arrives here.
//
// ── WHY IT COMES BACK DOWN ──
//
// The lift was bought against a specific finding. When the next check passes,
// the finding is closed and the tier is being paid for nothing. Nothing in this
// build looked for that moment before, so every lift was permanent — which is
// how a single bad minute at a provider became the price of a whole run.
type capabilityPolicy struct{}

func (capabilityPolicy) Class() Class { return Capability }

func (capabilityPolicy) Decide(e Evidence, l Limits) Verdict {
	if e.Passed {
		if e.Escalated {
			return Verdict{Action: ActionDeescalate,
				Reason: "the check passed, so the lifted tier is not buying anything"}
		}
		return Verdict{Action: ActionHold, Reason: "the check passed"}
	}
	// THE CAP IS ASKED BEFORE THE COUNT. Work already standing on a lifted tier
	// that has spent its ceiling has no cheaper answer left in this package, and
	// a verdict that said "escalate" and then failed to spend would be a lie in
	// the journal. It comes back as WORK — the caller's own business, with the
	// report attached — which is the one honest thing left to say.
	if e.Escalated && l.TierCapUSD > 0 && e.SpentUSD >= l.TierCapUSD {
		return Verdict{Class: Work, Action: ActionReport,
			Reason: "the lifted tier has spent what this work may spend on it"}
	}
	if e.Refuted >= l.SemanticFailures {
		return Verdict{Action: ActionEscalate,
			Reason: "the check found gaps again on the same tier"}
	}
	if e.TransportSeen > 0 {
		return Verdict{Action: ActionHold,
			Reason: "the last round lost calls on the wire, so the tier is not the suspect"}
	}
	return Verdict{Action: ActionHold, Reason: "the check found gaps"}
}

// ── work ────────────────────────────────────────────────────────────────────

// workPolicy takes NO ACTION. The job could not be done, or the request itself
// is what is wrong; either way this package has nothing to sell and hands the
// verdict back with the evidence on it for whoever owns the landing.
type workPolicy struct{}

func (workPolicy) Class() Class { return Work }

func (workPolicy) Decide(e Evidence, _ Limits) Verdict {
	reason := "the work did not come back done"
	if e.Status >= 400 && e.Upstream == "" {
		reason = "the request itself was refused"
	}
	return Verdict{Action: ActionReport, Reason: reason}
}
