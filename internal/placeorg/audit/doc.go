// Package audit is the op algebra that makes a nested place tree the right
// size. Classical structure decides every op except one: when no anchor says
// a place is about a thing of its own, a Judge is asked to choose THING or
// ACTIVITY, and silence keeps the place.
//
// The stage returns Ops. It never mutates the Tree it is handed. A private
// shadow copy is how one op sees the effect of the previous one, the way the
// reference implementation rewrites its labels as it goes. Interpose is one
// exception inside that shadow: the reference logged it and changed nothing,
// so the shadow is left alone and the op is proposed for the person to accept.
// Lift is the other: when separating the places would put more than the
// fan-out cap at the top, the parent stays and the op is only a proposal.
package audit
