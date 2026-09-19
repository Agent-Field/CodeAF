package e2e

import "testing"

// exec_needles_test.go holds the NAMES of the Wave 4 execution words the
// tmux suite will read off a real discussion once t-w4-live drives J27–J35.
// It is the same shape as collab_needles_test.go: the untagged word gate
// (tuiwords_test.go's TestEveryWordInTheTableIsWaitedForBySomething) needs to
// know they are alive, and inventing a tagged waitFor that this lane never ran
// would be a fake live pass.
func TestTheExecSurfaceWordsAreNamed(t *testing.T) {
	for _, name := range []string{
		"execLaunchOrJoinWord",
		"execPauseCoordWord",
		"execStopWorkWord",
		"execRevokeGrantWord",
	} {
		if say(t, name) == "" {
			t.Fatalf("the needle %q is in the table but spells nothing", name)
		}
	}
}
