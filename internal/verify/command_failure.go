package verify

// NewCommandFailure reports whether a red validation command contains a
// failure this work introduced. A command that was green before is new red in
// its entirety. When it was red on both sides, names supplied by the command
// are compared so one old failure cannot hide a different new one. If either
// red output supplies no parseable identity, the pair remains uncertain: the
// command was already red, and silence cannot identify what moved.
func NewCommandFailure(beforeRed bool, beforeIdentities, afterIdentities []string) ([]string, bool) {
	if !beforeRed {
		return UniqueChecks(afterIdentities), true
	}
	if len(beforeIdentities) == 0 || len(afterIdentities) == 0 {
		return nil, false
	}
	var before []string
	for _, name := range UniqueChecks(beforeIdentities) {
		before = append(before, CheckIdentity(name))
	}
	var fresh []string
	for _, name := range UniqueChecks(afterIdentities) {
		if len(Subtract([]string{CheckIdentity(name)}, before)) > 0 {
			fresh = append(fresh, name)
		}
	}
	return fresh, len(fresh) > 0
}
