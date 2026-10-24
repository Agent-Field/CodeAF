package selector

// ClassShape is one of the five task classes the selector seats. The five
// classes are derived from the session findings: reflex (no tools, small
// context), low (tools, moderate context), worker/high/mastermind matching
// the crewpick seat shapes' spirit (worker small, high larger, mastermind
// largest + images).
type ClassShape struct {
	ID          string  // reflex | low | worker | high | mastermind
	MinContext  int     // context gate
	NeedsTools  bool    // tool-use gate
	NeedsImages bool    // image gate
	Volume      float64 // seat-volume weight for the bill
	Role        string  // prediction role (worker|checker|thinker)
}

// DefaultClasses returns the five classes with the session's shapes.
func DefaultClasses() []ClassShape {
	return []ClassShape{
		{ID: "reflex", MinContext: 32_000, NeedsTools: false, NeedsImages: false, Volume: 0.60, Role: "worker"},
		{ID: "low", MinContext: 128_000, NeedsTools: true, NeedsImages: false, Volume: 0.28, Role: "worker"},
		{ID: "worker", MinContext: 200_000, NeedsTools: true, NeedsImages: false, Volume: 0.08, Role: "worker"},
		{ID: "high", MinContext: 200_000, NeedsTools: true, NeedsImages: true, Volume: 0.03, Role: "checker"},
		{ID: "mastermind", MinContext: 200_000, NeedsTools: true, NeedsImages: true, Volume: 0.01, Role: "thinker"},
	}
}
