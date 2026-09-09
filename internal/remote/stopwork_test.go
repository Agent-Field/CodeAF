package remote

import (
	"sync/atomic"
	"testing"
)

type stopWorkAgent struct {
	*fakeAgent
	stops atomic.Int32
	err   error
}

func (a *stopWorkAgent) StopWork() error { a.stops.Add(1); return a.err }

// A stop uses this connection's conversation and carries refusal back intact.
func TestStopWorkCrossesTheConversationDoor(t *testing.T) {
	far := &stopWorkAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: far}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if err := loop.Client.Agent().StopWork(); err != nil {
		t.Fatal(err)
	}
	if far.stops.Load() != 1 {
		t.Fatal("stop never reached the engine")
	}
}

func TestStopWorkReportsAnUnsupportedEngine(t *testing.T) {
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: &fakeAgent{model: "m"}}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if err := loop.Client.Agent().StopWork(); err == nil {
		t.Fatal("unsupported stop was reported successful")
	}
}
