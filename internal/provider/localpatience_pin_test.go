package provider

import lanes "github.com/Agent-Field/codeaf/internal/lane"

// EVERY TEST IN THIS PACKAGE THAT TALKS TO A LOOPBACK SERVER IS TALKING TO A
// "LOCAL ENDPOINT", and a local endpoint is given ten times the hosted wait
// under `response.patience: auto` (internal/lane's localpatience.go). The suite
// stands in for hosted providers with httptest servers and shortens the bounds
// it exercises to milliseconds, so letting those requests be stretched would
// turn every one of those cuts into a cut ten times later and every assertion
// about WHEN a call is abandoned into a different test.
//
// So the process-wide figure is pinned to one, the identity, for the whole
// package: a request is held to exactly the bound the test set. Tests that are
// about the stretch itself set the figure they want with [lanes.UseResponsePatience]
// and put it back through [usePatience].
func init() { lanes.UseResponsePatience(1) }

// usePatience publishes a `response.patience` for one test and restores the
// package's pinned one when the test ends. Zero is `auto`.
func usePatience(t interface {
	Helper()
	Cleanup(func())
}, factor float64) {
	t.Helper()
	lanes.UseResponsePatience(factor)
	t.Cleanup(func() { lanes.UseResponsePatience(1) })
}
