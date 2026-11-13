package remote

import (
	"sync"
	"testing"
)

type chatClaimAgent struct {
	*fakeAgent
	claimed sync.Map
}

func (a *chatClaimAgent) ClaimChatStart(id string) (bool, error) {
	_, loaded := a.claimed.LoadOrStore(id, true)
	return !loaded, nil
}

func TestChatStartClaimCrossesTheWireOnce(t *testing.T) {
	far := &chatClaimAgent{fakeAgent: &fakeAgent{}}
	first := laneLoop(t, far)
	second := laneLoop(t, far)
	for i, client := range []*Client{first.Client, second.Client, first.Client} {
		claimed, err := client.Agent().ClaimChatStart("request-one")
		if err != nil {
			t.Fatal(err)
		}
		if claimed != (i == 0) {
			t.Fatalf("claim %d=%v", i, claimed)
		}
	}
}
