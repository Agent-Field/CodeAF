package composer

// EscMsg is what [Model.Key] returns, as a command, when esc is pressed
// against an empty draft. It is the "not handled" half of the esc law
// (8.2.21): there is nothing left in the composer for esc to protect, so the
// decision — interrupt the turn this room is watching, or navigate/pop scope
// — belongs to whichever code drives the shell's Update loop, because only
// it knows whether this room is streaming. The composer's job stops at not
// swallowing the key.
type EscMsg struct{}
