package tui3

import "testing"

type releasingHostedAgent struct {
	*hostedAgent
	joined bool
}

func (h *releasingHostedAgent) DetachAndWait() error {
	h.joined = true
	return h.Detach()
}

// A clean quit requires every kept chat to join its release only while this
// terminal coordinates. Ordinary windows retain their existing departure.
func TestQuitJoinsEveryReleaseOnlyWhileCoordinating(t *testing.T) {
	for _, coordinating := range []bool{false, true} {
		t.Run(map[bool]string{false: "learner", true: "leader"}[coordinating], func(t *testing.T) {
			front := &releasingHostedAgent{hostedAgent: &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}}
			behind := &releasingHostedAgent{hostedAgent: &hostedAgent{fakeAgent: &fakeAgent{model: "m"}}}
			a := newTestApp(front)
			a.file = "/tmp/lab/one/transcript.jsonl"
			a.stirs = make(chan behindStirMsg, stirDepth)
			stowAgent(t, a, behind, "/tmp/lab/two/transcript.jsonl")
			a.waitForRelease = func() bool { return coordinating }
			a.leaveEverything()
			for _, agent := range []*releasingHostedAgent{front, behind} {
				if agent.joined != coordinating || agent.detaches != 1 || agent.closes != 0 {
					t.Fatal("quit used the wrong departure", agent.joined, agent.detaches, agent.closes)
				}
			}
		})
	}
}
