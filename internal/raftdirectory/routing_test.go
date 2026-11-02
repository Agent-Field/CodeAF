package raftdirectory_test

import (
	"os"
	"strings"
	"testing"
)

// R30 forbids another lease writer on the authoritative hosted path. Existing
// pre-cutover legacy rules remain tested until migration freezes that source.
func TestR30HostedDirectoryRoutesOnlyThroughGoPolicy(t *testing.T) {
	b, err := os.ReadFile("../../relay/hosted/src/raft/directory.js")
	must(t, err)
	s := string(b)
	// The hosted directory evaluates and projects through the reactor's paged
	// Go operations, which keep large legacy requests inside the command and
	// buffer limits: 41 starts the shared Go evaluation and 45 projects its
	// public result.
	for _, needle := range []string{"frame(41)", "frame(45)", "this.host.commit(originalSignedBytes)", "this.host.barrierRead(authenticatedRequest)"} {
		if !strings.Contains(s, needle) {
			t.Fatalf("missing policy boundary %s", needle)
		}
	}
	for _, needle := range []string{"rules.js", "rotateBy", "LeaseTTL", "expires =", "fence++"} {
		if strings.Contains(s, needle) {
			t.Fatalf("JS policy writer %s", needle)
		}
	}
	b, err = os.ReadFile("../../relay/hosted/src/raft/member.js")
	must(t, err)
	if strings.Contains(string(b), "env.raftEvaluate") || !strings.Contains(string(b), "directoryEvaluator(sharedRuntime()") {
		t.Fatal("production member lacks the shared Go evaluator")
	}
}
