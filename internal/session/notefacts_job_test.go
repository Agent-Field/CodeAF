package session

import (
	"strings"
	"testing"
)

// A JOB'S EXIT HANDS THE LANE THE JOB'S OWN LABEL, read off the job and not out
// of the sentence, and a registry without the titled lane still reports.
func TestAJobExitReachesTheTitledLaneWithItsOwnLabel(t *testing.T) {
	type said struct{ note, title string }
	titled := make(chan said, 1)
	registry := newJobRegistry(t.TempDir(), Place{}, func(string) { t.Error("the plain lane was used") })
	registry.notifyJob = func(note, title string) { titled <- said{note, title} }
	one := settlementJob(t, registry)

	registry.settleExit(one, 0)

	got := <-titled
	if want := jobTitle(one); got.title != want || !strings.Contains(got.note, "exited 0") {
		t.Fatalf("titled lane got %#v, want title %q", got, want)
	}

	plain := make(chan string, 1)
	bare := newJobRegistry(t.TempDir(), Place{}, func(note string) { plain <- note })
	bare.settleExit(settlementJob(t, bare), 0)
	if note := <-plain; !strings.Contains(note, "exited 0") {
		t.Fatalf("plain lane got %q", note)
	}
}
