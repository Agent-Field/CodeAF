package runs

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// A check's rehearsal must not sleep out a retry's backoff: the zoo bench lost
// minutes of the builder's wall to a 30s backoff doubled per failing step.
func TestARehearsalDoesNotSleepOutARetryBackoff(t *testing.T) {
	e := &engine{}
	step := &stepBody{RetryBackoff: "10m", RetryMax: 2}
	start := time.Now()
	if err := e.backoffNap(svc.WithDryRun(context.Background()), step, 2); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("the rehearsal slept")
	}
}
