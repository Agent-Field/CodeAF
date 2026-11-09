package resident

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// Text contract C1: health checks retain a login shell and pipefail while
// preserving inherited ordinary variables and service tokens.
func TestTextContractHealthShell(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("CODEAF_TEXT_TEST", "inherited")
	t.Setenv("MY_SERVICE_TOKEN", "health-value")
	service := store.Service{Dir: t.TempDir(), Health: store.ServiceHealth{Kind: store.ServiceHealthCmd, Value: `shopt -q login_shell && [[ "$CODEAF_TEXT_TEST" == inherited && "$MY_SERVICE_TOKEN" == health-value ]]`}}
	runtime := &platformServiceRuntime{}
	if err := runtime.Healthy(t.Context(), service); err != nil {
		t.Fatal(err)
	}
	service.Health.Value = "false | true"
	if err := runtime.Healthy(t.Context(), service); err == nil {
		t.Fatal("health check ignored pipeline failure")
	}
}
