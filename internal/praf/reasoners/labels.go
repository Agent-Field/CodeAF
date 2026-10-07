package reasoners

// The labels a reasoner's agent session carries (appx.HarnessOptions.Label):
// what the session is for, in a word. codeaf names the session's thread by it
// and keys each agent's turn and time limits on it (internal/praf's
// limits.go), so each is a constant here and written nowhere else.
const (
	LabelAnatomy        = "anatomy"
	LabelChallenge      = "challenge"
	LabelCoverage       = "coverage"
	LabelCrossRef       = "cross-ref"
	LabelDeepen         = "deepen"
	LabelEvidence       = "evidence"
	LabelIntake         = "intake"
	LabelLens           = "lens"
	LabelObligations    = "obligations"
	LabelPlan           = "plan"
	LabelPostWorthiness = "post worthiness"
	LabelReviewer       = "reviewer"
)

// Labels is every label a reasoner's session carries.
var Labels = []string{
	LabelAnatomy,
	LabelChallenge,
	LabelCoverage,
	LabelCrossRef,
	LabelDeepen,
	LabelEvidence,
	LabelIntake,
	LabelLens,
	LabelObligations,
	LabelPlan,
	LabelPostWorthiness,
	LabelReviewer,
}
