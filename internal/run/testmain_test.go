package run_test

import (
	"os"
	"testing"
)

// TestMain keeps subprocess workers made by this suite out of the plan that
// launched go test. Tests that exercise the bound door set PLANDB_DB themselves.
func TestMain(m *testing.M) {
	_ = os.Unsetenv("PLANDB_DB")
	_ = os.Unsetenv("PLANDB_RUN")
	os.Exit(m.Run())
}
