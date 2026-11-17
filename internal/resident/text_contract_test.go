package resident

import (
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// Unix parity 2 contract items 1/2: gated bytes, shell or status expectations.
// Text contract C1: health checks retain a login shell, with Windows pipefail,
// preserving inherited ordinary variables and service tokens.
func TestTextContractHealthShell(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("CODEAF_TEXT_TEST", "inherited")
	t.Setenv("MY_SERVICE_TOKEN", "health-value")
	service := store.Service{Dir: t.TempDir(), Health: store.ServiceHealth{Kind: store.ServiceHealthCmd, Value: `shopt -q login_shell && [[ "$CODEAF_TEXT_TEST" == inherited && "$MY_SERVICE_TOKEN" == health-value ]]`}}
	runner := &platformServiceRuntime{}
	if err := runner.Healthy(t.Context(), service); err != nil {
		t.Fatal(err)
	}
	service.Health.Value = "false | true"
	if err := runner.Healthy(t.Context(), service); (err != nil) != (runtime.GOOS == "windows") {
		t.Fatalf("pipeline status = %v", err)
	}
}
