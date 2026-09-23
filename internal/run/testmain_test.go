package run_test

import (
	"os"
	"testing"
)

// TestMain keeps subprocess workers made by this suite out of the plan that
// launched go test. Tests that exercise the bound door set PLANDB_DB themselves.
//
// It is also the door a delegated run's REAL child comes in by: a test that
// starts this very test binary as a program's process (delegate_child_test.go)
// marks it in the environment, and the binary then runs the fake program's
// body instead of the suite.
func TestMain(m *testing.M) {
	if code, child := runAsDelegateChild(); child {
		os.Exit(code)
	}
	_ = os.Unsetenv("PLANDB_DB")
	os.Exit(m.Run())
}
