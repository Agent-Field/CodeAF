package main

import (
	"testing"
)

// THE SEAMS THE FOUR BIG HANDS ARE BUILT FROM.
//
// build_harness, list_harnesses, run_adaptive and propose_task are absent from
// the belt rather than broken when their machinery is missing (internal/session's
// tools.go), which is the right posture and a silent one: a door that forgot to
// fill a seam ships a model that simply never has the verb, and nothing anywhere
// says so. internal/session's belt_wiring_test.go pins what filling the seams
// buys; this pins that THIS DOOR fills them.
//
// The gates, and what fills each here:
//
//	build_harness, list_harnesses   HarnessStore + RunHarness + AskConsent
//	run_adaptive                    OrchestrateRunner + AskConsent
//	propose_task                    a conversation rather than a task node
func TestTheV3DoorFillsEverySeamTheBigHandsNeed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	proc, err := openV3Process("chat")
	if err != nil {
		t.Fatalf("the process every v3 door builds once did not open: %v", err)
	}
	t.Cleanup(proc.closeAll)
	launch, err := openV3Launch(proc, v3Options{Model: "test/model", Workspace: t.TempDir()})
	if err != nil {
		t.Fatalf("the launch every v3 door assembles through did not open: %v", err)
	}
	cfg := launch.Config
	if cfg.HarnessStore == nil {
		t.Fatal("no registry reached the session: build_harness has nowhere to write")
	}
	if cfg.RunHarness == nil {
		t.Fatal("no harness runner reached the session: designing one is off")
	}
	// The interactive path is the ONLY one that says somebody is watching
	// (chatv3.go sets it after the launch, because a --once run answers no
	// questions), so this is the posture a conversation runs under.
	cfg.AskConsent = true
	// And the adaptive seam, wired where every door that builds a conversation
	// goes through (chatv3_orchestrate.go).
	cfg, runs := v3Adaptive(cfg)
	if cfg.OrchestrateRunner == nil || runs == nil {
		t.Fatal("no adaptive runner reached the session: run_adaptive is off")
	}
	// propose_task's own gate: a conversation has the hand and a task node does
	// not, because there is nobody in a node's world to show a proposal to.
	if cfg.InTask {
		t.Fatal("the door opened a conversation as a task node: propose_task is off")
	}
}
