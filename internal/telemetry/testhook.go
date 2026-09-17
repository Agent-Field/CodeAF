package telemetry

// The go-test rung is a production rule: a test binary is never a source of
// truth about usage, and it must stay unreachable by any environment variable
// or config value. But the wiring above this package — cmd/codeaf's lifecycle
// tests — runs inside a test binary too, and it has to exercise the spool
// paths for real or prove nothing.
//
// So the ONE door past the rung is a package-level func variable, set from a
// test only, restored by that test, and never touched by production code:
// no env var, no config key, no build flag can reach it. Setting it is what
// a test does, and a test is the only thing that can.

import "testing"

// EnableForTest turns the ladder on (on=true) or back to its honest answer
// (on=false) for the duration of one test, restoring the previous value when
// the test ends. It is the only way past the go-test and unstamped-build
// rungs of [offReason].
//
// It is deliberately exported and deliberately in the testhook file rather
// than a _test.go one, because the lifecycle tests in cmd/codeaf need it from
// their own package — and it is still a test-only surface by construction:
// nothing in the production call graph reads it, and a caller that is not a
// test has nothing to restore it with.
func EnableForTest(t testing.TB, on bool) {
	t.Helper()
	previous := forcedOn
	forcedOn = on
	t.Cleanup(func() { forcedOn = previous })
}
