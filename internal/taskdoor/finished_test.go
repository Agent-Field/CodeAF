package taskdoor

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// R7-3 keeps the compact newest-attempt bound durable across a store restart.
func TestAcknowledgedAttemptReplayProtectionIsBounded(t *testing.T) {
	clock := wallClock{}
	store, err := openAttempts(t.TempDir(), clock)
	if err != nil {
		t.Fatal(err)
	}
	now := clock.Now()
	for n := 0; n < MaxFinishedAttempts; n++ {
		store.finished[fmt.Sprintf("att_%016x", n)] = now.Add(time.Duration(n-MaxFinishedAttempts) * time.Second)
	}
	newest := fmt.Sprintf("att_%016x", MaxFinishedAttempts)
	if err := store.rememberFinished(newest); err != nil {
		t.Fatal(err)
	}
	reopened, err := openAttempts(store.root, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.finished) != MaxFinishedAttempts {
		t.Fatalf("retained %d acknowledgements", len(reopened.finished))
	}
	if _, old := reopened.finished["att_0000000000000000"]; old {
		t.Fatal("oldest acknowledgement was not compacted")
	}
	if _, _, err := reopened.admit(newest, Start{}, "busy"); err == nil || !strings.Contains(err.Error(), "already finished") {
		t.Fatalf("newest replay = %v", err)
	}
}
