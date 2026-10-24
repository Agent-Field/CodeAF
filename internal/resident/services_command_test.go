package resident

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/store"
	"runtime"
	"testing"
	"time"
)

// C18 preserves dev's command health check: only the check process matters,
// and its discarded output cannot create a drain failure or delay its result.
func TestHealthCommandIgnoresInheritedOutputPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	service := store.Service{Dir: t.TempDir(), Health: store.ServiceHealth{Kind: store.ServiceHealthCmd, Value: "sh -c '(sleep 2) & printf healthy; exit 0'"}}
	if err := (&platformServiceRuntime{}).Healthy(ctx, service); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("check waited for its child")
	}
	service.Health.Value = "exit 7"
	if err := (&platformServiceRuntime{}).Healthy(ctx, service); err == nil {
		t.Fatal("nonzero check was healthy")
	}
}
