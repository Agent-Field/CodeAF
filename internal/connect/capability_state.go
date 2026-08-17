package connect

// CapabilityState is one of the three answers a person can give about a
// capability. The words are the whole vocabulary of the feature: the panel
// writes them, the disk keeps them, the gate reads them, and the mid-chat
// "always" answer writes StateYes exactly as the panel does.
//
// They are a string rather than a number because they are written to a file a
// person can open, and "yes" in that file says what 0 does not.
type CapabilityState string

const (
	// StateYes is the person's named allow: this may run without asking.
	StateYes CapabilityState = "yes"
	// StateAsk puts the question before every call.
	StateAsk CapabilityState = "ask"
	// StateOff takes the capability away entirely — no tool, no listing, no
	// question. See the package comment on capability.go.
	StateOff CapabilityState = "off"
)

// valid reports whether a state is one of the three. Anything else — a word
// from a hand-edited file, a value from a caller that built one by hand — is
// nonsense, and nonsense is never obeyed: it is refused on the way in and
// ignored on the way out.
func (s CapabilityState) valid() bool {
	switch s {
	case StateYes, StateAsk, StateOff:
		return true
	default:
		return false
	}
}

// defaultState is what a capability nobody has touched is worth.
//
// THE DEFAULTS ARE TODAY'S BEHAVIOUR, EXACTLY. A capability that only looks is
// already armed and already runs without a question, so its default is yes; a
// capability that acts in the person's name is already asked about by
// internal/approval's floor, so its default is ask. Nothing is off by default,
// because nothing is missing from the belt today.
//
// The consequence is the one that matters for the wiring wave: WIRING THIS IN
// CHANGES NOTHING until a person touches a control. Every state read before
// anybody has said anything reproduces the session they already had, so the
// panel can ship ahead of anybody's opinion about it.
func defaultState(c Capability) CapabilityState {
	if c.Acts {
		return StateAsk
	}
	return StateYes
}
