package provider

// Verdict classifies how one unit of model work actually ended.
//
// It exists because every label the harness already had conflates two different
// questions. The scheduler's StateDone means "a non-empty string came back",
// which is equally true of a leaf that finished the job and of one that
// exhausted its budget mid-edit; profile.Record.Done inherits that conflation.
// For control flow it is the right call — a dependent still needs whatever text
// there is — and for learning it is fatal, because anything trained on it would
// be told that running out of money is a success.
//
// So the taxonomy sits alongside the state rather than replacing it. Nothing
// here decides what runs next; it only decides what may be learned from.
type Verdict string

const (
	// VerdictVerifiedSuccess is the only positive evidence there is: something
	// checked the answer and it held. For a structured call the schema and the
	// pass's own semantic test are that check; for a leaf it would be a test
	// suite. The whole cascade design rests on this verdict being cheap to
	// obtain — the router lab's winning policy beat every single model only
	// because failure was detectable for free.
	VerdictVerifiedSuccess Verdict = "verified_success"

	// VerdictUnverifiedSuccess is output nobody could check. It is deliberately
	// evidence in neither direction: the probe lab's lesson restated, which is
	// that when you cannot verify an outcome you must not pretend to have.
	VerdictUnverifiedSuccess Verdict = "unverified_success"

	// VerdictFormatFailure is a reply that did not parse or did not carry the
	// fields the schema required. It is the cascade's escalation trigger.
	VerdictFormatFailure Verdict = "format_failure"

	// VerdictSemanticFailure is a reply that parsed and was wrong — a cyclic
	// dependency set, an empty stage list, a ruler too thin to use. Only the
	// call site can know this, which is why it is reported back rather than
	// inferred.
	VerdictSemanticFailure Verdict = "semantic_failure"

	VerdictBudgetStop Verdict = "budget_stop" // the leaf ran out of tokens
	VerdictTurnCap    Verdict = "turn_cap"    // the leaf ran out of iterations

	// VerdictProviderFailure is a transport fact — a 429, a 5xx, a timeout — and
	// never ability evidence. Rating a model down because its provider was busy
	// would make the most popular model look like the weakest one.
	VerdictProviderFailure Verdict = "provider_failure"

	// VerdictEmptyResponse is the runaway-reasoning mode: the whole completion
	// budget spent on private deliberation, zero visible text returned. The
	// probe lab measured it at 15% of all failures and it is the single most
	// expensive outcome available — full price, nothing delivered — so it is
	// named rather than folded into "produced no result".
	VerdictEmptyResponse Verdict = "empty_response"
)

// Graded reports whether a verdict may move an ability rating, and in which
// direction.
//
// Two verdicts are deliberately inert. A provider failure says nothing about
// the model, and an unverified success says nothing about the answer; counting
// either would teach the ledger something that is not true. Everything else is
// evidence: budget, turn-cap and empty-response verdicts can only come from a
// leaf, and all three mean the model could not converge inside what it was
// given, which is exactly what a rating is for.
func (v Verdict) Graded() (positive bool, graded bool) {
	switch v {
	case VerdictVerifiedSuccess:
		return true, true
	case VerdictFormatFailure, VerdictSemanticFailure,
		VerdictBudgetStop, VerdictTurnCap, VerdictEmptyResponse:
		return false, true
	default:
		return false, false
	}
}

// Weight is how much of a rating step one graded verdict is worth.
//
// Graded is a yes-or-no question and this is the follow-up: not every failure
// says the same amount about the model that produced it. A reply that did not
// parse, was semantically wrong, or was empty is the model failing at the work
// it was handed. A leaf that ran out of budget or turns may be the same thing —
// or it may be a leaf that was three nodes' worth of work, which is a fact about
// the planner and not about the model. BASELINE.md measured that variance
// directly: a byte-identical brief drew graphs from 5 to 26 nodes and cost
// tracked node count almost exactly, so sizing dominates what a leaf costs and
// therefore what exhausts it.
//
// A quarter, rather than zero, because it is still evidence — a model that
// wanders is a model that runs out — and rather than one, because arm B watched
// five budget stops on a single oversized task outvote a prior and reroute every
// leaf on the panel. Weighted at a quarter those five move a rating about as far
// as one wrong answer does, which is the right size for what they are.
func (v Verdict) Weight() float64 {
	switch v {
	case VerdictBudgetStop, VerdictTurnCap:
		return 0.25
	default:
		return 1
	}
}

// Escalates reports whether a verdict is worth re-running on a stronger model.
// A provider failure is not — the next rung would hit the same weather — and an
// unverified success is not, because nothing said it was wrong.
func (v Verdict) Escalates() bool {
	positive, graded := v.Graded()
	return graded && !positive
}
