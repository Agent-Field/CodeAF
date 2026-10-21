package main

import "testing"

type sealingStub struct{ failing bool }

func (s sealingStub) SealFailing() bool { return s.failing }

// ON THE ORDINARY LAUNCH there is no local seal watch: the agent is a handle on
// an engine in another process, and the segment must read what that agent says.
func TestTheSealSegmentReadsTheAgentWhenThereIsNoLocalWatch(t *testing.T) {
	seam := sealSeamOf(sealingStub{failing: true}, nil)
	if seam.Failing == nil || !seam.Failing() {
		t.Fatal("a failing far seal did not reach the surface")
	}
	if seam.Notice != nil {
		t.Fatal("a seam with no local watch has nothing to drain")
	}
}

// Unknown is not a failure: an agent that cannot say, and no watch, draw nothing.
func TestNoAgentStateAndNoWatchDrawsNothing(t *testing.T) {
	if seam := sealSeamOf(struct{}{}, nil); seam.Failing != nil || seam.Notice != nil {
		t.Fatal("a seam appeared from nothing")
	}
}
