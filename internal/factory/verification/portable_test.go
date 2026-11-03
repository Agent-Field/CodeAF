package verification

import (
	"strings"
	"testing"
)

func TestMeasuredAbsoluteCheckPortableIdentity(t *testing.T) {
	failed, passed := 7, 0
	at := Snapshot{Head: strings.Repeat("a", 40), Fingerprint: strings.Repeat("b", 40)}
	old := Check{Args: []string{"/missing/toolchain/go", "test", "./..."}, Cwd: "/repo", Exit: &failed, Before: at, After: at}
	next := old
	next.Args = []string{"go", "test", "./..."}
	next.Exit = &passed
	if old.Key() != next.Key() {
		t.Error("measured absolute and portable invocation must match")
	}
	if strings.Contains(old.Label(), "/missing/toolchain/go") {
		t.Error("owed label leaked machine path", old.Label())
	}
	latest, _ := RequiredChecks([]Check{old, next}, at, true)
	if len(latest) != 1 {
		t.Errorf("portable rerun left %d requirements", len(latest))
	}
	for _, args := range [][]string{{"go", "test", "./other"}, {"go", "test", "-race", "./..."}, {"./go", "test", "./..."}, {"/missing/custom-check", "test", "./..."}} {
		other := next
		other.Args = args
		if other.Key() == old.Key() {
			t.Errorf("different command merged: %v", args)
		}
	}
	if CoversUnrun("/missing/toolchain/go test ./...", "/repo", next) {
		t.Error("unmeasured absolute run became a pass")
	}
	unknown := old
	unknown.Args = []string{"/missing/custom-check", "test"}
	if label := unknown.Label(); !strings.Contains(label, "missing on this machine: /missing/custom-check") {
		t.Error("unknown executable needs missing-path explanation", label)
	}
}
