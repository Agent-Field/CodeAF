package factory

// Rubric is how complex a job is, judged on five factors a model can judge
// from evidence: how many places change, how clear the wanted result is, how
// much shared code or data it touches, how a finished change is checked, and
// how far it leaves existing code. Each factor is 0, 1 or 2.
//
// A MODEL NEVER WRITES A PRICE. Dollars and minutes are measured from the
// factory's own finished jobs; the rubric is what those jobs are compared on.
type Rubric struct {
	Scope    Factor
	Clarity  Factor
	Coupling Factor
	Proof    Factor
	Novelty  Factor
	// From is who scored it: [RubricFromRead] (the arrival read, text only)
	// or [RubricFromScout] (Look closer, with the repository).
	From string
}

// Factor is one rubric factor: its score (0..2), a why of at most six words,
// and Low when the scorer could not see what the factor is about (a text
// read has not seen the repository, so its scope and coupling are low).
type Factor struct {
	Score int
	Why   string `json:",omitempty"`
	Low   bool   `json:",omitempty"`
}

// Who scored a rubric.
const (
	RubricFromRead  = "read"
	RubricFromScout = "scout"
)

// The complexity bands, and the size letter each one is.
const (
	BandSmall  = "small"
	BandMedium = "medium"
	BandLarge  = "large"
)

// FactorMost is the highest score a factor may have.
const FactorMost = 2

// Factors are the rubric's factors in their fixed order, by name.
func (r *Rubric) Factors() []*Factor {
	return []*Factor{&r.Scope, &r.Clarity, &r.Coupling, &r.Proof, &r.Novelty}
}

// FactorNames are the factors' names, in [Rubric.Factors] order.
var FactorNames = []string{"scope", "clarity", "coupling", "proof", "novelty"}

// Complexity is the sum of the factors, 0..10.
func (r Rubric) Complexity() int {
	sum := 0
	for _, f := range r.Factors() {
		sum += f.Score
	}
	return sum
}

// Band is the rubric's complexity band.
func (r Rubric) Band() string { return BandOf(r.Complexity()) }

// BandOf is the band of a complexity: 0–3 small, 4–6 medium, 7–10 large.
func BandOf(complexity int) string {
	switch {
	case complexity <= 3:
		return BandSmall
	case complexity <= 6:
		return BandMedium
	default:
		return BandLarge
	}
}

// BandSize is the size letter a band stands for, so start rules written in
// S, M and L keep working.
func BandSize(band string) string {
	switch band {
	case BandSmall:
		return "S"
	case BandMedium:
		return "M"
	case BandLarge:
		return "L"
	}
	return ""
}
