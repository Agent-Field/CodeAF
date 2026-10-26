package directory

import "github.com/Agent-Field/codeaf/internal/clusterstate"

// Lease rules live in clusterstate so every runtime applies the same decisions.
// These forwarding functions retain the directory API used by existing clients.
const ttlMs = clusterstate.LeaseTTLMillis

func Lifted(c Cell, until int64) Cell { return clusterstate.Lifted(c, until) }
func Acquire(c Cell, device string, now int64, force bool, expected ...*LeaseExpectation) (Cell, error) {
	return clusterstate.Acquire(c, device, now, force, expected...)
}
func Heartbeat(c Cell, device string, b Beat, now int64) (Cell, error) {
	return clusterstate.Heartbeat(c, device, b, now)
}
func PublishTo(c Cell, device string, p Publish, now int64) (Cell, error) {
	return clusterstate.PublishTo(c, device, p, now)
}
func ReleaseOf(c Cell, device string, fence uint64, at ...int64) (Cell, error) {
	return clusterstate.ReleaseOf(c, device, fence, at...)
}
func Created(in CellInit, device string, now int64) Cell {
	return clusterstate.Created(in, device, now)
}
func RevokeOf(target Device, caller, targetID string) (Device, error) {
	return clusterstate.RevokeOf(target, caller, targetID)
}
func Stamped(old Device, had bool, d Device, now int64) Device {
	return clusterstate.Stamped(old, had, d, now)
}
