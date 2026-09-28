package plandb

import (
	"os"
	"testing"
)

// TestMain removes the plan-store door inherited when the suite is launched by
// a plan worker. Individual tests bind PLANDB_DB explicitly when that is the
// behavior they mean to exercise; the ambient run store is never a fixture.
func TestMain(m *testing.M) {
	_ = os.Unsetenv("PLANDB_DB")
	_ = os.Unsetenv("PLANDB_RUN")
	os.Exit(m.Run())
}
