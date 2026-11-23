package resident

import (
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

// Unix parity contract 2: Unix health commands keep bash -lc's pipeline status.
func TestUnixParityHealthPipeline(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	service := store.Service{Dir: t.TempDir(), Health: store.ServiceHealth{Kind: store.ServiceHealthCmd, Value: "false | true"}}
	err := (&platformServiceRuntime{}).Healthy(t.Context(), service)
	if (err != nil) != (runtime.GOOS == "windows") {
		t.Fatalf("health pipeline = %v", err)
	}
}
