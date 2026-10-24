// Package executortest is how a test binary that exercises tools without a
// session says, once and by name, that its calls run on the host.
package executortest

import "github.com/Agent-Field/codeaf/internal/executor"

// Host makes the host seat the one seatless calls in this test binary run on.
// A production context that lost its session still fails with
// [executor.ErrNoSeat]; only a package that imports this from a _test.go file
// is spared.
func Host() { executor.UseInTests(executor.Host) }
