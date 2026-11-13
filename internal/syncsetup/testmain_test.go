package syncsetup

import (
	"fmt"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/testutil"
)

// TestMain gives fixtures physical paths so workspace bindings do not traverse
// host aliases, and moves temporary roots onto tmpfs when available.
func TestMain(m *testing.M) {
	if err := testutil.CanonicalizeTempRoots(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(runInMemoryTemp(m))
}
