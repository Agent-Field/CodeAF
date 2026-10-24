//go:build linux

package executortest

import (
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/executor"
)

// The CI-like test run supplies this only to test binaries. No production
// setting can turn off confinement or replace a capability measurement.
func init() {
	if env.Get("CODEAF_TEST_NO_USERNS") == "1" {
		executor.UseNamespaceProbeInTests(func() bool { return false })
	}
}
