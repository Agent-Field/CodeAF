package remote

import "testing"

type countedAgent struct {
	*fakeAgent
	users int
}

func (a *countedAgent) UserMessageCount() int { return a.users }

func TestWelcomeCarriesTheOpenedReplayUserCount(t *testing.T) {
	for _, count := range []int{0, 9} {
		engine := engineOn(&fakeAgent{})
		engine.Agent = &countedAgent{fakeAgent: engine.Agent.(*fakeAgent), users: count}
		link := dialAgent(t, engine)
		frame := link.hello(Hello{Version: Version})
		welcome := decode[Welcome](t, frame.Payload)
		if welcome.UserMessages == nil || *welcome.UserMessages != count {
			t.Fatalf("replay count = %v, want %d", welcome.UserMessages, count)
		}
		if err := link.end(); err != nil {
			t.Fatal(err)
		}
	}
}
