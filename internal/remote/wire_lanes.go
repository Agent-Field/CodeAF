package remote

// wire_lanes.go is the version-11 delta: the harness subscription this wire did
// not carry, and the one answer it asks for that had no door.
//
// The lane is stated in lanes.go and carried in standinglane.go; what is here is
// only what a frame has to say to name it.

import "encoding/json"

const (
	// MethodDesignWatch is the surface saying it draws the harness lane, and it
	// buys exactly what [MethodTaskWatch] buys: "design" frames from then on.
	// It is sent once per conversation the surface takes up, never on a frame.
	MethodDesignWatch = "Design.Watch"
	// MethodSubharnessResolve answers one intake card — whether the saved
	// program runs, and on which input. The design card's own answer has been
	// [MethodHarness] since version 1; this is the OTHER question that arrives
	// on the same subscription, and without it the card would be a page whose
	// keys pressed nothing.
	MethodSubharnessResolve = "Subharness.Resolve"
)

// SubharnessResolveArgs is one person's answer to one intake card. Input is the
// card as it was settled, or nil for the card exactly as it was raised — which
// is what internal/session's ResolveSubharness reads a nil as.
type SubharnessResolveArgs struct {
	ID    uint64          `json:"id"`
	Run   bool            `json:"run"`
	Input json.RawMessage `json:"input,omitempty"`
}
